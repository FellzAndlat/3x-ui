package youtubeproxy

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRoutingPresetsAcceptedByCores(t *testing.T) {
	for _, core := range []struct{ name, env string }{{"xray", "XUI_ADBLOCK_XRAY_BINARY"}, {"singbox", "XUI_ADBLOCK_SINGBOX_BINARY"}} {
		t.Run(core.name, func(t *testing.T) {
			raw, err := RoutingPreset(core.name, "127.0.0.1:18080")
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err = json.Unmarshal(raw, &config); err != nil {
				t.Fatal(err)
			}
			if core.name == "xray" {
				config["outbounds"] = append([]any{map[string]any{"protocol": "freedom", "tag": "direct"}}, config["outbounds"].([]any)...)
				config["log"] = map[string]any{"loglevel": "none"}
			} else {
				config["outbounds"] = append([]any{map[string]any{"type": "direct", "tag": "direct"}}, config["outbounds"].([]any)...)
				config["route"].(map[string]any)["final"] = "direct"
				config["log"] = map[string]any{"level": "error"}
			}
			binary := os.Getenv(core.env)
			if binary == "" {
				t.Skip("set " + core.env)
			}
			raw, err = json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.json")
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"check", "-c", path}
			if core.name == "xray" {
				args = []string{"run", "-test", "-config", path}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if out, err := exec.CommandContext(ctx, binary, args...).CombinedOutput(); err != nil {
				t.Fatalf("invalid preset: %v %s", err, out)
			}
		})
	}
	for _, address := range []string{"0.0.0.0:18080", "localhost:18080", "127.0.0.1:0"} {
		if _, err := RoutingPreset("xray", address); err == nil {
			t.Fatal("invalid preset target accepted")
		}
	}
}
