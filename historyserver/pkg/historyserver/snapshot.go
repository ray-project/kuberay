package historyserver

import (
	"time"

	eventtypes "github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
)

// SessionSnapshot is the in-memory representation of a single dead Ray
// session's processed event state.
type SessionSnapshot struct {
	// SessionKey is "{name}_{ns}_{session}" (see utils.BuildClusterSessionKey).
	SessionKey string `json:"sessionKey"`
	// GeneratedAt is the UTC timestamp when this snapshot was built.
	GeneratedAt time.Time `json:"generatedAt"`

	Tasks     map[string][]eventtypes.Task `json:"tasks"`  // taskID -> []attempt
	Actors    map[string]eventtypes.Actor  `json:"actors"` // actorID -> Actor
	Jobs      map[string]eventtypes.Job    `json:"jobs"`   // jobID -> Job
	Nodes     map[string]eventtypes.Node   `json:"nodes"`  // nodeID -> Node
	LogEvents LogEventPayload              `json:"logEvents"`
}

// LogEventPayload carries log events grouped by job ID.
type LogEventPayload struct {
	ByJobID map[string][]eventtypes.LogEvent `json:"byJobId"`
}
