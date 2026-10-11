//go:build windows

package runner

import (
	"os"
	"syscall"
)

// stillActive is the exit code Windows reports for a process still running.
const stillActive = 259

// alive reports whether the process exists and has not exited.
func alive(pid int) bool {
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	return syscall.GetExitCodeProcess(h, &code) == nil && code == stillActive
}

// terminate ends the process; Windows has no polite signal to send first.
func terminate(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	if err := p.Kill(); err != nil && alive(pid) {
		return err
	}
	return nil
}
