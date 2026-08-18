package glance

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAggregateFeedEndpointsUseRSSWidgetLimit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel>
  <title>Example Source</title><link>https://source.example/</link><description>Test feed</description>
  <item><title>Newest</title><link>https://source.example/newest</link><description>Newest description</description><pubDate>Fri, 03 Jan 2025 12:00:00 +0000</pubDate><category>news</category></item>
  <item><title>Middle</title><link>https://source.example/middle</link><description>Middle description</description><pubDate>Thu, 02 Jan 2025 12:00:00 +0000</pubDate></item>
  <item><title>Oldest</title><link>https://source.example/oldest</link><description>Oldest description</description><pubDate>Wed, 01 Jan 2025 12:00:00 +0000</pubDate></item>
</channel></rss>`)
	}))
	defer upstream.Close()

	widget := &rssWidget{
		widgetBase:   widgetBase{Type: "rss"},
		FeedRequests: []rssFeedRequest{{URL: upstream.URL}},
		Limit:        2,
	}
	if err := widget.initialize(); err != nil {
		t.Fatalf("initialize RSS widget: %v", err)
	}

	configuredPage := &page{Title: "News", Slug: "news", HeadWidgets: widgets{widget}}
	appConfig := config{}
	appConfig.Server.BaseURL = "/glance"
	app := &application{
		Config:     appConfig,
		slugToPage: map[string]*page{"news": configuredPage, "": configuredPage},
	}

	t.Run("JSON Feed", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "http://glance.test/api/pages/news/feed.json", nil)
		request.SetPathValue("page", "news")
		response := httptest.NewRecorder()

		app.handleAggregateFeedRequest(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
		}
		if got := response.Header().Get("Content-Type"); got != "application/feed+json; charset=utf-8" {
			t.Fatalf("unexpected content type: %s", got)
		}

		var feed aggregateJSONFeed
		if err := json.Unmarshal(response.Body.Bytes(), &feed); err != nil {
			t.Fatalf("decode JSON Feed: %v", err)
		}
		if feed.Version != jsonFeedVersion {
			t.Fatalf("unexpected JSON Feed version: %s", feed.Version)
		}
		if feed.HomePageURL != "http://glance.test/glance/news" {
			t.Fatalf("unexpected home page URL: %s", feed.HomePageURL)
		}
		if feed.FeedURL != "http://glance.test/glance/api/pages/news/feed.json" {
			t.Fatalf("unexpected feed URL: %s", feed.FeedURL)
		}
		if len(feed.Items) != 2 {
			t.Fatalf("expected configured limit of 2 items, got %d", len(feed.Items))
		}
		if feed.Items[0].Title != "Newest" || feed.Items[1].Title != "Middle" {
			t.Fatalf("items were not returned newest first: %#v", feed.Items)
		}
	})

	t.Run("RSS XML", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodGet, "http://glance.test/api/feed.xml", nil)
		response := httptest.NewRecorder()

		app.handleAggregateFeedRequest(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
		}
		if got := response.Header().Get("Content-Type"); got != "application/rss+xml; charset=utf-8" {
			t.Fatalf("unexpected content type: %s", got)
		}
		if !strings.HasPrefix(response.Body.String(), xml.Header) {
			t.Fatal("RSS response is missing the XML declaration")
		}

		var feed aggregateRSSFeed
		if err := xml.Unmarshal(response.Body.Bytes(), &feed); err != nil {
			t.Fatalf("decode RSS XML: %v", err)
		}
		if feed.Version != "2.0" {
			t.Fatalf("unexpected RSS version: %s", feed.Version)
		}
		if len(feed.Channel.Items) != 2 {
			t.Fatalf("expected configured limit of 2 items, got %d", len(feed.Channel.Items))
		}
		if feed.Channel.Items[0].Title != "Newest" {
			t.Fatalf("expected newest item first, got %q", feed.Channel.Items[0].Title)
		}
	})
}

func TestAggregateRSSItemsIncludesNestedWidgets(t *testing.T) {
	item := rssFeedItem{Title: "Nested", PublishedAt: time.Now()}
	nestedRSS := &rssWidget{Items: rssFeedItemList{item}}
	page := &page{
		HeadWidgets: widgets{
			&groupWidget{containerWidgetBase: containerWidgetBase{Widgets: widgets{nestedRSS}}},
		},
	}

	items := aggregateRSSItems(page)
	if len(items) != 1 || items[0].Title != item.Title {
		t.Fatalf("nested RSS item was not included: %#v", items)
	}
}
