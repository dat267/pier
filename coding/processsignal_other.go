//go:build !unix

package coding

import "syscall"

// platformSignalNames holds the few SIG<NAME> spellings that exist off unix; anything
// else falls back to Go's own name.
var platformSignalNames = map[syscall.Signal]string{
	syscall.SIGINT:  "SIGINT",
	syscall.SIGTERM: "SIGTERM",
}
