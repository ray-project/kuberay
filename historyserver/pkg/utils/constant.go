package utils

import (
	"os"
	"path/filepath"
)

const (
	defaultTmpRayRoot = "/tmp/ray"

	// Lowercase, normalized CRD kinds. Used both as the comparison key
	// for ToLower(OwnerKind) and as the cluster-metadata path subdir segment.
	RayJobKind     = "rayjob"
	RayServiceKind = "rayservice"
	RayClusterKind = "raycluster"

	RayTokenMountPath                 = "/var/run/secrets/ray.io/serviceaccount"
	RAY_ENABLE_K8S_TOKEN_AUTH_ENV_VAR = "RAY_ENABLE_K8S_TOKEN_AUTH"
	RAY_AUTH_TOKEN_ENV_VAR            = "RAY_AUTH_TOKEN"
	RAY_AUTH_MODE_ENV_VAR             = "RAY_AUTH_MODE"

	RAY_AUTH_TOKEN_SECRET_KEY = "auth_token"
	RayAuthModeToken          = "token"
	RayAuthHeader             = "x-ray-authorization"
	RayK8sTokenPath           = RayTokenMountPath + "/token"

	// RayContainerIndex is the index of the Ray container in the head pod template.
	RayContainerIndex = 0
	// DashboardPortName is the name the ray-operator gives the dashboard port.
	DashboardPortName = "dashboard"
	// DefaultDashboardPort is used when the head container does not declare a dashboard port.
	DefaultDashboardPort = 8265
)

func GetTmpRayRoot() string {
	if tmpRoot := os.Getenv("RAY_TMP_ROOT"); tmpRoot != "" {
		return tmpRoot
	}
	return defaultTmpRayRoot
}

func GetRayPrevLogsPath() string {
	return filepath.Join(GetTmpRayRoot(), "prev-logs")
}

func GetRayPersistCompletePath() string {
	return filepath.Join(GetTmpRayRoot(), "persist-complete-logs")
}

func GetRaySessionLatestPath() string {
	return filepath.Join(GetTmpRayRoot(), "session_latest")
}
