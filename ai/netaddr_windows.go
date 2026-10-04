//go:build windows

package ai

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// isAddressInUse reports whether err is a bind failure because the address is
// already taken (Windows reports WSAEADDRINUSE).
func isAddressInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, windows.WSAEADDRINUSE)
}
