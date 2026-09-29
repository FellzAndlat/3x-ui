package singbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/types/known/emptypb"
)

const singBoxAPIAddress = "127.0.0.1:10091"

const (
	ConnectionEventOpened = 1
	ConnectionEventClosed = 2
)

const trafficSnapshotMissingGrace = 12

type (
	connectionSubscribeRequest struct{ Interval int64 }
	closeConnectionRequest     struct{ ID string }
)

type singBoxConnection struct {
	ID            string
	Inbound       string
	InboundType   string
	IPVersion     int32
	Network       string
	Source        string
	Destination   string
	Domain        string
	Protocol      string
	User          string
	FromOutbound  string
	CreatedAt     int64
	ClosedAt      int64
	Uplink        int64
	Downlink      int64
	UplinkTotal   int64
	DownlinkTotal int64
	Rule          string
	Outbound      string
	OutboundType  string
	Chain         []string
}

type connectionEvent struct {
	Type          int32
	ID            string
	Connection    *singBoxConnection
	UplinkDelta   int64
	DownlinkDelta int64
	ClosedAt      int64
}

type (
	connectionEvents struct {
		Events []*connectionEvent
		Reset  bool
	}
	connectionAPIProtoCodec struct{ trafficOnly bool }
)

func (connectionAPIProtoCodec) Name() string { return "singbox-api" }

func (connectionAPIProtoCodec) Marshal(v any) ([]byte, error) {
	var out []byte
	switch req := v.(type) {
	case *connectionSubscribeRequest:
		if req.Interval != 0 {
			out = protowire.AppendTag(out, 1, protowire.VarintType)
			out = protowire.AppendVarint(out, uint64(req.Interval))
		}
	case *closeConnectionRequest:
		out = protowire.AppendTag(out, 1, protowire.BytesType)
		out = protowire.AppendString(out, req.ID)
	default:
		return nil, fmt.Errorf("unsupported sing-box API request type %T", v)
	}
	return out, nil
}

func (c connectionAPIProtoCodec) Unmarshal(data []byte, v any) error {
	if _, empty := v.(*emptypb.Empty); empty {
		return nil
	}
	resp, ok := v.(*connectionEvents)
	if !ok {
		return fmt.Errorf("unsupported sing-box API response type %T", v)
	}
	resp.Events = resp.Events[:0]
	resp.Reset = false
	for len(data) > 0 {
		field, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]
		switch field {
		case 1:
			if typ != protowire.BytesType {
				return fmt.Errorf("invalid ConnectionEvents.events wire type %d", typ)
			}
			payload, used := protowire.ConsumeBytes(data)
			if used < 0 {
				return protowire.ParseError(used)
			}
			event, err := decodeConnectionEvent(payload, c.trafficOnly)
			if err != nil {
				return err
			}
			resp.Events = append(resp.Events, event)
			data = data[used:]
		case 2:
			if typ != protowire.VarintType {
				return fmt.Errorf("invalid ConnectionEvents.reset wire type %d", typ)
			}
			value, used := protowire.ConsumeVarint(data)
			if used < 0 {
				return protowire.ParseError(used)
			}
			resp.Reset = value != 0
			data = data[used:]
		default:
			used := protowire.ConsumeFieldValue(field, typ, data)
			if used < 0 {
				return protowire.ParseError(used)
			}
			data = data[used:]
		}
	}
	return nil
}

func decodeConnectionEvent(data []byte, trafficOnly bool) (*connectionEvent, error) {
	event := &connectionEvent{}
	for len(data) > 0 {
		field, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		data = data[n:]
		switch field {
		case 1:
			if typ != protowire.VarintType {
				return nil, fmt.Errorf("invalid ConnectionEvent.type wire type %d", typ)
			}
			value, used := protowire.ConsumeVarint(data)
			if used < 0 {
				return nil, protowire.ParseError(used)
			}
			event.Type = int32(value)
			data = data[used:]
		case 2:
			if typ != protowire.BytesType {
				return nil, fmt.Errorf("invalid ConnectionEvent.id wire type %d", typ)
			}
			value, used := protowire.ConsumeBytes(data)
			if used < 0 {
				return nil, protowire.ParseError(used)
			}
			event.ID = string(value)
			data = data[used:]
		case 3:
			if typ != protowire.BytesType {
				return nil, fmt.Errorf("invalid ConnectionEvent.connection wire type %d", typ)
			}
			value, used := protowire.ConsumeBytes(data)
			if used < 0 {
				return nil, protowire.ParseError(used)
			}
			connection, err := decodeConnection(value, trafficOnly)
			if err != nil {
				return nil, err
			}
			event.Connection = connection
			data = data[used:]
		case 4, 5, 6:
			if typ != protowire.VarintType {
				return nil, fmt.Errorf("invalid ConnectionEvent numeric field %d wire type %d", field, typ)
			}
			value, used := protowire.ConsumeVarint(data)
			if used < 0 {
				return nil, protowire.ParseError(used)
			}
			switch field {
			case 4:
				event.UplinkDelta = int64(value)
			case 5:
				event.DownlinkDelta = int64(value)
			case 6:
				event.ClosedAt = int64(value)
			}
			data = data[used:]
		default:
			used := protowire.ConsumeFieldValue(field, typ, data)
			if used < 0 {
				return nil, protowire.ParseError(used)
			}
			data = data[used:]
		}
	}
	return event, nil
}

