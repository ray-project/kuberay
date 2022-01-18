package util

// ClientOptions contains configuration needed to create a Kubernetes client
type ClientOptions struct {
	QPS   float32
	Burst int
}

// TODO: this needs to be revised.
const (
	// Label keys
	RayClusterNameLabelKey        = "ray.io/cluster-name"
	RayClusterUserLabelKey        = "ray.io/user"
	RayClusterVersionLabelKey     = "ray.io/version"
	RayClusterEnvironmentLabelKey = "ray.io/environment"

	// Annotation keys
	// Role level
	RayClusterComputeTemplateAnnotationKey = "ray.io/compute-template"
	RayClusterImageAnnotationKey           = "ray.io/compute-image"

	RayClusterDefaultImageRepository = "rayproject/ray"
)
