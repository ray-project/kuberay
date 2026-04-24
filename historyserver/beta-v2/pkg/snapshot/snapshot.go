// Package snapshot defines the SessionSnapshot format that the History Server
// v2 beta processor writes to object storage for each dead Ray session.
//
// A SessionSnapshot is the serialized form of what v1 keeps in per-pod in-memory
// maps (Tasks / Actors / Jobs / Nodes / LogEvents). Once a session's RayCluster
// CR is deleted, the processor builds a single immutable snapshot and persists
// it at {clusterNameID}/{session}/processed/session.json. History Server pods
// then serve that file as a stateless reader.
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
	// GeneratedAt is the UTC timestamp at which the processor built this snapshot.
	GeneratedAt time.Time `json:"generatedAt"`

	Tasks     map[string][]types.Task `json:"tasks"`     // taskID -> []attempt
	Actors    map[string]types.Actor  `json:"actors"`    // actorID -> Actor
	Jobs      map[string]types.Job    `json:"jobs"`      // jobID -> Job
	Nodes     map[string]types.Node   `json:"nodes"`     // nodeID -> Node
	LogEvents LogEventPayload         `json:"logEvents"` // for /events endpoint
}

// LogEventPayload carries the log events grouped by job_id. This mirrors the
// shape used by the v1 /events endpoint (Ray Dashboard compatibility).
type LogEventPayload struct {
	ByJobID map[string][]types.LogEvent `json:"byJobId"`
}

// Storage path convention constants for the processed snapshot file.
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
