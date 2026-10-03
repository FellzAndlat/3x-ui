package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/SawaMEN/3x-ui/v3/internal/web/global"
)

type domainValidatorSubServer struct {
	legacyPath string
}

func (s *domainValidatorSubServer) GetCtx() context.Context {
	return context.Background()
}

func (s *domainValidatorSubServer) ServeLegacySubscription(http.ResponseWriter, *http.Request) bool {
	return false
}

func (s *domainValidatorSubServer) IsLegacySubscriptionRequest(r *http.Request) bool {
	return r != nil && r.URL != nil && r.URL.Path == s.legacyPath
}

func domainValidatorStatus(host, path string) int {
	engine := gin.New()
	engine.Use(DomainValidatorMiddleware("panel.example.com"))
	engine.NoRoute(func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = host
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	return recorder.Code
}

func TestDomainValidatorAllowsOnlyValidatedLegacySubscriptionOnOtherDomain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := global.GetSubServer()
	global.SetSubServer(&domainValidatorSubServer{legacyPath: "/legacy/550e8400-e29b-41d4-a716-446655440000/"})
	defer global.SetSubServer(previous)

	if status := domainValidatorStatus("subscriptions.example.com", "/legacy/550e8400-e29b-41d4-a716-446655440000/"); status != http.StatusOK {
		t.Fatalf("legacy subscription status = %d, want %d", status, http.StatusOK)
	}
	if status := domainValidatorStatus("subscriptions.example.com", "/panel/settings"); status != http.StatusForbidden {
		t.Fatalf("unrelated path status = %d, want %d", status, http.StatusForbidden)
	}
	if status := domainValidatorStatus("panel.example.com", "/panel/settings"); status != http.StatusOK {
		t.Fatalf("configured domain status = %d, want %d", status, http.StatusOK)
	}
}
