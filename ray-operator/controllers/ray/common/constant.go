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
	RayClusterServingServiceLabelKey   = "ray.io/serve"

	// Ray GCS HA related annotations
	RayHAEnabledAnnotationKey         = "ray.io/ha-enabled"
	RayExternalStorageNSAnnotationKey = "ray.io/external-storage-namespace"
	RayNodeHealthStateAnnotationKey   = "ray.io/health-state"

	// Pod health state values
	PodUnhealthy = "Unhealthy"

	EnableAgentServiceKey  = "ray.io/enableAgentService"
	EnableAgentServiceTrue = "true"

	EnableRayClusterServingServiceTrue  = "true"
	EnableRayClusterServingServiceFalse = "false"

	KubernetesApplicationNameLabelKey = "app.kubernetes.io/name"
	KubernetesCreatedByLabelKey       = "app.kubernetes.io/created-by"

	// Use as separator for pod name, for example, raycluster-small-size-worker-0
	DashSymbol = "-"

	// Use as default port
	DefaultClientPort = 10001
	// For Ray >= 1.11.0, "DefaultRedisPort" actually refers to the GCS server port.
	// However, the role of this port is unchanged in Ray APIs like ray.init and ray start.
	// This is the port used by Ray workers and drivers inside the Ray cluster to connect to the Ray head.
	DefaultRedisPort                = 6379
	DefaultDashboardPort            = 8265
	DefaultMetricsPort              = 8080
	DefaultDashboardAgentListenPort = 52365
	DefaultServingPort              = 8000

	DefaultClientPortName               = "client"
	DefaultRedisPortName                = "redis"
	DefaultDashboardName                = "dashboard"
	DefaultMetricsName                  = "metrics"
	DefaultDashboardAgentListenPortName = "dashboard-agent"
	DefaultServingPortName              = "serve"

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
	RAY_ADDRESS             = "RAY_ADDRESS"
	REDIS_PASSWORD          = "REDIS_PASSWORD"
	RAY_EXTERNAL_STORAGE_NS = "RAY_external_storage_namespace"

	// Ray core default configurations
	DefaultRedisPassword = "5241590000000000"

	LOCAL_HOST = "127.0.0.1"
	// Ray HA default readiness probe values
	DefaultReadinessProbeInitialDelaySeconds = 10
	DefaultReadinessProbeTimeoutSeconds      = 1
	DefaultReadinessProbePeriodSeconds       = 3
	DefaultReadinessProbeSuccessThreshold    = 0
	DefaultReadinessProbeFailureThreshold    = 20

	// Ray HA default liveness probe values
	DefaultLivenessProbeInitialDelaySeconds = 10
	DefaultLivenessProbeTimeoutSeconds      = 1
	DefaultLivenessProbePeriodSeconds       = 3
	DefaultLivenessProbeSuccessThreshold    = 0
	DefaultLivenessProbeFailureThreshold    = 40

	// Ray health check related configurations
	RayAgentRayletHealthPath  = "api/local_raylet_healthz"
	RayDashboardGCSHealthPath = "api/gcs_healthz"
)

type ServiceType string

const (
	HeadService    ServiceType = "headService"
	AgentService   ServiceType = "agentService"
	ServingService ServiceType = "serveService"
)
