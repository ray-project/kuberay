package clusterlogs

import (
	"path"
	"strings"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

const (
	ClusterHistoryDir = "cluster-history"
	LogsSubDir        = "logs"
	NodeEventsSubDir  = "node_events"
	JobEventsSubDir   = "job_events"
)

// Prefix returns the hierarchical cluster directory prefix under rootDir:
// - raycluster: rootDir/cluster-history/raycluster/<namespace>/<cluster-name>
// - rayjob:     rootDir/cluster-history/rayjob/<namespace>/<rayjob-name>/<cluster-name>
// - rayservice: rootDir/cluster-history/rayservice/<namespace>/<rayservice-name>/<cluster-name>
func Prefix(rootDir string, c *utils.ClusterInfo) string {
	k := strings.ToLower(strings.TrimSpace(c.OwnerKind))
	hasOwner := (k == utils.RayJobKind || k == utils.RayServiceKind) && strings.TrimSpace(c.OwnerName) != ""

	subDir := utils.RayClusterKind
	if hasOwner {
		subDir = k
	}

	parts := []string{rootDir, ClusterHistoryDir, subDir, c.Namespace}
	if hasOwner {
		parts = append(parts, strings.TrimSpace(c.OwnerName))
	}
	parts = append(parts, c.Name)

	return path.Join(parts...)
}

// SessionDir returns the path to a session's directory under a cluster:
// <prefix>/<session-name>
func SessionDir(rootDir string, c *utils.ClusterInfo) string {
	cp := Prefix(rootDir, c)
	return path.Join(cp, c.SessionName)
}

// FetchedEndpointsDir returns the directory containing dashboard endpoint snapshots:
// <prefix>/<session-name>/fetched_endpoints
func FetchedEndpointsDir(prefix, sessionName string) string {
	return path.Join(prefix, sessionName, utils.RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME)
}

// NodeDir returns the path to a node's directory under a session:
// <prefix>/<session-name>/<node-name>
func NodeDir(rootDir string, c *utils.ClusterInfo, nodeName string) string {
	sDir := SessionDir(rootDir, c)
	return path.Join(sDir, nodeName)
}

// LogsDir returns the log directory for a specific node and session:
// <prefix>/<session-name>/<node-name>/logs
func LogsDir(rootDir string, c *utils.ClusterInfo, nodeName string) string {
	nDir := NodeDir(rootDir, c, nodeName)
	return path.Join(nDir, LogsSubDir)
}

// NodeEventsDir returns the node_events directory for a specific node and session:
// <prefix>/<session-name>/<node-name>/node_events
func NodeEventsDir(rootDir string, c *utils.ClusterInfo, nodeName string) string {
	nDir := NodeDir(rootDir, c, nodeName)
	return path.Join(nDir, NodeEventsSubDir)
}

// JobEventsDir returns the job_events directory for a specific node and session (and optional jobID):
// <prefix>/<session-name>/<node-name>/job_events/[jobID]
func JobEventsDir(rootDir string, c *utils.ClusterInfo, nodeName, jobID string) string {
	nDir := NodeDir(rootDir, c, nodeName)
	if jobID == "" {
		return path.Join(nDir, JobEventsSubDir)
	}
	return path.Join(nDir, JobEventsSubDir, jobID)
}

// RelLogsDir returns: <session-name>/<node-name>/logs
func RelLogsDir(sessionName, nodeName string) string {
	return path.Join(sessionName, nodeName, LogsSubDir)
}

// RelNodeEventsDir returns: <session-name>/<node-name>/node_events
func RelNodeEventsDir(sessionName, nodeName string) string {
	return path.Join(sessionName, nodeName, NodeEventsSubDir)
}

// RelJobEventsDir returns: <session-name>/<node-name>/job_events/[jobID]
func RelJobEventsDir(sessionName, nodeName, jobID string) string {
	p := path.Join(sessionName, nodeName, JobEventsSubDir)
	if jobID == "" {
		return p
	}
	return path.Join(p, jobID)
}
