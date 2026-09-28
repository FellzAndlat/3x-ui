package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"

	"github.com/SawaMEN/3x-ui/v3/internal/amneziawg"
	"github.com/SawaMEN/3x-ui/v3/internal/amneziawgnet"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/externalvpn"
	"github.com/SawaMEN/3x-ui/v3/internal/mieru"
	"github.com/SawaMEN/3x-ui/v3/internal/mtproto"
	"github.com/SawaMEN/3x-ui/v3/internal/tuic"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

type LocalDeps struct {
	APIPort        func() int
	CoreType       func() string
	RestartCore    func(context.Context) error
	SetNeedRestart func()
}

type Local struct {
	deps LocalDeps
	mu   sync.Mutex
}

func NewLocal(deps LocalDeps) *Local { return &Local{deps: deps} }
func (l *Local) Name() string        { return "local" }

func (l *Local) isSingBox() bool {
	return l.deps.CoreType != nil && l.deps.CoreType() == "sing-box"
}

func (l *Local) applyCoreChange(ctx context.Context) error {
	if l.isSingBox() {
		if l.deps.RestartCore == nil {
			return errors.New("sing-box runtime restart is not configured")
		}
		return l.deps.RestartCore(ctx)
	}
	if l.deps.SetNeedRestart != nil {
		l.deps.SetNeedRestart()
	}
	return nil
}

func (l *Local) withAPI(fn func(api *xray.XrayAPI) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.deps.APIPort == nil {
		return errors.New("local xray API port is not configured")
	}
	port := l.deps.APIPort()
	if port == 0 {
		return nil
	}
	if port < 0 {
		return errors.New("local xray is not running")
	}
	var api xray.XrayAPI
	if err := api.Init(port); err != nil {
		return err
	}
	defer api.Close()
	return fn(&api)
}

func (l *Local) AddInbound(ctx context.Context, ib *model.Inbound) error {
	if ib.Protocol == model.Mieru {
		inst, ok := mieru.InstanceFromInbound(ib)
		if !ok {
			return nil
		}
		return mieru.GetManager().Ensure(inst)
	}
	if ib.Protocol == model.Pingtunnel || ib.Protocol == model.TrustTunnel {
		inst, err := externalvpn.FromInbound(ib)
		if err != nil {
			return err
		}
		return externalvpn.GetManager().Ensure(inst)
	}
	if ib.Protocol == model.MTProto {
		inst, ok := mtproto.InstanceFromInbound(ib)
		if !ok {
			return nil
		}
		return mtproto.GetManager().Ensure(inst)
	}
	if ib.Protocol == model.AmneziaWG {
		inst, ok := amneziawg.InstanceFromInbound(ib)
		if !ok {
			return nil
		}
		err := amneziawgnet.GetManager().Ensure(amneziawgnet.Desired{Instance: inst, Options: amneziawgnet.DeviceOptions{HeaderProtectionKey: inst.Obfuscation.HeaderProtectionKey, ContentPaddingAddition: inst.Obfuscation.ContentPaddingAddition, RekeyAfterTime: inst.Obfuscation.RekeyAfterTime, RekeyTimeout: inst.Obfuscation.RekeyTimeout, RejectAfterTime: inst.Obfuscation.RejectAfterTime, KeepaliveTimeout: inst.Obfuscation.KeepaliveTimeout, MaxHandshakeAttempts: inst.Obfuscation.MaxHandshakeAttempts, RandomTrailers: inst.Obfuscation.RandomTrailers, DisableCookies: inst.Obfuscation.DisableCookies}})
		if l.deps.SetNeedRestart != nil {
			l.deps.SetNeedRestart()
		}
		return err
	}
	if ib.Protocol == model.TUIC {
		if l.isSingBox() {
			return l.applyCoreChange(ctx)
		}
		inst, ok := tuic.InstanceFromInbound(ib)
		if !ok {
			return nil
		}
		return tuic.GetManager().Ensure(inst)
	}
	if l.isSingBox() {
		return l.applyCoreChange(ctx)
	}
	body, err := json.MarshalIndent(ib.GenXrayInboundConfig(), "", "  ")
	if err != nil {
		return err
	}
	return l.withAPI(func(api *xray.XrayAPI) error { return api.AddInbound(body) })
}

func (l *Local) DelInbound(ctx context.Context, ib *model.Inbound) error {
	if ib.Protocol == model.Mieru {
		mieru.GetManager().Remove(ib.Id)
		return nil
	}
	if ib.Protocol == model.Pingtunnel || ib.Protocol == model.TrustTunnel {
		externalvpn.GetManager().Remove(ib.Id)
		return nil
	}
	if ib.Protocol == model.MTProto {
		mtproto.GetManager().Remove(ib.Id)
		return nil
	}
	if ib.Protocol == model.AmneziaWG {
		amneziawgnet.GetManager().Remove(ib.Id)
		if l.deps.SetNeedRestart != nil {
			l.deps.SetNeedRestart()
		}
		return nil
	}
	if ib.Protocol == model.TUIC {
		tuic.GetManager().Remove(ib.Id)
		if l.isSingBox() {
			return l.applyCoreChange(ctx)
		}
		return nil
	}
	if l.isSingBox() {
		return l.applyCoreChange(ctx)
	}
	return l.withAPI(func(api *xray.XrayAPI) error { return api.DelInbound(ib.Tag) })
}

