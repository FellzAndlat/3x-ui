package adblock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCachedSourcesRevalidateAndRefreshIndependently(t *testing.T) {
	var round atomic.Int32
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		currentRound := round.Load()
		if currentRound == 0 {
			w.Header().Set("ETag", `"v1"`)
			w.Header().Set("Last-Modified", "Wed, 01 Oct 2025 00:00:00 GMT")
			if r.URL.Path == "/one" {
				_, _ = w.Write([]byte("one.ads.example"))
			} else {
				_, _ = w.Write([]byte("old.ads.example"))
			}
			return
		}
		if r.URL.Path == "/one" {
			if currentRound < 3 && (r.Header.Get("If-None-Match") != `"v1"` || r.Header.Get("If-Modified-Since") == "") {
				t.Error("missing conditional headers")
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if currentRound == 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("ETag", `"v2"`)
		_, _ = w.Write([]byte("new.ads.example"))
	}))
	defer server.Close()
	manager := Manager{Client: server.Client()}
	sources := []string{server.URL + "/one", server.URL + "/two"}
	first, err := manager.FetchCached(context.Background(), sources, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	round.Store(1)
	partial, err := manager.FetchCached(context.Background(), sources, first.Cache, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(partial.Warnings) != 1 || strings.Join(partial.Domains, ",") != "old.ads.example,one.ads.example" || partial.Cache[sources[0]].UpdatedAt != first.Cache[sources[0]].UpdatedAt {
		t.Fatalf("lost source cache: %+v", partial)
	}
	round.Store(2)
	recovered, err := manager.FetchCached(context.Background(), sources, partial.Cache, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered.Warnings) != 0 || strings.Join(recovered.Domains, ",") != "new.ads.example,one.ads.example" {
		t.Fatalf("recovery failed: %+v", recovered)
	}
	before := requests.Load()
	local, err := manager.FetchCached(context.Background(), sources, recovered.Cache, true)
	if err != nil || requests.Load() != before || len(local.Domains) != 2 {
		t.Fatal("local change downloaded sources")
	}
	round.Store(3)
	if _, err := manager.FetchCached(context.Background(), sources[:1], nil, false); err == nil {
		t.Fatal("304 accepted without cache")
	}
}
func TestCachedSourcesMissingCacheAndCancellationAreAtomic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "broken", 502) }))
	defer server.Close()
	manager := Manager{Client: server.Client()}
	if _, err := manager.FetchCached(context.Background(), []string{server.URL}, nil, false); err == nil {
		t.Fatal("uncached source failure ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cache := map[string]SourceCache{server.URL: {Domains: []string{"ads.example"}}}
	if _, err := manager.FetchCached(ctx, []string{server.URL}, cache, true); err == nil {
		t.Fatal("cancellation ignored")
	}
	if len(cache[server.URL].Domains) != 1 || cache[server.URL].LastError != "" {
		t.Fatal("caller cache mutated")
	}
}
