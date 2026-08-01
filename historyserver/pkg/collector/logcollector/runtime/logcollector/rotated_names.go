package logcollector

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/storage/clusterlogs"
)

const (
	// captureIDSeparator joins a rotated file's original name to its capture ID in
	// both staging file names and storage object keys.
	captureIDSeparator = ".rotated."

	// captureIDRandomBytes is how much crypto/rand entropy each capture ID carries.
	captureIDRandomBytes = 8

	// captureIDNanosDigits zero-pads the timestamp so capture IDs sort in capture
	// order for the lifetime of int64 nanoseconds.
	captureIDNanosDigits = 19
)

// rotationBackupRe matches a Ray rotation backup: the complete name of the active
// file followed by a positive index.
//
// Ray has a single rotation naming convention. Python's RotatingFileHandler appends
// ".1", ".2", ... and Ray patches spdlog to agree
// (thirdparty/patches/spdlog-rotation-file-format.patch rewrites "mylog.3.txt" to
// "mylog.txt.3"), so C++ components rotate the same way: worker-abc.out.1,
// raylet.out.1, dashboard.log.1.
var rotationBackupRe = regexp.MustCompile(`^(.+)\.([1-9][0-9]*)$`)

// captureIDRe matches the format produced by captureIDGenerator.next.
var captureIDRe = regexp.MustCompile(`^[0-9]{19}\.[0-9a-f]{16}$`)

// parseBackupName splits a rotation backup's basename into the name of the active
// file it rotated from, plus its backup index.
//
// Ray imposes no upper bound on RAY_ROTATION_BACKUP_COUNT, so no cap is applied
// here; the only rejected numeric form is one too large for an int, which no
// rotation cascade can produce.
func parseBackupName(name string) (base string, index int, ok bool) {
	m := rotationBackupRe.FindStringSubmatch(name)
	if m == nil {
		return "", 0, false
	}
	index, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], index, true
}

// candidateStatus explains why a directory entry is or is not capturable.
type candidateStatus int

const (
	candidateEligible candidateStatus = iota
	candidateNotBackupName
	candidateNotRegular
	candidateUnknownBase
)

func (s candidateStatus) String() string {
	switch s {
	case candidateEligible:
		return "eligible"
	case candidateNotBackupName:
		return "not a rotation backup name"
	case candidateNotRegular:
		return "not a regular file"
	case candidateUnknownBase:
		return "no known active base file"
	default:
		return fmt.Sprintf("unknown status %d", int(s))
	}
}

// baseKnownFunc reports whether base is, or recently was, the active file that
// backups in dir rotate from.
type baseKnownFunc func(dir, base string) bool

// evaluateCandidate decides whether dir/name is a Ray rotation backup the collector
// may capture. mode must come from an Lstat, so that symlinks are rejected instead
// of followed.
//
// Requiring a known active base is what keeps unrelated files that merely end in
// ".<digits>" out of the capture path: rotation always leaves the active file in
// place. The "recently observed" half of baseKnownFunc matters because a rotation
// cascade briefly unlinks the active name before the writer recreates it.
func evaluateCandidate(dir, name string, mode fs.FileMode, baseKnown baseKnownFunc) candidateStatus {
	base, _, ok := parseBackupName(name)
	if !ok {
		return candidateNotBackupName
	}
	if !mode.IsRegular() {
		return candidateNotRegular
	}
	if baseKnown == nil || !baseKnown(dir, base) {
		return candidateUnknownBase
	}
	return candidateEligible
}

// captureIDGenerator mints capture IDs. A rotation filename such as "raylet.out.1"
// is reused by every later segment, so the ID — not the filename and not the inode
// — is a captured segment's permanent identity.
//
// IDs must stay unique across collector restarts within a session: a restart that
// reset a counter would mint an ID already used by an earlier object and overwrite
// it. Wall-clock nanoseconds plus 64 bits of crypto/rand survive both a restart and
// a clock that steps backwards.
type captureIDGenerator struct {
	now  func() time.Time
	rand io.Reader
}

func newCaptureIDGenerator() *captureIDGenerator {
	return &captureIDGenerator{now: time.Now, rand: rand.Reader}
}

