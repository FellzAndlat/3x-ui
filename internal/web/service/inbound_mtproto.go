package service

import (
	"context"

	"github.com/SawaMEN/3x-ui/v3/internal/database"
	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/mtproto"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

// DesiredMtprotoInstances derives the Telemt sidecar configs this panel should
// be running: one instance per enabled local MTProto inbound, serving only the
// secrets of clients that are both enabled in the inbound settings and not
// depletion-disabled in client_traffics. That is the same effective client set
// buildInboundForLocalRuntime pushes on interactive edits, so the reconcile job
// and the push paths agree on one fingerprint. A disagreement would surface as
// a needless Telemt reload/restart, and a job that read only the raw settings
// would keep serving depleted clients until an unrelated restart. Inbounds whose
// every secret is filtered away are omitted so Reconcile stops their sidecar.
func (s *InboundService) DesiredMtprotoInstances() ([]mtproto.Instance, error) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).
		Where("protocol = ? AND enable = ? AND node_id IS NULL", model.MTProto, true).
		Find(&inbounds).Error
	if err != nil {
		return nil, err
	}
	if len(inbounds) == 0 {
		return nil, nil
	}

	ids := make([]int, 0, len(inbounds))
	for _, ib := range inbounds {
		ids = append(ids, ib.Id)
	}
	var disabledRows []xray.ClientTraffic
	err = db.Model(xray.ClientTraffic{}).
		Where("inbound_id IN ? AND enable = ?", ids, false).
		Select("inbound_id", "email").
		Find(&disabledRows).Error
	if err != nil {
		return nil, err
	}
	disabled := make(map[int]map[string]struct{}, len(disabledRows))
	for _, row := range disabledRows {
		if disabled[row.InboundId] == nil {
			disabled[row.InboundId] = map[string]struct{}{}
		}
		disabled[row.InboundId][row.Email] = struct{}{}
	}

	instances := make([]mtproto.Instance, 0, len(inbounds))
	for _, ib := range inbounds {
		inst, ok := mtproto.InstanceFromInbound(ib)
		if !ok {
			continue
		}
		if off := disabled[ib.Id]; len(off) > 0 {
			kept := make([]mtproto.SecretEntry, 0, len(inst.Secrets))
			for _, sec := range inst.Secrets {
				if _, skip := off[sec.Name]; !skip {
					kept = append(kept, sec)
				}
			}
			inst.Secrets = kept
		}
		if len(inst.Secrets) == 0 {
			continue
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// applyLocalMtproto pushes a single local MTProto inbound's current client set
// to its Telemt sidecar right after a client edit commits, so add/remove,
// re-key and enable-toggle changes take effect immediately instead of waiting
// for the reconcile job. Telemt reloads the generated config in place through
// its native API; if reload fails, the manager falls back to restarting only
// that inbound's Telemt process. The inbound is re-read after commit so this
// path and the periodic reconcile use the same effective client set. Failures
// are logged and swallowed because the reconcile job is the backstop.
func (s *InboundService) applyLocalMtproto(inboundId int) {
	inbound, err := s.GetInbound(inboundId)
	if err != nil || inbound == nil || inbound.Protocol != model.MTProto || inbound.NodeID != nil {
		return
	}
	rt, err := s.runtimeFor(inbound)
	if err != nil {
		return
	}
	payload := inbound
	if inbound.Enable {
		if built, bErr := s.buildInboundForLocalRuntime(database.GetDB(), inbound); bErr == nil {
			payload = built
		}
	}
	if err := rt.UpdateInbound(context.Background(), inbound, payload); err != nil {
		logger.Debug("mtproto: immediate client apply failed for inbound", inboundId, ":", err)
	}
}

func (s *InboundService) resetMtprotoClientQuota(email string) {
	mgr := mtproto.GetManager()
	if !mgr.HasRunning() {
		return
	}
	id, ok := s.localMtprotoInboundIdForEmail(email)
	if !ok {
		return
	}
	s.applyLocalMtproto(id)
	mgr.ResetQuota(email)
}

func (s *InboundService) resetAllMtprotoQuotas() {
	mgr := mtproto.GetManager()
	if !mgr.HasRunning() {
		return
	}
	desired, err := s.DesiredMtprotoInstances()
	if err != nil {
		return
	}
	mgr.Reconcile(desired)
	for _, inst := range desired {
		for _, sec := range inst.Secrets {
			mgr.ResetQuota(sec.Name)
		}
	}
}

func (s *InboundService) localMtprotoInboundIdForEmail(email string) (int, bool) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	if err := db.Model(model.Inbound{}).
		Where("protocol = ? AND node_id IS NULL", model.MTProto).
		Find(&inbounds).Error; err != nil {
		return 0, false
	}
	for _, ib := range inbounds {
		inst, ok := mtproto.InstanceFromInbound(ib)
		if !ok {
			continue
		}
		for _, sec := range inst.Secrets {
			if sec.Name == email {
				return ib.Id, true
			}
		}
	}
	return 0, false
}
