package common

const (

	// Default application name
	DefaultServeAppName = "default"
	// Belows used as label key
	RayServiceLabelKey                 = "ray.io/service"
	RayClusterLabelKey                 = "ray.io/cluster"
	RayNodeTypeLabelKey                = "ray.io/node-type"
	RayNodeGroupLabelKey               = "ray.io/group"
	RayNodeLabelKey                    = "ray.io/is-ray-node"
	RayIDLabelKey                      = "ray.io/identifier"
	RayClusterDashboardServiceLabelKey = "ray.io/cluster-dashboard"
	RayClusterServingServiceLabelKey   = "ray.io/serve"
	RayServiceClusterHashKey           = "ray.io/cluster-hash"

	// Batch scheduling labels
	// TODO(tgaddair): consider making these part of the CRD
	RaySchedulerName     = "ray.io/scheduler-name"
	RayPriorityClassName = "ray.io/priority-class-name"

	// Ray GCS FT related annotations
	RayFTEnabledAnnotationKey         = "ray.io/ft-enabled"
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

	// The default AppProtocol for Kubernetes service
	DefaultServiceAppProtocol = "tcp"

	// The default application name
	ApplicationName = "kuberay"

	// The default name for kuberay operator
	ComponentName = "kuberay-operator"

	// The defaule RayService Identifier.
	RayServiceCreatorLabelValue = "rayservice"

	// Check node if ready by checking the path exists or not
	PodReadyFilepath = "POD_READY_FILEPATH"

	// Use as container env variable
	NAMESPACE                               = "NAMESPACE"
	RAY_CLUSTER_NAME                        = "RAY_CLUSTER_NAME"
	RAY_IP                                  = "RAY_IP"
	FQ_RAY_IP                               = "FQ_RAY_IP"
	RAY_PORT                                = "RAY_PORT"
	RAY_ADDRESS                             = "RAY_ADDRESS"
	REDIS_PASSWORD                          = "REDIS_PASSWORD"
	RAY_DASHBOARD_ENABLE_K8S_DISK_USAGE     = "RAY_DASHBOARD_ENABLE_K8S_DISK_USAGE"
	RAY_EXTERNAL_STORAGE_NS                 = "RAY_external_storage_namespace"
	RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S  = "RAY_gcs_rpc_server_reconnect_timeout_s"
	RAY_TIMEOUT_MS_TASK_WAIT_FOR_DEATH_INFO = "RAY_timeout_ms_task_wait_for_death_info"
	RAY_GCS_SERVER_REQUEST_TIMEOUT_SECONDS  = "RAY_gcs_server_request_timeout_seconds"
	RAY_SERVE_KV_TIMEOUT_S                  = "RAY_SERVE_KV_TIMEOUT_S"
	SERVE_CONTROLLER_PIN_ON_NODE            = "RAY_INTERNAL_SERVE_CONTROLLER_PIN_ON_NODE"
	RAY_USAGE_STATS_KUBERAY_IN_USE          = "RAY_USAGE_STATS_KUBERAY_IN_USE"
	RAYCLUSTER_DEFAULT_REQUEUE_SECONDS_ENV  = "RAYCLUSTER_DEFAULT_REQUEUE_SECONDS_ENV"
	RAYCLUSTER_DEFAULT_REQUEUE_SECONDS      = 300

	// This KubeRay operator environment variable is used to determine if random Pod
	// deletion should be enabled. Note that this only takes effect when autoscaling
	// is enabled for the RayCluster. This is a feature flag for v0.6.0, and will be
	// removed if the default behavior is stable enoguh.
	ENABLE_RANDOM_POD_DELETE = "ENABLE_RANDOM_POD_DELETE"

	// Ray core default configurations
	DefaultRedisPassword                 = "5241590000000000"
	DefaultWorkerRayGcsReconnectTimeoutS = "600"

	LOCAL_HOST = "127.0.0.1"
	// Ray FT default readiness probe values
	DefaultReadinessProbeInitialDelaySeconds = 10
	DefaultReadinessProbeTimeoutSeconds      = 1
	DefaultReadinessProbePeriodSeconds       = 3
	DefaultReadinessProbeSuccessThreshold    = 0
	DefaultReadinessProbeFailureThreshold    = 20

	// Ray FT default liveness probe values
	DefaultLivenessProbeInitialDelaySeconds = 10
	DefaultLivenessProbeTimeoutSeconds      = 1
	DefaultLivenessProbePeriodSeconds       = 3
	DefaultLivenessProbeSuccessThreshold    = 0
	DefaultLivenessProbeFailureThreshold    = 40

	// Ray health check related configurations
	RayAgentRayletHealthPath  = "api/local_raylet_healthz"
	RayDashboardGCSHealthPath = "api/gcs_healthz"

	// Default autoscaler image when running Ray at versions older than 2.0.0
	FallbackDefaultAutoscalerImage = "rayproject/ray:2.0.0"

	// Finalizers for RayJob
	RayJobStopJobFinalizer = "ray.io/rayjob-finalizer"
)

type ServiceType string

const (
	HeadService    ServiceType = "headService"
	ServingService ServiceType = "serveService"
)
