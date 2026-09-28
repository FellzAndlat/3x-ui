package sub

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
	"github.com/gin-gonic/gin"
)

func TestLegacyHiddifySubID(t *testing.T) {
	const id = "768e8bdd-bee3-4442-9006-b26464148aaa"
	aliases := []service.HiddifyLegacySubscriptionAlias{
		{Path: "NvReJ7i2bXWM8kPqdZwz"},
		{Path: "/older-path/"},
	}

	for _, path := range []string{
		"/NvReJ7i2bXWM8kPqdZwz/" + id,
		"/NvReJ7i2bXWM8kPqdZwz/" + id + "/",
		"/older-path/" + id + "/",
	} {
		got, ok := legacyHiddifySubID(path, aliases)
		if !ok || got != id {
			t.Fatalf("legacyHiddifySubID(%q) = %q, %v", path, got, ok)
		}
	}

	for _, path := range []string{
		"/wrong/" + id + "/",
		"/NvReJ7i2bXWM8kPqdZwz/not-a-uuid/",
		"/NvReJ7i2bXWM8kPqdZwz/" + id + "/extra",
	} {
		if got, ok := legacyHiddifySubID(path, aliases); ok {
			t.Fatalf("legacyHiddifySubID(%q) unexpectedly matched %q", path, got)
		}
	}
}

func TestLegacyHiddifyRouteAcceptsNewSubscriptionDomain(t *testing.T) {
	initSubDB(t)
	const id = "b1337b29-8d60-4491-a468-c2bf120cb878"
	if err := (&service.SettingService{}).AddHiddifyLegacySubscriptionAlias(service.HiddifyLegacySubscriptionAlias{Path: "BackupPath123", Domains: []string{"old.example.com"}}); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Create(&model.ClientRecord{Email: "hiddify_user_b1337b29", SubID: id, HiddifySubURI: "https://cdn.example.com/BackupPath123/"}).Error; err != nil {
		t.Fatal(err)
	}
	s := NewServer()
	router := gin.New()
	router.Use(s.subscriptionDomainValidator("old.example.com"))
	router.NoRoute(func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/BackupPath123/" + id, http.StatusNoContent},
		{"/BackupPath123/" + id + "/", http.StatusNoContent},
		{"/BackupPath123/768e8bdd-bee3-4442-9006-b26464148aaa/", http.StatusForbidden},
		{"/wrong/" + id, http.StatusForbidden},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://cdn.example.com"+tc.path, nil)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != tc.want {
			t.Fatalf("GET %s on CDN host: HTTP %d, want %d", tc.path, res.Code, tc.want)
		}
	}
}

func TestPanelForwardsOnlyImportedHiddifySubscriptionPath(t *testing.T) {
	initSubDB(t)
	const id = "b1337b29-8d60-4491-a468-c2bf120cb878"
	if err := (&service.SettingService{}).AddHiddifyLegacySubscriptionAlias(service.HiddifyLegacySubscriptionAlias{Path: "BackupPath123"}); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Create(&model.ClientRecord{Email: "hiddify_user_b1337b29", SubID: id, HiddifySubURI: "https://cdn.example.com/BackupPath123/"}).Error; err != nil {
		t.Fatal(err)
	}
	s := NewServer()
	inner := gin.New()
	inner.NoRoute(func(c *gin.Context) { c.Status(http.StatusOK) })
	s.httpServer = &http.Server{Handler: inner}
	panel := gin.New()
	panel.NoRoute(func(c *gin.Context) {
		if s.ServeLegacySubscription(c.Writer, c.Request) {
			c.Abort()
			return
		}
		c.Status(http.StatusNotFound)
	})
	for _, tc := range []struct {
		path string
		forward bool
	}{
		{"/BackupPath123/" + id, true},
		{"/other/" + id, false},
		{"/BackupPath123/not-a-uuid", false},
	} {
		req := httptest.NewRequest(http.MethodGet, "https://cdn.example.com"+tc.path, nil)
		res := httptest.NewRecorder()
		forwarded := s.ServeLegacySubscription(res, req)
		if forwarded != tc.forward {
			t.Fatalf("%s forwarded = %v, want %v", tc.path, forwarded, tc.forward)
		}
		if forwarded && res.Code != http.StatusOK {
			t.Fatalf("%s: HTTP %d", tc.path, res.Code)
		}
		res = httptest.NewRecorder()
		panel.ServeHTTP(res, req)
		want := http.StatusNotFound
		if tc.forward {
			want = http.StatusOK
		}
		if res.Code != want {
			t.Fatalf("panel GET %s: HTTP %d, want %d", tc.path, res.Code, want)
		}
	}
}

func TestNormalizeRequestHost(t *testing.T) {
	for input, want := range map[string]string{
		"VETROFF.FUN:443": "vetroff.fun",
		"vetroff.fun.":    "vetroff.fun",
		"[::1]:2096":      "::1",
	} {
		if got := normalizeRequestHost(input); got != want {
			t.Fatalf("normalizeRequestHost(%q) = %q, want %q", input, got, want)
		}
	}
}
