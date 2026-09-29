package job

import (
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/tuic"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

type TuicJob struct {
	inboundService service.InboundService
	mu             sync.Mutex
	pending        pendingTrafficBatch
}

func NewTuicJob() *TuicJob {
	return new(TuicJob)
}

func (j *TuicJob) Run() {
	// CollectTraffic advances each relay's counter baseline. Serialize the poll
	// so overlapping scheduler runs cannot consume a newer snapshot while an
	// earlier one is still being committed.
	j.mu.Lock()
	defer j.mu.Unlock()

	core, err := (&service.SettingService{}).GetCoreType()
	if err != nil {
		logger.Warning("tuic job: get selected core failed:", err)
		return
	}
	if core == service.CoreTypeSingBox {
		// Stop the sidecar immediately on a core switch even if a historical
		// traffic batch cannot be persisted yet. The batch remains retryable.
		tuic.GetManager().StopAll()
		if j.pending.hasData() {
			if _, _, retryErr := j.pending.flush(j.inboundService.AddTraffic); retryErr != nil {
				logger.Warning("tuic job: retry pending traffic after core switch failed:", retryErr)
			}
		}
		return
	}

	// Retry a batch that was already consumed from the TUIC relays before asking
	// them for another snapshot. Otherwise a transient DB failure permanently
	// loses the bytes because CollectTraffic has already advanced its baseline.
	if j.pending.hasData() {
		if _, _, retryErr := j.pending.flush(j.inboundService.AddTraffic); retryErr != nil {
			logger.Warning("tuic job: retry pending traffic failed:", retryErr)
			return
		}
	}

	desired, err := j.inboundService.DesiredTuicInstances()
	if err != nil {
		logger.Warning("tuic job: get desired instances failed:", err)
		return
	}

	activeTags := make([]string, 0, len(desired))
	for _, inst := range desired {
		activeTags = append(activeTags, inst.Tag)
	}

	mgr := tuic.GetManager()
	mgr.Reconcile(desired)

	deltas := mgr.CollectTraffic()
	onlineEmails, _ := mgr.GetActiveClients(30 * time.Second)

	inboundUp := make(map[string]int64)
	inboundDown := make(map[string]int64)
	for _, d := range deltas {
		inboundUp[d.Tag] += d.Up
		inboundDown[d.Tag] += d.Down
	}

	traffics := make([]*xray.Traffic, 0, len(inboundUp))
	for tag, up := range inboundUp {
		traffics = append(traffics, &xray.Traffic{
			IsInbound: true,
			Tag:       tag,
			Up:        up,
			Down:      inboundDown[tag],
		})
	}

	// Build zero-byte client traffic entries for active clients so adjustTraffics can
	// activate delayed-start expiryTime for TUIC clients without inflating traffic.
	clientTraffics := make([]*xray.ClientTraffic, 0, len(onlineEmails))
	for _, email := range onlineEmails {
		clientTraffics = append(clientTraffics, &xray.ClientTraffic{
			Email: email,
			Up:    0,
			Down:  0,
		})
	}

	if len(traffics) > 0 || len(clientTraffics) > 0 {
		needRestart, _, err := j.inboundService.AddTraffic(traffics, clientTraffics)
		if err != nil {
			// The relay baselines have already advanced. Keep the exact consumed
			// batch and retry it before collecting a newer TUIC snapshot.
			j.pending.remember(traffics, clientTraffics)
			logger.Warning("tuic job: add traffic failed; batch queued for retry:", err)
		} else if needRestart {
			if desired, err := j.inboundService.DesiredTuicInstances(); err == nil {
				mgr.Reconcile(desired)
			}
		}
	}

	if len(onlineEmails) > 0 {
		if err := j.inboundService.BumpClientsLastOnline(onlineEmails); err != nil {
			logger.Warning("tuic job: bump last online for tuic clients failed:", err)
		}
	}

	j.inboundService.RefreshLocalOnlineClients(onlineEmails, activeTags)
}
