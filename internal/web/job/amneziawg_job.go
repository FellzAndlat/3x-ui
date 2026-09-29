package job

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/amneziawg"
	"github.com/SawaMEN/3x-ui/v3/internal/amneziawgnet"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

// AmneziaWGJob converges embedded AmneziaWG interfaces (inbounds AND the
// template's "amneziawg" outbounds) every 10s. When sing-box is selected,
// traffic is sampled directly from each embedded device; Xray already owns
// accounting when Xray is the selected core.
type AmneziaWGJob struct {
	inboundService service.InboundService
	settingService service.SettingService
	mu             sync.Mutex
	lastTraffic    map[amneziaWGTrafficKey]amneziaWGTrafficSample
}

type amneziaWGTrafficKey struct {
	inboundID int
	email     string
}

type amneziaWGTrafficSample struct {
	rx uint64
	tx uint64
}

// NewAmneziaWGJob creates a new AmneziaWG reconcile job instance.
func NewAmneziaWGJob() *AmneziaWGJob {
	return &AmneziaWGJob{lastTraffic: make(map[amneziaWGTrafficKey]amneziaWGTrafficSample)}
}

const amneziaWGOnlineWindow = 3 * time.Minute

// pruneAmneziaWGBaselines removes only baselines that are known to be stale.
// A desired inbound whose diagnostics are temporarily unavailable must retain
// its baseline; otherwise the next successful cumulative sample would replay
// already-accounted bytes as fresh traffic.
func pruneAmneziaWGBaselines(
	last map[amneziaWGTrafficKey]amneziaWGTrafficSample,
	desiredIDs map[int]struct{},
	observedIDs map[int]struct{},
	current map[amneziaWGTrafficKey]struct{},
) {
	for key := range last {
		if _, desired := desiredIDs[key.inboundID]; !desired {
			delete(last, key)
			continue
		}
		if _, observed := observedIDs[key.inboundID]; !observed {
			continue
		}
		if _, present := current[key]; !present {
			delete(last, key)
		}
	}
}

func (j *AmneziaWGJob) collectTraffic(coreType string, desired []amneziawg.Instance) {
	if coreType != service.CoreTypeSingBox || len(desired) == 0 {
		return
	}
	now := time.Now()
	var traffic []*xray.Traffic
	var clientTraffic []*xray.ClientTraffic
	activeEmails := make([]string, 0)
	activeTags := make([]string, 0)
	seenActiveTags := make(map[string]struct{})
	desiredIDs := make(map[int]struct{}, len(desired))
	observedIDs := make(map[int]struct{}, len(desired))
	current := make(map[amneziaWGTrafficKey]struct{})

	for _, inst := range desired {
		desiredIDs[inst.Id] = struct{}{}
		diag, err := j.inboundService.GetAmneziaWGDiagnostics(inst.Id)
		if err != nil || !diag.Running {
			continue
		}
		observedIDs[inst.Id] = struct{}{}
		for _, client := range diag.Clients {
			key := amneziaWGTrafficKey{inboundID: inst.Id, email: client.Email}
			current[key] = struct{}{}
			j.mu.Lock()
			prev := j.lastTraffic[key]
			j.lastTraffic[key] = amneziaWGTrafficSample{rx: client.RxBytes, tx: client.TxBytes}
			j.mu.Unlock()

			deltaRx, deltaTx := client.RxBytes, client.TxBytes
			if prev.rx <= client.RxBytes {
				deltaRx = client.RxBytes - prev.rx
			}
			if prev.tx <= client.TxBytes {
				deltaTx = client.TxBytes - prev.tx
			}
			if deltaRx > 0 || deltaTx > 0 {
				traffic = append(traffic, &xray.Traffic{
					IsInbound: true,
					Tag:       inst.Tag,
					Up:        int64(deltaRx),
					Down:      int64(deltaTx),
				})
				clientTraffic = append(clientTraffic, &xray.ClientTraffic{
					Email: client.Email,
					Up:    int64(deltaRx),
					Down:  int64(deltaTx),
				})
			}
			if !client.LastHandshake.IsZero() && now.Sub(client.LastHandshake) <= amneziaWGOnlineWindow {
				activeEmails = append(activeEmails, client.Email)
				if _, ok := seenActiveTags[inst.Tag]; !ok {
					seenActiveTags[inst.Tag] = struct{}{}
					activeTags = append(activeTags, inst.Tag)
				}
			}
		}
	}

	j.mu.Lock()
	pruneAmneziaWGBaselines(j.lastTraffic, desiredIDs, observedIDs, current)
	j.mu.Unlock()

	if len(traffic) > 0 || len(clientTraffic) > 0 {
		if _, _, err := j.inboundService.AddTraffic(traffic, clientTraffic); err != nil {
			logger.Warning("amneziawg job: add traffic failed:", err)
		}
	}
	if len(activeEmails) > 0 {
		if err := j.inboundService.BumpClientsLastOnline(activeEmails); err != nil {
			logger.Warning("amneziawg job: bump last online failed:", err)
		}
	}
	j.inboundService.RefreshLocalOnlineClients(activeEmails, activeTags)
}

