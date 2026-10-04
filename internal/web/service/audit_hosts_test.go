package service

import (
	"testing"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/web/entity"
)

func TestAuditHostEndpointsValidatedBeforeWrite(t *testing.T) {
	setupBulkDB(t)
	svc := &HostService{}
	ib := mkInbound(t, 443, model.VLESS, `{"clients":[]}`)
	for _, host := range []string{"example.org:70000", "example.org:bad", "https://example.org", "[invalid]:443", "example.org:-1"} {
		if _, err := svc.AddHostGroup(&entity.HostGroup{Remark: "invalid", InboundIds: []int{ib.Id}, Hosts: []string{host}}); err == nil {
			t.Errorf("accepted invalid endpoint %q", host)
		}
	}
	for _, host := range []string{"", "example.org", "example.org:0", "[2001:db8::1]:8443", "2001:db8::1"} {
		if _, err := svc.AddHostGroup(&entity.HostGroup{Remark: "valid", InboundIds: []int{ib.Id}, Hosts: []string{host}}); err != nil {
			t.Errorf("rejected valid endpoint %q: %v", host, err)
		}
	}
}

func TestAuditHostGroupIDCollisionRejected(t *testing.T) {
	setupBulkDB(t)
	svc := &HostService{}
	ib := mkInbound(t, 443, model.VLESS, `{"clients":[]}`)
	req := &entity.HostGroup{GroupId: "same", Remark: "original", InboundIds: []int{ib.Id}, Hosts: []string{"original.example"}}
	if _, err := svc.AddHostGroup(req); err != nil {
		t.Fatal(err)
	}
	req.Remark = "replacement"
	if _, err := svc.AddHostGroup(req); err == nil {
		t.Fatal("duplicate host group ID accepted")
	}
	group, err := svc.GetHostGroup("same")
	if err != nil || group.Remark != "original" {
		t.Fatalf("collision changed the original group: %v, %v", group, err)
	}
}
