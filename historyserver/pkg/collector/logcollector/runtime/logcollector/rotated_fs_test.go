package logcollector

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"testing"
)

// writeFile creates a file with content, failing the test on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestStatInode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "raylet.out")
	writeFile(t, path, "log line")

	key, nlink, err := statInode(path)
	if err != nil {
		t.Fatalf("statInode() error: %v", err)
	}
	if key.Ino == 0 {
		t.Error("statInode() returned inode 0")
	}
	if nlink != 1 {
		t.Errorf("statInode() nlink = %d, want 1", nlink)
	}

	// A second file in the same directory is a different inode on the same device.
	other := filepath.Join(dir, "gcs_server.out")
	writeFile(t, other, "log line")
	otherKey, _, err := statInode(other)
	if err != nil {
		t.Fatalf("statInode() error: %v", err)
	}
	if otherKey == key {
		t.Error("statInode() returned the same key for two distinct files")
	}
	if otherKey.Dev != key.Dev {
		t.Errorf("statInode() device differs within one directory: %d vs %d", otherKey.Dev, key.Dev)
	}

	if _, _, err := statInode(filepath.Join(dir, "missing.out")); !isVanished(err) {
		t.Errorf("statInode() on a missing file = %v, want a not-exist error", err)
	}
}

func TestCaptureLinkPinsInode(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")

	src := filepath.Join(logsDir, "raylet.out.1")
	writeFile(t, src, "rotated content")

	entry := stagedEntry{
		State: statePending, SessionName: "session-1", NodeName: "node-1",
		OriginalName: "raylet.out.1", CaptureID: "0001780000000000000.a1b2c3d4e5f60718",
	}
	dst := entry.path(stagingRoot)
	if err := captureLink(src, dst); err != nil {
		t.Fatalf("captureLink() error: %v", err)
	}

	srcKey, srcLinks, err := statInode(src)
	if err != nil {
		t.Fatalf("statInode(src) error: %v", err)
	}
	dstKey, dstLinks, err := statInode(dst)
	if err != nil {
		t.Fatalf("statInode(dst) error: %v", err)
	}
	if srcKey != dstKey {
		t.Errorf("staged link is a different inode: %s vs %s", srcKey, dstKey)
	}
	if srcLinks != 2 || dstLinks != 2 {
		t.Errorf("link count = (%d, %d), want (2, 2)", srcLinks, dstLinks)
	}
}

func TestCaptureSurvivesRotationAndDeletion(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")

	src := filepath.Join(logsDir, "raylet.out.1")
	const content = "the segment Ray is about to rotate away"
	writeFile(t, src, content)

	entry := stagedEntry{
		State: statePending, SessionName: "session-1", NodeName: "node-1",
		OriginalName: "raylet.out.1", CaptureID: "0001780000000000000.a1b2c3d4e5f60718",
	}
	staged := entry.path(stagingRoot)
	if err := captureLink(src, staged); err != nil {
		t.Fatalf("captureLink() error: %v", err)
	}
	pinned, _, err := statInode(staged)
	if err != nil {
		t.Fatalf("statInode(staged) error: %v", err)
	}

	// Ray's next rotation renames the segment to ".2".
	rotated := filepath.Join(logsDir, "raylet.out.2")
	if err := os.Rename(src, rotated); err != nil {
		t.Fatalf("rename src: %v", err)
	}
	afterRename, links, err := statInode(staged)
	if err != nil {
		t.Fatalf("statInode(staged) after rename error: %v", err)
	}
	if afterRename != pinned {
		t.Errorf("staged link changed inode after rename: %s -> %s", pinned, afterRename)
	}
	if links != 2 {
		t.Errorf("link count after rename = %d, want 2", links)
	}

	// Rotation eventually deletes the segment; the staged bytes must survive.
	if err := os.Remove(rotated); err != nil {
		t.Fatalf("remove rotated file: %v", err)
	}
	afterDelete, links, err := statInode(staged)
	if err != nil {
		t.Fatalf("statInode(staged) after delete error: %v", err)
	}
	if afterDelete != pinned {
		t.Errorf("staged link changed inode after delete: %s -> %s", pinned, afterDelete)
	}
	if links != 1 {
		t.Errorf("link count after delete = %d, want 1", links)
	}
	got, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("read staged file: %v", err)
	}
	if string(got) != content {
		t.Errorf("staged content = %q, want %q", got, content)
	}
}

