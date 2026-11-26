//go:build !windows

package api

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"

	pty "github.com/aymanbagabas/go-pty"
)

// A torn-down agy must be reaped, or every relaunch leaves a zombie behind.
func TestAgyRunner_KillSession_ReapsProcess(t *testing.T) {
	p, err := pty.New()
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}
	// CommandContext like the real launch, so os/exec runs a context watcher.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := p.CommandContext(ctx, "sleep", "30")
	if err := cmd.Start(); err != nil {
		_ = p.Close()
		t.Fatalf("start: %v", err)
	}
	pid := cmd.Process.Pid

	r := NewAntigravityCLIRunner(nil)
	defer r.Stop()
	r.killSession(&agySession{pty: p, cmd: cmd})

	// A zombie still answers signal 0; only a reaped process returns ESRCH.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pid %d was killed but never reaped (zombie)", pid)
}