// next returns a fresh capture ID, or an error if the entropy source fails. A
// failure must abort the capture rather than fall back to a predictable ID.
func (g *captureIDGenerator) next() (string, error) {
	b := make([]byte, captureIDRandomBytes)
	if _, err := io.ReadFull(g.rand, b); err != nil {
		return "", fmt.Errorf("generate capture ID: read %d random bytes: %w", captureIDRandomBytes, err)
	}
	return fmt.Sprintf("%0*d.%s", captureIDNanosDigits, g.now().UnixNano(), hex.EncodeToString(b)), nil
}

// captureFileName is the leaf name a captured backup takes in staging and in
// storage: the original rotation name, so operators can still read it, plus the
// capture ID that makes it unique across segments.
func captureFileName(originalName, captureID string) string {
	return originalName + captureIDSeparator + captureID
}

// parseCaptureFileName is the inverse of captureFileName. It splits on the last
// separator so that an original name containing ".rotated." round-trips.
func parseCaptureFileName(fileName string) (originalName, captureID string, ok bool) {
	i := strings.LastIndex(fileName, captureIDSeparator)
	if i <= 0 {
		return "", "", false
	}
	originalName = fileName[:i]
	captureID = fileName[i+len(captureIDSeparator):]
	if !captureIDRe.MatchString(captureID) {
		return "", "", false
	}
	return originalName, captureID, true
}

// clusterIdentity carries every value clusterlogs.LogsDir needs. RayJob and
// RayService clusters nest their logs under the owner name while RayCluster ones do
// not, so dropping the owner fields would silently write to the wrong prefix.
type clusterIdentity struct {
	RootDir     string
	OwnerKind   string
	OwnerName   string
	Namespace   string
	ClusterName string
}

// logsPrefix is the node's log directory in storage: every object this collector
// writes for that node lives under it, and nothing it writes may escape it.
func (c clusterIdentity) logsPrefix(sessionName, nodeName string) string {
	return clusterlogs.LogsDir(c.RootDir, c.OwnerKind, c.OwnerName, c.Namespace, c.ClusterName, sessionName, nodeName)
}

// objectKey returns the storage key for a captured backup. Keys stay flat — one
// object per captured segment directly beside the node's other logs — because the
// History Server lists a node's logs non-recursively unless the caller passes a
// "**" glob, so anything nested under an extra directory would be invisible to the
// ordinary listing.
//
// relDir is the one exception, and it is not the capture's doing: Ray already nests
// some logs (events/, serve/), and the legacy shutdown upload mirrors that same
// structure, so a captured segment lands exactly where its uncaptured siblings do.
func (c clusterIdentity) objectKey(sessionName, nodeName, relDir, originalName, captureID string) string {
	return path.Join(c.logsPrefix(sessionName, nodeName), relDir, captureFileName(originalName, captureID))
}

// stagingState is the durable, on-disk record of how far a capture has progressed.
// It is a directory level rather than in-memory bookkeeping so that a collector
// restart can tell an unsent capture from an already-uploaded one.
type stagingState string

const (
	statePending  stagingState = "pending"
	stateUploaded stagingState = "uploaded"
)

func validStagingState(s stagingState) bool {
	return s == statePending || s == stateUploaded
}

// stagedEntry identifies one captured backup on the staging volume:
//
//	<staging root>/<session>/<node>/<state>/<relative dir>/<name>.rotated.<capture ID>
//
// Build entries with newStagedEntry: path and objectKey join these fields into
// filesystem and object paths, so an unvalidated field would be a traversal.
type stagedEntry struct {
	State        stagingState
	SessionName  string
	NodeName     string
	RelDir       string // slash-separated, empty when the backup sits directly in logs/
	OriginalName string // the rotation name at capture time, e.g. "worker-abc.out.1"
	CaptureID    string
}

// newStagedEntry validates every component that ends up in a path. It is the only
// place a stagedEntry should be constructed outside of parsing.
func newStagedEntry(state stagingState, sessionName, nodeName, relDir, originalName, captureID string) (stagedEntry, error) {
	if !validStagingState(state) {
		return stagedEntry{}, fmt.Errorf("staged entry: unknown state %q", state)
	}
	for _, f := range []struct{ label, value string }{
		{"session name", sessionName},
		{"node name", nodeName},
		{"original name", originalName},
	} {
		if err := validatePathSegment(f.label, f.value); err != nil {
			return stagedEntry{}, err
		}
	}
	if err := validateRelDir(relDir); err != nil {
		return stagedEntry{}, err
	}
	// A capture without a well-formed ID has no stable identity, so it must never
	// reach the staging volume: a failed ID generation has to abort the capture.
	if !captureIDRe.MatchString(captureID) {
		return stagedEntry{}, fmt.Errorf("staged entry: malformed capture ID %q", captureID)
	}
	return stagedEntry{
		State:        state,
		SessionName:  sessionName,
		NodeName:     nodeName,
		RelDir:       relDir,
		OriginalName: originalName,
		CaptureID:    captureID,
	}, nil
}

