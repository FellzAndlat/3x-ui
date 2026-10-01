package controller

import (
	"net/http"
	"testing"
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
