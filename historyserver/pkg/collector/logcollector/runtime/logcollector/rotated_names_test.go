package logcollector

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixedClock returns a deterministic clock for capture ID tests.
func fixedClock(nanos int64) func() time.Time {
	return func() time.Time { return time.Unix(0, nanos) }
}

// testGenerator builds a capture ID generator with a fixed clock and a repeatable
// entropy source.
func testGenerator(nanos int64, entropy string) *captureIDGenerator {
	return &captureIDGenerator{now: fixedClock(nanos), rand: strings.NewReader(entropy)}
}

func TestParseBackupName(t *testing.T) {
	tests := []struct {
		name      string
		wantBase  string
		wantIndex int
		wantOK    bool
	}{
		// Ray's single rotation convention: the complete active name plus an index.
		{name: "worker-abc-01000000-123.out.1", wantBase: "worker-abc-01000000-123.out", wantIndex: 1, wantOK: true},
		{name: "worker-abc-01000000-123.err.2", wantBase: "worker-abc-01000000-123.err", wantIndex: 2, wantOK: true},
		{name: "raylet.out.1", wantBase: "raylet.out", wantIndex: 1, wantOK: true},
		{name: "gcs_server.err.5", wantBase: "gcs_server.err", wantIndex: 5, wantOK: true},
		{name: "dashboard.log.1", wantBase: "dashboard.log", wantIndex: 1, wantOK: true},
		{name: "event_EXPORT_TASK.log.12", wantBase: "event_EXPORT_TASK.log", wantIndex: 12, wantOK: true},
		// Ray patches spdlog so that the index never lands before the extension;
		// "raylet.1.out" is not a rotation backup and must not be captured as one.
		{name: "raylet.1.out", wantOK: false},
		{name: "worker-abc-01000000-123.1.err", wantOK: false},
		// Active files are never backups.
		{name: "raylet.out", wantOK: false},
		{name: "dashboard.log", wantOK: false},
		// Rejected numeric forms.
		{name: "raylet.out.0", wantOK: false},
		{name: "raylet.out.01", wantOK: false},
		{name: "raylet.out.-1", wantOK: false},
		{name: "raylet.out.1a", wantOK: false},
		{name: "raylet.out.", wantOK: false},
		{name: ".1", wantOK: false},
		{name: "1", wantOK: false},
		{name: "", wantOK: false},
		// Ray does not cap the backup count, so a large index stays valid.
		{name: "raylet.out.4096", wantBase: "raylet.out", wantIndex: 4096, wantOK: true},
		// An index too large for an int cannot come from a rotation cascade.
		{name: "raylet.out.99999999999999999999999", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, index, ok := parseBackupName(tt.name)
			if ok != tt.wantOK {
				t.Fatalf("parseBackupName(%q) ok = %v, want %v", tt.name, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if base != tt.wantBase || index != tt.wantIndex {
				t.Errorf("parseBackupName(%q) = (%q, %d), want (%q, %d)", tt.name, base, index, tt.wantBase, tt.wantIndex)
			}
		})
	}
}

func TestEvaluateCandidate(t *testing.T) {
	knownBase := func(_, base string) bool { return base == "raylet.out" }

	tests := []struct {
		name       string
		fileName   string
		mode       fs.FileMode
		baseKnown  baseKnownFunc
		wantStatus candidateStatus
	}{
		{name: "regular backup with known base", fileName: "raylet.out.1", mode: 0, baseKnown: knownBase, wantStatus: candidateEligible},
		{name: "not a backup name", fileName: "raylet.out", mode: 0, baseKnown: knownBase, wantStatus: candidateNotBackupName},
		{name: "directory", fileName: "raylet.out.1", mode: fs.ModeDir, baseKnown: knownBase, wantStatus: candidateNotRegular},
		{name: "symlink", fileName: "raylet.out.1", mode: fs.ModeSymlink, baseKnown: knownBase, wantStatus: candidateNotRegular},
		{name: "socket", fileName: "raylet.out.1", mode: fs.ModeSocket, baseKnown: knownBase, wantStatus: candidateNotRegular},
		{name: "unknown base", fileName: "user-data.1", mode: 0, baseKnown: knownBase, wantStatus: candidateUnknownBase},
		{name: "no base oracle", fileName: "raylet.out.1", mode: 0, baseKnown: nil, wantStatus: candidateUnknownBase},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := evaluateCandidate("/tmp/ray/session/logs", tt.fileName, tt.mode, tt.baseKnown)
			if status != tt.wantStatus {
				t.Errorf("evaluateCandidate(%q) = %v, want %v", tt.fileName, status, tt.wantStatus)
			}
		})
	}
}

