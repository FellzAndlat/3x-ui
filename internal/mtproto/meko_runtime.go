package mtproto

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/logger"
)

const mekoHelperPath = "/usr/local/x-ui/telemt-meko-fix.sh"

var mekoSync struct {
	sync.Mutex
	timer *time.Timer
	run   sync.Mutex
}

// scheduleMekoSync coalesces rapid MTProto lifecycle changes (for example an
// inbound restart that stops the old Telemt process and immediately starts a
// new one). The helper discovers ports from the final generated TOML set and
// reads the panel-managed MEKO settings itself.
func scheduleMekoSync() {
	if _, err := os.Stat(mekoHelperPath); err != nil {
		return
	}
	mekoSync.Lock()
	defer mekoSync.Unlock()
	if mekoSync.timer != nil {
		mekoSync.timer.Stop()
	}
	mekoSync.timer = time.AfterFunc(300*time.Millisecond, applyMekoRules)
}

func applyMekoRules() {
	mekoSync.Lock()
	mekoSync.timer = nil
	mekoSync.Unlock()

	// iptables updates are transactional only per command. Never let two
	// sidecar lifecycle events rebuild the managed chains concurrently.
	mekoSync.run.Lock()
	defer mekoSync.run.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/bash", mekoHelperPath, "apply")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			logger.Warningf("mtproto: MEKO V3 reconcile timed out: %v", ctx.Err())
			return
		}
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		logger.Warningf("mtproto: MEKO V3 reconcile failed: %v: %s", err, string(out))
	}
}
