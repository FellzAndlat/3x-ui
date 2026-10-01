package singbox

import (
	"context"
	"errors"
	"fmt"
)

// ActiveSession is the stable panel-facing view of a live sing-box connection.
type ActiveSession struct {
	ID          string   `json:"id"`
	Core        string   `json:"core"`
	Inbound     string   `json:"inbound"`
	InboundType string   `json:"inboundType,omitempty"`
	User        string   `json:"user,omitempty"`
	Outbound    string   `json:"outbound,omitempty"`
	Network     string   `json:"network,omitempty"`
	Source      string   `json:"source,omitempty"`
	Destination string   `json:"destination,omitempty"`
	Domain      string   `json:"domain,omitempty"`
	Rule        string   `json:"rule,omitempty"`
	CreatedAt   int64    `json:"createdAt,omitempty"`
	Upload      int64    `json:"upload"`
	Download    int64    `json:"download"`
	Chain       []string `json:"chain,omitempty"`
}

func activeSessionFromConnection(connection *singBoxConnection) ActiveSession {
	if connection == nil {
		return ActiveSession{Core: "sing-box"}
	}
	return ActiveSession{
		ID:          connection.ID,
		Core:        "sing-box",
		Inbound:     connection.Inbound,
		InboundType: connection.InboundType,
		User:        connection.User,
		Outbound:    connection.Outbound,
		Network:     connection.Network,
		Source:      connection.Source,
		Destination: connection.Destination,
		Domain:      connection.Domain,
		Rule:        connection.Rule,
		CreatedAt:   connection.CreatedAt,
		Upload:      connection.UplinkTotal,
		Download:    connection.DownlinkTotal,
		Chain:       append([]string(nil), connection.Chain...),
	}
}

// ActiveSessions returns a snapshot without retaining connection state between calls.
func (c *ConnectionAPIClient) ActiveSessions(ctx context.Context) ([]ActiveSession, error) {
	connections, err := c.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	sessions := make([]ActiveSession, 0, len(connections))
	for _, connection := range connections {
		if connection == nil || connection.ID == "" {
			continue
		}
		sessions = append(sessions, activeSessionFromConnection(connection))
	}
	return sessions, nil
}

// DisconnectUser closes all currently visible connections for one authenticated user.
// An empty inbound matches the user across all inbounds.
func (c *ConnectionAPIClient) DisconnectUser(ctx context.Context, inbound, user string) (int, error) {
	if user == "" {
		return 0, nil
	}
	return c.disconnectMatching(ctx, func(connection *singBoxConnection) bool {
		return connection.User == user && (inbound == "" || connection.Inbound == inbound)
	})
}

// DisconnectInbound closes all currently visible connections for an inbound tag.
func (c *ConnectionAPIClient) DisconnectInbound(ctx context.Context, inbound string) (int, error) {
	if inbound == "" {
		return 0, nil
	}
	return c.disconnectMatching(ctx, func(connection *singBoxConnection) bool {
		return connection.Inbound == inbound
	})
}

func matchingConnectionIDs(connections []*singBoxConnection, match func(*singBoxConnection) bool) []string {
	ids := make([]string, 0, len(connections))
	seen := make(map[string]struct{}, len(connections))
	for _, connection := range connections {
		if connection == nil || connection.ID == "" || !match(connection) {
			continue
		}
		if _, exists := seen[connection.ID]; exists {
			continue
		}
		seen[connection.ID] = struct{}{}
		ids = append(ids, connection.ID)
	}
	return ids
}

func (c *ConnectionAPIClient) disconnectMatching(ctx context.Context, match func(*singBoxConnection) bool) (int, error) {
	connections, err := c.Snapshot(ctx)
	if err != nil {
		return 0, err
	}

	closed := 0
	var closeErr error
	for _, id := range matchingConnectionIDs(connections, match) {
		if err := c.CloseConnection(ctx, id); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("close sing-box connection %s: %w", id, err))
			continue
		}
		closed++
	}
	return closed, closeErr
}
