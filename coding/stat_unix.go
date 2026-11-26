//go:build unix && !darwin && !freebsd && !netbsd

package coding

import (
	"os"
	"syscall"
)

// statDeviceInode returns the device and inode numbers for a file. The device
// type differs across this file's platforms (openbsd int32, dragonfly uint32,
// linux uint64), so both fields are converted, as stat_bsd.go does.
func statDeviceInode(info os.FileInfo) (uint64, uint64) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(stat.Dev), uint64(stat.Ino)
	}
	return 0, 0
}

// statChangeTimeNano returns the inode change time in nanoseconds. Nsec is
// int32 on some of this file's platforms (aix, openbsd), so both terms are
// converted before they are combined.
func statChangeTimeNano(info os.FileInfo) int64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(stat.Ctim.Sec)*1e9 + int64(stat.Ctim.Nsec)
	}
	return 0
}
