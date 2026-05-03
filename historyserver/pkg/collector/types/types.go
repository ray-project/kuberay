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
	OwnerKind            string
	OwnerName            string
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
