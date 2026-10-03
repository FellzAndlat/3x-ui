package job

import (
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
	"github.com/SawaMEN/3x-ui/v3/internal/web/websocket"
)

type OutboundAutoBalancerJob struct{}

func NewOutboundAutoBalancerJob() *OutboundAutoBalancerJob { return &OutboundAutoBalancerJob{} }
func (j *OutboundAutoBalancerJob) Run() {
	changes, err := (&service.OutboundSubscriptionService{}).ProbeAutomaticSubscriptions(0)
	if err != nil {
		logger.Warning("outbound auto balancer:", err)
	}
	applied, applyErr := (&service.OutboundSubscriptionService{}).ApplyAutomaticSelections()
	if applyErr != nil {
		logger.Warning("outbound auto balancer apply:", applyErr)
	}
	if changes > 0 || applied > 0 {
		websocket.BroadcastInvalidate(websocket.MessageTypeOutbounds)
	}
}