func TestCaptureIDFormatAndUniqueness(t *testing.T) {
	g := newCaptureIDGenerator()

	const iterations = 2000
	seen := make(map[string]struct{}, iterations)
	for range iterations {
		id, err := g.next()
		if err != nil {
			t.Fatalf("next() error: %v", err)
		}
		if !captureIDRe.MatchString(id) {
			t.Fatalf("capture ID %q does not match %v", id, captureIDRe)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("capture ID %q generated twice", id)
		}
		seen[id] = struct{}{}
	}
}

func TestCaptureIDSurvivesRestart(t *testing.T) {
	// Two collector instances that restart within the same nanosecond must not mint
	// the same ID: a reused ID would overwrite the earlier run's object.
	const sameNanos = int64(1780000000000000000)
	first := &captureIDGenerator{now: fixedClock(sameNanos), rand: newCaptureIDGenerator().rand}
	second := &captureIDGenerator{now: fixedClock(sameNanos), rand: newCaptureIDGenerator().rand}

	idA, err := first.next()
	if err != nil {
		t.Fatalf("first.next() error: %v", err)
	}
	idB, err := second.next()
	if err != nil {
		t.Fatalf("second.next() error: %v", err)
	}
	if idA == idB {
		t.Errorf("restarted collectors reused capture ID %q", idA)
	}

	// A clock that steps backwards must not collide either: the random half differs.
	rewound := testGenerator(sameNanos-1_000_000, "entropy!")
	idC, err := rewound.next()
	if err != nil {
		t.Fatalf("rewound.next() error: %v", err)
	}
	if idC == idA || idC == idB {
		t.Errorf("capture ID %q collided after the clock stepped backwards", idC)
	}
}

