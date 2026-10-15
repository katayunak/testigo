//go:build unix

package gocmd

import (
	"os/exec"
	"syscall"
)

// killGroupOnCancel puts go and everything it starts in one process group, so
// a deadline also kills the test binary go test is waiting on. Killing only the
// go process leaves the binary running.
func killGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
