package logcollector

import "github.com/ray-project/kuberay/historyserver/utils"

const (
	JOBSTATUS_PENDING   = "PENDING"
	JOBSTATUS_RUNNING   = "RUNNING"
	JOBSTATUS_STOPPED   = "STOPPED"
	JOBSTATUS_SUCCEEDED = "SUCCEEDED"
	JOBSTATUS_FAILED    = "FAILED"
)

type MetaType string

const (
	MetaTypeUrl       = "URL"
	MetaTypeFile      = "FILE"
	MetaTypeFileNames = "FILENAMES"
)

type UrlInfo struct {
	Key  string
	Url  string
	Hash string
	Path string
	Type string
}

type JobUrlInfo struct {
	UrlInfos    []*UrlInfo
	Status      string
	StopPersist bool
}

var metaCommonUrlInfo = []*UrlInfo{
	&UrlInfo{
		Key:  utils.OssMetaFile_ClusterStatus,
		Url:  "http://localhost:8265/api/cluster_status?format=1",
		Type: MetaTypeUrl,
	},
	&UrlInfo{
		Key:  utils.OssMetaFile_LOGICAL_ACTORS,
		Url:  "http://localhost:8265/logical/actors",
		Type: MetaTypeUrl,
	},
	&UrlInfo{
		Key: utils.OssMetaFile_ALLTASKS_DETAIL,
		// limit 最多 10000
		Url:  "http://localhost:8265/api/v0/tasks?detail=1&limit=10000",
		Type: MetaTypeUrl,
	},

	&UrlInfo{Key: utils.OssMetaFile_Applications,
		Url:  "http://localhost:8265/api/serve/applications/",
		Type: MetaTypeUrl,
	},

	&UrlInfo{
		Key:  utils.OssMetaFile_Events,
		Url:  "http://localhost:8265/events",
		Type: MetaTypeUrl,
	},
	&UrlInfo{
		Key: utils.OssMetaFile_PlacementGroups,
		// limit 最多 10000
		Url:  "http://localhost:8265/api/v0/placement_groups?detail=1&limit=10000",
		Type: MetaTypeUrl,
	},
}

var NodeSummaryUrlInfo = &UrlInfo{
	Key:  utils.OssMetaFile_NodeSummaryKey,
	Url:  "http://localhost:8265/nodes?view=summary",
	Type: MetaTypeUrl,
}

// key is node id
var NodeUrlInfo = make(map[string][]*UrlInfo)

var JobsUrlInfo = &UrlInfo{
	Key:  utils.OssMetaFile_Jobs,
	Url:  "http://localhost:8265/api/jobs/",
	Type: MetaTypeUrl,
}
