package utils

import (
	"fmt"
	"path"
	"strings"
)

// ClusterRef identifies a RayCluster using the Kubernetes NamespacedName convention.
// Ref: https://github.com/kubernetes/apimachinery/blob/72791e98/pkg/types/namespacedname.go#L27-L30
type ClusterRef struct {
	Namespace string
	Name      string
}

// ClusterSessionRef identifies one lifecycle (session) of a RayCluster.
type ClusterSessionRef struct {
	ClusterRef
	SessionName string
}

// ClusterRef's StoragePrefix returns the storage path prefix for a RayCluster:
// "{namespace}/{name}".
func (c ClusterRef) StoragePrefix() string {
	return path.Join(c.Namespace, c.Name)
}

// ClusterSessionRef's StoragePrefix returns the storage path prefix for one cluster session:
// "{namespace}/{name}/{session}".
func (c ClusterSessionRef) StoragePrefix() string {
	return path.Join(c.Namespace, c.Name, c.SessionName)
}

// ParseMetaDirRelPath parses a relative metadir path of the form "{namespace}/{name}/{session}" produced by storage
// writers into a ClusterSessionRef.
// Extra trailing segments are ignored, so callers may pass either the full object key (trimmed to the metadir root) or any
// deeper path that begins with these three components.
func ParseMetaDirRelPath(relPath string) (ClusterSessionRef, error) {
	trimmedPath := strings.Trim(relPath, "/")
	pathParts := strings.Split(trimmedPath, "/")
	if len(pathParts) < 3 || pathParts[0] == "" || pathParts[1] == "" || pathParts[2] == "" {
		return ClusterSessionRef{}, fmt.Errorf("invalid meta path %q, expected {namespace}/{name}/{session}", relPath)
	}

	return ClusterSessionRef{
		ClusterRef:  ClusterRef{Namespace: pathParts[0], Name: pathParts[1]},
		SessionName: pathParts[2],
	}, nil
}
