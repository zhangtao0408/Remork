//go:build !unix

package execx

import "os/exec"

func configureCommandForCancellation(cmd *exec.Cmd) {
}

func cleanupCommandGroup(cmd *exec.Cmd) {
}
