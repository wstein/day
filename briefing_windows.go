//go:build windows

package main

import "os/exec"

// ownGroup: no process groups on Windows; only stw itself is killed at the limit (day is built there, not released).
func ownGroup(c *exec.Cmd) bool { return false }