func decodeTrafficConnection(data []byte) (*singBoxConnection, error) {
	connection := &singBoxConnection{}
	for len(data) > 0 {
		field, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		data = data[n:]
		switch field {
		case 1, 2, 10:
			if typ != protowire.BytesType {
				return nil, fmt.Errorf("invalid Connection traffic string field %d wire type %d", field, typ)
			}
			value, used := protowire.ConsumeBytes(data)
			if used < 0 {
				return nil, protowire.ParseError(used)
			}
			switch field {
			case 1:
				connection.ID = string(value)
			case 2:
				connection.Inbound = string(value)
			case 10:
				connection.User = string(value)
			}
			data = data[used:]
		case 12, 13, 16, 17:
			if typ != protowire.VarintType {
				return nil, fmt.Errorf("invalid Connection traffic numeric field %d wire type %d", field, typ)
			}
			value, used := protowire.ConsumeVarint(data)
			if used < 0 {
				return nil, protowire.ParseError(used)
			}
			switch field {
			case 12:
				connection.CreatedAt = int64(value)
			case 13:
				connection.ClosedAt = int64(value)
			case 16:
				connection.UplinkTotal = int64(value)
			case 17:
				connection.DownlinkTotal = int64(value)
			}
			data = data[used:]
		default:
			used := protowire.ConsumeFieldValue(field, typ, data)
			if used < 0 {
				return nil, protowire.ParseError(used)
			}
			data = data[used:]
		}
	}
	return connection, nil
}

func decodeConnection(data []byte, trafficOnly bool) (*singBoxConnection, error) {
	if trafficOnly {
		return decodeTrafficConnection(data)
	}
	connection := &singBoxConnection{}
	for len(data) > 0 {
		field, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		data = data[n:]
		switch field {
		case 1, 2, 3, 5, 6, 7, 8, 9, 10, 11, 18, 19, 20, 21:
			if typ != protowire.BytesType {
				return nil, fmt.Errorf("invalid Connection string field %d wire type %d", field, typ)
			}
			value, used := protowire.ConsumeBytes(data)
			if used < 0 {
				return nil, protowire.ParseError(used)
			}
			text := string(value)
			switch field {
			case 1:
				connection.ID = text
			case 2:
				connection.Inbound = text
			case 3:
				connection.InboundType = text
			case 5:
				connection.Network = text
			case 6:
				connection.Source = text
			case 7:
				connection.Destination = text
			case 8:
				connection.Domain = text
			case 9:
				connection.Protocol = text
			case 10:
				connection.User = text
			case 11:
				connection.FromOutbound = text
			case 18:
				connection.Rule = text
			case 19:
				connection.Outbound = text
			case 20:
				connection.OutboundType = text
			case 21:
				connection.Chain = append(connection.Chain, text)
			}
			data = data[used:]
		case 4:
			if typ != protowire.VarintType {
				return nil, fmt.Errorf("invalid Connection.ipVersion wire type %d", typ)
			}
			value, used := protowire.ConsumeVarint(data)
			if used < 0 {
				return nil, protowire.ParseError(used)
			}
			connection.IPVersion = int32(value)
			data = data[used:]
		case 12, 13, 14, 15, 16, 17:
			if typ != protowire.VarintType {
				return nil, fmt.Errorf("invalid Connection numeric field %d wire type %d", field, typ)
			}
			value, used := protowire.ConsumeVarint(data)
			if used < 0 {
				return nil, protowire.ParseError(used)
			}
			switch field {
			case 12:
				connection.CreatedAt = int64(value)
			case 13:
				connection.ClosedAt = int64(value)
			case 14:
				connection.Uplink = int64(value)
			case 15:
				connection.Downlink = int64(value)
			case 16:
				connection.UplinkTotal = int64(value)
			case 17:
				connection.DownlinkTotal = int64(value)
			}
			data = data[used:]
		case 22:
			if typ != protowire.BytesType {
				return nil, fmt.Errorf("invalid Connection.processInfo wire type %d", typ)
			}
			_, used := protowire.ConsumeBytes(data)
			if used < 0 {
				return nil, protowire.ParseError(used)
			}
			data = data[used:]
		default:
			used := protowire.ConsumeFieldValue(field, typ, data)
			if used < 0 {
				return nil, protowire.ParseError(used)
			}
			data = data[used:]
		}
	}
	return connection, nil
}

