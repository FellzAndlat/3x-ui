package service

import (
	"context"
	"errors"
	"sync"

	"github.com/SawaMEN/3x-ui/v3/internal/singbox"
)

var (
	singBoxSessionAPI = singbox.NewConnectionAPIClient()
	singBoxSessionMu  sync.Mutex
)

func (s *SingBoxService) ensureSessionAPI() error {
	if !singBoxProcess.SupportsNativeAPI() {
		return errors.New("sing-box native connection API requires sing-box 1.14 or newer")
	}
	if !s.IsRunning() {
		return errors.New("sing-box is not running")
	}
	return nil
}

// ActiveSessions returns the current sing-box connection snapshot without
// sharing state with the traffic accounting poller.
func (s *SingBoxService) ActiveSessions(ctx context.Context) ([]singbox.ActiveSession, error) {
	if err := s.ensureSessionAPI(); err != nil {
		return nil, err
	}
	singBoxSessionMu.Lock()
	defer singBoxSessionMu.Unlock()
	return singBoxSessionAPI.ActiveSessions(ctx)
}

// DisconnectUserSessions closes every currently tracked connection belonging
// to user. inbound can be empty to match the user across all inbounds.
func (s *SingBoxService) DisconnectUserSessions(ctx context.Context, inbound, user string) (int, error) {
	if user == "" {
		return 0, errors.New("user is required")
	}
	if err := s.ensureSessionAPI(); err != nil {
		return 0, err
	}
	singBoxSessionMu.Lock()
	defer singBoxSessionMu.Unlock()
	return singBoxSessionAPI.DisconnectUser(ctx, inbound, user)
}

// DisconnectInboundSessions closes every currently tracked connection for an
// inbound tag without changing that inbound's configuration.
func (s *SingBoxService) DisconnectInboundSessions(ctx context.Context, inbound string) (int, error) {
	if inbound == "" {
		return 0, errors.New("inbound is required")
	}
	if err := s.ensureSessionAPI(); err != nil {
		return 0, err
	}
	singBoxSessionMu.Lock()
	defer singBoxSessionMu.Unlock()
	return singBoxSessionAPI.DisconnectInbound(ctx, inbound)
}
