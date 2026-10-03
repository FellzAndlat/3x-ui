package adblock

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagerFetchMergesSourcesAndAllowlist(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/one", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0.0.0.0 ads.example.com\ntracker.example.net\n"))
	})
	mux.HandleFunc("/two", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("||tracker.example.net^\nallow.example.org\n"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	manager := Manager{
		Client: server.Client(),
		ValidateURL: func(raw string) (string, error) {
			if !strings.HasPrefix(raw, server.URL) {
				return "", fmt.Errorf("unexpected URL")
			}
			return raw, nil
		},
	}
	result, err := manager.Fetch(context.Background(), []string{server.URL + "/one", server.URL + "/two"}, []string{"allow.example.org"})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.SourceCount != 2 {
		t.Fatalf("SourceCount = %d, want 2", result.SourceCount)
	}
	if got := strings.Join(result.Domains, ","); got != "ads.example.com,domain:tracker.example.net,tracker.example.net" {
		t.Fatalf("Domains = %q", got)
	}
}

func TestManagerFetchKeepsResultAtomicOnSourceFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ads.example.com\n"))
	})
	mux.HandleFunc("/fail", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "broken", http.StatusBadGateway)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	manager := Manager{Client: server.Client()}
	result, err := manager.Fetch(context.Background(), []string{server.URL + "/ok", server.URL + "/fail"}, nil)
	if err == nil {
		t.Fatal("Fetch() error = nil, want failure")
	}
	if len(result.Domains) != 0 || result.SourceCount != 0 {
		t.Fatalf("failed update leaked partial result: %#v", result)
	}
}

func TestManagerFetchRejectsOversizedSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ads.example.com\ntracker.example.net\n"))
	}))
	defer server.Close()

	manager := Manager{Client: server.Client(), MaxSourceBytes: 8}
	_, err := manager.Fetch(context.Background(), []string{server.URL}, nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Fetch() error = %v, want size error", err)
	}
}

func TestManagerRejectsEmptyOrHTMLSource(t *testing.T) {
	for _, body := range []string{"", "# no entries\n", "<html>login required</html>"} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			_, err := (Manager{Client: server.Client()}).Fetch(context.Background(), []string{server.URL}, nil)
			if err == nil {
				t.Fatal("empty/unsupported source must preserve previous list")
			}
		})
	}
}

func TestManagerDeduplicatesSourcesAndAllowsEverything(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests++; _, _ = w.Write([]byte("ads.example.com\n")) }))
	defer server.Close()
	result, err := (Manager{Client: server.Client()}).Fetch(context.Background(), []string{server.URL, server.URL}, []string{"example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || result.SourceCount != 1 || len(result.Domains) != 0 {
		t.Fatalf("got %+v, requests=%d", result, requests)
	}
}
