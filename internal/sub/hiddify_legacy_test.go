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

func TestLegacyHiddifyDomainBypassRequiresPanelForward(t *testing.T) {
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

	path := "/BackupPath123/" + id

	// A request arriving directly on the generic subscription listener must not
	// bypass its configured subscription domain just because it matches Hiddify.
	req := httptest.NewRequest(http.MethodGet, "http://cdn.example.com"+path, nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("direct Hiddify request: HTTP %d, want %d", res.Code, http.StatusForbidden)
	}

	// The same imported URL is still valid after the public panel listener has
	// explicitly selected it and forwarded it to the subscription engine.
	req = markLegacyHiddifyPanelForward(httptest.NewRequest(http.MethodGet, "http://cdn.example.com"+path, nil))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("panel-forwarded Hiddify request: HTTP %d, want %d", res.Code, http.StatusNoContent)
	}

	// A non-imported subscription ID must not gain the bypass even when marked.
	req = markLegacyHiddifyPanelForward(httptest.NewRequest(http.MethodGet, "http://cdn.example.com/BackupPath123/768e8bdd-bee3-4442-9006-b26464148aaa/", nil))
	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("unknown forwarded Hiddify request: HTTP %d, want %d", res.Code, http.StatusForbidden)
	}
}

func TestLegacyHiddifyHandlerRejectsDirectSubscriptionListener(t *testing.T) {
	initSubDB(t)
	const id = "b1337b29-8d60-4491-a468-c2bf120cb878"
	if err := (&service.SettingService{}).AddHiddifyLegacySubscriptionAlias(service.HiddifyLegacySubscriptionAlias{Path: "BackupPath123"}); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Create(&model.ClientRecord{Email: "hiddify_direct_b1337b29", SubID: id, HiddifySubURI: "https://cdn.example.com/BackupPath123/"}).Error; err != nil {
		t.Fatal(err)
	}

	s := NewServer()
	router := gin.New()
	router.NoRoute(s.legacyHiddifySubscription)

	req := httptest.NewRequest(http.MethodGet, "http://old.example.com/BackupPath123/"+id, nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("direct Hiddify request on subscription listener: HTTP %d, want %d", res.Code, http.StatusNotFound)
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
	inner.NoRoute(func(c *gin.Context) {
		if !isLegacyHiddifyPanelForward(c.Request) {
			c.Status(http.StatusForbidden)
			return
		}
		c.Status(http.StatusOK)
	})
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
		path    string
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
