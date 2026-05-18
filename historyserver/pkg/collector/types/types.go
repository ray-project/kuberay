package types

import (
	"time"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

type RayHistoryServerConfig struct {
	RootDir string
}

type RayCollectorConfig struct {
	RootDir     string
	SessionDir  string
	RayNodeName string

	Role                string
	RayClusterName      string
	RayClusterNamespace string
	LogBatching         int
	PushInterval        time.Duration
	DashboardAddress    string

	AdditionalEndpoints  []string
	EndpointPollInterval time.Duration

	// Event collector disk-first storage configuration.
	EventDataDir          string        // root directory for JSONL event files
	EventRotationInterval time.Duration // time-based rotation trigger
	EventMaxFileSizeMB    int           // size-based rotation trigger (MB)
	EventMaxDiskMB        int           // backpressure threshold (MB)
	// EventCompressionEnabled is a single switch that controls the entire
	// disk-first + gzip pipeline. When true, events are written to local
	// JSONL files and rotated/gzipped/uploaded asynchronously. When false,
	// the local disk pipeline is bypassed entirely and events are buffered
	// in-memory and periodically flushed (matching legacy semantics).
	EventCompressionEnabled bool
	// EventFlushInterval controls how often the in-memory buffer is flushed
	// to remote storage when EventCompressionEnabled is false. Defaults to
	// 1h to match the legacy behavior; can be set lower for more frequent
	// writes.
	EventFlushInterval time.Duration
}

// ValidateRayHanderConfig is
func ValidateRayHanderConfig(c *RayCollectorConfig, fldpath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	if len(c.SessionDir) == 0 {
		allErrs = append(allErrs, field.Invalid(fldpath, c.SessionDir, "session-dir must be set"))
	}
	if len(c.RayClusterName) == 0 {
		allErrs = append(allErrs, field.Invalid(fldpath, c.RayClusterName, "ray_cluster_name must be set"))
	}
	if len(c.RayNodeName) == 0 {
		allErrs = append(allErrs, field.Invalid(fldpath, c.RayNodeName, "ray_node_name must be set"))
	}
	if len(c.RayClusterNamespace) == 0 {
		allErrs = append(allErrs, field.Invalid(fldpath, c.RayClusterNamespace, "ray_cluster_namespace must be set"))
	}

	if c.Role == "Head" {
		if len(c.RayClusterName) == 0 {
			allErrs = append(allErrs, field.Invalid(fldpath, c.RayClusterName, "ray_cluster_name must be set"))
		}
		if len(c.RayClusterNamespace) == 0 {
			allErrs = append(allErrs, field.Invalid(fldpath, c.RayClusterNamespace, "ray_cluster_namespace must be set"))
		}
	}
	return allErrs
}
