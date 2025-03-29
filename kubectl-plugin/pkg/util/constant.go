package util

const (
	RayVersion = "2.41.0"
	RayImage   = "rayproject/ray:" + RayVersion

	RayClusterLabelKey   = "ray.io/cluster"
	RayIsRayNodeLabelKey = "ray.io/is-ray-node"
	RayNodeGroupLabelKey = "ray.io/group"
	RayNodeTypeLabelKey  = "ray.io/node-type"

	ResourceNvidiaGPU = "nvidia.com/gpu"
	ResourceGoogleTPU = "google.com/tpu"

	FieldManager = "ray-kubectl-plugin"

	DefaultHeadCPU                = "2"
	DefaultHeadMemory             = "4Gi"
	DefaultHeadGPU                = "0"
	DefaultHeadEphemeralStorage   = ""
	DefaultWorkerReplicas         = int32(1)
	DefaultWorkerCPU              = "2"
	DefaultWorkerMemory           = "4Gi"
	DefaultWorkerGPU              = "0"
	DefaultWorkerEphemeralStorage = ""
)
