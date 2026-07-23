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
func Prefix(rootDir, ownerKind, ownerName, namespace, clusterName string) string {
	k := strings.ToLower(strings.TrimSpace(ownerKind))
	hasOwner := (k == utils.RayJobKind || k == utils.RayServiceKind) && strings.TrimSpace(ownerName) != ""

	subDir := utils.RayClusterKind
	if hasOwner {
		subDir = k
	}

	parts := []string{ClusterHistoryDir, subDir, namespace}
	if hasOwner {
		parts = append(parts, strings.TrimSpace(ownerName))
	}
	parts = append(parts, clusterName)

	if rootDir == "" {
		return path.Join(parts...)
	}
	return path.Join(rootDir, path.Join(parts...))
}

// SessionDir returns the path to a session's directory under a cluster:
// <prefix>/<session-name>
func SessionDir(rootDir, ownerKind, ownerName, namespace, clusterName, sessionName string) string {
	cp := Prefix(rootDir, ownerKind, ownerName, namespace, clusterName)
	return path.Join(cp, sessionName)
}

// NodeDir returns the path to a node's directory under a session:
// <prefix>/<session-name>/<node-name>
func NodeDir(rootDir, ownerKind, ownerName, namespace, clusterName, sessionName, nodeName string) string {
	sDir := SessionDir(rootDir, ownerKind, ownerName, namespace, clusterName, sessionName)
	return path.Join(sDir, nodeName)
}

// LogsDir returns the log directory for a specific node and session:
// <prefix>/<session-name>/<node-name>/logs
func LogsDir(rootDir, ownerKind, ownerName, namespace, clusterName, sessionName, nodeName string) string {
	nDir := NodeDir(rootDir, ownerKind, ownerName, namespace, clusterName, sessionName, nodeName)
	return path.Join(nDir, LogsSubDir)
}

// NodeEventsDir returns the node_events directory for a specific node and session:
// <prefix>/<session-name>/<node-name>/node_events
func NodeEventsDir(rootDir, ownerKind, ownerName, namespace, clusterName, sessionName, nodeName string) string {
	nDir := NodeDir(rootDir, ownerKind, ownerName, namespace, clusterName, sessionName, nodeName)
	return path.Join(nDir, NodeEventsSubDir)
}

// JobEventsDir returns the job_events directory for a specific node and session (and optional jobID):
// <prefix>/<session-name>/<node-name>/job_events/[jobID]
func JobEventsDir(rootDir, ownerKind, ownerName, namespace, clusterName, sessionName, nodeName, jobID string) string {
	nDir := NodeDir(rootDir, ownerKind, ownerName, namespace, clusterName, sessionName, nodeName)
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
