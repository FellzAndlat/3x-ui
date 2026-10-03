package mtproto

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func serverPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return port
}

func TestScrapeStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/stats/users" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sesame" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"data":[`+
			`{"username":"alice","current_connections":2,"active_unique_ips":1,"total_octets":300},`+
			`{"username":"bob","current_connections":0,"active_unique_ips":0,"total_octets":12}`+
			`],"revision":"abc"}`)
	}))
	defer srv.Close()

	users, ok := scrapeStats(serverPort(t, srv), "sesame")
	if !ok {
		t.Fatal("scrapeStats should succeed against Telemt /v1/stats/users")
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d: %+v", len(users), users)
	}
	if users["alice"].TotalOctets != 300 || users["alice"].CurrentConnections != 2 {
		t.Fatalf("alice stats parsed wrong: %+v", users["alice"])
	}
	if users["bob"].TotalOctets != 12 || users["bob"].CurrentConnections != 0 {
		t.Fatalf("bob stats parsed wrong: %+v", users["bob"])
	}
}

func TestScrapeStatsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	port := serverPort(t, srv)
	srv.Close()
	if _, ok := scrapeStats(port, ""); ok {
		t.Fatal("scrapeStats must report ok=false when the endpoint is unreachable")
	}
}
