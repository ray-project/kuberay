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
	AutoscalingStatus *string        `json:"autoscalingStatus"`
	AutoscalingError  *string        `json:"autoscalingError"`
	ClusterStatus     *ClusterStatus `json:"clusterStatus"`
}

type ClusterStatus struct {
	ActiveNodes       map[string]int    `json:"activeNodes"`
	PendingNodes      []string          `json:"pendingNodes"`
	FailedNodes       []string          `json:"failedNodes"`
	LoadMetricsReport LoadMetricsReport `json:"loadMetricsReport"`
}

type LoadMetricsReport struct {
	UsedResources               map[string]float64 `json:"usedResources"`
	TotalResources              map[string]float64 `json:"totalResources,omitempty"`
	AvailableResources          map[string]float64 `json:"availableResources,omitempty"`
	ResourceDemand              []interface{}      `json:"resourceDemand"`
	ResourceDemandSummary       []interface{}      `json:"resourceDemandSummary"`
	PlacementGroupDemandSummary []interface{}      `json:"placementGroupDemandSummary"`
}

type ResourceDemand struct {
	Resources map[string]float64 `json:"resources"` // TODO check all types
	Count     int                `json:"count"`
}
