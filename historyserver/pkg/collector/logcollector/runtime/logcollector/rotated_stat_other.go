//go:build !unix

package logcollector

import (
	"fmt"
	"io/fs"
)

// inodeFromFileInfo has no portable implementation: rotated-log capture relies on
// hard links and link counts, which the collector only ever runs against a unix
// filesystem shared with the Ray container.
func inodeFromFileInfo(fi fs.FileInfo) (inodeKey, uint64, error) {
	return inodeKey{}, 0, fmt.Errorf("rotated log capture is unsupported on this platform: cannot read inode of %s", fi.Name())
}
