package logcollector

import (
	"testing"
)

func testEntry(originalName, captureID string) stagedEntry {
	return stagedEntry{
		State:        statePending,
		SessionName:  "session-1",
		NodeName:     "node-1",
		OriginalName: originalName,
		CaptureID:    captureID,
	}
}

func TestCaptureIndexAddIsIdempotentPerInode(t *testing.T) {
	ix := newCaptureIndex()
	key := inodeKey{Dev: 1, Ino: 42}

	first, added, err := ix.add(key, testEntry("raylet.out.1", "0001780000000000000.a1b2c3d4e5f60718"))
	if err != nil {
		t.Fatalf("add() error: %v", err)
	}
	if !added {
		t.Fatal("add() reported the first capture as already present")
	}

	// The same physical file reappears as ".2" after the next rotation. It is the
	// same pinned inode, so it must stay one capture with one ID and one object.
	second, added, err := ix.add(key, testEntry("raylet.out.2", "0001780000000000001.ffffffffffffffff"))
	if err != nil {
		t.Fatalf("add() error on second reference: %v", err)
	}
	if added {
		t.Error("add() created a second capture for an already pinned inode")
	}
	if second != first {
		t.Error("add() returned a different capture for an already pinned inode")
	}
	if second.Entry.CaptureID != first.Entry.CaptureID {
		t.Errorf("capture ID changed on re-discovery: %q -> %q", first.Entry.CaptureID, second.Entry.CaptureID)
	}
	if second.Entry.OriginalName != "raylet.out.1" {
		t.Errorf("original name changed on re-discovery: %q", second.Entry.OriginalName)
	}
	if ix.len() != 1 {
		t.Errorf("index holds %d captures, want 1", ix.len())
	}
}

func TestCaptureIndexDistinctInodesAreDistinctCaptures(t *testing.T) {
	ix := newCaptureIndex()

	// Two segments that successively occupy the same rotation filename.
	if _, _, err := ix.add(inodeKey{Dev: 1, Ino: 42}, testEntry("raylet.out.1", "0001780000000000000.aaaaaaaaaaaaaaaa")); err != nil {
		t.Fatalf("add() error: %v", err)
	}
	if _, _, err := ix.add(inodeKey{Dev: 1, Ino: 43}, testEntry("raylet.out.1", "0001780000000000001.bbbbbbbbbbbbbbbb")); err != nil {
		t.Fatalf("add() error: %v", err)
	}

	if ix.len() != 2 {
		t.Fatalf("index holds %d captures, want 2", ix.len())
	}
	entries := ix.entries()
	if entries[0].CaptureID == entries[1].CaptureID {
		t.Error("two segments sharing a rotation filename were given the same capture ID")
	}

	identity := clusterIdentity{RootDir: "root", Namespace: "default", ClusterName: "my-cluster"}
	if entries[0].objectKey(identity) == entries[1].objectKey(identity) {
		t.Errorf("two segments sharing a rotation filename map to one object key: %q", entries[0].objectKey(identity))
	}
}

func TestCaptureIndexAddRejectsNonPendingState(t *testing.T) {
	ix := newCaptureIndex()
	entry := testEntry("raylet.out.1", "0001780000000000000.a1b2c3d4e5f60718").withState(stateUploaded)

	if _, _, err := ix.add(inodeKey{Dev: 1, Ino: 42}, entry); err == nil {
		t.Error("add() accepted a capture that did not start as pending")
	}
}

