package service

import (
	"strings"
	"testing"
)

func TestTelemtControlAPIAutomation(t *testing.T) {
	for _, listen := range []string{"0.0.0.0:9091", "example.com:9091", "127.0.0.1:0", "[::]:9091"} {
		if validTelemtAPIListen(listen) {
			t.Fatalf("unsafe listener %s", listen)
		}
	}
	saved := telemtAPISettings{Listen: "127.0.0.1:32123", Auth: "Bearer existing"}
	api, err := prepareTelemtAPISettings(saved, 443)
	if err != nil || api != saved {
		t.Fatalf("saved API settings changed: %+v %v", api, err)
	}
	api, err = prepareTelemtAPISettings(telemtAPISettings{}, 443)
	if err != nil || !validTelemtAPIListen(api.Listen) || !strings.HasPrefix(api.Auth, "Bearer ") || len(api.Auth) != 71 {
		t.Fatalf("API not automated: %+v %v", api, err)
	}
	api, err = prepareTelemtAPISettings(saved, 32123)
	if err != nil || api.Listen == saved.Listen || api.Auth != saved.Auth {
		t.Fatalf("proxy/API port collision: %+v %v", api, err)
	}
}
