package main

import (
	"net"
	"strings"
	"testing"
)

func TestMediaType(t *testing.T) {
	for _, test := range []struct{ url, want string }{
		{"https://www.youtube.com/watch?v=x", "video"},
		{"https://www.bilibili.com/video/BV1", "video"},
		{"https://example.com/image.png?x=1", "image"},
		{"https://example.com/article", "link"},
	} {
		if got := mediaType(test.url); got != test.want {
			t.Errorf("mediaType(%q) = %q, want %q", test.url, got, test.want)
		}
	}
}

func TestExtractURLsRejectsPrivateAndLimitsDuplicates(t *testing.T) {
	content := "https://127.0.0.1/a https://example.com/a https://example.com/a https://localhost/x"
	urls := extractURLs(content)
	if len(urls) != 1 || urls[0] != "https://example.com/a" {
		t.Fatalf("urls = %#v", urls)
	}
}

func TestExtractURLsLimitsToFour(t *testing.T) {
	content := "https://example.com/1 https://example.com/2 https://example.com/3 https://example.com/4 https://example.com/5 https://example.com/6"
	if got := len(extractURLs(content)); got != maxLinkCards {
		t.Fatalf("got %d URLs, want %d", got, maxLinkCards)
	}
}

func TestBuildLinkCardsUsesDirectImageAsImageURL(t *testing.T) {
	cards := buildLinkCards("https://example.com/image.png")
	if len(cards) != 1 || cards[0].ImageURL == nil || *cards[0].ImageURL != cards[0].URL {
		t.Fatalf("cards = %#v", cards)
	}
}

func TestIsPublicIPRejectsReservedNetworks(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "100.64.0.1", "169.254.169.254", "192.0.2.1", "198.18.0.1", "2001:db8::1"} {
		if isPublicIP(net.ParseIP(raw)) {
			t.Errorf("isPublicIP(%q) = true, want false", raw)
		}
	}
	for _, raw := range []string{"1.1.1.1", "2606:4700:4700::1111"} {
		if !isPublicIP(net.ParseIP(raw)) {
			t.Errorf("isPublicIP(%q) = false, want true", raw)
		}
	}
}

func TestParseMetadataIgnoresAttributeOrder(t *testing.T) {
	metadata := parseMetadata(strings.NewReader(`<html><head><title>Fallback</title><meta content="Summary" name="description"><meta content="https://example.com/a.png" property="og:image"><meta content="Site" property="og:site_name"></head></html>`))
	if metadata.title != "Fallback" || metadata.description != "Summary" || metadata.image == "" || metadata.siteName != "Site" {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("计算机平台", 3); got != "计算机" {
		t.Fatalf("truncateRunes() = %q", got)
	}
}

func TestTruncateRunesKeepsShortValue(t *testing.T) {
	if got := truncateRunes("short", 10); got != "short" {
		t.Fatalf("truncateRunes() = %q", got)
	}
}
