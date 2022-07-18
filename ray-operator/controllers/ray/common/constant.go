package common

const (
	// Belows used as label key
	RayServiceLabelKey                 = "ray.io/service"
	RayClusterLabelKey                 = "ray.io/cluster"
	RayNodeTypeLabelKey                = "ray.io/node-type"
	RayNodeGroupLabelKey               = "ray.io/group"
	RayNodeLabelKey                    = "ray.io/is-ray-node"
	RayIDLabelKey                      = "ray.io/identifier"
	RayClusterDashboardServiceLabelKey = "ray.io/cluster-dashboard"

	// Ray GCS HA related annotations
	RayHAEnabledAnnotationKey         = "ray.io/ha-enabled"
	RayExternalStorageNSAnnotationKey = "ray.io/external-storage-namespace"
	RayNodeHealthStateAnnotationKey   = "ray.io/health-state"

	// Pod health state values
	PodUnhealthy = "Unhealthy"

	EnableAgentServiceKey  = "ray.io/enableAgentService"
	EnableAgentServiceTrue = "true"

	KubernetesApplicationNameLabelKey = "app.kubernetes.io/name"
	KubernetesCreatedByLabelKey       = "app.kubernetes.io/created-by"

	// Use as separator for pod name, for example, raycluster-small-size-worker-0
	DashSymbol = "-"

	// Use as default port
	DefaultClientPort               = 10001
	DefaultRedisPort                = 6379
	DefaultDashboardPort            = 8265
	DefaultMetricsPort              = 8080
	DefaultDashboardAgentListenPort = 52365

	DefaultClientPortName               = "client"
	DefaultRedisPortName                = "redis"
	DefaultDashboardName                = "dashboard"
	DefaultMetricsName                  = "metrics"
	DefaultDashboardAgentListenPortName = "dashboard-agent"

	// The default application name
	ApplicationName = "kuberay"

	// The default name for kuberay operator
	ComponentName = "kuberay-operator"

	// Check node if ready by checking the path exists or not
	PodReadyFilepath = "POD_READY_FILEPATH"

	// Use as container env variable
	NAMESPACE               = "NAMESPACE"
	CLUSTER_NAME            = "CLUSTER_NAME"
	RAY_IP                  = "RAY_IP"
	RAY_PORT                = "RAY_PORT"
	REDIS_PASSWORD          = "REDIS_PASSWORD"
	RAY_EXTERNAL_STORAGE_NS = "RAY_external_storage_namespace"

	// Ray core default configurations
	DefaultRedisPassword = "5241590000000000"

	// Ray HA default readiness probe values
	DefaultReadinessProbeInitialDelaySeconds = 10
	DefaultReadinessProbeTimeoutSeconds      = 0
	DefaultReadinessProbePeriodSeconds       = 0
	DefaultReadinessProbeSuccessThreshold    = 0
	DefaultReadinessProbeFailureThreshold    = 15

	// Ray HA default liveness probe values
	DefaultLivenessProbeInitialDelaySeconds = 10
	DefaultLivenessProbeTimeoutSeconds      = 0
	DefaultLivenessProbePeriodSeconds       = 0
	DefaultLivenessProbeSuccessThreshold    = 0
	DefaultLivenessProbeFailureThreshold    = 30

	// Ray health check related configurations
	DefaultRayAgentPort       = 52365
	DefaultRayDashboardPort   = 8265
	RayAgentRayletHealthPath  = "api/local_raylet_healthz"
	RayDashboardGCSHealthPath = "api/gcs_healthz"
)

type ServiceType string

const (
	HeadService  ServiceType = "headService"
	AgentService ServiceType = "agentService"
)