func TestCaptureIDGenerationErrorPropagates(t *testing.T) {
	wantErr := errors.New("entropy source unavailable")
	g := &captureIDGenerator{now: time.Now, rand: failingReader{err: wantErr}}

	id, err := g.next()
	if err == nil {
		t.Fatalf("next() = %q, want error", id)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("next() error = %v, want it to wrap %v", err, wantErr)
	}
	if id != "" {
		t.Errorf("next() returned ID %q alongside an error", id)
	}

	// A truncated read is a failure too: a short ID would weaken uniqueness.
	short := &captureIDGenerator{now: time.Now, rand: bytes.NewReader([]byte{1, 2, 3})}
	if _, err := short.next(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("short read error = %v, want io.ErrUnexpectedEOF", err)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestCaptureFileNameRoundTrip(t *testing.T) {
	const id = "0001780000000000000.a1b2c3d4e5f60718"

	tests := []string{
		"raylet.out.1",
		"worker-abc-01000000-123.out.2",
		"dashboard.log.1",
		// An original name that itself contains the separator must round-trip.
		"weird.rotated.name.log.3",
	}

	for _, original := range tests {
		t.Run(original, func(t *testing.T) {
			fileName := captureFileName(original, id)
			if !strings.HasPrefix(fileName, original) {
				t.Errorf("captureFileName(%q) = %q, want it to keep the original name readable", original, fileName)
			}
			gotName, gotID, ok := parseCaptureFileName(fileName)
			if !ok {
				t.Fatalf("parseCaptureFileName(%q) not recognized", fileName)
			}
			if gotName != original || gotID != id {
				t.Errorf("parseCaptureFileName(%q) = (%q, %q), want (%q, %q)", fileName, gotName, gotID, original, id)
			}
		})
	}

	rejected := []string{
		"raylet.out.1",                                    // no capture ID
		"raylet.out.1.rotated.",                           // empty ID
		"raylet.out.1.rotated.not-an-id",                  // malformed ID
		".rotated.0001780000000000000.a1b2c3d4e5f60718",   // no original name
		"raylet.out.1.rotated.0001780000000000000.a1b2c3", // truncated ID
	}
	for _, fileName := range rejected {
		if _, _, ok := parseCaptureFileName(fileName); ok {
			t.Errorf("parseCaptureFileName(%q) accepted a malformed name", fileName)
		}
	}
}

func TestObjectKeyIsFlatAndOwnerAware(t *testing.T) {
	const id = "0001780000000000000.a1b2c3d4e5f60718"

	tests := []struct {
		name     string
		identity clusterIdentity
		relDir   string
		original string
		want     string
	}{
		{
			name:     "raycluster keeps session/node/logs ordering",
			identity: clusterIdentity{RootDir: "root", Namespace: "default", ClusterName: "my-cluster"},
			original: "worker-abc.out.1",
			want:     "root/cluster-history/raycluster/default/my-cluster/session-1/node-1/logs/worker-abc.out.1.rotated." + id,
		},
		{
			name:     "nested relative directory is preserved",
			identity: clusterIdentity{RootDir: "root", Namespace: "default", ClusterName: "my-cluster"},
			relDir:   "events",
			original: "event_EXPORT_TASK.log.2",
			want:     "root/cluster-history/raycluster/default/my-cluster/session-1/node-1/logs/events/event_EXPORT_TASK.log.2.rotated." + id,
		},
		{
			name: "rayjob nests under the owner name",
			identity: clusterIdentity{
				RootDir: "root", OwnerKind: "rayjob", OwnerName: "job-1",
				Namespace: "default", ClusterName: "my-cluster",
			},
			original: "raylet.out.1",
			want:     "root/cluster-history/rayjob/default/job-1/my-cluster/session-1/node-1/logs/raylet.out.1.rotated." + id,
		},
		{
			name: "rayservice nests under the owner name",
			identity: clusterIdentity{
				RootDir: "root", OwnerKind: "rayservice", OwnerName: "svc-1",
				Namespace: "default", ClusterName: "my-cluster",
			},
			original: "raylet.out.1",
			want:     "root/cluster-history/rayservice/default/svc-1/my-cluster/session-1/node-1/logs/raylet.out.1.rotated." + id,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.identity.objectKey("session-1", "node-1", tt.relDir, tt.original, id)
			if got != tt.want {
				t.Errorf("objectKey() = %q, want %q", got, tt.want)
			}
			// The captured object must sit in the node's own logs directory, so the
			// History Server's non-recursive listing can see it.
			if strings.Contains(got, "/rotated/") {
				t.Errorf("objectKey() = %q, want no extra directory level", got)
			}
		})
	}
}

func TestStagedPathRoundTrip(t *testing.T) {
	root := filepath.Join("/tmp", "ray", "rotated-staging")

	tests := []struct {
		name  string
		entry stagedEntry
		want  string
	}{
		{
			name: "pending at the top level",
			entry: stagedEntry{
				State: statePending, SessionName: "session-1", NodeName: "node-1",
				OriginalName: "raylet.out.1", CaptureID: "0001780000000000000.a1b2c3d4e5f60718",
			},
			want: root + "/session-1/node-1/pending/raylet.out.1.rotated.0001780000000000000.a1b2c3d4e5f60718",
		},
		{
			name: "uploaded in a nested directory",
			entry: stagedEntry{
				State: stateUploaded, SessionName: "session-1", NodeName: "node-1", RelDir: "events/subdir",
				OriginalName: "event.log.3", CaptureID: "0001780000000000001.00ff00ff00ff00ff",
			},
			want: root + "/session-1/node-1/uploaded/events/subdir/event.log.3.rotated.0001780000000000001.00ff00ff00ff00ff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.entry.path(root)
			if got != filepath.FromSlash(tt.want) {
				t.Fatalf("path() = %q, want %q", got, tt.want)
			}
			parsed, err := parseStagedPath(root, got)
			if err != nil {
				t.Fatalf("parseStagedPath(%q) error: %v", got, err)
			}
			if parsed != tt.entry {
				t.Errorf("parseStagedPath() = %+v, want %+v", parsed, tt.entry)
			}
		})
	}
}

