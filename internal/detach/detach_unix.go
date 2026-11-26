//go:build unix

package detach

import (
	"os/exec"
	"syscall"
	"testing"
)

// Start runs exe with args in its own session: an agent that ends the
// hook's process group when the hook returns doesn't end it. Without Stdin,
// Stdout and Stderr it gets /dev/null, so it holds none of the hook's pipes
// and the agent doesn't wait for it. It returns at once.
func Start(exe string, args []string) error {
	// A test binary would run its tests instead.
	if testing.Testing() {
		return nil
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
