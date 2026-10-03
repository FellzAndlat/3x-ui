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

func isActiveConnection(connection *singBoxConnection) bool {
	return connection != nil && connection.ID != "" && connection.ClosedAt == 0
}

// ActiveSessions returns a snapshot without retaining connection state between calls.
func (c *ConnectionAPIClient) ActiveSessions(ctx context.Context) ([]ActiveSession, error) {
	connections, err := c.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	sessions := make([]ActiveSession, 0, len(connections))
	for _, connection := range connections {
		// sing-box keeps recently closed connections in the initial reset
		// snapshot and marks them with ClosedAt. They are history, not live
		// sessions, even when the enclosing event type is NEW.
		if !isActiveConnection(connection) {
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
	return c.DisconnectUsers(ctx, inbound, []string{user})
}

// DisconnectUsers closes all currently visible connections for the supplied
// authenticated users using one snapshot. An empty inbound matches across all
// inbounds; empty user names are ignored.
func (c *ConnectionAPIClient) DisconnectUsers(ctx context.Context, inbound string, users []string) (int, error) {
	wanted := make(map[string]struct{}, len(users))
	for _, user := range users {
		if user != "" {
			wanted[user] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return 0, nil
	}
	return c.disconnectMatching(ctx, func(connection *singBoxConnection) bool {
		if inbound != "" && connection.Inbound != inbound {
			return false
		}
		_, ok := wanted[connection.User]
		return ok
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
		if !isActiveConnection(connection) || !match(connection) {
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
