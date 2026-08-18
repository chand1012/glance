package glance

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"
)

const jsonFeedVersion = "https://jsonfeed.org/version/1.1"

type aggregateJSONFeed struct {
	Version     string                  `json:"version"`
	Title       string                  `json:"title"`
	HomePageURL string                  `json:"home_page_url"`
	FeedURL     string                  `json:"feed_url"`
	Items       []aggregateJSONFeedItem `json:"items"`
}

type aggregateJSONFeedItem struct {
	ID            string                  `json:"id"`
	URL           string                  `json:"url"`
	Title         string                  `json:"title"`
	ContentText   string                  `json:"content_text,omitempty"`
	DatePublished string                  `json:"date_published"`
	Tags          []string                `json:"tags,omitempty"`
	Source        aggregateJSONFeedSource `json:"_glance_source"`
}

type aggregateJSONFeedSource struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

type aggregateRSSFeed struct {
	XMLName xml.Name            `xml:"rss"`
	Version string              `xml:"version,attr"`
	AtomNS  string              `xml:"xmlns:atom,attr"`
	Channel aggregateRSSChannel `xml:"channel"`
}

type aggregateRSSChannel struct {
	Title         string               `xml:"title"`
	Link          string               `xml:"link"`
	Description   string               `xml:"description"`
	LastBuildDate string               `xml:"lastBuildDate,omitempty"`
	AtomLink      aggregateRSSAtomLink `xml:"atom:link"`
	Items         []aggregateRSSItem   `xml:"item"`
}

type aggregateRSSAtomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type aggregateRSSItem struct {
	Title       string                 `xml:"title"`
	Link        string                 `xml:"link"`
	GUID        aggregateRSSGUID       `xml:"guid"`
	Description string                 `xml:"description,omitempty"`
	PubDate     string                 `xml:"pubDate"`
	Categories  []string               `xml:"category,omitempty"`
	Source      aggregateRSSItemSource `xml:"source"`
}

type aggregateRSSGUID struct {
	IsPermaLink bool   `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

type aggregateRSSItemSource struct {
	URL   string `xml:"url,attr,omitempty"`
	Value string `xml:",chardata"`
}

func (a *application) handleAggregateFeedRequest(w http.ResponseWriter, r *http.Request) {
	pageSlug := r.PathValue("page")
	page, exists := a.slugToPage[pageSlug]
	if !exists {
		a.handleNotFound(w, r)
		return
	}

	if a.handleUnauthorizedResponse(w, r, showUnauthorizedJSON) {
		return
	}

	page.mu.Lock()
	defer page.mu.Unlock()

	page.updateOutdatedWidgets()
	items := aggregateRSSItems(page)
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].PublishedAt.After(items[j].PublishedAt)
	})

	feedURL := absoluteURLForRequest(r, a.Config.Server.BaseURL+r.URL.RequestURI())
	homePagePath := a.Config.Server.BaseURL + "/"
	if pageSlug != "" {
		homePagePath += page.Slug
	}
	homePageURL := absoluteURLForRequest(r, homePagePath)
	title := page.Title + " RSS Feed"

	switch path.Ext(r.URL.Path) {
	case ".xml":
		writeAggregateRSS(w, title, homePageURL, feedURL, items)
	case ".json":
		writeAggregateJSONFeed(w, title, homePageURL, feedURL, items)
	default:
		a.handleNotFound(w, r)
	}
}

func aggregateRSSItems(page *page) rssFeedItemList {
	items := make(rssFeedItemList, 0)

	var collect func(widgets)
	collect = func(widgetList widgets) {
		for _, widget := range widgetList {
			switch widget := widget.(type) {
			case *rssWidget:
				items = append(items, widget.Items...)
			case *groupWidget:
				collect(widget.Widgets)
			case *splitColumnWidget:
				collect(widget.Widgets)
			}
		}
	}

	collect(page.HeadWidgets)
	for i := range page.Columns {
		collect(page.Columns[i].Widgets)
	}

	return items
}

func writeAggregateJSONFeed(w http.ResponseWriter, title, homePageURL, feedURL string, items rssFeedItemList) {
	feed := aggregateJSONFeed{
		Version:     jsonFeedVersion,
		Title:       title,
		HomePageURL: homePageURL,
		FeedURL:     feedURL,
		Items:       make([]aggregateJSONFeedItem, 0, len(items)),
	}

	for _, item := range items {
		feed.Items = append(feed.Items, aggregateJSONFeedItem{
			ID:            item.Link,
			URL:           item.Link,
			Title:         item.Title,
			ContentText:   item.Description,
			DatePublished: item.PublishedAt.Format(time.RFC3339),
			Tags:          item.Categories,
			Source: aggregateJSONFeedSource{
				Name: item.ChannelName,
				URL:  item.ChannelURL,
			},
		})
	}

	w.Header().Set("Content-Type", "application/feed+json; charset=utf-8")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(feed); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeAggregateRSS(w http.ResponseWriter, title, homePageURL, feedURL string, items rssFeedItemList) {
	feed := aggregateRSSFeed{
		Version: "2.0",
		AtomNS:  "http://www.w3.org/2005/Atom",
		Channel: aggregateRSSChannel{
			Title:       title,
			Link:        homePageURL,
			Description: "RSS articles aggregated by Glance",
			AtomLink: aggregateRSSAtomLink{
				Href: feedURL,
				Rel:  "self",
				Type: "application/rss+xml",
			},
			Items: make([]aggregateRSSItem, 0, len(items)),
		},
	}

	if len(items) > 0 {
		feed.Channel.LastBuildDate = items[0].PublishedAt.Format(time.RFC1123Z)
	}

	for _, item := range items {
		feed.Channel.Items = append(feed.Channel.Items, aggregateRSSItem{
			Title:       item.Title,
			Link:        item.Link,
			GUID:        aggregateRSSGUID{IsPermaLink: true, Value: item.Link},
			Description: item.Description,
			PubDate:     item.PublishedAt.Format(time.RFC1123Z),
			Categories:  item.Categories,
			Source:      aggregateRSSItemSource{URL: item.ChannelURL, Value: item.ChannelName},
		})
	}

	contents, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	w.Write(contents)
	w.Write([]byte("\n"))
}

func absoluteURLForRequest(r *http.Request, urlPath string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwardedProto := r.Header.Get("X-Forwarded-Proto"); forwardedProto != "" {
		scheme = strings.TrimSpace(strings.Split(forwardedProto, ",")[0])
	}

	return scheme + "://" + r.Host + urlPath
}