func TestStagedPathTransitionPreservesIdentity(t *testing.T) {
	root := filepath.Join("/tmp", "ray", "rotated-staging")
	pending := stagedEntry{
		State: statePending, SessionName: "session-1", NodeName: "node-1", RelDir: "events",
		OriginalName: "event.log.1", CaptureID: "0001780000000000000.a1b2c3d4e5f60718",
	}

	uploaded := pending.withState(stateUploaded)
	if uploaded.CaptureID != pending.CaptureID || uploaded.OriginalName != pending.OriginalName ||
		uploaded.RelDir != pending.RelDir || uploaded.SessionName != pending.SessionName ||
		uploaded.NodeName != pending.NodeName {
		t.Fatalf("withState() changed capture identity: %+v -> %+v", pending, uploaded)
	}
	if pending.State != statePending {
		t.Errorf("withState() mutated the receiver: %+v", pending)
	}

	// Only the state segment of the path differs, so the object key cannot drift.
	identity := clusterIdentity{RootDir: "root", Namespace: "default", ClusterName: "my-cluster"}
	if pending.objectKey(identity) != uploaded.objectKey(identity) {
		t.Errorf("object key changed across states: %q vs %q", pending.objectKey(identity), uploaded.objectKey(identity))
	}
	if strings.Replace(pending.path(root), string(statePending), string(stateUploaded), 1) != uploaded.path(root) {
		t.Errorf("staging path changed by more than its state segment: %q vs %q", pending.path(root), uploaded.path(root))
	}
}

func TestParseStagedPathRejectsMalformed(t *testing.T) {
	root := filepath.Join("/tmp", "ray", "rotated-staging")
	const leaf = "raylet.out.1.rotated.0001780000000000000.a1b2c3d4e5f60718"

	tests := []struct {
		name string
		path string
	}{
		{name: "too shallow", path: filepath.Join(root, "session-1", leaf)},
		{name: "unknown state", path: filepath.Join(root, "session-1", "node-1", "draft", leaf)},
		{name: "missing capture ID", path: filepath.Join(root, "session-1", "node-1", "pending", "raylet.out.1")},
		{name: "outside staging root", path: filepath.Join("/tmp", "ray", "prev-logs", "session-1", "node-1", "pending", leaf)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if entry, err := parseStagedPath(root, tt.path); err == nil {
				t.Errorf("parseStagedPath(%q) = %+v, want error", tt.path, entry)
			}
		})
	}
}

func TestRelDirFor(t *testing.T) {
	logsDir := filepath.Join("/tmp", "ray", "session_2026-07-31", "logs")

	tests := []struct {
		name    string
		file    string
		want    string
		wantErr bool
	}{
		{name: "top level", file: filepath.Join(logsDir, "raylet.out.1"), want: ""},
		{name: "nested", file: filepath.Join(logsDir, "events", "event.log.1"), want: "events"},
		{name: "deeply nested", file: filepath.Join(logsDir, "serve", "replica", "r.log.1"), want: "serve/replica"},
		{name: "escapes logs dir", file: filepath.Join("/tmp", "ray", "elsewhere", "raylet.out.1"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := relDirFor(logsDir, tt.file)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("relDirFor(%q) = %q, want error", tt.file, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("relDirFor(%q) error: %v", tt.file, err)
			}
			if got != tt.want {
				t.Errorf("relDirFor(%q) = %q, want %q", tt.file, got, tt.want)
			}
		})
	}
}

