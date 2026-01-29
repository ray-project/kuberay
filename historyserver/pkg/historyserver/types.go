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

type ClusterStatusResponse struct {
	Result bool              `json:"result"`
	Msg    string            `json:"msg"`
	Data   ClusterStatusData `json:"data"`
}

type ClusterStatusData struct {
	AutoscalingStatus *string `json:"autoscalingStatus"`
	AutoscalingError  *string `json:"autoscalingError"`
	ClusterStatus     any     `json:"clusterStatus"` // TODO: update it once ray dashboard api /api/cluster_status supports autoscaler info
}

type FormattedClusterStatusResponse struct {
	Result bool                       `json:"result"`
	Msg    string                     `json:"msg"`
	Data   FormattedClusterStatusData `json:"data"`
}

type FormattedClusterStatusData struct {
	ClusterStatus string `json:"clusterStatus"`
}
