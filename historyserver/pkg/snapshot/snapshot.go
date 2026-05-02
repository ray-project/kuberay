// Package snapshot defines the SessionSnapshot format held in memory for
// each dead Ray session.
package snapshot

import (
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
)

// SessionSnapshot is the in-memory representation of a single dead Ray
// session's processed event state.
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
