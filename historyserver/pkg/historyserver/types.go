package historyserver

// TODO(jwj): Can be extracted to a task-specific interface, e.g., TaskSummaryProvider.
type TaskDataResult struct {
	Total                 int                      `json:"total"`
	NumAfterTruncation    int                      `json:"num_after_truncation"`
	NumFiltered           int                      `json:"num_filtered"`
	Result                []map[string]interface{} `json:"result"`
	PartialFailureWarning string                   `json:"partial_failure_warning"`
	Warnings              []string                 `json:"warnings"`
}

type TaskData struct {
	Result TaskDataResult `json:"result"`
}

type RespTasksInfo struct {
	Result bool     `json:"result"`
	Msg    string   `json:"msg"`
	Data   TaskData `json:"data"`
}

type ReplyActorInfo struct {
	Result bool          `json:"result"`
	Msg    string        `json:"msg"`
	Data   ActorInfoData `json:"data"`
}

type ActorInfoData struct {
	Detail map[string]interface{} `json:"detail"`
}

// --- Lineage Summary Types ---

// Link represents a navigation reference to a task or actor detail page
type Link struct {
	Type string `json:"type"` // task or actor
	ID   string `json:"id"`   // task ID or actor ID
}

// NestedTaskSummary represents an entry in the task lineage tree.
// An entry can be:
// - a task (type = NORMAL_TASK, ACTOR_TASK, or ACTOR_CREATION_TASK)
// - an actor grouping its creation and method tasks (type = ACTOR)
// - a group of same-named siblings, created when >1 sibling shares a name (type = GROUP)
// Ref: https://github.com/ray-project/ray/blob/f3d444ab01279a3870033fb4d34314cd8c987b22/python/ray/util/state/common.py#L996-L1012
type NestedTaskSummary struct {
	Name        string               `json:"name"`
	Key         string               `json:"key"`
	Type        string               `json:"type"`
	Timestamp   *int64               `json:"timestamp"`
	StateCounts map[string]int       `json:"state_counts"`
	Children    []*NestedTaskSummary `json:"children"`
	Link        *Link                `json:"link,omitempty"`
}

// TaskSummaries is the response for summary_by=lineage
// Ref: https://github.com/ray-project/ray/blob/f3d444ab01279a3870033fb4d34314cd8c987b22/python/ray/util/state/common.py#L1022-L1033
type TaskSummaries struct {
	Summary             []*NestedTaskSummary `json:"summary"`
	TotalTasks          int                  `json:"total_tasks"`
	TotalActorTasks     int                  `json:"total_actor_tasks"`
	TotalActorScheduled int                  `json:"total_actor_scheduled"`
	SummaryBy           string               `json:"summary_by"`
}
