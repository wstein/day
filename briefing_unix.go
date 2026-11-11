//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// ownGroup puts the command in its own process group and makes cancelling kill the whole group (git children included).
func ownGroup(c *exec.Cmd) bool {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	return true
}
