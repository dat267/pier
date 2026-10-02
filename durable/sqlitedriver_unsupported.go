//go:build dragonfly || solaris || aix

package durable

// modernc.org/libc (and therefore modernc.org/sqlite) has no support for
// these platforms; the SQLite backend reports an explicit error instead of
// failing to build (D184).
const sqliteDriverSupported = false
