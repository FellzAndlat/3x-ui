package singbox

import (
	"net/http"
	"testing"
)

func TestNewClashHTTPClientDisablesEnvironmentProxy(t *testing.T) {
	client := newClashHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Clash client transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("Clash client must not use HTTP_PROXY/HTTPS_PROXY for local controller requests")
	}
}
