package common

const (
	// Belows used as label key
	RayClusterLabelKey   = "ray.io/cluster"
	RayNodeTypeLabelKey  = "ray.io/node-type"
	RayNodeGroupLabelKey = "ray.io/group"
	RayNodeLabelKey      = "ray.io/is-ray-node"
	RayIDLabelKey        = "ray.io/identifier"

	// Use as separator for pod name, for example, raycluster-small-size-worker-0
	DashSymbol = "-"

	// Use as default port
	DefaultClientPort    = 10001
	DefaultRedisPort     = 6379
	DefaultDashboardPort = 8265

	DefaultClientPortName = "client"
	DefaultRedisPortName  = "redis"
	DefaultDashboardName  = "dashboard"

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
