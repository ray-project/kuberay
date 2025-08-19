package types

import (
	"time"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

type RayCollectorConfig struct {
	// need to fill these fields after start
	SessionDir  string
	RayNodeName string
	// fill these fields by cmdline arguments
	Role           string
	RayClusterName string
	RayClusterID   string
	LogBatching    int
	PushInterval   time.Duration
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
	if len(c.RayClusterID) == 0 {
		allErrs = append(allErrs, field.Invalid(fldpath, c.RayClusterID, "ray_cluster_id must be set"))
	}

	if c.Role == "Head" {
		if len(c.RayClusterName) == 0 {
			allErrs = append(allErrs, field.Invalid(fldpath, c.RayClusterName, "ray_cluster_name must be set"))
		}
		if len(c.RayClusterID) == 0 {
			allErrs = append(allErrs, field.Invalid(fldpath, c.RayClusterID, "ray_cluster_id must be set"))
		}
	}
	return allErrs
}
