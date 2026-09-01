package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const maxLinkCards = 5

var urlPattern = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `]+`)
var imageExtensionPattern = regexp.MustCompile(`(?i)\.(?:jpg|jpeg|png|webp|gif)(?:$|[?#])`)

type linkCard struct {
	URL                          string
	Title, Description, ImageURL *string
	MediaType                    string
	Position                     int
	SiteName                     *string
}

func extractURLs(content string) []string {
	seen := make(map[string]struct{})
	urls := make([]string, 0, maxLinkCards)
	for _, raw := range urlPattern.FindAllString(content, -1) {
		raw = strings.TrimRight(raw, ".,;:!?)]}）。，；：！？")
		if _, ok := seen[raw]; ok || !isPublicURL(raw) {
			continue
		}
		seen[raw] = struct{}{}
		urls = append(urls, raw)
		if len(urls) == maxLinkCards {
			break
		}
	}
	return urls
}

func isPublicURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return isPublicIP(ip)
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return false
		}
	}
	return true
}

func isPublicIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast()
}

func mediaType(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "link"
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "youtube.com" || strings.HasSuffix(host, ".youtube.com") || host == "youtu.be" || host == "bilibili.com" || strings.HasSuffix(host, ".bilibili.com") {
		return "video"
	}
	if imageExtensionPattern.MatchString(parsed.RequestURI()) {
		return "image"
	}
	return "link"
}

func buildLinkCards(content string) []linkCard {
	urls := extractURLs(content)
	cards := make([]linkCard, 0, len(urls))
	for i, raw := range urls {
		cards = append(cards, linkCard{URL: raw, MediaType: mediaType(raw), Position: i + 1})
	}
	return cards
}

func fetchLinkMetadata(ctx context.Context, card linkCard) linkCard {
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		if !isPublicURL(req.URL.String()) {
			return errors.New("redirect target is not public")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, card.URL, nil)
	if err != nil {
		return card
	}
	req.Header.Set("User-Agent", "csqa-link-card/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return card
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || resp.ContentLength > 1<<20 {
		return card
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return card
	}
	text := string(body)
	if title := htmlMeta(text, `(?i)<title[^>]*>([^<]{1,512})</title>`); title != "" {
		card.Title = stringPtr(title)
	}
	if image := htmlMeta(text, `(?i)<meta[^>]+property=["']og:image["'][^>]+content=["']([^"']+)`); image != "" && isPublicURL(image) {
		card.ImageURL = stringPtr(image)
	}
	if card.MediaType == "link" && card.ImageURL != nil {
		card.MediaType = "image"
	}
	return card
}

func htmlMeta(body, expression string) string {
	match := regexp.MustCompile(expression).FindStringSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}
func stringPtr(value string) *string { return &value }
