package logcollector

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// stagingDirPerm keeps the staging tree collector-private. Unlike prev-logs, Ray
// never reads these directories; only the collector writes and drains them.
const stagingDirPerm fs.FileMode = 0o750

// statInode returns the device/inode pair and current link count of path without
// following symlinks. A hard link is a real directory entry for the same inode, so
// this reports on the staged link exactly as it does on Ray's own name.
func statInode(path string) (inodeKey, uint64, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return inodeKey{}, 0, fmt.Errorf("stat %s: %w", path, err)
	}
	key, nlink, err := inodeFromFileInfo(fi)
	if err != nil {
		return inodeKey{}, 0, fmt.Errorf("stat %s: %w", path, err)
	}
	return key, nlink, nil
}

// captureLink pins src by hard-linking it to dst, creating dst's parent directory.
//
// Pinning is what makes capture safe: from this point the bytes survive Ray
// rotating the name away or deleting it, and no copy is made, so a large segment
// costs no additional blocks while Ray still holds its own link.
func captureLink(src, dst string) error {
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, stagingDirPerm); err != nil {
		return fmt.Errorf("create staging directory %s: %w", dir, err)
	}
	if err := os.Link(src, dst); err != nil {
		if isAlreadyStaged(err) {
			return fmt.Errorf("hard link capture: staging path %s already exists, so a capture ID was reused: %w", dst, err)
		}
		return fmt.Errorf("hard link capture: %w", err)
	}
	return nil
}

// isUnsupportedLinkError reports whether err means hard-link capture cannot work in
// this deployment: the staging tree is on a different filesystem than the logs
// (EXDEV), or the collector may not link the Ray container's files (EPERM/EACCES,
// typically a custom image whose Ray user differs from the collector's UID).
//
// v1 warns and skips those segments. The alternative — copying — would need a
// content hash to stay deduplicated, because a dev+inode marker can match a
// completely unrelated file after the kernel reuses an inode number.
func isUnsupportedLinkError(err error) bool {
	return errors.Is(err, syscall.EXDEV) ||
		errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.EACCES)
}

// isVanished reports whether err means the file is already gone. Losing that race
// with Ray's rotation delete is expected and is not a capture failure.
func isVanished(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

// isAlreadyStaged reports whether a capture link collided with an existing file.
//
// This is not deduplication: every discovery mints a fresh capture ID and so a
// fresh destination, which is why the index — not EEXIST — is what stops an inode
// being captured twice. A collision here means the same capture ID was used twice,
// which is a bug worth surfacing rather than a condition to swallow.
func isAlreadyStaged(err error) bool {
	return errors.Is(err, fs.ErrExist)
}

// promoteCapture moves a capture from pending to uploaded, on disk and then in the
// index. It is the only way either changes: there is no memory-only promotion, and
// no disk-only one.
//
// Every fallible step happens before the rename. Once the rename succeeds all that
// remains is an assignment to a struct owned by the single event-loop goroutine,
// which cannot fail — so disk and index cannot end up disagreeing. Rolling the
// rename back instead would only add a second operation that can fail on its own.
//
// The rename is atomic, so a crash leaves the capture in exactly one state and a
// restarting collector can tell an unsent capture from a finished one by path alone.
// Capture identity is carried across unchanged: same original name, same capture ID,
// therefore the same object key on any retry.
func promoteCapture(stagingRoot string, ix *captureIndex, key inodeKey) (stagedEntry, error) {
	c, ok := ix.lookup(key)
	if !ok {
		return stagedEntry{}, fmt.Errorf("promote capture: no capture pinned for %s", key)
	}
	if !validTransition(c.Entry.State, stateUploaded) {
		return stagedEntry{}, fmt.Errorf("promote capture %s: cannot move from %q to %q",
			c.Entry.CaptureID, c.Entry.State, stateUploaded)
	}

	promoted := c.Entry.withState(stateUploaded)
	src := c.Entry.path(stagingRoot)
	dst := promoted.path(stagingRoot)

	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, stagingDirPerm); err != nil {
		return stagedEntry{}, fmt.Errorf("create staging directory %s: %w", dir, err)
	}
	if err := os.Rename(src, dst); err != nil {
		// Disk and index are both still pending, so the upload can be retried.
		return stagedEntry{}, fmt.Errorf("promote capture %s to uploaded: %w", c.Entry.CaptureID, err)
	}

	c.Entry = promoted
	return promoted, nil
}

// releaseCapture unlinks a fully uploaded capture and only then forgets it.
//
// It reads the link count itself rather than accepting one, because a caller-
// supplied count is a claim about the past: between the caller's stat and this call
// Ray may have created or dropped a link. Everything the safety decision rests on is
// therefore established here, immediately before the unlink.
//
// Removing the index entry after — never before — the unlink matters too: the kernel
// may hand that inode number to an unrelated file the moment the last link
// disappears, so a stale entry could later match the wrong file.
func releaseCapture(stagingRoot string, ix *captureIndex, key inodeKey) error {
	c, ok := ix.lookup(key)
	if !ok {
		return fmt.Errorf("release capture: no capture pinned for %s", key)
	}
	if c.Entry.State != stateUploaded {
		return fmt.Errorf("release capture %s: not releasable in state %q", c.Entry.CaptureID, c.Entry.State)
	}

	p := c.Entry.path(stagingRoot)
	staged, nlink, err := statInode(p)
	if err != nil {
		// The index says the capture is staged here. If it is not, disk and index
		// disagree: dropping the entry now could strand a link staged elsewhere, so
		// this has to surface rather than look like a completed release.
		return fmt.Errorf("release capture %s: index and staging volume disagree about %s: %w", c.Entry.CaptureID, p, err)
	}
	if staged != c.Inode {
		return fmt.Errorf("release capture %s: %s now holds %s, not the pinned %s", c.Entry.CaptureID, p, staged, c.Inode)
	}
	if !c.releasable(nlink) {
		return fmt.Errorf("release capture %s: %d link(s) remain, so Ray still holds the segment", c.Entry.CaptureID, nlink)
	}

	if err := os.Remove(p); err != nil {
		return fmt.Errorf("release capture %s: %w", c.Entry.CaptureID, err)
	}
	ix.remove(c.Inode)
	return nil
}

// regularFileExists reports whether path is a regular file, without following
// symlinks.
func regularFileExists(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode().IsRegular()
}

// baseKnownWith answers "does this backup belong to a log file that rotates here?"
// by checking the live directory first and the index's memory second, so a backup
// that appears while its active name is briefly unlinked is still recognized.
func baseKnownWith(ix *captureIndex) baseKnownFunc {
	return func(dir, base string) bool {
		if regularFileExists(filepath.Join(dir, base)) {
			return true
		}
		return ix != nil && ix.baseObserved(dir, base)
	}
}
