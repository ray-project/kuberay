package util

import "time"

// ClientOptions contains configuration needed to create a Kubernetes client
type ClientOptions struct {
	QPS   float32
	Burst int
}

// TODO: this needs to be revised.
const (
	// Label keys
	RayClusterNameLabelKey            = "ray.io/cluster-name"
	RayClusterUserLabelKey            = "ray.io/user"
	RayClusterVersionLabelKey         = "ray.io/version"
	RayClusterEnvironmentLabelKey     = "ray.io/environment"
	KubernetesApplicationNameLabelKey = "app.kubernetes.io/name"
	KubernetesManagedByLabelKey       = "app.kubernetes.io/managed-by"

	// Annotation keys
	// Role level
	RayClusterComputeTemplateAnnotationKey = "ray.io/compute-template"
	RayClusterImageAnnotationKey           = "ray.io/compute-image"

	RayClusterDefaultImageRepository = "rayproject/ray"

	// Max retry times for HTTP Client
	HTTPClientDefaultMaxRetry = 3

	// Retry backoff settings
	HTTPClientDefaultBackoffBase = float64(2)
	HTTPClientDefaultInitBackoff = 500 * time.Millisecond
	HTTPClientDefaultMaxBackoff  = 10 * time.Second

	// Overall timeout for retries
	HTTPClientDefaultOverallTimeout = 30 * time.Second
)

const (
	// The application name
	ApplicationName = "kuberay"

	// The component name for apiserver
	ComponentName = "kuberay-apiserver"

	// Timeout for apiserver gRPC server
	GRPCServerDefaultTimeout = 60 * time.Second
)
