//go:build unix

package tsconformtest

import (
	"os/exec"
	"syscall"
)

// ownGroup makes the process of cmd the leader of a group of its own, so that killGroup ends it and every
// process it started.
func ownGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// killGroup kills the group of the process of cmd.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