func TestCaptureOfSuccessiveSegmentsAtSamePath(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "logs")
	stagingRoot := filepath.Join(dir, "rotated-staging")
	path := filepath.Join(logsDir, "raylet.out.1")

	g := newCaptureIDGenerator()
	identity := clusterIdentity{RootDir: "root", Namespace: "default", ClusterName: "my-cluster"}
	ix := newCaptureIndex()

	captureSegment := func(content string) (inodeKey, stagedEntry) {
		t.Helper()
		writeFile(t, path, content)
		key, _, err := statInode(path)
		if err != nil {
			t.Fatalf("statInode() error: %v", err)
		}
		id, err := g.next()
		if err != nil {
			t.Fatalf("next() error: %v", err)
		}
		entry := stagedEntry{
			State: statePending, SessionName: "session-1", NodeName: "node-1",
			OriginalName: "raylet.out.1", CaptureID: id,
		}
		if err := captureLink(path, entry.path(stagingRoot)); err != nil {
			t.Fatalf("captureLink() error: %v", err)
		}
		if _, _, err := ix.add(key, entry); err != nil {
			t.Fatalf("add() error: %v", err)
		}
		return key, entry
	}

	firstKey, first := captureSegment("first segment")
	// Ray deletes the segment and a later rotation puts a new one at the same name.
	// Our link keeps the old inode alive, so the new file must be a different inode.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove first segment: %v", err)
	}
	secondKey, second := captureSegment("second segment")

	if firstKey == secondKey {
		t.Fatalf("both segments reported inode %s; the staged link should have kept the first alive", firstKey)
	}
	if first.CaptureID == second.CaptureID {
		t.Error("segments sharing a rotation filename were given the same capture ID")
	}
	if first.objectKey(identity) == second.objectKey(identity) {
		t.Errorf("segments sharing a rotation filename map to one object key: %q", first.objectKey(identity))
	}
	if ix.len() != 2 {
		t.Errorf("index holds %d captures, want 2", ix.len())
	}

	for _, e := range []stagedEntry{first, second} {
		content, err := os.ReadFile(e.path(stagingRoot))
		if err != nil {
			t.Fatalf("read staged capture %s: %v", e.CaptureID, err)
		}
		if len(content) == 0 {
			t.Errorf("staged capture %s is empty", e.CaptureID)
		}
	}
}

