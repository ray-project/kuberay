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

	// Check node if ready by checking the path exists or not
	PodReadyFilepath = "POD_READY_FILEPATH"

	// Use as container env variable
	NAMESPACE      = "NAMESPACE"
	CLUSTER_NAME   = "CLUSTER_NAME"
	RAY_IP         = "RAY_IP"
	RAY_PORT       = "RAY_PORT"
	REDIS_PASSWORD = "REDIS_PASSWORD"

	// Ray core default configurations
	DefaultRedisPassword = "5241590000000000"
)

type ServiceType string

const (
	HeadService  ServiceType = "headService"
	AgentService ServiceType = "agentService"
)
