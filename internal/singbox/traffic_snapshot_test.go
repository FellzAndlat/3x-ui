package singbox

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestDecodeTrafficConnectionKeepsSnapshotCounters(t *testing.T) {
	var connection []byte
	connection = protowire.AppendTag(connection, 1, protowire.BytesType)
	connection = protowire.AppendString(connection, "conn-traffic")
	connection = protowire.AppendTag(connection, 2, protowire.BytesType)
	connection = protowire.AppendString(connection, "in-tuic")
	connection = protowire.AppendTag(connection, 10, protowire.BytesType)
	connection = protowire.AppendString(connection, "user@example")
	connection = protowire.AppendTag(connection, 12, protowire.VarintType)
	connection = protowire.AppendVarint(connection, 1_000)
	connection = protowire.AppendTag(connection, 13, protowire.VarintType)
	connection = protowire.AppendVarint(connection, 2_000)
	connection = protowire.AppendTag(connection, 16, protowire.VarintType)
	connection = protowire.AppendVarint(connection, 12_345)
	connection = protowire.AppendTag(connection, 17, protowire.VarintType)
	connection = protowire.AppendVarint(connection, 67_890)

	var data []byte
	data = protowire.AppendTag(data, 1, protowire.VarintType)
	data = protowire.AppendVarint(data, ConnectionEventOpened)
	data = protowire.AppendTag(data, 2, protowire.BytesType)
	data = protowire.AppendString(data, "conn-traffic")
	data = protowire.AppendTag(data, 3, protowire.BytesType)
	data = protowire.AppendBytes(data, connection)

	got, err := decodeConnectionEvent(data, true)
	if err != nil {
		t.Fatalf("decodeConnectionEvent(trafficOnly) error = %v", err)
	}
	if got.ID != "conn-traffic" || got.Connection == nil {
		t.Fatalf("unexpected traffic event: %+v", got)
	}
	conn := got.Connection
	if conn.ID != "conn-traffic" || conn.Inbound != "in-tuic" || conn.User != "user@example" {
		t.Fatalf("unexpected traffic connection identity: %+v", conn)
	}
	if conn.CreatedAt != 1_000 || conn.ClosedAt != 2_000 || conn.UplinkTotal != 12_345 || conn.DownlinkTotal != 67_890 {
		t.Fatalf("unexpected traffic snapshot counters: %+v", conn)
	}
}

func TestNormalizeTrafficSnapshotDerivesDeltas(t *testing.T) {
	client := &ConnectionAPIClient{}

	first := connectionEvents{Reset: true, Events: []*connectionEvent{{
		Type: ConnectionEventOpened,
		ID:   "conn-1",
		Connection: &singBoxConnection{
			ID:            "conn-1",
			Inbound:       "in-vless",
			User:          "user@example",
			CreatedAt:     900,
			UplinkTotal:   100,
			DownlinkTotal: 200,
		},
	}}}
	client.normalizeTrafficSnapshot(&first, 1_000)
	if first.Events[0].UplinkDelta != 0 || first.Events[0].DownlinkDelta != 0 {
		t.Fatalf("first snapshot must establish baseline, got up=%d down=%d", first.Events[0].UplinkDelta, first.Events[0].DownlinkDelta)
	}

	second := connectionEvents{Reset: true, Events: []*connectionEvent{{
		Type: ConnectionEventOpened,
		ID:   "conn-1",
		Connection: &singBoxConnection{
			ID:            "conn-1",
			Inbound:       "in-vless",
			User:          "user@example",
			CreatedAt:     900,
			UplinkTotal:   160,
			DownlinkTotal: 275,
		},
	}}}
	client.normalizeTrafficSnapshot(&second, 2_000)
	if second.Events[0].UplinkDelta != 60 || second.Events[0].DownlinkDelta != 75 {
		t.Fatalf("unexpected incremental delta: up=%d down=%d", second.Events[0].UplinkDelta, second.Events[0].DownlinkDelta)
	}

	third := connectionEvents{Reset: true, Events: []*connectionEvent{
		{
			Type: ConnectionEventOpened,
			ID:   "conn-1",
			Connection: &singBoxConnection{
				ID:            "conn-1",
				Inbound:       "in-vless",
				User:          "user@example",
				CreatedAt:     900,
				UplinkTotal:   10,
				DownlinkTotal: 20,
			},
		},
		{
			Type: ConnectionEventOpened,
			ID:   "conn-2",
			Connection: &singBoxConnection{
				ID:            "conn-2",
				Inbound:       "in-tuic",
				User:          "tuic@example",
				CreatedAt:     2_500,
				UplinkTotal:   33,
				DownlinkTotal: 44,
			},
		},
	}}
	client.normalizeTrafficSnapshot(&third, 3_000)
	if third.Events[0].UplinkDelta != 10 || third.Events[0].DownlinkDelta != 20 {
		t.Fatalf("counter reset must count post-reset bytes, got up=%d down=%d", third.Events[0].UplinkDelta, third.Events[0].DownlinkDelta)
	}
	if third.Events[1].UplinkDelta != 33 || third.Events[1].DownlinkDelta != 44 {
		t.Fatalf("new connection must count bytes since previous snapshot, got up=%d down=%d", third.Events[1].UplinkDelta, third.Events[1].DownlinkDelta)
	}
}

