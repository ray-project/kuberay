package util

// ClientOptions contains configuration needed to create a Kubernetes client
type ClientOptions struct {
	QPS   float32
	Burst int
}

const (
	// Label keys
	RayClusterNameLabelKey                   = "ray.io/cluster-name"
	RayClusterUserLabelKey                   = "ray.io/user"
	RayClusterVersionLabelKey                = "ray.io/version"
	RayClusterEnvironmentLabelKey            = "ray.io/environment"
	RayClusterComputeRuntimeTemplateLabelKey = "ray.io/compute-runtime-template"
	RayClusterClusterRuntimeTemplateLabelKey = "ray.io/cluster-runtime-template"

	// Annotation keys
	RayClusterCloudAnnotationKey  = "ray.io/compute-runtime-cloud"
	RayClusterRegionAnnotationKey = "ray.io/compute-runtime-region"
	RayClusterAZAnnotationKey     = "ray.io/compute-runtime-availability-zone"
	RayClusterImageAnnotationKey  = "ray.io/cluster-runtime-image"

	// What if we have multiple groups?
	RayClusterHeadCpuAnnotationKey      = "ray.io/head-resource-cpu"
	RayClusterHeadMemoryAnnotationKey   = "ray.io/head-resource-memory"
	RayClusterWorkerCpuAnnotationKey    = "ray.io/worker-resource-cpu"
	RayClusterWorkerMemoryAnnotationKey = "ray.io/head-resource-memory"
	RayClusterWorkerGpuAnnotationKey    = "ray.io/head-resource-gpu"
)
