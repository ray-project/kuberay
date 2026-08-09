package storage

import (
	"strings"
)

// ListSessionNodeDirs returns node directory names under <prefix>/<sessionName>/.
func ListSessionNodeDirs(reader StorageReader, prefix string, sessionName string) []string {
	var nodes []string
	for _, entry := range reader.ListFiles(prefix, sessionName) {
		if !strings.HasSuffix(entry, "/") {
			continue
		}
		name := strings.TrimSuffix(entry, "/")
		if name == "" {
			continue
		}
		nodes = append(nodes, name)
	}
	return nodes
}
