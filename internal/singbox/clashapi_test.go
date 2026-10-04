package singbox

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestClashAPIInfoUsesOnlyConfiguredController(t *testing.T) {
	tests := []struct {
		name       string
		config     string
		wantURL    string
		wantSecret string
		wantErr    string
	}{
		{name: "no experimental block", config: `{}`},
		{name: "no clash api block", config: `{"experimental":{}}`},
		{name: "no controller", config: `{"experimental":{"clash_api":{"secret":"unused"}}}`, wantSecret: "unused"},
		{name: "empty controller", config: `{"experimental":{"clash_api":{"external_controller":"","secret":"unused"}}}`, wantSecret: "unused"},
		{name: "loopback controller", config: `{"experimental":{"clash_api":{"external_controller":"127.0.0.1:10090","secret":"panel-secret"}}}`, wantURL: "http://127.0.0.1:10090", wantSecret: "panel-secret"},
		{name: "malformed json", config: `{`, wantErr: "parse sing-box config"},
		{name: "invalid controller", config: `{"experimental":{"clash_api":{"external_controller":"bad"}}}`, wantErr: "invalid sing-box Clash API controller"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotURL, gotSecret, err := clashAPIInfo([]byte(tc.config))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("clashAPIInfo error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("clashAPIInfo returned error: %v", err)
			}
			if gotURL != tc.wantURL || gotSecret != tc.wantSecret {
				t.Fatalf("clashAPIInfo = (%q, %q), want (%q, %q)", gotURL, gotSecret, tc.wantURL, tc.wantSecret)
			}
		})
	}
}

func TestClashControllerURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "shorthand wildcard", input: ":9090", want: "http://127.0.0.1:9090"},
		{name: "ipv4 wildcard", input: "0.0.0.0:9091", want: "http://127.0.0.1:9091"},
		{name: "ipv6 wildcard", input: "[::]:9092", want: "http://[::1]:9092"},
		{name: "ipv4 loopback", input: "127.0.0.1:9093", want: "http://127.0.0.1:9093"},
		{name: "ipv6 loopback", input: "[::1]:9094", want: "http://[::1]:9094"},
		{name: "localhost", input: "localhost:9095", want: "http://127.0.0.1:9095"},
		{name: "disabled", input: "", wantErr: "disabled"},
		{name: "URL is not a bind address", input: "http://127.0.0.1:9090", wantErr: "bind address"},
		{name: "remote address", input: "192.0.2.10:9090", wantErr: "not assigned to this host"},
		{name: "hostname", input: "example.com:9090", wantErr: "must be an IP address or localhost"},
		{name: "missing port", input: "127.0.0.1", wantErr: "invalid sing-box Clash API controller"},
		{name: "zero port", input: "127.0.0.1:0", wantErr: "invalid sing-box Clash API controller port"},
		{name: "invalid port", input: "127.0.0.1:not-a-port", wantErr: "invalid sing-box Clash API controller port"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := clashControllerURLWithLocalCheck(tc.input, func(net.IP) (bool, error) { return false, nil })
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("clashControllerURL(%q) error = %v, want containing %q", tc.input, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("clashControllerURL(%q) returned error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("clashControllerURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestClashControllerURLAllowsAssignedAddress(t *testing.T) {
	addresses, err := net.InterfaceAddrs()
	if errors.Is(err, os.ErrPermission) {
		t.Skipf("network interface inspection unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range addresses {
		ipNet, ok := address.(*net.IPNet)
		if !ok || ipNet.IP == nil || ipNet.IP.IsLoopback() || ipNet.IP.IsUnspecified() {
			continue
		}
		input := net.JoinHostPort(ipNet.IP.String(), "19090")
		want := "http://" + input
		got, err := clashControllerURL(input)
		if err != nil {
			t.Fatalf("clashControllerURL(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("clashControllerURL(%q) = %q, want %q", input, got, want)
		}
		return
	}
	t.Skip("no non-loopback IP address available")
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

func TestConnectionsReportsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
	}))
	defer server.Close()

	client := &ClashStatsClient{client: server.Client(), baseURL: server.URL}
	_, err := client.Connections(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProxyDelayRejectsInvalidURL(t *testing.T) {
	client := &ClashStatsClient{client: &http.Client{}, baseURL: "http://127.0.0.1:10090"}
	for _, raw := range []string{"", "example.com", "http://example.com", "https://:443", "file:///etc/passwd", "ftp://example.com/file"} {
		if _, err := client.ProxyDelay(context.Background(), "proxy", raw, time.Second); err == nil {
			t.Fatalf("expected URL %q to be rejected", raw)
		}
	}
}

func TestProxyDelayRejectsDisabledClashAPI(t *testing.T) {
	client := &ClashStatsClient{client: &http.Client{}, baseURL: ""}
	_, err := client.ProxyDelay(context.Background(), "proxy", "https://example.com", time.Second)
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProxyDelayReportsConfigurationError(t *testing.T) {
	client := &ClashStatsClient{client: &http.Client{}, configErr: net.InvalidAddrError("bad controller")}
	_, err := client.ProxyDelay(context.Background(), "proxy", "https://example.com", time.Second)
	if err == nil || !strings.Contains(err.Error(), "bad controller") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClashClientDoesNotFollowRedirect(t *testing.T) {
	targetHit := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHit = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"delay":1}`))
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()

	client := &ClashStatsClient{
		client:  redirect.Client(),
		baseURL: redirect.URL,
		secret:  "panel-secret",
	}
	_, err := client.ProxyDelay(context.Background(), "proxy", "https://example.com", time.Second)
	if err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("unexpected redirect result: %v", err)
	}
	if targetHit {
		t.Fatal("Clash API client followed a redirect and could have leaked its Authorization header")
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

func TestClashControllerLocalCheck(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		got, err := clashControllerURLWithLocalCheck("192.0.2.10:9090", func(ip net.IP) (bool, error) {
			if !ip.Equal(net.ParseIP("192.0.2.10")) {
				t.Fatalf("wrong address %v", ip)
			}
			return allowed, nil
		})
		if allowed {
			if err != nil || got != "http://192.0.2.10:9090" {
				t.Fatalf("assigned address: %q %v", got, err)
			}
		} else if err == nil || got != "" {
			t.Fatal("remote address accepted")
		}
	}
	wantErr := errors.New("interface inspection failed")
	got, err := clashControllerURLWithLocalCheck("192.0.2.10:9090", func(net.IP) (bool, error) { return false, wantErr })
	if got != "" || !errors.Is(err, wantErr) {
		t.Fatalf("inspection error not propagated: %q %v", got, err)
	}
}
