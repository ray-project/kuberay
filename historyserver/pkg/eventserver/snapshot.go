package eventserver

import (
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/eventserver/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// SessionSnapshot is the in-memory representation of a dead session's processed event state.
type SessionSnapshot struct {
	SessionKey string `json:"sessionKey"`
	// GeneratedAt is the UTC timestamp when this snapshot was built.
	GeneratedAt time.Time `json:"generatedAt"`

	Tasks     map[string][]types.Task `json:"tasks"`
	Actors    map[string]types.Actor  `json:"actors"`
	Jobs      map[string]types.Job    `json:"jobs"`
	Nodes     map[string]types.Node   `json:"nodes"`
	LogEvents LogEventPayload         `json:"logEvents"`
}

// LogEventPayload carries log events grouped by job ID.
type LogEventPayload struct {
	ByJobID map[string][]types.LogEvent `json:"byJobId"`
}

// BuildSnapshot builds a SessionSnapshot from the handler's per-session state.
func (h *EventHandler) BuildSnapshot(session utils.ClusterInfo) *SessionSnapshot {
	clusterSessionKey := utils.BuildClusterSessionKey(session.Name, session.Namespace, session.SessionName)
	return &SessionSnapshot{
		SessionKey:  clusterSessionKey,
		GeneratedAt: time.Now().UTC(),
		Tasks:       groupTasksByID(h.getTasks(clusterSessionKey)),
		Actors:      h.getActorsMap(clusterSessionKey),
		Jobs:        h.getJobsMap(clusterSessionKey),
		Nodes:       h.getNodeMap(clusterSessionKey),
		LogEvents: LogEventPayload{
			ByJobID: h.ClusterLogEventMap.GetRawEventsByJobID(clusterSessionKey),
		},
	}
}

// groupTasksByID re-nests the flat []Task returned by getTasks into the
// map[taskID][]attempt shape expected by SessionSnapshot.Tasks.
func groupTasksByID(tasks []types.Task) map[string][]types.Task {
	if len(tasks) == 0 {
		return map[string][]types.Task{}
	}
	out := make(map[string][]types.Task, len(tasks))
	for _, t := range tasks {
		out[t.TaskID] = append(out[t.TaskID], t)
	}
	return out
}
