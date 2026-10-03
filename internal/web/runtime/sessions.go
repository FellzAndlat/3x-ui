package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
)

// SessionRuntime is an optional runtime capability. It is intentionally kept
// out of Runtime so mixed-version nodes and existing runtime test doubles do
// not have to implement session management before they support it.
type SessionRuntime interface {
	ActiveSessions(ctx context.Context) ([]singbox.ActiveSession, error)
	DisconnectUserSessions(ctx context.Context, inbound, user string) (int, error)
	DisconnectUsersSessions(ctx context.Context, inbound string, users []string) (int, error)
	DisconnectInboundSessions(ctx context.Context, inbound string) (int, error)
}

// SessionRuntimeFor resolves the selected local/remote runtime and verifies
// that it supports the optional session capability.
func (m *Manager) SessionRuntimeFor(nodeID *int) (SessionRuntime, error) {
	rt, err := m.RuntimeFor(nodeID)
	if err != nil {
		return nil, err
	}
	sessions, ok := rt.(SessionRuntime)
	if !ok {
		return nil, fmt.Errorf("runtime %s does not support session management", rt.Name())
	}
	return sessions, nil
}

func (l *Local) ActiveSessions(ctx context.Context) ([]singbox.ActiveSession, error) {
	if !l.isSingBox() {
		return nil, errors.New("active sessions are currently available only for sing-box")
	}
	client := singbox.NewConnectionAPIClient()
	defer client.Close()
	return client.ActiveSessions(ctx)
}

func (l *Local) DisconnectUserSessions(ctx context.Context, inbound, user string) (int, error) {
	return l.DisconnectUsersSessions(ctx, inbound, []string{user})
}

func (l *Local) DisconnectUsersSessions(ctx context.Context, inbound string, users []string) (int, error) {
	if !l.isSingBox() {
		return 0, errors.New("session disconnect is currently available only for sing-box")
	}
	client := singbox.NewConnectionAPIClient()
	defer client.Close()
	return client.DisconnectUsers(ctx, inbound, users)
}

func (l *Local) DisconnectInboundSessions(ctx context.Context, inbound string) (int, error) {
	if !l.isSingBox() {
		return 0, errors.New("session disconnect is currently available only for sing-box")
	}
	client := singbox.NewConnectionAPIClient()
	defer client.Close()
	return client.DisconnectInbound(ctx, inbound)
}

func (r *Remote) ActiveSessions(ctx context.Context) ([]singbox.ActiveSession, error) {
	env, err := r.do(ctx, http.MethodGet, "panel/api/server/singbox/sessions", nil)
	if err != nil {
		return nil, err
	}
	var sessions []singbox.ActiveSession
	if len(env.Obj) == 0 {
		return sessions, nil
	}
	if err := json.Unmarshal(env.Obj, &sessions); err != nil {
		return nil, fmt.Errorf("decode active sessions: %w", err)
	}
	return sessions, nil
}

func (r *Remote) DisconnectUserSessions(ctx context.Context, inbound, user string) (int, error) {
	return r.DisconnectUsersSessions(ctx, inbound, []string{user})
}

func (r *Remote) DisconnectUsersSessions(ctx context.Context, inbound string, users []string) (int, error) {
	return r.disconnectSessions(ctx, "panel/api/server/singbox/sessions/disconnect-users", map[string]any{
		"inbound": inbound,
		"users":   users,
	})
}

func (r *Remote) DisconnectInboundSessions(ctx context.Context, inbound string) (int, error) {
	return r.disconnectSessions(ctx, "panel/api/server/singbox/sessions/disconnect-inbound", map[string]string{
		"inbound": inbound,
	})
}

func (r *Remote) disconnectSessions(ctx context.Context, path string, body any) (int, error) {
	env, err := r.do(ctx, http.MethodPost, path, body)
	if err != nil {
		return 0, err
	}
	var result struct {
		Closed int `json:"closed"`
	}
	if len(env.Obj) == 0 {
		return 0, nil
	}
	if err := json.Unmarshal(env.Obj, &result); err != nil {
		return 0, fmt.Errorf("decode disconnect result: %w", err)
	}
	return result.Closed, nil
}
