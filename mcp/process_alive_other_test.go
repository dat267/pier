//go:build !windows

package mcp

import (
	"errors"
	"syscall"
)

// processAlive probes a pid without signalling it.
func processAlive(pid int) (bool, error) {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true, nil
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == syscall.ESRCH {
		return false, nil
	}
	return false, err
}
