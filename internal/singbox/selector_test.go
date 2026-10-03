package singbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSelectorSwitchAuthenticatesAndConfirms(t *testing.T) {
	current := "a"
	puts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxies/sub-auto-7" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("invalid request %s", r.URL)
			w.WriteHeader(403)
			return
		}
		if r.Method == http.MethodPut {
			var payload struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			current = payload.Name
			puts++
			w.WriteHeader(204)
			return
		}
		_ = json.NewEncoder(w).Encode(SelectorState{Now: current, All: []string{"a", "b", "block"}})
	}))
	defer server.Close()
	client, err := NewSelectorClient(strings.TrimPrefix(server.URL, "http://"), "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, target := range []string{"b", "b", "block", "a"} {
		actual, err := client.Select(context.Background(), "sub-auto-7", target)
		if err != nil || actual != target {
			t.Fatal(actual, err)
		}
	}
	if puts != 3 {
		t.Fatal("unnecessary switch", puts)
	}
	if _, err := client.Select(context.Background(), "sub-auto-7", "missing"); err == nil || puts != 3 {
		t.Fatal("selected missing member")
	}
}
func TestSelectorRejectsRemoteAndUnconfirmedSwitch(t *testing.T) {
	for _, address := range []string{"example.com:9090", "192.168.1.1:9090", "https://127.0.0.1:9090", "bad"} {
		if _, err := NewSelectorClient(address, ""); err == nil {
			t.Fatal(address)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(204)
			return
		}
		_ = json.NewEncoder(w).Encode(SelectorState{Now: "a", All: []string{"a", "b"}})
	}))
	defer server.Close()
	client, err := NewSelectorClient(strings.TrimPrefix(server.URL, "http://"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if actual, err := client.Select(context.Background(), "sel", "b"); actual != "a" || err == nil {
		t.Fatal("false success", actual, err)
	}
}