func TestValidTransition(t *testing.T) {
	tests := []struct {
		from, to stagingState
		want     bool
	}{
		{from: statePending, to: stateUploaded, want: true},
		{from: statePending, to: statePending, want: false},
		{from: stateUploaded, to: statePending, want: false},
		{from: stateUploaded, to: stateUploaded, want: false},
	}
	for _, tt := range tests {
		if got := validTransition(tt.from, tt.to); got != tt.want {
			t.Errorf("validTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestCaptureReleasable(t *testing.T) {
	tests := []struct {
		name  string
		state stagingState
		nlink uint64
		want  bool
	}{
		{name: "uploaded and last link", state: stateUploaded, nlink: 1, want: true},
		{name: "uploaded but Ray still holds a link", state: stateUploaded, nlink: 2, want: false},
		// Releasing pending data would lose it: nothing has reached storage yet.
		{name: "pending and last link", state: statePending, nlink: 1, want: false},
		{name: "pending with Ray's link", state: statePending, nlink: 2, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &capture{Inode: inodeKey{Dev: 1, Ino: 42}, Entry: testEntry("raylet.out.1", "id").withState(tt.state)}
			if got := c.releasable(tt.nlink); got != tt.want {
				t.Errorf("releasable(nlink=%d) with state %q = %v, want %v", tt.nlink, tt.state, got, tt.want)
			}
		})
	}
}

func TestCaptureIndexRestore(t *testing.T) {
	ix := newCaptureIndex()
	key := inodeKey{Dev: 1, Ino: 42}
	entry := testEntry("raylet.out.1", "0001780000000000000.a1b2c3d4e5f60718").withState(stateUploaded)

	restored, err := ix.restore(key, entry)
	if err != nil {
		t.Fatalf("restore() error: %v", err)
	}
	if restored.Entry.State != stateUploaded {
		t.Errorf("restore() lost the durable state: %q", restored.Entry.State)
	}

	// Restoring the identical record twice is harmless. Anything that differs is
	// not: one inode staged under two records is a corrupt staging tree, and
	// accepting either would make the result depend on walk order.
	if _, err := ix.restore(key, entry); err != nil {
		t.Errorf("restore() rejected an identical entry: %v", err)
	}
	conflicts := map[string]stagedEntry{
		"different capture ID": testEntry("raylet.out.1", "0001780000000000009.cccccccccccccccc").withState(stateUploaded),
		// Same capture ID, but a leftover pending record from the same capture.
		"different state":         entry.withState(statePending),
		"different original name": testEntry("raylet.out.2", entry.CaptureID).withState(stateUploaded),
	}
	for name, conflicting := range conflicts {
		if _, err := ix.restore(key, conflicting); err == nil {
			t.Errorf("restore() accepted a conflicting record (%s) for one inode", name)
		}
	}
	if c, _ := ix.lookup(key); c.Entry != entry {
		t.Errorf("a rejected restore changed the tracked entry to %+v", c.Entry)
	}
	if _, err := ix.restore(inodeKey{Dev: 1, Ino: 43}, stagedEntry{State: "draft"}); err == nil {
		t.Error("restore() accepted an unknown staging state")
	}
}

func TestCaptureIndexRemove(t *testing.T) {
	ix := newCaptureIndex()
	key := inodeKey{Dev: 1, Ino: 42}
	if _, _, err := ix.add(key, testEntry("raylet.out.1", "0001780000000000000.a1b2c3d4e5f60718")); err != nil {
		t.Fatalf("add() error: %v", err)
	}

	ix.remove(key)
	if _, ok := ix.lookup(key); ok {
		t.Error("lookup() found a removed capture")
	}
	if ix.len() != 0 {
		t.Errorf("index holds %d captures after remove, want 0", ix.len())
	}
	ix.remove(key) // removing twice must not panic
}

func TestCaptureIndexObservedBases(t *testing.T) {
	ix := newCaptureIndex()
	const dirA, dirB = "/tmp/ray/session-1/logs", "/tmp/ray/session-1/logs/events"

	if ix.baseObserved(dirA, "raylet.out") {
		t.Error("baseObserved() reported an unseen base")
	}
	ix.observeBase(dirA, "raylet.out")
	if !ix.baseObserved(dirA, "raylet.out") {
		t.Error("baseObserved() forgot a recorded base")
	}
	// Bases are scoped per directory: Ray reuses names across subdirectories.
	if ix.baseObserved(dirB, "raylet.out") {
		t.Error("baseObserved() leaked a base across directories")
	}
	ix.observeBase(dirB, "raylet.out")
	if !ix.baseObserved(dirB, "raylet.out") {
		t.Error("baseObserved() forgot a base in a second directory")
	}
}

func TestCaptureIndexEntriesOrderedByCaptureID(t *testing.T) {
	ix := newCaptureIndex()
	ids := []string{
		"0001780000000000002.cccccccccccccccc",
		"0001780000000000000.aaaaaaaaaaaaaaaa",
		"0001780000000000001.bbbbbbbbbbbbbbbb",
	}
	inodes := []uint64{10, 11, 12}
	for i, id := range ids {
		if _, _, err := ix.add(inodeKey{Dev: 1, Ino: inodes[i]}, testEntry("raylet.out.1", id)); err != nil {
			t.Fatalf("add() error: %v", err)
		}
	}

	entries := ix.entries()
	if len(entries) != len(ids) {
		t.Fatalf("entries() returned %d entries, want %d", len(entries), len(ids))
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].CaptureID > entries[i].CaptureID {
			t.Errorf("entries() not ordered by capture ID: %q before %q", entries[i-1].CaptureID, entries[i].CaptureID)
		}
	}
}
