package job

import (
	"sync"

	"github.com/SawaMEN/3x-ui/v3/internal/externalvpn"
	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"
	"github.com/SawaMEN/3x-ui/v3/internal/xray"
)

type ExternalVPNJob struct {
	inbounds service.InboundService
	mu       sync.Mutex
	pending  pendingTrafficBatch
}

func NewExternalVPNJob() *ExternalVPNJob { return &ExternalVPNJob{} }
func (j *ExternalVPNJob) Run() {
	// CollectTraffic advances the external VPN manager's cumulative-counter
	// baseline. Serialize the full poll so overlapping runs cannot consume a
	// newer snapshot while an older batch is still being committed.
	j.mu.Lock()
	defer j.mu.Unlock()

	desired, err := j.inbounds.DesiredExternalVPNInstances()
	if err != nil {
		logger.Warning("external VPN reconcile:", err)
		return
	}
	mgr := externalvpn.GetManager()
	mgr.Reconcile(desired)

	// A failed AddTraffic must be retried before sampling again. CollectTraffic
	// updates the manager baseline immediately, so reading a second snapshot first
	// would permanently lose the uncommitted delta.
	if j.pending.hasData() {
		_, disabled, retryErr := j.pending.flush(j.inbounds.AddTraffic)
		if retryErr != nil {
			logger.Warning("external VPN traffic retry:", retryErr)
			return
		}
		if disabled {
			if current, refreshErr := j.inbounds.DesiredExternalVPNInstances(); refreshErr == nil {
				mgr.Reconcile(current)
			}
		}
	}

	rows := mgr.CollectTraffic()
	inbound := map[string]*xray.Traffic{}
	clients := make([]*xray.ClientTraffic, 0, len(rows))
	activeEmails := []string{}
	activeTags := []string{}
	for _, row := range rows {
		if row.Active {
			activeEmails = append(activeEmails, row.Email)
			activeTags = append(activeTags, row.Tag)
		}
		if inbound[row.Tag] == nil {
			inbound[row.Tag] = &xray.Traffic{Tag: row.Tag, IsInbound: true}
		}
		inbound[row.Tag].Up += row.Up
		inbound[row.Tag].Down += row.Down
		clients = append(clients, &xray.ClientTraffic{Email: row.Email, Up: row.Up, Down: row.Down})
	}
	totals := make([]*xray.Traffic, 0, len(inbound))
	for _, t := range inbound {
		totals = append(totals, t)
	}
	if len(totals) > 0 || len(clients) > 0 {
		if _, disabled, err := j.inbounds.AddTraffic(totals, clients); err != nil {
			// CollectTraffic has already advanced its baseline. Preserve this exact
			// batch and retry it before collecting a newer snapshot.
			j.pending.remember(totals, clients)
			logger.Warning("external VPN traffic; batch queued for retry:", err)
		} else if disabled {
			if current, err := j.inbounds.DesiredExternalVPNInstances(); err == nil {
				mgr.Reconcile(current)
			}
		}
	}
	if len(activeEmails) > 0 {
		if err := j.inbounds.BumpClientsLastOnline(activeEmails); err != nil {
			logger.Warning("external VPN online clients:", err)
		}
	}
	j.inbounds.RefreshLocalOnlineClients(activeEmails, activeTags)
}
