package common

const (
	// Belows used as label key
	RayClusterLabelKey   = "ray.io/cluster"
	RayNodeTypeLabelKey  = "ray.io/node-type"
	RayNodeGroupLabelKey = "ray.io/group"
	RayNodeLabelKey      = "ray.io/is-ray-node"
	RayIDLabelKey        = "ray.io/identifier"

	// rayOperator is the value of ray-operator used as identifier for the pod
	rayOperator = "ray-operator"

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
	Namespace        = "NAMESPACE"
	ClusterName      = "CLUSTER_NAME"
	RayIpEnv         = "RAY_IP"
	RayPortEnv       = "RAY_PORT"
	RedisPasswordEnv = "REDIS_PASSWORD"
)
