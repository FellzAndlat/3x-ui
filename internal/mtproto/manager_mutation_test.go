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

func TestScrapeStatsTelemt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/users" || r.Header.Get("Authorization") != "Bearer sesame" {
			http.Error(w, "bad request", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"data":[{"username":"alice","current_connections":2,"total_octets":300},{"username":"bob","current_connections":0,"total_octets":12}],"revision":"x"}`)
	}))
	defer srv.Close()
	users, ok := scrapeStats(serverPort(t, srv), "sesame")
	if !ok || len(users) != 2 || users["alice"].Connections != 2 || users["alice"].TotalOctets != 300 {
		t.Fatalf("bad Telemt stats parse: ok=%v users=%+v", ok, users)
	}
}

func TestRequestReloadTelemt(t *testing.T) {
	var method, path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	if !requestReload(serverPort(t, srv), "sesame") {
		t.Fatal("202 reload must be accepted")
	}
	if method != http.MethodPost || path != "/v1/system/reload" || auth != "Bearer sesame" {
		t.Fatalf("unexpected reload request: %s %s %q", method, path, auth)
	}
}
