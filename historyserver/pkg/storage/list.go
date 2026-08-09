package storage

import (
	"strings"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// ListSessionNodeDirs returns node directory names under <prefix>/<sessionName>/.
func ListSessionNodeDirs(reader StorageReader, prefix string, sessionName string) []string {
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
