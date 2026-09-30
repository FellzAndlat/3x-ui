package runtime

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
)

func remoteForTestServer(t *testing.T, server *httptest.Server) *Remote {
	t.Helper()
	host, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return NewRemote(&model.Node{
		Id:                  1,
		Name:                "test-node",
		Address:             host,
		Port:                port,
		Scheme:              "http",
		BasePath:            "/",
		ApiToken:            "test-token",
		AllowPrivateAddress: true,
	}, nil)
}

func writeOKEnvelope(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "msg": "", "obj": nil})
}

func TestRemoteRestartXrayUsesCoreAwareEndpoint(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		writeOKEnvelope(w)
	}))
	defer server.Close()

	remote := remoteForTestServer(t, server)
	if err := remote.RestartXray(context.Background()); err != nil {
		t.Fatalf("RestartXray() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/panel/api/server/restartCoreService" {
		t.Fatalf("path = %q, want core-aware endpoint", gotPath)
	}
}

func TestRemoteRestartXrayFallsBackForLegacyNode(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/panel/api/server/restartCoreService":
			http.NotFound(w, r)
		case "/panel/api/server/restartXrayService":
			writeOKEnvelope(w)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	remote := remoteForTestServer(t, server)
	if err := remote.RestartXray(context.Background()); err != nil {
		t.Fatalf("RestartXray() fallback error = %v", err)
	}
	want := []string{"/panel/api/server/restartCoreService", "/panel/api/server/restartXrayService"}
	if len(paths) != len(want) || paths[0] != want[0] || paths[1] != want[1] {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestRemoteRestartXrayDoesNotHideCoreFailure(t *testing.T) {
	legacyCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/panel/api/server/restartXrayService" {
			legacyCalled = true
		}
		http.Error(w, "core failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	remote := remoteForTestServer(t, server)
	err := remote.RestartXray(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("RestartXray() error = %v, want HTTP 500", err)
	}
	if legacyCalled {
		t.Fatal("legacy restart endpoint must not be called after a real core failure")
	}
}

type mutableEgressResolver struct{ url string }

func (r *mutableEgressResolver) NodeEgressProxyURL(int) string { return r.url }

func TestRemoteHTTPClientRequiresConfiguredEgress(t *testing.T) {
	node := &model.Node{
		Id:                  77,
		Name:                "egress-node",
		Address:             "127.0.0.1",
		Port:                1,
		Scheme:              "http",
		OutboundTag:         "proxy-out",
		AllowPrivateAddress: true,
	}

	if _, err := NewRemote(node, nil).httpClient(); err == nil || !strings.Contains(err.Error(), "refusing direct connection") {
		t.Fatalf("nil resolver error = %v, want strict egress failure", err)
	}

	resolver := &mutableEgressResolver{}
	remote := NewRemote(node, resolver)
	if _, err := remote.httpClient(); err == nil || !strings.Contains(err.Error(), "egress bridge is unavailable") {
		t.Fatalf("empty resolver error = %v, want missing bridge failure", err)
	}

	resolver.url = "socks5://127.0.0.1:11080"
	first, err := remote.httpClient()
	if err != nil {
		t.Fatalf("httpClient() after bridge appears: %v", err)
	}
	resolver.url = "socks5://127.0.0.1:11081"
	second, err := remote.httpClient()
	if err != nil {
		t.Fatalf("httpClient() after bridge changes: %v", err)
	}
	if first == second {
		t.Fatal("httpClient() must rebuild when the resolved egress bridge changes")
	}
}
