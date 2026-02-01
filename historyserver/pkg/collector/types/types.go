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

	Role           string
	RayClusterName string
	RayClusterID   string
	LogBatching    int
	PushInterval   time.Duration

	DashboardAddress             string
	SupportRayEventUnSupportData bool
}

type UrlInfo struct {
	Key  string
	Url  string
	Hash string
	Type string
}

const (
	JOBSTATUS_PENDING   = "PENDING"
	JOBSTATUS_RUNNING   = "RUNNING"
	JOBSTATUS_STOPPED   = "STOPPED"
	JOBSTATUS_SUCCEEDED = "SUCCEEDED"
	JOBSTATUS_FAILED    = "FAILED"
)

type JobUrlInfo struct {
	Url         *UrlInfo
	Status      string
	StopPersist bool
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