func (l *Local) UpdateInbound(ctx context.Context, oldIb, newIb *model.Inbound) error {
	if oldIb.Protocol == model.Pingtunnel || oldIb.Protocol == model.TrustTunnel || newIb.Protocol == model.Pingtunnel || newIb.Protocol == model.TrustTunnel {
		if err := l.DelInbound(ctx, oldIb); err != nil {
			return err
		}
		if newIb.Enable {
			return l.AddInbound(ctx, newIb)
		}
		return nil
	}
	if l.isSingBox() && oldIb.Protocol != model.MTProto && oldIb.Protocol != model.AmneziaWG && oldIb.Protocol != model.Mieru && newIb.Protocol != model.MTProto && newIb.Protocol != model.AmneziaWG && newIb.Protocol != model.Mieru {
		if oldIb.Protocol == model.TUIC {
			tuic.GetManager().Remove(oldIb.Id)
		}
		return l.applyCoreChange(ctx)
	}
	if oldIb.Protocol == model.Mieru || newIb.Protocol == model.Mieru {
		return l.updateMieruInbound(ctx, oldIb, newIb)
	}
	if oldIb.Protocol == model.MTProto || newIb.Protocol == model.MTProto {
		return l.updateMtprotoInbound(ctx, oldIb, newIb)
	}
	if oldIb.Protocol == model.AmneziaWG || newIb.Protocol == model.AmneziaWG {
		return l.updateAmneziaWGInbound(ctx, oldIb, newIb)
	}
	if oldIb.Protocol == model.TUIC || newIb.Protocol == model.TUIC {
		return l.updateTuicInbound(ctx, oldIb, newIb)
	}
	_ = l.DelInbound(ctx, oldIb)
	if !newIb.Enable {
		return nil
	}
	return l.AddInbound(ctx, newIb)
}

func (l *Local) updateMtprotoInbound(ctx context.Context, oldIb, newIb *model.Inbound) error {
	if oldIb.Protocol == model.MTProto && newIb.Protocol != model.MTProto {
		mtproto.GetManager().Remove(oldIb.Id)
		if !newIb.Enable {
			return nil
		}
		return l.AddInbound(ctx, newIb)
	}
	if oldIb.Protocol != model.MTProto {
		_ = l.DelInbound(ctx, oldIb)
	}
	if !newIb.Enable {
		mtproto.GetManager().Remove(newIb.Id)
		return nil
	}
	inst, ok := mtproto.InstanceFromInbound(newIb)
	if !ok {
		mtproto.GetManager().Remove(newIb.Id)
		return nil
	}
	return mtproto.GetManager().Ensure(inst)
}

func (l *Local) updateAmneziaWGInbound(ctx context.Context, oldIb, newIb *model.Inbound) error {
	if l.deps.SetNeedRestart != nil {
		l.deps.SetNeedRestart()
	}
	if oldIb.Protocol == model.AmneziaWG && newIb.Protocol != model.AmneziaWG {
		amneziawgnet.GetManager().Remove(oldIb.Id)
		if !newIb.Enable {
			return nil
		}
		return l.AddInbound(ctx, newIb)
	}
	if oldIb.Protocol != model.AmneziaWG {
		_ = l.DelInbound(ctx, oldIb)
	}
	if !newIb.Enable {
		amneziawgnet.GetManager().Remove(newIb.Id)
		return nil
	}
	inst, ok := amneziawg.InstanceFromInbound(newIb)
	if !ok {
		amneziawgnet.GetManager().Remove(newIb.Id)
		return nil
	}
	return amneziawgnet.GetManager().Ensure(amneziawgnet.Desired{Instance: inst, Options: amneziawgnet.DeviceOptions{HeaderProtectionKey: inst.Obfuscation.HeaderProtectionKey, ContentPaddingAddition: inst.Obfuscation.ContentPaddingAddition, RekeyAfterTime: inst.Obfuscation.RekeyAfterTime, RekeyTimeout: inst.Obfuscation.RekeyTimeout, RejectAfterTime: inst.Obfuscation.RejectAfterTime, KeepaliveTimeout: inst.Obfuscation.KeepaliveTimeout, MaxHandshakeAttempts: inst.Obfuscation.MaxHandshakeAttempts, RandomTrailers: inst.Obfuscation.RandomTrailers, DisableCookies: inst.Obfuscation.DisableCookies}})
}

