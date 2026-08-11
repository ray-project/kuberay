//go:build unix

package logcollector

import (
	"fmt"
	"io/fs"
	"syscall"
)

// inodeFromFileInfo extracts the device/inode pair and current link count from a
// stat result.
func inodeFromFileInfo(fi fs.FileInfo) (inodeKey, uint64, error) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return inodeKey{}, 0, fmt.Errorf("unexpected stat type %T for %s", fi.Sys(), fi.Name())
	}
	return inodeKey{Dev: statNumber(st.Dev), Ino: statNumber(st.Ino)}, statNumber(st.Nlink), nil
}

// statNumber widens the platform-specific integers in syscall.Stat_t: the device,
// inode and link-count fields differ in both width and signedness across the unix
// targets this collector is built and tested on.
func statNumber[T ~int16 | ~uint16 | ~int32 | ~uint32 | ~int64 | ~uint64](v T) uint64 {
	return uint64(v)
}
