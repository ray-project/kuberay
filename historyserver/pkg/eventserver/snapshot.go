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

	Tasks     []types.Task           `json:"tasks"`
	Actors    map[string]types.Actor `json:"actors"`
	Jobs      map[string]types.Job   `json:"jobs"`
	Nodes     map[string]types.Node  `json:"nodes"`
	LogEvents LogEventPayload        `json:"logEvents"`
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
		Tasks:       h.getTasks(clusterSessionKey),
		Actors:      h.getActorsMap(clusterSessionKey),
		Jobs:        h.getJobsMap(clusterSessionKey),
		Nodes:       h.getNodeMap(clusterSessionKey),
		LogEvents: LogEventPayload{
			ByJobID: h.ClusterLogEventMap.GetRawEventsByJobID(clusterSessionKey),
		},
	}
}

// TaskAttemptsByID returns all attempts for taskID from a flat task list.
func TaskAttemptsByID(tasks []types.Task, taskID string) ([]types.Task, bool) {
	var attempts []types.Task
	for _, t := range tasks {
		if t.TaskID == taskID {
			attempts = append(attempts, t)
		}
	}
	if len(attempts) == 0 {
		return nil, false
	}
	return attempts, true
}
