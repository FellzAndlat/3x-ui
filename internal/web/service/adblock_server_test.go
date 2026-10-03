package service

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/adblock/youtubeproxy"
	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
)

func TestAdBlockManagedServerScopeAndPause(t *testing.T) {
	setupBulkDB(t)
	t.Setenv("XUI_DB_FOLDER", t.TempDir())
	defer youtubeproxy.Commit(false)
	s := &SettingService{}
	policies := policyFixture().Policies
	raw, _ := json.Marshal(policies)
	server := AdBlockServerSettings{Enabled: true, Limits: youtubeproxy.DefaultOptions()}
	serverRaw, _ := json.Marshal(server)
	if err := saveAdBlockValues(map[string]string{"adBlockEnable": "true", "adBlockPolicies": string(raw), "adBlockPolicyDomains": `{"light":"light.example"}`, "adBlockServer": string(serverRaw)}); err != nil {
		t.Fatal(err)
	}
	cfg := scopedXrayConfig()
	if err := s.applyXrayYouTubeServer(cfg); err != nil {
		t.Fatal(err)
	}
	text := string(cfg.RouterConfig)
	if strings.Contains(text, `"user":["alice"]`) || !strings.Contains(text, "youtube-server-filter") || !youtubeproxy.Status().Running {
		t.Fatalf("incorrect scope: %s", text)
	}
	if err := s.SetAdBlockPause(5); err != nil {
		t.Fatal(err)
	}
	paused := scopedXrayConfig()
	if err := s.applyXrayYouTubeServer(paused); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(paused.RouterConfig), "youtube-server-filter") {
		t.Fatal("paused interception active")
	}
	if err := s.MarkAdBlockApplied(nil); err != nil {
		t.Fatal(err)
	}
	if youtubeproxy.Status().Running {
		t.Fatal("proxy stayed running after route removal")
	}
}
func TestAdBlockManagedServerRealCores(t *testing.T) {
	for _, core := range []struct{ name, env string }{{"xray", "XUI_ADBLOCK_XRAY_BINARY"}, {"singbox", "XUI_ADBLOCK_SINGBOX_BINARY"}} {
		t.Run(core.name, func(t *testing.T) {
			setupBulkDB(t)
			t.Setenv("XUI_DB_FOLDER", t.TempDir())
			defer youtubeproxy.Commit(false)
			s := &SettingService{}
			policies := policyFixture().Policies
			raw, _ := json.Marshal(policies)
			server := AdBlockServerSettings{Enabled: true, Outbound: "direct", Limits: youtubeproxy.DefaultOptions()}
			sr, _ := json.Marshal(server)
			if err := saveAdBlockValues(map[string]string{"adBlockEnable": "true", "adBlockPolicies": string(raw), "adBlockPolicyDomains": `{"light":"light.example"}`, "adBlockServer": string(sr)}); err != nil {
				t.Fatal(err)
			}
			args := []string{"run", "-test", "-config"}
			if core.name == "xray" {
				cfg := scopedXrayConfig()
				if err := s.applyXrayYouTubeServer(cfg); err != nil {
					t.Fatal(err)
				}
				raw, _ = json.Marshal(cfg)
			} else {
				cfg := singbox.NewConfig()
				cfg.Outbounds = []map[string]any{{"type": "direct", "tag": "direct"}}
				cfg.Route["final"] = "direct"
				if err := s.applySingBoxYouTubeServer(cfg); err != nil {
					t.Fatal(err)
				}
				raw, _ = cfg.Marshal()
				args = []string{"check", "-c"}
			}
			binary := os.Getenv(core.env)
			if binary == "" {
				t.Skip("set " + core.env)
			}
			path := filepath.Join(t.TempDir(), "cfg.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if out, err := exec.CommandContext(ctx, binary, append(args, path)...).CombinedOutput(); err != nil {
				t.Fatalf("core rejected managed rules: %v %s", err, out)
			}
		})
	}
}

func TestAdBlockManagedServerRejectsMissingUpstream(t *testing.T) {
	setupBulkDB(t)
	t.Setenv("XUI_DB_FOLDER", t.TempDir())
	defer youtubeproxy.Commit(false)
	s := &SettingService{}
	server := AdBlockServerSettings{Enabled: true, Outbound: "missing", Limits: youtubeproxy.DefaultOptions()}
	raw, _ := json.Marshal(server)
	if err := saveAdBlockValues(map[string]string{"adBlockEnable": "true", "adBlockServer": string(raw)}); err != nil {
		t.Fatal(err)
	}
	if err := s.applyXrayYouTubeServer(scopedXrayConfig()); err == nil {
		t.Fatal("missing upstream silently routed direct")
	}
	if youtubeproxy.Status().Running {
		t.Fatal("invalid configuration started proxy")
	}
}
