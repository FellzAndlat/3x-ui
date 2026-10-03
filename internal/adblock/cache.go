package adblock

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type SourceCache struct {
	Domains      []string `json:"domains"`
	ETag         string   `json:"etag,omitempty"`
	LastModified string   `json:"lastModified,omitempty"`
	UpdatedAt    string   `json:"updatedAt"`
	CheckedAt    string   `json:"checkedAt"`
	LastError    string   `json:"lastError,omitempty"`
}
type SourceStatus struct {
	URL         string `json:"url"`
	DomainCount int    `json:"domainCount"`
	UpdatedAt   string `json:"updatedAt"`
	CheckedAt   string `json:"checkedAt"`
	LastError   string `json:"lastError"`
	Stale       bool   `json:"stale"`
}

// FetchCached refreshes each source independently. A failed source can only
// fall back to its own validated cache; new failed sources abort the transaction.
func (m Manager) FetchCached(ctx context.Context, sources []string, previous map[string]SourceCache, useCache bool) (FetchResult, error) {
	result := FetchResult{Cache: map[string]SourceCache{}}
	set := map[string]bool{}
	cacheEntries := 0
	cacheBytes := 0
	for _, raw := range sources {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		if err := ctx.Err(); err != nil {
			return FetchResult{}, err
		}
		url, err := m.validateURL(raw)
		if err != nil {
			return FetchResult{}, err
		}
		if _, seen := result.Cache[url]; seen {
			continue
		}
		if len(result.Cache) >= DefaultMaxSources {
			return FetchResult{}, fmt.Errorf("too many sources")
		}
		cached := previous[url]
		entry := cached
		if !useCache || len(cached.Domains) == 0 {
			var size int64
			entry, size, err = m.fetchCachedSource(ctx, url, cached)
			if err != nil {
				if ctx.Err() != nil {
					return FetchResult{}, ctx.Err()
				}
				if len(cached.Domains) == 0 {
					return FetchResult{}, fmt.Errorf("source %q has no working cache: %w", url, err)
				}
				entry = cached
				entry.LastError = err.Error()
				entry.CheckedAt = time.Now().UTC().Format(time.RFC3339)
			}
			result.BytesRead += size
		}
		if entry.LastError != "" {
			result.Warnings = append(result.Warnings, url+": "+entry.LastError)
		}
		cacheEntries += len(entry.Domains)
		for _, domain := range entry.Domains {
			cacheBytes += len(domain) + 4
		}
		if cacheEntries > 4*DefaultMaxDomains || cacheBytes > 64<<20 {
			return FetchResult{}, fmt.Errorf("source caches exceed combined cache budget (1,200,000 entries / 64 MiB)")
		}
		result.Cache[url] = entry
		result.SourceCount++
		for _, domain := range entry.Domains {
			set[domain] = true
		}
		maxDomains := m.MaxDomains
		if maxDomains <= 0 {
			maxDomains = DefaultMaxDomains
		}
		if len(set) > maxDomains {
			return FetchResult{}, fmt.Errorf("combined blocklist exceeds %d domains", maxDomains)
		}
	}
	for domain := range set {
		result.Domains = append(result.Domains, domain)
	}
	sort.Strings(result.Domains)
	return result, nil
}
func (m Manager) fetchCachedSource(ctx context.Context, url string, cached SourceCache) (SourceCache, int64, error) {
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	copyClient := *client
	oldRedirect := copyClient.CheckRedirect
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		if _, err := m.validateURL(req.URL.String()); err != nil {
			return err
		}
		if oldRedirect != nil {
			return oldRedirect(req, via)
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return SourceCache{}, 0, err
	}
	req.Header.Set("User-Agent", "3x-ui-adblock/1")
	req.Header.Set("Accept", "text/plain, */*;q=0.1")
	if len(cached.Domains) > 0 {
		if cached.ETag != "" {
			req.Header.Set("If-None-Match", cached.ETag)
		}
		if cached.LastModified != "" {
			req.Header.Set("If-Modified-Since", cached.LastModified)
		}
	}
	resp, err := copyClient.Do(req)
	if err != nil {
		return SourceCache{}, 0, err
	}
	defer resp.Body.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	if resp.StatusCode == http.StatusNotModified && len(cached.Domains) > 0 {
		cached.CheckedAt = now
		cached.LastError = ""
		return cached, 0, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SourceCache{}, 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "text/html") || strings.HasPrefix(contentType, "application/xhtml+xml") {
		return SourceCache{}, 0, fmt.Errorf("HTML received instead of blocklist")
	}
	maxBytes := m.MaxSourceBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxSourceBytes
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return SourceCache{}, 0, err
	}
	if int64(len(body)) > maxBytes {
		return SourceCache{}, 0, fmt.Errorf("source exceeds %d bytes", maxBytes)
	}
	domains, err := ParseDomains(bytes.NewReader(body), nil)
	if err != nil {
		return SourceCache{}, 0, err
	}
	if len(domains) == 0 {
		return SourceCache{}, 0, fmt.Errorf("no supported block entries")
	}
	return SourceCache{Domains: domains, ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified"), UpdatedAt: now, CheckedAt: now}, int64(len(body)), nil
}
func CacheStatuses(cache map[string]SourceCache) []SourceStatus {
	result := make([]SourceStatus, 0, len(cache))
	for url, entry := range cache {
		result = append(result, SourceStatus{url, len(entry.Domains), entry.UpdatedAt, entry.CheckedAt, entry.LastError, entry.LastError != ""})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].URL < result[j].URL })
	return result
}
