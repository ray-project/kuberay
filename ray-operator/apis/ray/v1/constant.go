package v1

// In KubeRay, the Ray container must be the first application container in a head or worker Pod.
const RayContainerIndex = 0

// Use as container env variable
const RAY_REDIS_ADDRESS = "RAY_REDIS_ADDRESS"

// Ray GCS FT related annotations
const RayFTEnabledAnnotationKey = "ray.io/ft-enabled"
