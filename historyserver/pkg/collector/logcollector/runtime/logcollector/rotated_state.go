package logcollector

import (
	"fmt"
	"sort"
)

// inodeKey correlates a staged hard link with the file Ray rotated.
//
// It is only meaningful while the collector's own link keeps the inode alive: once
// the last link is dropped the kernel may hand the same number to an unrelated
// file. So inodeKey answers "have I already pinned this exact file?" and nothing
// else — permanent identity is the capture ID.
type inodeKey struct {
	Dev uint64
	Ino uint64
}

func (k inodeKey) String() string {
	return fmt.Sprintf("dev=%d,ino=%d", k.Dev, k.Ino)
}

// capture is one rotated segment the collector has pinned with a hard link.
type capture struct {
	Inode inodeKey
	Entry stagedEntry
}

// releasable reports whether the staged link may be unlinked. Both conditions
// matter: the bytes must already be in storage, and nlink == 1 proves ours is the
// last remaining link, so Ray has finished with the segment and no writer can
// reach it by name any more.
func (c *capture) releasable(nlink uint64) bool {
	return c.Entry.State == stateUploaded && nlink == 1
}

// captureIndex tracks every pinned segment for one collector.
//
// It is deliberately free of locks: a single owner goroutine is the only caller
// that reads or mutates it, so "look up the inode, create the link, register the
// entry" is one indivisible step from the collector's point of view. Two events for
// the same pinned inode therefore cannot mint two capture IDs.
type captureIndex struct {
	byInode map[inodeKey]*capture

	// bases remembers active log file names per directory. A rotation cascade
	// briefly unlinks the active name, so a backup can legitimately appear while
	// its base is missing; without this memory such a segment would look like an
	// unrelated file ending in ".<digits>" and be skipped.
	bases map[string]map[string]struct{}
}

func newCaptureIndex() *captureIndex {
	return &captureIndex{
		byInode: make(map[inodeKey]*capture),
		bases:   make(map[string]map[string]struct{}),
	}
}

func (ix *captureIndex) len() int {
	return len(ix.byInode)
}

func (ix *captureIndex) lookup(key inodeKey) (*capture, bool) {
	c, ok := ix.byInode[key]
	return c, ok
}

// add registers a freshly pinned segment. It reports added=false and returns the
// existing capture when the inode is already pinned, which is how a ".1" event and
// a later ".2" event for the same physical file collapse to one capture.
func (ix *captureIndex) add(key inodeKey, e stagedEntry) (*capture, bool, error) {
	if e.State != statePending {
		return nil, false, fmt.Errorf("add capture %s: new captures start in %q, got %q", key, statePending, e.State)
	}
	if existing, ok := ix.byInode[key]; ok {
		return existing, false, nil
	}
	c := &capture{Inode: key, Entry: e}
	ix.byInode[key] = c
	return c, true, nil
}

// restore re-registers an entry discovered on the staging volume at startup,
// accepting either durable state. Reconstruction must finish before any live event
// is handled, otherwise a capture from the previous run could be duplicated.
func (ix *captureIndex) restore(key inodeKey, e stagedEntry) (*capture, error) {
	if !validStagingState(e.State) {
		return nil, fmt.Errorf("restore capture %s: unknown staging state %q", key, e.State)
	}
	if existing, ok := ix.byInode[key]; ok {
		// Every field has to agree, not just the ID. One inode found under two
		// staging records — say a leftover pending link and an uploaded one from the
		// same capture — is a corrupt staging tree, and which record won would
		// otherwise depend on the order the filesystem walk happened to return.
		if existing.Entry != e {
			return nil, fmt.Errorf("restore capture %s: inode already staged as %+v, cannot also be %+v",
				key, existing.Entry, e)
		}
		return existing, nil
	}
	c := &capture{Inode: key, Entry: e}
	ix.byInode[key] = c
	return c, nil
}

// remove drops a capture from the index once its staged link is gone.
func (ix *captureIndex) remove(key inodeKey) {
	delete(ix.byInode, key)
}

// entries returns every tracked entry, sorted by capture ID so that iteration is
// deterministic rather than in Go's randomized map order. Nothing depends on that
// order for correctness.
func (ix *captureIndex) entries() []stagedEntry {
	out := make([]stagedEntry, 0, len(ix.byInode))
	for _, c := range ix.byInode {
		out = append(out, c.Entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CaptureID < out[j].CaptureID })
	return out
}

// observeBase records that name is an active (non-backup) log file in dir.
func (ix *captureIndex) observeBase(dir, name string) {
	d, ok := ix.bases[dir]
	if !ok {
		d = make(map[string]struct{})
		ix.bases[dir] = d
	}
	d[name] = struct{}{}
}

// baseObserved reports whether name was previously seen as an active log file in dir.
func (ix *captureIndex) baseObserved(dir, name string) bool {
	_, ok := ix.bases[dir][name]
	return ok
}

// validTransition allows only the one durable state change a capture can make.
// Nothing moves back to pending: re-uploading a changed file rewrites the same
// object key and leaves the capture uploaded.
func validTransition(from, to stagingState) bool {
	return from == statePending && to == stateUploaded
}
