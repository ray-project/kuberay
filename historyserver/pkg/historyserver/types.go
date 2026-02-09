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

// Link represents a navigation reference to a task or actor detail page.
// Fields:
// - Type: "task" or "actor"
// - ID: the task ID (hex) or actor ID (hex)
type Link struct {
	Type string `json:"type"` // "task" or "actor"
	ID   string `json:"id"`
}

// NestedTaskSummary represents a node in the lineage tree.
// A node can be a task, an actor container, or a group of same-named siblings.
// Ref: https://github.com/ray-project/ray/blob/f3d444ab01279a3870033fb4d34314cd8c987b22/python/ray/util/state/common.py#L996-L1012
type NestedTaskSummary struct {
	Name        string               `json:"name"`                // task/actor/group name
	Key         string               `json:"key"`                 // task ID, "actor:{id}", or group name
	Type        string               `json:"type"`                // NORMAL_TASK, ACTOR_TASK, ACTOR_CREATION_TASK, ACTOR, GROUP
	Timestamp   *int64               `json:"timestamp,omitempty"` // Unix milliseconds timestamp for sorting
	StateCounts map[string]int       `json:"state_counts"`        // aggregated counts by task state (e.g., RUNNING, FINISHED)
	Children    []*NestedTaskSummary `json:"children"`            // nested child nodes in the lineage tree
	Link        *Link                `json:"link,omitempty"`      // navigation link (nil for GROUP nodes)
}

// TaskSummaries is the top-level lineage summary returned by BuildLineageSummary.
// Counters match Ray Dashboard's task_name_to_summary format.
// Ref: https://github.com/ray-project/ray/blob/f3d444ab01279a3870033fb4d34314cd8c987b22/python/ray/util/state/common.py#L1022-L1033
type TaskSummaries struct {
	Summary             []*NestedTaskSummary `json:"summary"`
	TotalTasks          int                  `json:"total_tasks"`           // count of NORMAL_TASK
	TotalActorTasks     int                  `json:"total_actor_tasks"`     // count of ACTOR_TASK
	TotalActorScheduled int                  `json:"total_actor_scheduled"` // count of ACTOR_CREATION_TASK
	SummaryBy           string               `json:"summary_by"`            // always "lineage"
}
