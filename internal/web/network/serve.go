package network

import (
	"errors"
	"net"
	"net/http"

	"github.com/SawaMEN/3x-ui/v3/internal/logger"
)

// ServeHTTP runs a panel HTTP server and records unexpected listener failures.
// Normal server/listener shutdown is intentionally silent.
func ServeHTTP(server *http.Server, listener net.Listener, name string) error {
	err := server.Serve(listener)
	if err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	logger.Error(name, " stopped unexpectedly: ", err)
	return err
}