// validatePathSegment rejects anything that would climb out of, or disappear from,
// the directory it is joined into.
func validatePathSegment(label, value string) error {
	if value == "" {
		return fmt.Errorf("staged entry: empty %s", label)
	}
	if value != path.Base(value) || value == "." || value == ".." {
		return fmt.Errorf("staged entry: %s %q must be a single path segment", label, value)
	}
	return nil
}

// validateRelDir accepts only a clean, relative, slash-separated directory. Ray
// nests some logs (events/, serve/), and that structure is mirrored into staging
// and storage, so it has to be reproduced exactly and without traversal.
func validateRelDir(relDir string) error {
	if relDir == "" {
		return nil
	}
	if path.IsAbs(relDir) || filepath.IsAbs(relDir) {
		return fmt.Errorf("staged entry: relative directory %q must not be absolute", relDir)
	}
	if path.Clean(relDir) != relDir {
		return fmt.Errorf("staged entry: relative directory %q must be clean", relDir)
	}
	for seg := range strings.SplitSeq(relDir, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("staged entry: relative directory %q must not contain %q", relDir, seg)
		}
	}
	return nil
}

// path returns the entry's absolute location under stagingRoot.
func (e stagedEntry) path(stagingRoot string) string {
	return filepath.Join(
		stagingRoot,
		e.SessionName,
		e.NodeName,
		string(e.State),
		filepath.FromSlash(e.RelDir),
		captureFileName(e.OriginalName, e.CaptureID),
	)
}

// withState returns a copy of the entry in a different staging state, preserving
// capture identity.
func (e stagedEntry) withState(s stagingState) stagedEntry {
	e.State = s
	return e
}

// objectKey returns where this entry belongs in storage.
func (e stagedEntry) objectKey(c clusterIdentity) string {
	return c.objectKey(e.SessionName, e.NodeName, e.RelDir, e.OriginalName, e.CaptureID)
}

// parseStagedPath reconstructs an entry from a path found on the staging volume,
// which is how a restarting collector recovers both capture identity and upload
// state without re-deriving either.
func parseStagedPath(stagingRoot, absPath string) (stagedEntry, error) {
	rel, err := filepath.Rel(stagingRoot, absPath)
	if err != nil {
		return stagedEntry{}, fmt.Errorf("staging path %s is not under %s: %w", absPath, stagingRoot, err)
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 4 || parts[0] == ".." {
		return stagedEntry{}, fmt.Errorf("staging path %s is not <session>/<node>/<state>/<name>", absPath)
	}

	originalName, captureID, ok := parseCaptureFileName(parts[len(parts)-1])
	if !ok {
		return stagedEntry{}, fmt.Errorf("staging path %s does not end in <name>%s<capture ID>", absPath, captureIDSeparator)
	}

	entry, err := newStagedEntry(
		stagingState(parts[2]),
		parts[0],
		parts[1],
		path.Join(parts[3:len(parts)-1]...),
		originalName,
		captureID,
	)
	if err != nil {
		return stagedEntry{}, fmt.Errorf("staging path %s: %w", absPath, err)
	}
	return entry, nil
}

// relDirFor returns the slash-separated directory of a log file relative to the
// session's logs directory, empty when the file sits directly in it. Ray nests some
// logs (events/, serve/), and that structure is preserved in both staging and
// storage.
func relDirFor(logsDir, filePath string) (string, error) {
	rel, err := filepath.Rel(logsDir, filepath.Dir(filePath))
	if err != nil {
		return "", fmt.Errorf("relative directory of %s under %s: %w", filePath, logsDir, err)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return "", nil
	}
	if err := validateRelDir(rel); err != nil {
		return "", fmt.Errorf("log file %s is not safely under logs directory %s: %w", filePath, logsDir, err)
	}
	return rel, nil
}
