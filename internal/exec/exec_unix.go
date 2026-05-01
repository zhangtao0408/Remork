//go:build unix

package execx

import (
	"os"
	"os/exec"
	"syscall"
)

func configureCommandForCancellation(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return killCommandGroup(cmd)
	}
}

func cleanupCommandGroup(cmd *exec.Cmd) {
	_ = killCommandGroup(cmd)
}

func killCommandGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err == syscall.ESRCH {
		return os.ErrProcessDone
	}
	return err
}
