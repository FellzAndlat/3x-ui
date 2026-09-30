package service

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
	webruntime "github.com/SawaMEN/3x-ui/v3/internal/web/runtime"
)

func probeStatusObject(t *testing.T, obj map[string]any) HeartbeatPatch {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/panel/api/server/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "msg": "", "obj": obj})
	}))
	defer server.Close()

	host, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	node := &model.Node{
		Address:             host,
		Port:                port,
		Scheme:              "http",
		BasePath:            "/",
		AllowPrivateAddress: true,
	}
	patch, err := (&NodeService{}).probe(context.Background(), node, "")
	if err != nil {
		t.Fatalf("probe() error = %v", err)
	}
	return patch
}

func TestProbeKeepsXrayAndSingBoxIndependent(t *testing.T) {
	patch := probeStatusObject(t, map[string]any{
		"xray":    map[string]any{"version": "25.10.31", "state": "stop", "errorMsg": ""},
		"singbox": map[string]any{"installed": true, "version": "1.12.0", "state": "running", "errorMsg": ""},
		"core":    map[string]any{"type": "sing-box", "running": "sing-box", "state": "running", "version": "1.12.0", "errorMsg": ""},
	})
	if patch.XrayVersion != "25.10.31" || patch.SingBoxVersion != "1.12.0" {
		t.Fatalf("versions = xray:%q sing-box:%q", patch.XrayVersion, patch.SingBoxVersion)
	}
	if !patch.SingBoxKnown || !patch.SingBoxInstalled {
		t.Fatalf("sing-box flags = known:%v installed:%v", patch.SingBoxKnown, patch.SingBoxInstalled)
	}
	if patch.CoreType != CoreTypeSingBox || patch.RunningCore != CoreTypeSingBox {
		t.Fatalf("core = configured:%q running:%q", patch.CoreType, patch.RunningCore)
	}
}

func TestProbeReportsKnownMissingSingBox(t *testing.T) {
	patch := probeStatusObject(t, map[string]any{
		"xray":    map[string]any{"version": "25.10.31", "state": "running", "errorMsg": ""},
		"singbox": map[string]any{"installed": false, "version": "", "state": "stop", "errorMsg": ""},
		"core":    map[string]any{"type": "xray", "running": "xray", "state": "running", "version": "25.10.31", "errorMsg": ""},
	})
	if !patch.SingBoxKnown || patch.SingBoxInstalled || patch.SingBoxVersion != "" {
		t.Fatalf("sing-box = known:%v installed:%v version:%q", patch.SingBoxKnown, patch.SingBoxInstalled, patch.SingBoxVersion)
	}
	if patch.RunningCore != CoreTypeXray {
		t.Fatalf("running core = %q, want xray", patch.RunningCore)
	}
}

func TestProbeLegacyXrayNodeLeavesSingBoxUnknown(t *testing.T) {
	patch := probeStatusObject(t, map[string]any{
		"xray": map[string]any{"version": "25.10.31", "state": "running", "errorMsg": ""},
	})
	if patch.SingBoxKnown {
		t.Fatal("legacy node must not be reported as known missing sing-box")
	}
	if patch.CoreType != CoreTypeXray || patch.RunningCore != CoreTypeXray {
		t.Fatalf("legacy core = configured:%q running:%q", patch.CoreType, patch.RunningCore)
	}
}

func TestNodeViewExposesCoreStatus(t *testing.T) {
	view := toNodeView(&model.Node{
		XrayVersion: "25.10.31", SingBoxVersion: "1.12.0",
		SingBoxInstalled: true, SingBoxKnown: true,
		CoreType: CoreTypeSingBox, RunningCore: CoreTypeSingBox,
		SingBoxState: "running", SingBoxError: "",
	})
	if view.SingBoxVersion != "1.12.0" || !view.SingBoxInstalled || !view.SingBoxKnown {
		t.Fatalf("node view lost sing-box status: %#v", view)
	}
	if view.CoreType != CoreTypeSingBox || view.RunningCore != CoreTypeSingBox {
		t.Fatalf("node view lost core status: %#v", view)
	}
}

func TestProbeRefusesDirectFallbackForConfiguredOutbound(t *testing.T) {
	oldManager := webruntime.GetManager()
	webruntime.SetManager(nil)
	defer webruntime.SetManager(oldManager)

	node := &model.Node{
		Id:                  2147483000,
		Address:             "127.0.0.1",
		Port:                1,
		Scheme:              "http",
		BasePath:            "/",
		OutboundTag:         "__missing_node_bridge__",
		CoreType:            CoreTypeSingBox,
		AllowPrivateAddress: true,
	}
	patch, err := (&NodeService{}).Probe(context.Background(), node)
	if err == nil || !strings.Contains(err.Error(), "outbound bridge is unavailable") {
		t.Fatalf("Probe() error = %v, want missing egress bridge", err)
	}
	if patch.CoreType != CoreTypeSingBox {
		t.Fatalf("core type = %q, want preserved singbox", patch.CoreType)
	}
	if patch.LastError == "" {
		t.Fatal("Probe() must expose the missing bridge error in the heartbeat patch")
	}
}

func TestProbeLegacyHyphenatedSingBoxCore(t *testing.T) {
	patch := probeStatusObject(t, map[string]any{
		"xray": map[string]any{"version": "1.12.0", "state": "running", "errorMsg": ""},
		"core": map[string]any{"type": "sing-box", "running": "sing-box", "state": "running", "version": "1.12.0", "errorMsg": ""},
	})
	if patch.CoreType != CoreTypeSingBox || patch.RunningCore != CoreTypeSingBox {
		t.Fatalf("core = configured:%q running:%q", patch.CoreType, patch.RunningCore)
	}
	if !patch.SingBoxKnown || !patch.SingBoxInstalled || patch.SingBoxVersion != "1.12.0" {
		t.Fatalf("legacy sing-box = known:%v installed:%v version:%q", patch.SingBoxKnown, patch.SingBoxInstalled, patch.SingBoxVersion)
	}
	if patch.SingBoxState != "running" {
		t.Fatalf("legacy sing-box state = %q", patch.SingBoxState)
	}
	if patch.XrayVersion != "" || patch.XrayState != "" || patch.XrayError != "" {
		t.Fatalf("legacy mirrored sing-box leaked into Xray status: version=%q state=%q error=%q", patch.XrayVersion, patch.XrayState, patch.XrayError)
	}
}
