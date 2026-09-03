# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine3.24 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN CGO_ENABLED=0 \
    GOOS="${TARGETOS}" \
    GOARCH="${TARGETARCH}" \
    GOARM="${TARGETVARIANT#v}" \
    go build -trimpath -ldflags="-s -w" -o /out/glance .

FROM alpine:3.24

WORKDIR /app
COPY --from=build /out/glance ./glance

LABEL org.opencontainers.image.source="https://github.com/chand1012/glance" \
      org.opencontainers.image.description="Glance dashboard container image" \
      org.opencontainers.image.licenses="AGPL-3.0-only"

EXPOSE 8080/tcp
ENTRYPOINT ["/app/glance", "--config", "/app/config/glance.yml"]