type connectionTrafficSnapshot struct {
	uplink           int64
	downlink         int64
	missingSnapshots uint8
}

func snapshotTrafficDelta(current, previous int64, exists, countInitial bool) int64 {
	if current <= 0 {
		return 0
	}
	if !exists {
		if countInitial {
			return current
		}
		return 0
	}
	if current >= previous {
		return current - previous
	}
	// The core restarted/reset this connection counter. Count only the bytes
	// visible after the reset instead of subtracting into a negative delta.
	return current
}

func (c *ConnectionAPIClient) normalizeTrafficSnapshot(response *connectionEvents, snapshotAt int64) {
	if response == nil || !response.Reset {
		return
	}

	c.trafficMu.Lock()
	defer c.trafficMu.Unlock()

	if c.trafficSnapshots == nil {
		c.trafficSnapshots = make(map[string]connectionTrafficSnapshot)
	}
	previousAt := c.trafficSnapshotAt
	seen := make(map[string]struct{}, len(response.Events))

	for _, event := range response.Events {
		if event == nil || event.Connection == nil {
			continue
		}
		connection := event.Connection
		id := event.ID
		if id == "" {
			id = connection.ID
		}
		if id == "" {
			continue
		}
		if event.ID == "" {
			event.ID = id
		}
		seen[id] = struct{}{}

		currentUp := connection.UplinkTotal
		currentDown := connection.DownlinkTotal
		if currentUp < 0 {
			currentUp = 0
		}
		if currentDown < 0 {
			currentDown = 0
		}

		previous, exists := c.trafficSnapshots[id]
		// The first reset snapshot establishes a baseline: its cumulative totals
		// can already be present in client_traffics when the panel reconnects to
		// a sing-box process that survived a panel restart. Later unknown IDs are
		// counted only when sing-box says the connection was created after the
		// previous snapshot boundary.
		countInitial := previousAt > 0 && connection.CreatedAt > 0 && connection.CreatedAt >= previousAt
		event.UplinkDelta = snapshotTrafficDelta(currentUp, previous.uplink, exists, countInitial)
		event.DownlinkDelta = snapshotTrafficDelta(currentDown, previous.downlink, exists, countInitial)

		c.trafficSnapshots[id] = connectionTrafficSnapshot{
			uplink:   currentUp,
			downlink: currentDown,
		}

		// sing-box's reset snapshot retains recently closed connections as NEW
		// events. Normalize them so PollTraffic does not mark stale users online;
		// their final byte delta is still committed above.
		if connection.ClosedAt > 0 {
			event.Type = ConnectionEventClosed
			if event.ClosedAt == 0 {
				event.ClosedAt = connection.ClosedAt
			}
		}
	}

	// A transiently missing connection must not lose its baseline or its next
	// appearance would be treated as a new connection. Retain a short grace
	// window, then evict stale IDs to bound memory.
	for id, snapshot := range c.trafficSnapshots {
		if _, ok := seen[id]; ok {
			continue
		}
		if snapshot.missingSnapshots+1 >= trafficSnapshotMissingGrace {
			delete(c.trafficSnapshots, id)
			continue
		}
		snapshot.missingSnapshots++
		c.trafficSnapshots[id] = snapshot
	}
	c.trafficSnapshotAt = snapshotAt
}

