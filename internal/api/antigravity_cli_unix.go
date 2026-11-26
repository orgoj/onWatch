//go:build !windows

package api

import (
	"os"
	"syscall"

	pty "github.com/aymanbagabas/go-pty"
)

// killAgyTree terminates the managed agy process and its language-server child.
// agy is launched in a new PTY session (setsid), so it leads its own process
// group; signalling the negative PID reaps the whole tree. We only ever pass a
// process this runner started, never a user's interactive agy.
func killAgyTree(p *os.Process) {
	if p == nil {
		return
	}
	if pgid, err := syscall.Getpgid(p.Pid); err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}
	_ = p.Kill()
}

// reapAgy waits on a killed agy so relaunches do not pile up zombies. Waiting
// on the PTY command (not just the PID) also releases os/exec's context
// watcher. It runs off the caller's lock in case the exit is not immediate.
func reapAgy(cmd *pty.Cmd) {
	go func() { _ = cmd.Wait() }()
}
