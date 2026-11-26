//go:build windows

package api

import (
	"os"
	"os/exec"
	"strconv"

	pty "github.com/aymanbagabas/go-pty"
)

// killAgyTree terminates the managed agy process and its child language server.
// On Windows there is no process-group signal, so taskkill /T reaps the tree.
// We only ever pass a process this runner started.
func killAgyTree(p *os.Process) {
	if p == nil {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(p.Pid)).Run()
	_ = p.Kill()
}

// reapAgy is a no-op on Windows: zombies are a Unix concept, and go-pty's
// Windows Wait is not safe to race with its context watcher.
func reapAgy(*pty.Cmd) {}
