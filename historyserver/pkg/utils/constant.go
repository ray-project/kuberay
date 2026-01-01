package utils

import "time"

// Ray session directory related constants.
const (
	RAY_SESSIONDIR_LOGDIR_NAME  = "logs"
	RAY_SESSIONDIR_METADIR_NAME = "meta"
)

// Local Ray runtime paths.
const (
	RaySessionLatestPath = "/tmp/ray/session_latest"
	RayNodeIDPath        = "/tmp/ray/raylet_node_id"
)

// OSS meta file keys used by history server.
const (
	OssMetaFile_BasicInfo = "ack__basicinfo"

	OssMetaFile_NodeSummaryKey                        = "restful__nodes_view_summary"
	OssMetaFile_Node_Prefix                           = "restful__nodes_"
	OssMetaFile_JOBTASK_DETAIL_Prefix                 = "restful__api__v0__tasks_detail_job_id_"
	OssMetaFile_JOBTASK_SUMMARIZE_BY_FUNC_NAME_Prefix = "restful__api__v0__tasks_summarize_by_func_name_job_id_"
	OssMetaFile_JOBTASK_SUMMARIZE_BY_LINEAGE_Prefix   = "restful__api__v0__tasks_summarize_by_lineage_job_id_"
	OssMetaFile_JOBDATASETS_Prefix                    = "restful__api__data__datasets_job_id_"
	OssMetaFile_NodeLogs_Prefix                       = "restful__api__v0__logs_node_id_"
	OssMetaFile_ClusterStatus                         = "restful__api__cluster_status"
	OssMetaFile_LOGICAL_ACTORS                        = "restful__logical__actors"
	OssMetaFile_ALLTASKS_DETAIL                       = "restful__api__v0__tasks_detail"
	OssMetaFile_Events                                = "restful__events"
	OssMetaFile_PlacementGroups                       = "restful__api__v0__placement_groups_detail"
	OssMetaFile_ClusterSessionName                    = "static__api__cluster_session_name"
	OssMetaFile_Jobs                                  = "restful__api__jobs"
	OssMetaFile_Applications                          = "restful__api__serve__applications"
)

// Ray history server log file name.
const RayHistoryServerLogName = "historyserver-ray.log"

const (
	// DefaultMaxRetryAttempts controls how many times we retry reading
	// local Ray metadata files (e.g. session dir, node id) before failing.
	DefaultMaxRetryAttempts = 3
	// DefaultInitialRetryDelay is the base delay before the first retry.
	// Subsequent retries use an exponential backoff based on this value.
	DefaultInitialRetryDelay = 5 * time.Second
)
