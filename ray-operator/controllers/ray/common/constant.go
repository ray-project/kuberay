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
	NAMESPACE      = "NAMESPACE"
	CLUSTER_NAME   = "CLUSTER_NAME"
	RAY_IP         = "RAY_IP"
	RAY_ADDRESS    = "RAY_ADDRESS"
	RAY_PORT       = "RAY_PORT"
	REDIS_PASSWORD = "REDIS_PASSWORD"

	// Ray core default configurations
	DefaultRedisPassword = "5241590000000000"

	LOCAL_HOST = "127.0.0.1"
)

type ServiceType string

const (
	HeadService    ServiceType = "headService"
	AgentService   ServiceType = "agentService"
	ServingService ServiceType = "serveService"
)