func TestNewStagedEntryRejectsUnsafeComponents(t *testing.T) {
	const goodID = "0001780000000000000.a1b2c3d4e5f60718"

	tests := []struct {
		name         string
		state        stagingState
		sessionName  string
		nodeName     string
		relDir       string
		originalName string
		captureID    string
		wantErr      bool
	}{
		{name: "valid", state: statePending, sessionName: "session-1", nodeName: "node-1", originalName: "raylet.out.1", captureID: goodID},
		{name: "valid nested", state: stateUploaded, sessionName: "session-1", nodeName: "node-1", relDir: "events/subdir", originalName: "e.log.1", captureID: goodID},
		{name: "unknown state", state: "draft", sessionName: "session-1", nodeName: "node-1", originalName: "raylet.out.1", captureID: goodID, wantErr: true},
		{name: "empty session", sessionName: "", nodeName: "node-1", state: statePending, originalName: "raylet.out.1", captureID: goodID, wantErr: true},
		{name: "session traversal", sessionName: "..", nodeName: "node-1", state: statePending, originalName: "raylet.out.1", captureID: goodID, wantErr: true},
		{name: "session with separator", sessionName: "a/b", nodeName: "node-1", state: statePending, originalName: "raylet.out.1", captureID: goodID, wantErr: true},
		{name: "node traversal", sessionName: "session-1", nodeName: "../../etc", state: statePending, originalName: "raylet.out.1", captureID: goodID, wantErr: true},
		{name: "name traversal", sessionName: "session-1", nodeName: "node-1", state: statePending, originalName: "../raylet.out.1", captureID: goodID, wantErr: true},
		{name: "empty name", sessionName: "session-1", nodeName: "node-1", state: statePending, originalName: "", captureID: goodID, wantErr: true},
		{name: "absolute relDir", sessionName: "session-1", nodeName: "node-1", relDir: "/etc", state: statePending, originalName: "raylet.out.1", captureID: goodID, wantErr: true},
		{name: "relDir traversal", sessionName: "session-1", nodeName: "node-1", relDir: "../../..", state: statePending, originalName: "raylet.out.1", captureID: goodID, wantErr: true},
		{name: "relDir hidden traversal", sessionName: "session-1", nodeName: "node-1", relDir: "events/../../..", state: statePending, originalName: "raylet.out.1", captureID: goodID, wantErr: true},
		{name: "relDir unclean", sessionName: "session-1", nodeName: "node-1", relDir: "./events", state: statePending, originalName: "raylet.out.1", captureID: goodID, wantErr: true},
		{name: "relDir trailing slash", sessionName: "session-1", nodeName: "node-1", relDir: "events/", state: statePending, originalName: "raylet.out.1", captureID: goodID, wantErr: true},
		// A capture ID that was never generated must not reach the staging volume.
		{name: "empty capture ID", sessionName: "session-1", nodeName: "node-1", state: statePending, originalName: "raylet.out.1", captureID: "", wantErr: true},
		{name: "malformed capture ID", sessionName: "session-1", nodeName: "node-1", state: statePending, originalName: "raylet.out.1", captureID: "rot7", wantErr: true},
		{name: "capture ID with traversal", sessionName: "session-1", nodeName: "node-1", state: statePending, originalName: "raylet.out.1", captureID: "../../etc/passwd", wantErr: true},
	}

	stagingRoot := filepath.Join("/tmp", "ray", "rotated-staging")
	identity := clusterIdentity{RootDir: "root", Namespace: "default", ClusterName: "my-cluster"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, err := newStagedEntry(tt.state, tt.sessionName, tt.nodeName, tt.relDir, tt.originalName, tt.captureID)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("newStagedEntry() = %+v, want error", entry)
				}
				return
			}
			if err != nil {
				t.Fatalf("newStagedEntry() error: %v", err)
			}
			// Every accepted entry must stay inside both roots it is joined into.
			if got := entry.path(stagingRoot); !strings.HasPrefix(got, stagingRoot+string(filepath.Separator)) {
				t.Errorf("path() = %q, want it under %q", got, stagingRoot)
			}
			if got := entry.objectKey(identity); !strings.HasPrefix(got, "root/cluster-history/") {
				t.Errorf("objectKey() = %q, want it under the cluster prefix", got)
			}
		})
	}
}

func TestCaptureFileNameRoundTripsNestedSeparator(t *testing.T) {
	// A previously captured name fed back through capture must still split at the
	// last separator, so identity cannot drift.
	const innerID = "0001780000000000000.a1b2c3d4e5f60718"
	const outerID = "0001780000000000001.00ff00ff00ff00ff"

	original := captureFileName("raylet.out.1", innerID)
	fileName := captureFileName(original, outerID)

	gotName, gotID, ok := parseCaptureFileName(fileName)
	if !ok {
		t.Fatalf("parseCaptureFileName(%q) not recognized", fileName)
	}
	if gotID != outerID {
		t.Errorf("capture ID = %q, want %q", gotID, outerID)
	}
	if gotName != original {
		t.Errorf("original name = %q, want %q", gotName, original)
	}
}
