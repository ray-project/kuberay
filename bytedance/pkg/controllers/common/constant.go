package common

const (
	// General purpose
	RayOperatorName = "ray-operator"

	// Pod, Service Labels
	RayClusterLabelKey   = "ray.io/cluster"
	RayNodeTypeLabelKey  = "ray.io/node-type"
	RayNodeGroupLabelKey = "ray.io/group"
	RayIDLabelKey        = "ray.io/id"

	// Default port
	DefaultRedisPort     = 6379
	DefaultDashboardPort = 8695
	DefaultClientPort    = 10001

	// Container env variable
	Namespace             = "NAMESPACE"
	ClusterName           = "CLUSTER_NAME"
	RayHeadServiceIpEnv   = "RAY_HEAD_SERVICE_IP"
	RayHeadServicePortEnv = "RAY_HEAD_SERVICE_PORT"
	RayRedisPasswordEnv   = "REDIS_PASSWORD"
)