func (l *Local) updateTuicInbound(ctx context.Context, oldIb, newIb *model.Inbound) error {
	if oldIb.Protocol == model.TUIC && newIb.Protocol != model.TUIC {
		tuic.GetManager().Remove(oldIb.Id)
		if !newIb.Enable {
			return nil
		}
		return l.AddInbound(ctx, newIb)
	}
	if oldIb.Protocol != model.TUIC {
		_ = l.DelInbound(ctx, oldIb)
	}
	if !newIb.Enable {
		tuic.GetManager().Remove(newIb.Id)
		return nil
	}
	inst, ok := tuic.InstanceFromInbound(newIb)
	if !ok {
		tuic.GetManager().Remove(newIb.Id)
		return nil
	}
	return tuic.GetManager().Ensure(inst)
}

func (l *Local) updateMieruInbound(ctx context.Context, oldIb, newIb *model.Inbound) error {
	if oldIb.Protocol == model.Mieru && newIb.Protocol != model.Mieru {
		mieru.GetManager().Remove(oldIb.Id)
		if !newIb.Enable {
			return nil
		}
		return l.AddInbound(ctx, newIb)
	}
	if oldIb.Protocol != model.Mieru {
		_ = l.DelInbound(ctx, oldIb)
	}
	if !newIb.Enable {
		mieru.GetManager().Remove(newIb.Id)
		return nil
	}
	inst, ok := mieru.InstanceFromInbound(newIb)
	if !ok {
		mieru.GetManager().Remove(newIb.Id)
		return nil
	}
	return mieru.GetManager().Ensure(inst)
}

func (l *Local) AddUser(ctx context.Context, ib *model.Inbound, userMap map[string]any) error {
	if ib.Protocol == model.MTProto || ib.Protocol == model.AmneziaWG || ib.Protocol == model.TUIC || ib.Protocol == model.Mieru || ib.Protocol == model.Pingtunnel || ib.Protocol == model.TrustTunnel {
		return nil
	}
	if l.isSingBox() {
		return l.applyCoreChange(ctx)
	}
	return l.withAPI(func(api *xray.XrayAPI) error { return api.AddUser(string(ib.Protocol), ib.Tag, userMap) })
}

func (l *Local) RemoveUser(ctx context.Context, ib *model.Inbound, email string) error {
	if ib.Protocol == model.MTProto || ib.Protocol == model.AmneziaWG || ib.Protocol == model.TUIC || ib.Protocol == model.Mieru || ib.Protocol == model.Pingtunnel || ib.Protocol == model.TrustTunnel {
		return nil
	}
	if l.isSingBox() {
		return l.applyCoreChange(ctx)
	}
	return l.withAPI(func(api *xray.XrayAPI) error { return api.RemoveUser(ib.Tag, email) })
}

func (l *Local) AddClient(ctx context.Context, ib *model.Inbound, client model.Client) error {
	if !client.Enable {
		return nil
	}
	return l.AddUser(ctx, ib, map[string]any{"email": client.Email, "id": client.ID, "security": client.Security, "flow": client.Flow, "auth": client.Auth, "password": client.Password, "publicKey": client.PublicKey, "allowedIPs": client.AllowedIPs, "preSharedKey": client.PreSharedKey, "keepAlive": wgKeepAlive(client.KeepAliveSeconds())})
}

func (l *Local) DeleteUser(ctx context.Context, ib *model.Inbound, email string) error {
	if email == "" {
		return nil
	}
	if err := l.RemoveUser(ctx, ib, email); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil
		}
		return err
	}
	return nil
}
func (l *Local) DeleteClient(context.Context, string) error { return nil }
func (l *Local) UpdateUser(ctx context.Context, ib *model.Inbound, oldEmail string, payload model.Client) error {
	if ib.Protocol == model.Mieru {
		return nil // The caller reapplies the full inbound after committing its client list.
	}
	if l.isSingBox() {
		// sing-box reads the client list from the database when regenerating
		// its config. Avoid the remove+add double restart used by Xray's API.
		return l.applyCoreChange(ctx)
	}
	if oldEmail != "" {
		if err := l.RemoveUser(ctx, ib, oldEmail); err != nil && !strings.Contains(err.Error(), "not found") {
			return err
		}
	}
	if !payload.Enable {
		return nil
	}
	return l.AddUser(ctx, ib, map[string]any{"email": payload.Email, "id": payload.ID, "security": payload.Security, "flow": payload.Flow, "auth": payload.Auth, "password": payload.Password, "publicKey": payload.PublicKey, "allowedIPs": payload.AllowedIPs, "preSharedKey": payload.PreSharedKey, "keepAlive": wgKeepAlive(payload.KeepAliveSeconds())})
}

func wgKeepAlive(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	return strconv.Itoa(seconds)
}

func (l *Local) RestartXray(ctx context.Context) error {
	return l.applyCoreChange(ctx)
}

func (l *Local) ResetClientTraffic(_ context.Context, _ *model.Inbound, _ string) error { return nil }

func (l *Local) ResetAllTraffics(_ context.Context) error { return nil }

func (l *Local) ResetInboundTraffic(_ context.Context, _ *model.Inbound) error { return nil }