// SnapshotTrafficEvents is a low-allocation variant used by the traffic poll.
// SubscribeConnections starts with Reset=true and cumulative connection totals.
// Convert that reset snapshot to deltas locally because this poll uses a
// short-lived stream and therefore never waits for the server's later update
// events.
func (c *ConnectionAPIClient) SnapshotTrafficEvents(ctx context.Context) (connectionEvents, error) {
	if err := c.connFor(ctx); err != nil {
		return connectionEvents{}, err
	}
	// Capture the boundary before subscribing. If a connection is created after
	// the server builds this snapshot but before the next poll, CreatedAt remains
	// on the correct side of the boundary and its initial bytes are counted once.
	snapshotAt := time.Now().UnixMilli()
	stream, err := c.conn.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, "/daemon.StartedService/SubscribeConnections", grpc.ForceCodec(connectionAPIProtoCodec{trafficOnly: true}))
	if err != nil {
		c.Close()
		return connectionEvents{}, err
	}
	if err := stream.SendMsg(&connectionSubscribeRequest{Interval: 0}); err != nil {
		c.Close()
		return connectionEvents{}, err
	}
	if err := stream.CloseSend(); err != nil {
		c.Close()
		return connectionEvents{}, err
	}
	var response connectionEvents
	if err := stream.RecvMsg(&response); err != nil && !errors.Is(err, io.EOF) {
		c.Close()
		return connectionEvents{}, err
	}
	c.normalizeTrafficSnapshot(&response, snapshotAt)
	return response, nil
}

type ConnectionAPIClient struct {
	conn              *grpc.ClientConn
	trafficMu         sync.Mutex
	trafficSnapshots  map[string]connectionTrafficSnapshot
	trafficSnapshotAt int64
}

func NewConnectionAPIClient() *ConnectionAPIClient { return &ConnectionAPIClient{} }
func (c *ConnectionAPIClient) Close() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

func (c *ConnectionAPIClient) connFor(ctx context.Context) error {
	if c.conn != nil {
		return nil
	}
	dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	//nolint:staticcheck // DialContext/WithBlock preserve bounded synchronous dial semantics.
	conn, err := grpc.DialContext(dialCtx, singBoxAPIAddress, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

func (c *ConnectionAPIClient) CloseConnection(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	if err := c.connFor(ctx); err != nil {
		return err
	}
	var response emptypb.Empty
	if err := c.conn.Invoke(ctx, "/daemon.StartedService/CloseConnection", &closeConnectionRequest{ID: id}, &response, grpc.ForceCodec(connectionAPIProtoCodec{})); err != nil {
		c.Close()
		return err
	}
	return nil
}

// SnapshotEvents opens a short-lived stream and returns the protobuf events
// without losing traffic deltas, reset markers, or closed events.
func (c *ConnectionAPIClient) SnapshotEvents(ctx context.Context) (connectionEvents, error) {
	if err := c.connFor(ctx); err != nil {
		return connectionEvents{}, err
	}
	stream, err := c.conn.NewStream(ctx, &grpc.StreamDesc{ServerStreams: true}, "/daemon.StartedService/SubscribeConnections", grpc.ForceCodec(connectionAPIProtoCodec{}))
	if err != nil {
		c.Close()
		return connectionEvents{}, err
	}
	if err := stream.SendMsg(&connectionSubscribeRequest{Interval: 0}); err != nil {
		c.Close()
		return connectionEvents{}, err
	}
	if err := stream.CloseSend(); err != nil {
		c.Close()
		return connectionEvents{}, err
	}
	var response connectionEvents
	if err := stream.RecvMsg(&response); err != nil && !errors.Is(err, io.EOF) {
		c.Close()
		return connectionEvents{}, err
	}
	return response, nil
}

// Snapshot keeps the historical API used by the dashboard callers while
// delegating to the event-aware implementation.
func (c *ConnectionAPIClient) Snapshot(ctx context.Context) ([]*singBoxConnection, error) {
	response, err := c.SnapshotEvents(ctx)
	if err != nil {
		return nil, err
	}
	latest := make(map[string]*singBoxConnection, len(response.Events))
	order := make([]string, 0, len(response.Events))
	for _, event := range response.Events {
		if event == nil || event.ID == "" {
			continue
		}
		if event.Type == ConnectionEventClosed {
			delete(latest, event.ID)
			continue
		}
		if event.Connection == nil {
			continue
		}
		if _, exists := latest[event.ID]; !exists {
			order = append(order, event.ID)
		}
		latest[event.ID] = event.Connection
	}
	connections := make([]*singBoxConnection, 0, len(latest))
	for _, id := range order {
		if connection := latest[id]; connection != nil {
			connections = append(connections, connection)
		}
	}
	return connections, nil
}