func TestNormalizeTrafficSnapshotMarksRetainedClosedConnection(t *testing.T) {
	client := &ConnectionAPIClient{}
	client.trafficSnapshots = map[string]connectionTrafficSnapshot{
		"conn-closed": {uplink: 100, downlink: 200},
	}
	client.trafficSnapshotAt = 1_000

	response := connectionEvents{Reset: true, Events: []*connectionEvent{{
		Type: ConnectionEventOpened,
		ID:   "conn-closed",
		Connection: &singBoxConnection{
			ID:            "conn-closed",
			Inbound:       "in-tuic",
			User:          "user@example",
			CreatedAt:     500,
			ClosedAt:      1_500,
			UplinkTotal:   125,
			DownlinkTotal: 260,
		},
	}}}

	client.normalizeTrafficSnapshot(&response, 2_000)
	event := response.Events[0]
	if event.Type != ConnectionEventClosed || event.ClosedAt != 1_500 {
		t.Fatalf("retained closed connection was not normalized: %+v", event)
	}
	if event.UplinkDelta != 25 || event.DownlinkDelta != 60 {
		t.Fatalf("final closed-connection bytes were lost: up=%d down=%d", event.UplinkDelta, event.DownlinkDelta)
	}
}

func TestNormalizeTrafficSnapshotKeepsBaselineAcrossTemporaryAbsence(t *testing.T) {
	client := &ConnectionAPIClient{}
	first := connectionEvents{Reset: true, Events: []*connectionEvent{{
		Type: ConnectionEventOpened,
		ID:   "conn-gap",
		Connection: &singBoxConnection{
			ID:            "conn-gap",
			CreatedAt:     500,
			UplinkTotal:   100,
			DownlinkTotal: 200,
		},
	}}}
	client.normalizeTrafficSnapshot(&first, 1_000)

	empty := connectionEvents{Reset: true}
	client.normalizeTrafficSnapshot(&empty, 2_000)

	reappeared := connectionEvents{Reset: true, Events: []*connectionEvent{{
		Type: ConnectionEventOpened,
		ID:   "conn-gap",
		Connection: &singBoxConnection{
			ID:            "conn-gap",
			CreatedAt:     500,
			UplinkTotal:   150,
			DownlinkTotal: 260,
		},
	}}}
	client.normalizeTrafficSnapshot(&reappeared, 3_000)
	if reappeared.Events[0].UplinkDelta != 50 || reappeared.Events[0].DownlinkDelta != 60 {
		t.Fatalf("temporary absence lost traffic baseline: up=%d down=%d", reappeared.Events[0].UplinkDelta, reappeared.Events[0].DownlinkDelta)
	}
}

