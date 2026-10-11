//go:build !windows

package runner

import (
	"errors"
	"syscall"
)

// alive reports whether the process exists: signal 0 probes without killing.
func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// terminate asks the process to end; one already gone has ended.
func terminate(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
