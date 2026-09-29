package singbox

import "testing"

func TestNormalizeTrafficSnapshotDoesNotReplayStaleUnknownConnection(t *testing.T) {
	client := NewConnectionAPIClient()
	client.trafficSnapshotAt = 2_000

	response := connectionEvents{Reset: true, Events: []*connectionEvent{{
		Type: ConnectionEventOpened,
		ID:   "conn-stale",
		Connection: &singBoxConnection{
			ID:            "conn-stale",
			CreatedAt:     1_000,
			UplinkTotal:   500,
			DownlinkTotal: 800,
		},
	}}}
	client.normalizeTrafficSnapshot(&response, 3_000)

	if response.Events[0].UplinkDelta != 0 || response.Events[0].DownlinkDelta != 0 {
		t.Fatalf("stale unknown connection totals were replayed: %d/%d", response.Events[0].UplinkDelta, response.Events[0].DownlinkDelta)
	}
}

func TestNormalizeTrafficSnapshotCountsUnknownConnectionCreatedAfterBoundary(t *testing.T) {
	client := NewConnectionAPIClient()
	client.trafficSnapshotAt = 2_000

	response := connectionEvents{Reset: true, Events: []*connectionEvent{{
		Type: ConnectionEventOpened,
		ID:   "conn-new",
		Connection: &singBoxConnection{
			ID:            "conn-new",
			CreatedAt:     2_500,
			UplinkTotal:   11,
			DownlinkTotal: 22,
		},
	}}}
	client.normalizeTrafficSnapshot(&response, 3_000)

	if response.Events[0].UplinkDelta != 11 || response.Events[0].DownlinkDelta != 22 {
		t.Fatalf("new connection initial bytes were lost: %d/%d", response.Events[0].UplinkDelta, response.Events[0].DownlinkDelta)
	}
}
