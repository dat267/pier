//go:build linux || darwin || windows || freebsd || openbsd || netbsd

package durable

// Pure-Go SQLite driver (user-approved dependency).
import _ "modernc.org/sqlite"

// The SQLite backend runs on the platforms modernc.org/libc supports.
const sqliteDriverSupported = true
