package singbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestClashControllerURLUsesLoopback(t *testing.T) {
	tests := map[string]string{
		"":                 clashAPIAddress,
		":9090":            "http://127.0.0.1:9090",
		"0.0.0.0:9091":     "http://127.0.0.1:9091",
		"[::]:9092":         "http://127.0.0.1:9092",
		"127.0.0.1:9093":   "http://127.0.0.1:9093",
		"192.0.2.10:9094":  "http://127.0.0.1:9094",
		"http://0.0.0.0:90": "http://127.0.0.1:90",
	}
	for input, want := range tests {
		if got := clashControllerURL(input); got != want {
			t.Fatalf("clashControllerURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestProxyDelay(t *testing.T) {
	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		if !strings.HasPrefix(r.URL.Path, "/proxies/") || !strings.HasSuffix(r.URL.Path, "/delay") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("url"); got != "https://example.com/generate_204" {
			t.Fatalf("unexpected test url %q", got)
		}
		if got := r.URL.Query().Get("timeout"); got != "2500" {
			t.Fatalf("unexpected timeout %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"delay":123,"delay2":456}`))
	}))
	defer server.Close()

	client := &ClashStatsClient{
		client:  server.Client(),
		baseURL: server.URL,
		secret:  "panel-secret",
	}
	result, err := client.ProxyDelay(context.Background(), "proxy / one", "https://example.com/generate_204", 2500*time.Millisecond)
	if err != nil {
		t.Fatalf("ProxyDelay returned error: %v", err)
	}
	if result.Delay != 123 || result.Delay2 != 456 {
		t.Fatalf("unexpected delay result: %+v", result)
	}
	if gotAuthorization != "Bearer panel-secret" {
		t.Fatalf("unexpected Authorization header %q", gotAuthorization)
	}
}

func TestProxyDelayReportsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"message":"context deadline exceeded"}`))
	}))
	defer server.Close()

	client := &ClashStatsClient{client: server.Client(), baseURL: server.URL}
	_, err := client.ProxyDelay(context.Background(), "dead", "https://example.com", time.Second)
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProxyDelayRejectsInvalidURL(t *testing.T) {
	client := NewClashStatsClient()
	for _, raw := range []string{"", "example.com", "file:///etc/passwd", "ftp://example.com/file"} {
		if _, err := client.ProxyDelay(context.Background(), "proxy", raw, time.Second); err == nil {
			t.Fatalf("expected URL %q to be rejected", raw)
		}
	}
}

func TestProxyDelayEncodesTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decoded, err := url.PathUnescape(strings.TrimSuffix(strings.TrimPrefix(r.URL.EscapedPath(), "/proxies/"), "/delay"))
		if err != nil {
			t.Fatal(err)
		}
		if decoded != "a/b c" {
			t.Fatalf("unexpected tag %q", decoded)
		}
		_, _ = w.Write([]byte(`{"delay":1}`))
	}))
	defer server.Close()

	client := &ClashStatsClient{client: server.Client(), baseURL: server.URL}
	if _, err := client.ProxyDelay(context.Background(), "a/b c", "https://example.com", time.Second); err != nil {
		t.Fatalf("ProxyDelay returned error: %v", err)
	}
}
