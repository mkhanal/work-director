//go:build windows

package cli

import "syscall"

// detached starts a process in a group of its own, so it outlives the
// console or app that started it.
func detached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