// Run reconciles desired AmneziaWG inbounds with running embedded interfaces.
func (j *AmneziaWGJob) Run() {
	desired, err := j.inboundService.DesiredAmneziaWGInstances()
	if err != nil {
		logger.Warning("amneziawg job: get desired instances failed:", err)
		return
	}

	wanted := make([]amneziawgnet.Desired, 0, len(desired))
	for _, inst := range desired {
		wanted = append(wanted, amneziawgnet.Desired{
			Instance: inst,
			Options: amneziawgnet.DeviceOptions{
				HeaderProtectionKey:    inst.Obfuscation.HeaderProtectionKey,
				ContentPaddingAddition: inst.Obfuscation.ContentPaddingAddition,
				RekeyAfterTime:         inst.Obfuscation.RekeyAfterTime,
				RekeyTimeout:           inst.Obfuscation.RekeyTimeout,
				RejectAfterTime:        inst.Obfuscation.RejectAfterTime,
				KeepaliveTimeout:       inst.Obfuscation.KeepaliveTimeout,
				MaxHandshakeAttempts:   inst.Obfuscation.MaxHandshakeAttempts,
				RandomTrailers:         inst.Obfuscation.RandomTrailers,
				DisableCookies:         inst.Obfuscation.DisableCookies,
			},
		})
	}
	amneziawgnet.GetManager().Reconcile(wanted)

	coreType, _ := j.settingService.GetCoreType()
	j.collectTraffic(coreType, desired)

	outboundDesired, err := j.desiredOutboundInstances()
	if err != nil {
		logger.Warning("amneziawg job: get desired outbound instances failed:", err)
		return
	}
	amneziawgnet.GetOutboundManager().Reconcile(outboundDesired)
}

// desiredOutboundInstances derives client instances per template "amneziawg" outbound.
func (j *AmneziaWGJob) desiredOutboundInstances() ([]amneziawgnet.OutboundDesired, error) {
	template, err := j.settingService.GetXrayConfigTemplate()
	if err != nil {
		return nil, err
	}
	if template == "" {
		return nil, nil
	}
	cfg := &xray.Config{}
	if err := json.Unmarshal([]byte(template), cfg); err != nil {
		return nil, err
	}
	if len(cfg.OutboundConfigs) == 0 {
		return nil, nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(cfg.OutboundConfigs, &raws); err != nil {
		return nil, err
	}
	out := make([]amneziawgnet.OutboundDesired, 0, len(raws))
	for _, raw := range raws {
		if !amneziawg.IsAmneziaWGOutbound(raw) {
			continue
		}
		var probe struct {
			Tag string `json:"tag"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil || probe.Tag == "" {
			continue
		}
		inst, ok := amneziawg.InstanceFromOutbound(probe.Tag, raw)
		if !ok {
			continue
		}
		out = append(out, amneziawgnet.OutboundDesired{
			Instance: inst,
			Options: amneziawgnet.DeviceOptions{
				HeaderProtectionKey:    inst.Obfuscation.HeaderProtectionKey,
				ContentPaddingAddition: inst.Obfuscation.ContentPaddingAddition,
				RekeyAfterTime:         inst.Obfuscation.RekeyAfterTime,
				RekeyTimeout:           inst.Obfuscation.RekeyTimeout,
				RejectAfterTime:        inst.Obfuscation.RejectAfterTime,
				KeepaliveTimeout:       inst.Obfuscation.KeepaliveTimeout,
				MaxHandshakeAttempts:   inst.Obfuscation.MaxHandshakeAttempts,
				RandomTrailers:         inst.Obfuscation.RandomTrailers,
				DisableCookies:         inst.Obfuscation.DisableCookies,
			},
		})
	}
	return out, nil
}
