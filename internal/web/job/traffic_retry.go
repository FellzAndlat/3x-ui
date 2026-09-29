package job

import (
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

// pendingTrafficBatch keeps deltas that were already consumed from a runtime
// counter source but could not be committed to the database. Runtime collectors
// such as Mieru and MTProto advance their own cursors when they are sampled, so
// dropping a failed AddTraffic call would permanently lose those bytes.
type pendingTrafficBatch struct {
	inbounds []*xray.Traffic
	clients  []*xray.ClientTraffic
}

func (b *pendingTrafficBatch) hasData() bool {
	return len(b.inbounds) > 0 || len(b.clients) > 0
}

func (b *pendingTrafficBatch) remember(inbounds []*xray.Traffic, clients []*xray.ClientTraffic) {
	b.inbounds = inbounds
	b.clients = clients
}

// flush retries the exact batch before a collector is sampled again. On
// success it clears the batch; on failure it leaves the slices intact so the
// next poll can retry without double-consuming the runtime counters.
func (b *pendingTrafficBatch) flush(inboundService *service.InboundService) (bool, bool, error) {
	if !b.hasData() {
		return false, false, nil
	}
	needRestart, clientsDisabled, err := inboundService.AddTraffic(b.inbounds, b.clients)
	if err != nil {
		return needRestart, clientsDisabled, err
	}
	b.inbounds = nil
	b.clients = nil
	return needRestart, clientsDisabled, nil
}
