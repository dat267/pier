//go:build windows

package mcp

import (
	"errors"
	"os"
	"syscall"
)

// processAlive probes a pid (Windows cannot signal 0).
func processAlive(pid int) (bool, error) {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false, err
	}
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.EINVAL) {
		return false, nil
	}
	return false, err
}
