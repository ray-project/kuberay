package v1

const (
	// In KubeRay, the Ray container must be the first application container in a head or worker Pod.
	RayContainerIndex = 0

	// Use as container env variable
	RAY_REDIS_ADDRESS = "RAY_REDIS_ADDRESS"

	// Ray GCS FT related annotations
	RayFTEnabledAnnotationKey = "ray.io/ft-enabled"
)
