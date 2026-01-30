package historyserver

// GetLogFileOptions contains all options for fetching log files
type GetLogFileOptions struct {
	// Required parameters
	NodeID   string // The node id where the log file is located
	Filename string // The log file name

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
	// Whether to set Content-Disposition header for file download, default to false
	DownloadFile bool
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
