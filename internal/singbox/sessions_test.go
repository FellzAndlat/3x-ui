package singbox

import "testing"

func TestActiveSessionFromConnection(t *testing.T) {
	connection := &singBoxConnection{
		ID:            "4dc31f30-2f17-4896-a9f9-8318c51d2b66",
		Inbound:       "vless-443",
		InboundType:   "vless",
		User:          "alice",
		Outbound:      "direct",
		Network:       "tcp",
		Source:        "192.0.2.1:12345",
		Destination:   "198.51.100.2:443",
		Domain:        "example.com",
		Rule:          "domain_suffix=example.com",
		CreatedAt:     123456789,
		UplinkTotal:   321,
		DownlinkTotal: 654,
		Chain:         []string{"direct"},
	}

	session := activeSessionFromConnection(connection)
	if session.ID != connection.ID || session.Core != "sing-box" {
		t.Fatalf("unexpected identity: %+v", session)
	}
	if session.Inbound != connection.Inbound || session.User != connection.User || session.Outbound != connection.Outbound {
		t.Fatalf("unexpected routing identity: %+v", session)
	}
	if session.Source != connection.Source || session.Destination != connection.Destination || session.Domain != connection.Domain {
		t.Fatalf("unexpected endpoints: %+v", session)
	}
	if session.Upload != connection.UplinkTotal || session.Download != connection.DownlinkTotal {
		t.Fatalf("unexpected traffic totals: %+v", session)
	}
	if len(session.Chain) != 1 || session.Chain[0] != "direct" {
		t.Fatalf("unexpected chain: %+v", session.Chain)
	}

	// The exported snapshot must not alias mutable decoder-owned slices.
	session.Chain[0] = "changed"
	if connection.Chain[0] != "direct" {
		t.Fatal("ActiveSession.Chain aliases the source connection")
	}
}

func TestActiveSessionFromNilConnection(t *testing.T) {
	session := activeSessionFromConnection(nil)
	if session.Core != "sing-box" {
		t.Fatalf("core = %q, want sing-box", session.Core)
	}
}
