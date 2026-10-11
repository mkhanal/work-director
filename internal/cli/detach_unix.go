//go:build !windows

package cli

import "syscall"

// detached starts a process in a session of its own, so it outlives the
// terminal or app that started it.
func detached() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setsid: true} }
