package service

import (
	"context"
	"runtime/debug"
	"strings"

	"github.com/SawaMEN/3x-ui/v3/internal/database/model"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"

	"gorm.io/gorm"
)

type trafficLocalApplyAction uint8

const (
	trafficAddUser trafficLocalApplyAction = iota + 1
	trafficRemoveUser
	trafficDisableInbound
)

type trafficLocalApplyPlan struct {
	action  trafficLocalApplyAction
	inbound model.Inbound
	client  map[string]any
	email   string
}

type trafficMutationBatch struct {
	localPlans  []trafficLocalApplyPlan
	remotePlans []trafficInboundUpdatePlan
	nodeIDs     map[int]struct{}
}

type trafficInboundUpdatePlan struct{ oldInbound, newInbound model.Inbound }

func newTrafficMutationBatch() *trafficMutationBatch {
	return &trafficMutationBatch{nodeIDs: make(map[int]struct{})}
}

func (b *trafficMutationBatch) addNode(nodeID int) {
	if nodeID > 0 {
		b.nodeIDs[nodeID] = struct{}{}
	}
}

func (b *trafficMutationBatch) markNodesTx(tx *gorm.DB) error {
	if b == nil {
		return nil
	}
	nodeSvc := NodeService{}
	for nodeID := range b.nodeIDs {
		if err := nodeSvc.MarkNodeDirtyTx(tx, nodeID); err != nil {
			return err
		}
	}
	return nil
}

// applyTrafficRemotePlans is bounded like every per-client node push: the nodes are
// already dirty, so an offline or slow one defers to the reconcile.
func (s *InboundService) applyTrafficRemotePlans(plans []trafficInboundUpdatePlan) bool {
	ids := make([]int, len(plans))
	for i := range plans {
		ids[i] = plans[i].newInbound.Id
	}
	failed, panics := fanoutInboundResults(ids, inboundFanoutConcurrency, func(i int) bool {
		rt, push, _, err := s.nodePushPlan(&plans[i].newInbound)
		if err == nil && push {
			ctx, cancel := nodePushContext()
			err = rt.UpdateInbound(ctx, &plans[i].oldInbound, &plans[i].newInbound)
			cancel()
		}
		if err != nil {
			logger.Debug("traffic post-commit remote apply failed:", err)
		}
		return err != nil
	})
	needRestart := false
	for i := range failed {
		needRestart = needRestart || failed[i] || panics[i] != nil
	}
	return needRestart
}

// applyTrafficLocalPlan isolates best-effort runtime updates from the durable
// traffic writer. Traffic and lifecycle DB changes are already committed before
// this runs; a runtime bug must therefore request a restart, not turn a
// successful accounting write into a "traffic writer panic" response.
func (s *InboundService) applyTrafficLocalPlan(plan *trafficLocalApplyPlan) (needRestart bool) {
	if plan == nil {
		return false
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Errorf("traffic post-commit runtime apply panic for inbound %d (%s): %v\n%s", plan.inbound.Id, plan.inbound.Tag, recovered, debug.Stack())
			needRestart = true
		}
	}()

	if plan.inbound.Protocol == model.MTProto {
		s.applyLocalMtproto(plan.inbound.Id)
		return false
	}
	if plan.inbound.Protocol == model.AmneziaWG {
		s.applyLocalAmneziaWG(plan.inbound.Id)
		return false
	}
	if plan.inbound.Protocol == model.TUIC {
		s.applyLocalTuic(plan.inbound.Id)
		return false
	}
	if plan.inbound.Protocol == model.Mieru {
		s.applyLocalMieru(plan.inbound.Id)
		return false
	}

	rt, err := s.runtimeFor(&plan.inbound)
	if err == nil {
		switch plan.action {
		case trafficAddUser:
			err = rt.AddUser(context.Background(), &plan.inbound, plan.client)
		case trafficRemoveUser:
			err = rt.RemoveUser(context.Background(), &plan.inbound, plan.email)
			if err != nil && strings.Contains(err.Error(), "not found") {
				err = nil
			}
		case trafficDisableInbound:
			err = rt.DelInbound(context.Background(), &plan.inbound)
			if xray.IsMissingHandlerErr(err) {
				err = nil
			}
		}
	}
	if err != nil {
		logger.Debug("traffic post-commit runtime apply failed:", err)
		return true
	}
	return false
}

func (s *InboundService) applyTrafficMutationBatch(b *trafficMutationBatch) bool {
	if b == nil {
		return false
	}
	needRestart := false
	for i := range b.localPlans {
		needRestart = s.applyTrafficLocalPlan(&b.localPlans[i]) || needRestart
	}
	return needRestart
}
