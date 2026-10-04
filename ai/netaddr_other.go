//go:build !windows

package ai

import (
	"errors"
	"syscall"
)

// isAddressInUse reports whether err is a bind failure because the address is
// already taken.
func isAddressInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}