func TestBaseKnownWith(t *testing.T) {
	dir := t.TempDir()
	logsDir := filepath.Join(dir, "logs")
	writeFile(t, filepath.Join(logsDir, "raylet.out"), "active")

	ix := newCaptureIndex()
	baseKnown := baseKnownWith(ix)

	// The active file is present: the backup beside it is eligible.
	if status := evaluateCandidate(logsDir, "raylet.out.1", 0, baseKnown); status != candidateEligible {
		t.Errorf("backup with a live base = %v, want %v", status, candidateEligible)
	}

	// A file that merely ends in ".1" with no active base is left alone.
	if status := evaluateCandidate(logsDir, "user-data.1", 0, baseKnown); status != candidateUnknownBase {
		t.Errorf("unrelated numeric suffix = %v, want %v", status, candidateUnknownBase)
	}

	// A rotation cascade briefly unlinks the active name. Once observed, a backup
	// that appears while the base is missing is still recognized.
	ix.observeBase(logsDir, "gcs_server.out")
	if status := evaluateCandidate(logsDir, "gcs_server.out.1", 0, baseKnown); status != candidateEligible {
		t.Errorf("backup with a previously observed base = %v, want %v", status, candidateEligible)
	}
	if status := evaluateCandidate(filepath.Join(logsDir, "events"), "gcs_server.out.1", 0, baseKnown); status != candidateUnknownBase {
		t.Errorf("observed base leaked into another directory: %v", status)
	}

	// A symlink named like an active log file is not an active log file.
	linkTarget := filepath.Join(dir, "elsewhere.out")
	writeFile(t, linkTarget, "not a log")
	if err := os.Symlink(linkTarget, filepath.Join(logsDir, "dashboard.log")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if status := evaluateCandidate(logsDir, "dashboard.log.1", 0, baseKnown); status != candidateUnknownBase {
		t.Errorf("symlinked base = %v, want %v", status, candidateUnknownBase)
	}
}

func TestRegularFileExists(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "raylet.out")
	writeFile(t, file, "active")

	if !regularFileExists(file) {
		t.Error("regularFileExists() = false for a regular file")
	}
	if regularFileExists(dir) {
		t.Error("regularFileExists() = true for a directory")
	}
	if regularFileExists(filepath.Join(dir, "missing")) {
		t.Error("regularFileExists() = true for a missing file")
	}

	link := filepath.Join(dir, "link.out")
	if err := os.Symlink(file, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if regularFileExists(link) {
		t.Error("regularFileExists() followed a symlink")
	}
}

func TestIsUnsupportedLinkError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "cross-device link",
			err:  &os.LinkError{Op: "link", Old: "/tmp/ray/logs/raylet.out.1", New: "/staging/x", Err: syscall.EXDEV},
			want: true,
		},
		{
			name: "not permitted",
			err:  &os.LinkError{Op: "link", Old: "/tmp/ray/logs/raylet.out.1", New: "/staging/x", Err: syscall.EPERM},
			want: true,
		},
		{
			name: "access denied",
			err:  &os.LinkError{Op: "link", Old: "/tmp/ray/logs/raylet.out.1", New: "/staging/x", Err: syscall.EACCES},
			want: true,
		},
		{
			name: "wrapped by captureLink",
			err:  errors.Join(errors.New("hard link capture"), &os.LinkError{Op: "link", Err: syscall.EXDEV}),
			want: true,
		},
		{
			name: "vanished before capture",
			err:  &os.LinkError{Op: "link", Old: "/tmp/ray/logs/raylet.out.1", New: "/staging/x", Err: syscall.ENOENT},
			want: false,
		},
		{name: "unrelated error", err: errors.New("boom"), want: false},
		{name: "no error", err: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUnsupportedLinkError(tt.err); got != tt.want {
				t.Errorf("isUnsupportedLinkError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsVanished(t *testing.T) {
	if !isVanished(&os.LinkError{Op: "link", Err: syscall.ENOENT}) {
		t.Error("isVanished() = false for ENOENT")
	}
	if !isVanished(fs.ErrNotExist) {
		t.Error("isVanished() = false for fs.ErrNotExist")
	}
	if isVanished(&os.LinkError{Op: "link", Err: syscall.EXDEV}) {
		t.Error("isVanished() = true for EXDEV")
	}
	if isVanished(nil) {
		t.Error("isVanished() = true for nil")
	}
}

func TestCaptureLinkReportsMissingSource(t *testing.T) {
	dir := t.TempDir()
	err := captureLink(filepath.Join(dir, "logs", "missing.out.1"), filepath.Join(dir, "staging", "x"))
	if !isVanished(err) {
		t.Errorf("captureLink() on a missing source = %v, want a not-exist error", err)
	}
	if err != nil && !strings.Contains(err.Error(), "missing.out.1") {
		t.Errorf("captureLink() error %q does not name the source path", err)
	}
}

func TestRestartReconstructsStagedCaptures(t *testing.T) {
	dir := t.TempDir()
	stagingRoot := filepath.Join(dir, "rotated-staging")
	logsDir := filepath.Join(dir, "logs")

	g := newCaptureIDGenerator()
	original := newCaptureIndex()
	stage := func(relDir, name string, promote bool) stagedEntry {
		t.Helper()
		src := filepath.Join(logsDir, filepath.FromSlash(relDir), name)
		writeFile(t, src, "content of "+name)
		id, err := g.next()
		if err != nil {
			t.Fatalf("next() error: %v", err)
		}
		entry, err := newStagedEntry(statePending, "session-1", "node-1", relDir, name, id)
		if err != nil {
			t.Fatalf("newStagedEntry() error: %v", err)
		}
		if err := captureLink(src, entry.path(stagingRoot)); err != nil {
			t.Fatalf("captureLink() error: %v", err)
		}
		key, _, err := statInode(entry.path(stagingRoot))
		if err != nil {
			t.Fatalf("statInode() error: %v", err)
		}
		if _, _, err := original.add(key, entry); err != nil {
			t.Fatalf("add() error: %v", err)
		}
		if promote {
			entry, err = promoteCapture(stagingRoot, original, key)
			if err != nil {
				t.Fatalf("promoteCapture() error: %v", err)
			}
		}
		return entry
	}

	before := map[string]stagedEntry{}
	for _, e := range []stagedEntry{
		stage("", "raylet.out.1", false),
		stage("events", "event.log.2", true),
	} {
		before[e.CaptureID] = e
	}

	// Restart: a fresh index rebuilt from the staging volume alone.
	after := newCaptureIndex()
	err := filepath.WalkDir(stagingRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		entry, err := parseStagedPath(stagingRoot, p)
		if err != nil {
			return err
		}
		key, _, err := statInode(p)
		if err != nil {
			return err
		}
		_, err = after.restore(key, entry)
		return err
	})
	if err != nil {
		t.Fatalf("reconstruct staging volume: %v", err)
	}

	if after.len() != len(before) {
		t.Fatalf("reconstructed %d captures, want %d", after.len(), len(before))
	}
	for _, got := range after.entries() {
		want, ok := before[got.CaptureID]
		if !ok {
			t.Errorf("reconstruction minted a new capture ID %q", got.CaptureID)
			continue
		}
		if got != want {
			t.Errorf("reconstructed %+v, want %+v", got, want)
		}
	}
}

func TestCaptureLinkRejectsReusedCaptureID(t *testing.T) {
	dir := t.TempDir()
	stagingRoot := filepath.Join(dir, "rotated-staging")
	writeFile(t, filepath.Join(dir, "logs", "raylet.out.1"), "first")
	writeFile(t, filepath.Join(dir, "logs", "raylet.out.2"), "second")

	entry, err := newStagedEntry(statePending, "session-1", "node-1", "", "raylet.out.1", "0001780000000000000.a1b2c3d4e5f60718")
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	if err := captureLink(filepath.Join(dir, "logs", "raylet.out.1"), entry.path(stagingRoot)); err != nil {
		t.Fatalf("captureLink() error: %v", err)
	}

	// Reusing a capture ID would silently overwrite another segment's object, so the
	// collision must surface rather than be treated as deduplication.
	err = captureLink(filepath.Join(dir, "logs", "raylet.out.2"), entry.path(stagingRoot))
	if !isAlreadyStaged(err) {
		t.Fatalf("captureLink() on a reused capture ID = %v, want an already-exists error", err)
	}
	if !strings.Contains(err.Error(), entry.CaptureID) {
		t.Errorf("captureLink() error %q does not name the reused capture ID", err)
	}

	// The first capture's bytes must be untouched.
	got, err := os.ReadFile(entry.path(stagingRoot))
	if err != nil {
		t.Fatalf("read staged capture: %v", err)
	}
	if string(got) != "first" {
		t.Errorf("staged content = %q, want %q", got, "first")
	}
}

// stagedCapture pins one file and returns the index, its key and the source path.
func stagedCapture(t *testing.T, dir, relDir, name string) (*captureIndex, inodeKey, string, string) {
	t.Helper()
	stagingRoot := filepath.Join(dir, "rotated-staging")
	src := filepath.Join(dir, "logs", filepath.FromSlash(relDir), name)
	writeFile(t, src, "content of "+name)

	entry, err := newStagedEntry(statePending, "session-1", "node-1", relDir, name, "0001780000000000000.a1b2c3d4e5f60718")
	if err != nil {
		t.Fatalf("newStagedEntry() error: %v", err)
	}
	if err := captureLink(src, entry.path(stagingRoot)); err != nil {
		t.Fatalf("captureLink() error: %v", err)
	}
	key, _, err := statInode(entry.path(stagingRoot))
	if err != nil {
		t.Fatalf("statInode() error: %v", err)
	}
	ix := newCaptureIndex()
	if _, _, err := ix.add(key, entry); err != nil {
		t.Fatalf("add() error: %v", err)
	}
	return ix, key, stagingRoot, src
}

func TestPromoteCaptureMovesDiskAndIndexTogether(t *testing.T) {
	dir := t.TempDir()
	ix, key, stagingRoot, _ := stagedCapture(t, dir, "events", "event.log.1")
	pendingPath := mustEntry(t, ix, key).path(stagingRoot)

	promoted, err := promoteCapture(stagingRoot, ix, key)
	if err != nil {
		t.Fatalf("promoteCapture() error: %v", err)
	}
	if promoted.State != stateUploaded {
		t.Errorf("promoted state = %q, want %q", promoted.State, stateUploaded)
	}

	// Disk moved.
	if _, err := os.Lstat(pendingPath); !isVanished(err) {
		t.Errorf("pending path still exists after promotion: %v", err)
	}
	if _, err := os.Lstat(promoted.path(stagingRoot)); err != nil {
		t.Errorf("uploaded path missing after promotion: %v", err)
	}
	// Index moved with it, and still points at a path that exists.
	tracked := mustEntry(t, ix, key)
	if tracked != promoted {
		t.Errorf("index entry = %+v, want %+v", tracked, promoted)
	}
	if _, err := os.Lstat(tracked.path(stagingRoot)); err != nil {
		t.Errorf("index points at a path that does not exist: %v", err)
	}
}

func TestPromoteCaptureFailureLeavesDiskAndIndexPending(t *testing.T) {
	dir := t.TempDir()
	ix, key, stagingRoot, _ := stagedCapture(t, dir, "events", "event.log.1")
	pending := mustEntry(t, ix, key)

	// Block the uploaded tree so the rename cannot happen.
	writeFile(t, filepath.Join(stagingRoot, "session-1", "node-1", string(stateUploaded)), "blocker")

	if promoted, err := promoteCapture(stagingRoot, ix, key); err == nil {
		t.Fatalf("promoteCapture() = %+v, want error", promoted)
	}

	if tracked := mustEntry(t, ix, key); tracked != pending {
		t.Errorf("index moved to %+v although the rename failed, want %+v", tracked, pending)
	}
	if _, err := os.Lstat(pending.path(stagingRoot)); err != nil {
		t.Errorf("pending link lost after a failed promotion: %v", err)
	}
}

func TestReleaseCaptureRequiresUploadedAndLastLink(t *testing.T) {
	dir := t.TempDir()
	ix, key, stagingRoot, src := stagedCapture(t, dir, "", "raylet.out.1")
	pendingPath := mustEntry(t, ix, key).path(stagingRoot)

	// Pending data is never released, however few links remain.
	if err := releaseCapture(stagingRoot, ix, key); err == nil {
		t.Error("releaseCapture() released a pending capture")
	}
	if _, ok := ix.lookup(key); !ok {
		t.Fatal("releaseCapture() forgot a capture it refused to release")
	}
	if _, err := os.Lstat(pendingPath); err != nil {
		t.Errorf("pending link was unlinked: %v", err)
	}

	uploaded, err := promoteCapture(stagingRoot, ix, key)
	if err != nil {
		t.Fatalf("promoteCapture() error: %v", err)
	}

	// Ray still holds its own link: releaseCapture must read that for itself.
	if err := releaseCapture(stagingRoot, ix, key); err == nil {
		t.Error("releaseCapture() released a segment Ray still links to")
	}
	if _, ok := ix.lookup(key); !ok {
		t.Fatal("releaseCapture() forgot a capture it refused to release")
	}
	if _, err := os.Lstat(uploaded.path(stagingRoot)); err != nil {
		t.Errorf("staged link was unlinked: %v", err)
	}

	// Ray drops its name; ours is now the only link.
	if err := os.Remove(src); err != nil {
		t.Fatalf("remove source: %v", err)
	}
	if err := releaseCapture(stagingRoot, ix, key); err != nil {
		t.Fatalf("releaseCapture() error: %v", err)
	}
	if _, ok := ix.lookup(key); ok {
		t.Error("releaseCapture() kept the dev/inode entry after unlinking the last link")
	}
	if _, err := os.Lstat(uploaded.path(stagingRoot)); !isVanished(err) {
		t.Errorf("uploaded link still on disk after release: %v", err)
	}
	// Nothing may be left behind under pending/ either.
	if _, err := os.Lstat(pendingPath); !isVanished(err) {
		t.Errorf("pending link leaked: %v", err)
	}
}

func TestReleaseCaptureRefusesWhenStagingDisagrees(t *testing.T) {
	dir := t.TempDir()
	ix, key, stagingRoot, src := stagedCapture(t, dir, "", "raylet.out.1")

	uploaded, err := promoteCapture(stagingRoot, ix, key)
	if err != nil {
		t.Fatalf("promoteCapture() error: %v", err)
	}
	if err := os.Remove(src); err != nil {
		t.Fatalf("remove source: %v", err)
	}

	// Simulate a staging tree that no longer matches the index: the capture is
	// tracked as uploaded but its link is back under pending. Releasing must not
	// read the missing uploaded path as "already released" and drop the entry,
	// which would leak the pending link.
	pendingPath := uploaded.withState(statePending).path(stagingRoot)
	if err := os.MkdirAll(filepath.Dir(pendingPath), 0o750); err != nil {
		t.Fatalf("recreate pending directory: %v", err)
	}
	if err := os.Rename(uploaded.path(stagingRoot), pendingPath); err != nil {
		t.Fatalf("move staged link back to pending: %v", err)
	}

	err = releaseCapture(stagingRoot, ix, key)
	if err == nil {
		t.Fatal("releaseCapture() reported success although the uploaded path was missing")
	}
	if !strings.Contains(err.Error(), uploaded.CaptureID) {
		t.Errorf("releaseCapture() error %q does not name the capture", err)
	}
	if _, ok := ix.lookup(key); !ok {
		t.Error("releaseCapture() discarded the index entry although the staged link still exists")
	}
	if _, err := os.Lstat(pendingPath); err != nil {
		t.Errorf("staged link lost: %v", err)
	}
}

func TestReleaseCaptureRefusesWhenInodeChanged(t *testing.T) {
	dir := t.TempDir()
	ix, key, stagingRoot, src := stagedCapture(t, dir, "", "raylet.out.1")
	uploaded, err := promoteCapture(stagingRoot, ix, key)
	if err != nil {
		t.Fatalf("promoteCapture() error: %v", err)
	}
	if err := os.Remove(src); err != nil {
		t.Fatalf("remove source: %v", err)
	}

	// Replace the staged link with an unrelated file at the same path.
	if err := os.Remove(uploaded.path(stagingRoot)); err != nil {
		t.Fatalf("remove staged link: %v", err)
	}
	writeFile(t, uploaded.path(stagingRoot), "someone else's file")

	if err := releaseCapture(stagingRoot, ix, key); err == nil {
		t.Error("releaseCapture() unlinked a path that no longer holds the pinned inode")
	}
	if _, err := os.Lstat(uploaded.path(stagingRoot)); err != nil {
		t.Errorf("releaseCapture() removed the unrelated file: %v", err)
	}
}

func TestReleaseCaptureKeepsEntryWhenUnlinkFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions do not prevent unlink")
	}
	dir := t.TempDir()
	ix, key, stagingRoot, src := stagedCapture(t, dir, "", "raylet.out.1")
	uploaded, err := promoteCapture(stagingRoot, ix, key)
	if err != nil {
		t.Fatalf("promoteCapture() error: %v", err)
	}
	if err := os.Remove(src); err != nil {
		t.Fatalf("remove source: %v", err)
	}

	parent := filepath.Dir(uploaded.path(stagingRoot))
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatalf("chmod staging directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(parent, 0o750); err != nil {
			t.Logf("restore staging directory permissions: %v", err)
		}
	})

	if err := releaseCapture(stagingRoot, ix, key); err == nil {
		t.Fatal("releaseCapture() reported success although the unlink was denied")
	}
	// The inode is still pinned, so it must still be tracked: forgetting it here
	// would leave a link nobody ever releases.
	if _, ok := ix.lookup(key); !ok {
		t.Error("releaseCapture() forgot the capture although the unlink failed")
	}
	if _, err := os.Lstat(uploaded.path(stagingRoot)); err != nil {
		t.Errorf("staged link lost: %v", err)
	}
}

