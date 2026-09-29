package job

import (
	"sync"

	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/mieru"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

type MieruJob struct {
	inboundService service.InboundService
	mu             sync.Mutex
	pending        pendingTrafficBatch
}

func NewMieruJob() *MieruJob {
	return new(MieruJob)
}

func (j *MieruJob) Run() {
	// CollectTraffic advances Mieru's cumulative-counter cursors. Serialize the
	// whole poll so a slow database write cannot let another run consume a newer
	// snapshot before the previous delta has been acknowledged.
	j.mu.Lock()
	defer j.mu.Unlock()

	desired, err := j.inboundService.DesiredMieruInstances()
	if err != nil {
		logger.Warning("mieru job: get desired instances failed:", err)
		return
	}

	activeTags := make([]string, 0, len(desired))
	for _, inst := range desired {
		activeTags = append(activeTags, inst.Tag)
	}

	mgr := mieru.GetManager()
	mgr.Reconcile(desired)

	// A failed AddTraffic must be retried before asking the manager for another
	// delta snapshot; otherwise its cursor moves forward and those bytes are lost.
	if j.pending.hasData() {
		needRestart, _, retryErr := j.pending.flush(&j.inboundService)
		if retryErr != nil {
			logger.Warning("mieru job: retry pending traffic failed:", retryErr)
			return
		}
		if needRestart {
			if refreshed, refreshErr := j.inboundService.DesiredMieruInstances(); refreshErr == nil {
				desired = refreshed
				mgr.Reconcile(desired)
			}
		}
	}

	deltas, onlineEmails := mgr.CollectTraffic(desired)
	if len(deltas) > 0 || len(onlineEmails) > 0 {
		inboundByTag := make(map[string]*xray.Traffic)
		clientTraffic := make([]*xray.ClientTraffic, 0, len(deltas)+len(onlineEmails))
		seenClients := make(map[string]struct{}, len(deltas)+len(onlineEmails))

		for _, delta := range deltas {
			clientTraffic = append(clientTraffic, &xray.ClientTraffic{
				Email: delta.Email,
				Up:    delta.Up,
				Down:  delta.Down,
			})
			seenClients[delta.Email] = struct{}{}

			traffic := inboundByTag[delta.Tag]
			if traffic == nil {
				traffic = &xray.Traffic{
					IsInbound: true,
					Tag:       delta.Tag,
				}
				inboundByTag[delta.Tag] = traffic
			}
			traffic.Up += delta.Up
			traffic.Down += delta.Down
		}

		// Feed zero-byte activity for connected clients as well. This lets the
		// shared traffic layer convert delayed-start expiries on first use even
		// when the first poll observes no payload bytes.
		for _, email := range onlineEmails {
			if _, exists := seenClients[email]; exists {
				continue
			}
			clientTraffic = append(clientTraffic, &xray.ClientTraffic{
				Email: email,
				Up:    0,
				Down:  0,
			})
		}

		traffics := make([]*xray.Traffic, 0, len(inboundByTag))
		for _, traffic := range inboundByTag {
			traffics = append(traffics, traffic)
		}

		needRestart, _, trafficErr := j.inboundService.AddTraffic(traffics, clientTraffic)
		if trafficErr != nil {
			// CollectTraffic has already advanced its cursor. Keep this exact batch
			// and retry it before the next snapshot instead of silently losing it.
			j.pending.remember(traffics, clientTraffic)
			logger.Warning("mieru job: add traffic failed; batch queued for retry:", trafficErr)
		} else if needRestart {
			// Quota enforcement can disable Mieru users. Reconcile immediately
			// instead of waiting another poll for the sidecar to reload/remove them.
			if desired, refreshErr := j.inboundService.DesiredMieruInstances(); refreshErr == nil {
				mgr.Reconcile(desired)
			}
		}
	}

	if len(onlineEmails) > 0 {
		if err := j.inboundService.BumpClientsLastOnline(onlineEmails); err != nil {
			logger.Warning("mieru job: bump last online failed:", err)
		}
	}

	j.inboundService.RefreshLocalOnlineClients(onlineEmails, activeTags)
}
