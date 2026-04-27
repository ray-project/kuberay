// Package snapshot defines the SessionSnapshot format persisted to object
// storage for each dead Ray session.
//
// A SessionSnapshot is the immutable, serialized state of a session's
// processed events; History Server pods serve it as a stateless reader.
package snapshot

import (
	"path"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
)

// SessionSnapshot is the persisted, stateless representation of a single
// dead Ray session's processed event state.
type SessionSnapshot struct {
	// SessionKey is "{name}_{ns}_{session}" (see utils.BuildClusterSessionKey).
	SessionKey string `json:"sessionKey"`
	// GeneratedAt is the UTC timestamp when this snapshot was built.
	GeneratedAt time.Time `json:"generatedAt"`

	Tasks     map[string][]types.Task `json:"tasks"`  // taskID -> []attempt
	Actors    map[string]types.Actor  `json:"actors"` // actorID -> Actor
	Jobs      map[string]types.Job    `json:"jobs"`   // jobID -> Job
	Nodes     map[string]types.Node   `json:"nodes"`  // nodeID -> Node
	LogEvents LogEventPayload         `json:"logEvents"`
}

// LogEventPayload carries log events grouped by job ID.
type LogEventPayload struct {
	ByJobID map[string][]types.LogEvent `json:"byJobId"`
}

// Path constants for the processed snapshot file.
const (
	ProcessedDir = "processed"
	SnapshotFile = "session.json"
)

// SnapshotPath returns the path (relative to the cluster root "{name}_{ns}/")
// at which a session's snapshot file lives:
//
//	{sessionName}/processed/session.json
func SnapshotPath(sessionName string) string {
	return path.Join(sessionName, ProcessedDir, SnapshotFile)
}
