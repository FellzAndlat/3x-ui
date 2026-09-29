package singbox

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestDecodeTrafficConnectionKeepsSnapshotTotals(t *testing.T) {
	var connection []byte
	connection = protowire.AppendTag(connection, 1, protowire.BytesType)
	connection = protowire.AppendString(connection, "conn-traffic")
	connection = protowire.AppendTag(connection, 2, protowire.BytesType)
	connection = protowire.AppendString(connection, "inbound-tuic")
	connection = protowire.AppendTag(connection, 10, protowire.BytesType)
	connection = protowire.AppendString(connection, "user@example")
	connection = protowire.AppendTag(connection, 13, protowire.VarintType)
	connection = protowire.AppendVarint(connection, 123456)
	connection = protowire.AppendTag(connection, 16, protowire.VarintType)
	connection = protowire.AppendVarint(connection, 700)
	connection = protowire.AppendTag(connection, 17, protowire.VarintType)
	connection = protowire.AppendVarint(connection, 900)

	var eventData []byte
	eventData = protowire.AppendTag(eventData, 1, protowire.VarintType)
	eventData = protowire.AppendVarint(eventData, ConnectionEventOpened)
	eventData = protowire.AppendTag(eventData, 2, protowire.BytesType)
	eventData = protowire.AppendString(eventData, "conn-traffic")
	eventData = protowire.AppendTag(eventData, 3, protowire.BytesType)
	eventData = protowire.AppendBytes(eventData, connection)

	got, err := decodeConnectionEvent(eventData, true)
	if err != nil {
		t.Fatalf("decodeConnectionEvent() error = %v", err)
	}
	if got.ID != "conn-traffic" {
		t.Fatalf("traffic-only event ID = %q, want conn-traffic", got.ID)
	}
	if got.Connection == nil {
		t.Fatal("traffic-only event lost connection snapshot")
	}
	if got.Connection.Inbound != "inbound-tuic" || got.Connection.User != "user@example" {
		t.Fatalf("unexpected traffic identity: %+v", got.Connection)
	}
	if got.Connection.ClosedAt != 123456 || got.Connection.UplinkTotal != 700 || got.Connection.DownlinkTotal != 900 {
		t.Fatalf("unexpected traffic totals: %+v", got.Connection)
	}
}

func TestNormalizeTrafficSnapshotUsesCumulativeTotals(t *testing.T) {
	client := NewConnectionAPIClient()

	first := connectionEvents{Reset: true, Events: []*connectionEvent{{
		Type: ConnectionEventOpened,
		ID:   "conn-1",
		Connection: &singBoxConnection{
			Inbound:       "inbound-1",
			User:          "alice@example",
			UplinkTotal:   100,
			DownlinkTotal: 200,
		},
	}}}
	client.normalizeTrafficSnapshot(&first)
	if first.Events[0].UplinkDelta != 100 || first.Events[0].DownlinkDelta != 200 {
		t.Fatalf("first snapshot delta = %d/%d, want 100/200", first.Events[0].UplinkDelta, first.Events[0].DownlinkDelta)
	}

	second := connectionEvents{Reset: true, Events: []*connectionEvent{{
		Type: ConnectionEventOpened,
		ID:   "conn-1",
		Connection: &singBoxConnection{
			Inbound:       "inbound-1",
			User:          "alice@example",
			UplinkTotal:   145,
			DownlinkTotal: 260,
		},
	}}}
	client.normalizeTrafficSnapshot(&second)
	if second.Events[0].UplinkDelta != 45 || second.Events[0].DownlinkDelta != 60 {
		t.Fatalf("second snapshot delta = %d/%d, want 45/60", second.Events[0].UplinkDelta, second.Events[0].DownlinkDelta)
	}
}

func TestNormalizeTrafficSnapshotPreservesMissingBaseline(t *testing.T) {
	client := NewConnectionAPIClient()
	first := connectionEvents{Reset: true, Events: []*connectionEvent{{
		ID: "conn-1",
		Connection: &singBoxConnection{
			UplinkTotal:   100,
			DownlinkTotal: 200,
		},
	}}}
	client.normalizeTrafficSnapshot(&first)
	client.normalizeTrafficSnapshot(&connectionEvents{Reset: true})

	returned := connectionEvents{Reset: true, Events: []*connectionEvent{{
		ID: "conn-1",
		Connection: &singBoxConnection{
			UplinkTotal:   130,
			DownlinkTotal: 240,
		},
	}}}
	client.normalizeTrafficSnapshot(&returned)
	if returned.Events[0].UplinkDelta != 30 || returned.Events[0].DownlinkDelta != 40 {
		t.Fatalf("returned snapshot delta = %d/%d, want 30/40", returned.Events[0].UplinkDelta, returned.Events[0].DownlinkDelta)
	}
}

func TestNormalizeTrafficSnapshotHandlesCounterResetAndClosedConnection(t *testing.T) {
	client := NewConnectionAPIClient()
	first := connectionEvents{Reset: true, Events: []*connectionEvent{{
		ID: "conn-1",
		Connection: &singBoxConnection{
			UplinkTotal:   500,
			DownlinkTotal: 800,
		},
	}}}
	client.normalizeTrafficSnapshot(&first)

	reset := connectionEvents{Reset: true, Events: []*connectionEvent{{
		Type: ConnectionEventOpened,
		ID:   "conn-1",
		Connection: &singBoxConnection{
			ClosedAt:      987654,
			UplinkTotal:   25,
			DownlinkTotal: 35,
		},
	}}}
	client.normalizeTrafficSnapshot(&reset)
	if reset.Events[0].UplinkDelta != 25 || reset.Events[0].DownlinkDelta != 35 {
		t.Fatalf("reset snapshot delta = %d/%d, want 25/35", reset.Events[0].UplinkDelta, reset.Events[0].DownlinkDelta)
	}
	if reset.Events[0].Type != ConnectionEventClosed || reset.Events[0].ClosedAt != 987654 {
		t.Fatalf("closed snapshot not normalized: %+v", reset.Events[0])
	}

	repeated := connectionEvents{Reset: true, Events: []*connectionEvent{{
		Type: ConnectionEventOpened,
		ID:   "conn-1",
		Connection: &singBoxConnection{
			ClosedAt:      987654,
			UplinkTotal:   25,
			DownlinkTotal: 35,
		},
	}}}
	client.normalizeTrafficSnapshot(&repeated)
	if repeated.Events[0].UplinkDelta != 0 || repeated.Events[0].DownlinkDelta != 0 {
		t.Fatalf("repeated closed snapshot was counted twice: %d/%d", repeated.Events[0].UplinkDelta, repeated.Events[0].DownlinkDelta)
	}
}
