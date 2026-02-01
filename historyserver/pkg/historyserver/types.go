package historyserver

type ReplyTaskInfo struct {
	Data   TaskInfoData `json:"data"`
	Msg    string       `json:"msg"`
	Result bool         `json:"result"`
}
type TaskInfoData struct {
	Result TaskInfoDataResult `json:"result"`
}
type TaskInfoDataResult struct {
	NumAfterTruncation    int           `json:"num_after_truncation"`
	NumFiltered           int           `json:"num_filtered"`
	PartialFailureWarning string        `json:"partial_failure_warning"`
	Result                []interface{} `json:"result"`
	Total                 int           `json:"total"`
	Warnings              interface{}   `json:"warnings"`
}

type ReplyActorInfo struct {
	Result bool          `json:"result"`
	Msg    string        `json:"msg"`
	Data   ActorInfoData `json:"data"`
}

type ActorInfoData struct {
	Detail map[string]interface{} `json:"detail"`
}

// ============================================
// Lineage Summary Types
// ============================================

// Link represents a reference to navigate to task/actor detail page
type Link struct {
	Type string `json:"type"` // "task" or "actor"
	ID   string `json:"id"`
}

// NestedTaskSummary represents a node in the lineage tree.
// Can be a task, an actor container, or a group of same-named siblings.
type NestedTaskSummary struct {
	Name        string               `json:"name"`
	Key         string               `json:"key"`
	Type        string               `json:"type"` // NORMAL_TASK, ACTOR_TASK, ACTOR_CREATION_TASK, ACTOR, GROUP
	Timestamp   *int64               `json:"timestamp,omitempty"`
	StateCounts map[string]int       `json:"state_counts"`
	Children    []*NestedTaskSummary `json:"children"`
	Link        *Link                `json:"link,omitempty"`
}

// TaskSummaries is the top-level lineage summary
type TaskSummaries struct {
	Summary             []*NestedTaskSummary `json:"summary"`
	TotalTasks          int                  `json:"total_tasks"`
	TotalActorTasks     int                  `json:"total_actor_tasks"`
	TotalActorScheduled int                  `json:"total_actor_scheduled"`
	SummaryBy           string               `json:"summary_by"`
}
