package controller

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"

	"github.com/gin-gonic/gin"
)

func TestNormalizeSingBoxSessionNodeID(t *testing.T) {
	zero := 0
	positive := 7
	negative := -1

	tests := []struct {
		name    string
		input   *int
		wantNil bool
		want    int
		wantErr bool
	}{
		{name: "local nil", input: nil, wantNil: true},
		{name: "local zero", input: &zero, wantNil: true},
		{name: "remote node", input: &positive, want: positive},
		{name: "negative node", input: &negative, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeSingBoxSessionNodeID(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if got != nil {
					t.Fatalf("got nodeId %d, want local nil", *got)
				}
				return
			}
			if got == nil || *got != tt.want {
				t.Fatalf("got %v, want %d", got, tt.want)
			}
		})
	}
}

func TestNormalizeSingBoxSessionNodeTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	remote := 7
	local := 0

	t.Run("admin may target remote runtime", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		c.Set("api_token_scope", model.ApiScopeAdmin)
		got, err := normalizeSingBoxSessionNodeTarget(c, &remote)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil || *got != remote {
			t.Fatalf("got %v, want node %d", got, remote)
		}
	})

	t.Run("node sync may target local runtime", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		c.Set("api_token_scope", model.ApiScopeNodeSync)
		got, err := normalizeSingBoxSessionNodeTarget(c, &local)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("got %v, want local nil", got)
		}
	})

	t.Run("node sync may not relay to remote runtime", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		c.Set("api_token_scope", model.ApiScopeNodeSync)
		if _, err := normalizeSingBoxSessionNodeTarget(c, &remote); err == nil {
			t.Fatal("expected node-sync relay to be rejected")
		}
	})
}

func TestNormalizeSingBoxSessionUsers(t *testing.T) {
	got := normalizeSingBoxSessionUsers([]string{" alice ", "", "bob", "alice", "  bob  ", "carol"})
	want := []string{"alice", "bob", "carol"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeSingBoxSessionUsers() = %v, want %v", got, want)
	}
}

func TestSingBoxSessionNodeSyncScope(t *testing.T) {
	tests := []struct {
		path   string
		method string
	}{
		{path: "/server/singbox/sessions", method: http.MethodGet},
		{path: "/server/singbox/sessions/disconnect-user", method: http.MethodPost},
		{path: "/server/singbox/sessions/disconnect-users", method: http.MethodPost},
		{path: "/server/singbox/sessions/disconnect-inbound", method: http.MethodPost},
	}

	for _, tt := range tests {
		methods, ok := nodeSyncScopeAllow[tt.path]
		if !ok {
			t.Fatalf("node-sync scope missing %s", tt.path)
		}
		if _, ok := methods[tt.method]; !ok {
			t.Fatalf("node-sync scope missing %s %s", tt.method, tt.path)
		}
		if len(methods) != 1 {
			t.Fatalf("node-sync scope for %s grants %d methods, want exactly 1", tt.path, len(methods))
		}
	}
}
