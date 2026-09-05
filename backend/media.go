package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

const maxLinkCards = 4
const maxLinkURLRunes = 2048
const maxLinkDescriptionRunes = 16000

var urlPattern = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `]+`)
var imageExtensionPattern = regexp.MustCompile(`(?i)\.(?:jpg|jpeg|png|webp|gif)(?:$|[?#])`)
var nonPublicIPPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
}

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
		if len([]rune(raw)) > maxLinkURLRunes {
			continue
		}
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
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range nonPublicIPPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
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
		card := linkCard{URL: raw, MediaType: mediaType(raw), Position: i + 1}
		if card.MediaType == "image" {
			card.ImageURL = stringPtr(raw)
		}
		cards = append(cards, card)
	}
	return cards
}

func enrichLinkCards(ctx context.Context, cards []linkCard) []linkCard {
	var wg sync.WaitGroup
	for i := range cards {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			cards[index] = fetchLinkMetadata(ctx, cards[index])
		}(i)
	}
	wg.Wait()
	return cards
}

func fetchLinkMetadata(ctx context.Context, card linkCard) linkCard {
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client := newPublicHTTPClient(5 * time.Second)
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return card
	}
	if len(body) > 1<<20 {
		return card
	}
	metadata := parseMetadata(strings.NewReader(string(body)))
	if metadata.title != "" {
		card.Title = stringPtr(truncateRunes(metadata.title, 512))
	}
	if metadata.description != "" {
		card.Description = stringPtr(truncateRunes(metadata.description, maxLinkDescriptionRunes))
	}
	if metadata.siteName != "" {
		card.SiteName = stringPtr(truncateRunes(metadata.siteName, 255))
	}
	if metadata.image != "" {
		imageURL, err := url.Parse(metadata.image)
		baseURL, baseErr := url.Parse(card.URL)
		if err == nil && baseErr == nil {
			resolved := baseURL.ResolveReference(imageURL).String()
			if len([]rune(resolved)) <= maxLinkURLRunes && isPublicURL(resolved) {
				card.ImageURL = stringPtr(resolved)
			}
		}
	}
	if card.MediaType == "link" && card.ImageURL != nil {
		card.MediaType = "image"
	}
	return card
}

func newPublicHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{DialContext: publicDialContext}, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		if !isPublicURL(req.URL.String()) {
			return errors.New("redirect target is not public")
		}
		return nil
	}}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func publicDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("host could not be resolved")
	}
	dialer := &net.Dialer{}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			continue
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
	}
	if err == nil {
		err = errors.New("host resolved to a private address")
	}
	return nil, err
}

type pageMetadata struct{ title, description, image, siteName string }

func parseMetadata(reader io.Reader) pageMetadata {
	var metadata pageMetadata
	tokenizer := html.NewTokenizer(reader)
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			return metadata
		}
		token := tokenizer.Token()
		if tokenType == html.StartTagToken && token.Data == "title" && metadata.title == "" {
			if tokenizer.Next() == html.TextToken {
				metadata.title = strings.TrimSpace(tokenizer.Token().Data)
			}
			continue
		}
		if tokenType != html.SelfClosingTagToken && tokenType != html.StartTagToken || token.Data != "meta" {
			continue
		}
		attributes := make(map[string]string, len(token.Attr))
		for _, attribute := range token.Attr {
			attributes[strings.ToLower(attribute.Key)] = strings.TrimSpace(attribute.Val)
		}
		property := strings.ToLower(attributes["property"])
		name := strings.ToLower(attributes["name"])
		content := attributes["content"]
		switch {
		case property == "og:title" && content != "":
			metadata.title = content
		case (property == "og:description" || name == "description") && content != "":
			metadata.description = content
		case property == "og:image" && content != "":
			metadata.image = content
		case property == "og:site_name" && content != "":
			metadata.siteName = content
		}
	}
}
func stringPtr(value string) *string { return &value }