func TestNormalizeTrafficSnapshotTracksSameUserConnectionsIndependently(t *testing.T) {
	client := &ConnectionAPIClient{}
	first := connectionEvents{Reset: true, Events: []*connectionEvent{
		{
			ID: "conn-a",
			Connection: &singBoxConnection{
				ID:            "conn-a",
				Inbound:       "in-tuic",
				User:          "shared@example",
				CreatedAt:     500,
				UplinkTotal:   100,
				DownlinkTotal: 200,
			},
		},
		{
			ID: "conn-b",
			Connection: &singBoxConnection{
				ID:            "conn-b",
				Inbound:       "in-vless",
				User:          "shared@example",
				CreatedAt:     600,
				UplinkTotal:   300,
				DownlinkTotal: 400,
			},
		},
	}}
	client.normalizeTrafficSnapshot(&first, 1_000)

	second := connectionEvents{Reset: true, Events: []*connectionEvent{
		{
			ID: "conn-a",
			Connection: &singBoxConnection{
				ID:            "conn-a",
				Inbound:       "in-tuic",
				User:          "shared@example",
				CreatedAt:     500,
				UplinkTotal:   130,
				DownlinkTotal: 225,
			},
		},
		{
			ID: "conn-b",
			Connection: &singBoxConnection{
				ID:            "conn-b",
				Inbound:       "in-vless",
				User:          "shared@example",
				CreatedAt:     600,
				UplinkTotal:   307,
				DownlinkTotal: 450,
			},
		},
	}}
	client.normalizeTrafficSnapshot(&second, 2_000)

	if second.Events[0].UplinkDelta != 30 || second.Events[0].DownlinkDelta != 25 {
		t.Fatalf("conn-a delta = %d/%d, want 30/25", second.Events[0].UplinkDelta, second.Events[0].DownlinkDelta)
	}
	if second.Events[1].UplinkDelta != 7 || second.Events[1].DownlinkDelta != 50 {
		t.Fatalf("conn-b delta = %d/%d, want 7/50", second.Events[1].UplinkDelta, second.Events[1].DownlinkDelta)
	}
}

func TestNormalizeTrafficSnapshotResetsMissingGraceWhenConnectionReturns(t *testing.T) {
	client := &ConnectionAPIClient{}
	first := connectionEvents{Reset: true, Events: []*connectionEvent{{
		ID: "conn-gap",
		Connection: &singBoxConnection{
			ID:            "conn-gap",
			CreatedAt:     500,
			UplinkTotal:   100,
			DownlinkTotal: 100,
		},
	}}}
	client.normalizeTrafficSnapshot(&first, 1_000)

	snapshotAt := int64(2_000)
	for i := 0; i < trafficSnapshotMissingGrace-2; i++ {
		client.normalizeTrafficSnapshot(&connectionEvents{Reset: true}, snapshotAt)
		snapshotAt += 1_000
	}
	returned := connectionEvents{Reset: true, Events: []*connectionEvent{{
		ID: "conn-gap",
		Connection: &singBoxConnection{
			ID:            "conn-gap",
			CreatedAt:     500,
			UplinkTotal:   125,
			DownlinkTotal: 130,
		},
	}}}
	client.normalizeTrafficSnapshot(&returned, snapshotAt)
	if returned.Events[0].UplinkDelta != 25 || returned.Events[0].DownlinkDelta != 30 {
		t.Fatalf("first returned delta = %d/%d, want 25/30", returned.Events[0].UplinkDelta, returned.Events[0].DownlinkDelta)
	}

	snapshotAt += 1_000
	for i := 0; i < trafficSnapshotMissingGrace-2; i++ {
		client.normalizeTrafficSnapshot(&connectionEvents{Reset: true}, snapshotAt)
		snapshotAt += 1_000
	}
	returnedAgain := connectionEvents{Reset: true, Events: []*connectionEvent{{
		ID: "conn-gap",
		Connection: &singBoxConnection{
			ID:            "conn-gap",
			CreatedAt:     500,
			UplinkTotal:   140,
			DownlinkTotal: 150,
		},
	}}}
	client.normalizeTrafficSnapshot(&returnedAgain, snapshotAt)
	if returnedAgain.Events[0].UplinkDelta != 15 || returnedAgain.Events[0].DownlinkDelta != 20 {
		t.Fatalf("second returned delta = %d/%d, want 15/20", returnedAgain.Events[0].UplinkDelta, returnedAgain.Events[0].DownlinkDelta)
	}
}
