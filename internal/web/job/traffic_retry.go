package job

import "github.com/SawaMEN/3x-ui/v3/internal/xray"

type trafficCommitFunc func([]*xray.Traffic, []*xray.ClientTraffic) (bool, bool, error)

// pendingTrafficBatch keeps deltas that were already consumed from a runtime
// counter source but could not be committed to the database. Runtime collectors
// such as Mieru, MTProto and TUIC advance their own cursors when sampled, so
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

// appendBatch adds another already-consumed runtime snapshot to a batch that
// may itself still be waiting for persistence. This is used when a collector
// must stop immediately (for example on a core switch) before a previous DB
// retry has succeeded.
func (b *pendingTrafficBatch) appendBatch(inbounds []*xray.Traffic, clients []*xray.ClientTraffic) {
	b.inbounds = append(b.inbounds, inbounds...)
	b.clients = append(b.clients, clients...)
}

// flush retries the exact batch before a collector is sampled again. On
// success it clears the batch; on failure it leaves the slices intact so the
// next poll can retry without double-consuming the runtime counters.
func (b *pendingTrafficBatch) flush(commit trafficCommitFunc) (bool, bool, error) {
	if !b.hasData() {
		return false, false, nil
	}
	needRestart, clientsDisabled, err := commit(b.inbounds, b.clients)
	if err != nil {
		return needRestart, clientsDisabled, err
	}
	b.inbounds = nil
	b.clients = nil
	return needRestart, clientsDisabled, nil
}
