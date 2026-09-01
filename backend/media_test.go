package main

import "testing"

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

func TestExtractURLsLimitsToFive(t *testing.T) {
	content := "https://example.com/1 https://example.com/2 https://example.com/3 https://example.com/4 https://example.com/5 https://example.com/6"
	if got := len(extractURLs(content)); got != maxLinkCards {
		t.Fatalf("got %d URLs, want %d", got, maxLinkCards)
	}
}
