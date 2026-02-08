package historyserver

// GetLogFileOptions contains all options for fetching log files
type GetLogFileOptions struct {
	// Node identification (one of these is required if not using task_id/actor_id)
	NodeID   string // The node id where the log file is located
	NodeIP   string // The node ip address (will be resolved to node_id)

	// Log file identification (provide one of: Filename, TaskID, ActorID, PID)
	Filename string // The log file name (explicit path)
	TaskID   string // Task ID to resolve log file
	ActorID  string // Actor ID to resolve log file
	PID      int    // Process ID to resolve log file

	// Optional parameters with defaults
	// Number of lines to return, default to DEFAULT_LOG_LIMIT (1000)
	// -1 = all lines
	Lines int
	// Timeout in seconds for the request, default to 0 (no timeout)
	Timeout int
	// Attempt number for task retries, default to 0 (first attempt)
	AttemptNumber int
	// Whether to filter ANSI escape codes from logs, default to False
	FilterAnsiCode bool
	// If set, triggers download with Content-Disposition header using this filename.
	// If empty, no Content-Disposition header is set and the log content is displayed inline.
	DownloadFilename string
	// The suffix of the log file ("out" or "err"), default to "out"
	// Used when resolving by TaskID, ActorID, or PID
	Suffix string
}

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