func mustEntry(t *testing.T, ix *captureIndex, key inodeKey) stagedEntry {
	t.Helper()
	c, ok := ix.lookup(key)
	if !ok {
		t.Fatalf("no capture tracked for %s", key)
	}
	return c.Entry
}

func TestPromoteCaptureRejectsBeforeTouchingDisk(t *testing.T) {
	// Everything that can fail must fail before the rename, so a rejected promotion
	// leaves the staging volume exactly as it was.
	snapshot := func(t *testing.T, root string) []string {
		t.Helper()
		var paths []string
		err := filepath.WalkDir(root, func(p string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			paths = append(paths, rel)
			return nil
		})
		if err != nil {
			t.Fatalf("walk staging volume: %v", err)
		}
		sort.Strings(paths)
		return paths
	}

	t.Run("already uploaded", func(t *testing.T) {
		dir := t.TempDir()
		ix, key, stagingRoot, _ := stagedCapture(t, dir, "", "raylet.out.1")
		uploaded, err := promoteCapture(stagingRoot, ix, key)
		if err != nil {
			t.Fatalf("promoteCapture() error: %v", err)
		}
		before := snapshot(t, stagingRoot)

		if got, err := promoteCapture(stagingRoot, ix, key); err == nil {
			t.Fatalf("promoteCapture() = %+v, want error for an already uploaded capture", got)
		}
		if diff := snapshot(t, stagingRoot); !slices.Equal(diff, before) {
			t.Errorf("staging volume changed on a rejected promotion:\n got %v\nwant %v", diff, before)
		}
		if tracked := mustEntry(t, ix, key); tracked != uploaded {
			t.Errorf("index entry changed on a rejected promotion: %+v", tracked)
		}
	})

	t.Run("inode not pinned", func(t *testing.T) {
		dir := t.TempDir()
		ix, _, stagingRoot, _ := stagedCapture(t, dir, "", "raylet.out.1")
		before := snapshot(t, stagingRoot)

		if got, err := promoteCapture(stagingRoot, ix, inodeKey{Dev: 9, Ino: 9}); err == nil {
			t.Fatalf("promoteCapture() = %+v, want error for an unpinned inode", got)
		}
		if diff := snapshot(t, stagingRoot); !slices.Equal(diff, before) {
			t.Errorf("staging volume changed on a rejected promotion:\n got %v\nwant %v", diff, before)
		}
	})
}
