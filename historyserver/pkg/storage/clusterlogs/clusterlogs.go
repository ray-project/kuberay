package clusterlogs

import (
	"bytes"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

const (
	ClusterHistoryDir = "cluster-history"
	LogsSubDir        = "logs"
	NodeEventsSubDir  = "node_events"
	JobEventsSubDir   = "job_events"
	// ChunkDirSuffix is the suffix of the directory holding a file's chunks. Format: "<file>.chunks/<offset>"
	ChunkDirSuffix = ".chunks"
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

	parts := []string{rootDir, ClusterHistoryDir, subDir, namespace}
	if hasOwner {
		parts = append(parts, strings.TrimSpace(ownerName))
	}
	parts = append(parts, clusterName)

	return path.Join(parts...)
}

// SessionDir returns the path to a session's directory under a cluster:
// <prefix>/<session-name>
func SessionDir(rootDir, ownerKind, ownerName, namespace, clusterName, sessionName string) string {
	cp := Prefix(rootDir, ownerKind, ownerName, namespace, clusterName)
	return path.Join(cp, sessionName)
}

// FetchedEndpointsDir returns the directory containing dashboard endpoint snapshots:
// <prefix>/<session-name>/fetched_endpoints
func FetchedEndpointsDir(prefix, sessionName string) string {
	return path.Join(prefix, sessionName, utils.RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME)
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

// ListSessionNodeDirs returns node directory names under <prefix>/<sessionName>/.
func ListSessionNodeDirs(reader storage.StorageReader, prefix, sessionName string) []string {
	var nodes []string
	for _, entry := range reader.ListFiles(prefix, sessionName) {
		if !strings.HasSuffix(entry, "/") {
			continue
		}
		name := strings.TrimSuffix(entry, "/")
		if name == "" || name == utils.RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME {
			continue
		}
		nodes = append(nodes, name)
	}
	return nodes
}

// ListLogFiles lists dir like reader.ListFiles, but a file that so far exists
// only as chunks shows up as the file itself and the chunk directory is hidden.
func ListLogFiles(reader storage.StorageReader, prefix, dir string) []string {
	entries := reader.ListFiles(prefix, dir)
	seen := make(map[string]struct{}, len(entries))
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSuffix(entry, ChunkDirSuffix+"/")
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			files = append(files, name)
		}
	}
	return files
}

// ReadLogFile returns the whole object at logPath when it exists, otherwise
// join the file's chunks in offset order, or nil when there is neither.
//
// Only chunks that continue exactly where the previous one ended are used. A
// collector restart re-uploads from offset zero and leaves the earlier
// higher-offset chunks behind; splicing those in would duplicate bytes.
func ReadLogFile(reader storage.StorageReader, prefix, logPath string) io.Reader {
	if content := reader.GetContent(prefix, logPath); content != nil {
		return content
	}

	chunkDir := logPath + ChunkDirSuffix
	names := reader.ListFiles(prefix, chunkDir)
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)

	var joined bytes.Buffer
	for _, name := range names {
		offset, err := strconv.ParseInt(name, 10, 64)
		if err != nil || offset != int64(joined.Len()) {
			break
		}
		chunk := reader.GetContent(prefix, path.Join(chunkDir, name))
		if chunk == nil {
			break
		}
		if _, err := joined.ReadFrom(chunk); err != nil {
			break
		}
	}
	return bytes.NewReader(joined.Bytes())
}
