package adblock

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	DefaultMaxSourceBytes int64 = 32 << 20
	DefaultMaxDomains           = 300_000
	DefaultMaxSources           = 32
)

type URLValidator func(string) (string, error)

type Manager struct {
	Client         *http.Client
	ValidateURL    URLValidator
	MaxSourceBytes int64
	MaxDomains     int
}

type FetchResult struct {
	Cache       map[string]SourceCache
	Warnings    []string
	Domains     []string
	SourceCount int
	BytesRead   int64
}

func (m Manager) Fetch(ctx context.Context, sources, allowlist []string) (FetchResult, error) {
	result, err := m.FetchCached(ctx, sources, nil, false)
	if err != nil {
		return FetchResult{}, err
	}
	result.Domains, err = ParseDomains(strings.NewReader(strings.Join(result.Domains, "\n")), allowlist)
	if err != nil {
		return FetchResult{}, err
	}
	return result, nil
}

func (m Manager) validateURL(raw string) (string, error) {
	if m.ValidateURL != nil {
		return m.ValidateURL(raw)
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return "", errors.New("URL host is required")
	}
	if u.User != nil {
		return "", errors.New("URL credentials are not allowed")
	}
	return u.String(), nil
}
