package util

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ResourceType string

const (
	RayCluster ResourceType = "raycluster"
	RayJob     ResourceType = "rayjob"
	RayService ResourceType = "rayservice"
)

const (
	RayGroup   = "ray.io"
	RayVersion = "v1"
)

var RayClusterGVR = schema.GroupVersionResource{
	Group:    RayGroup,
	Version:  RayVersion,
	Resource: "rayclusters",
}

var RayJobGVR = schema.GroupVersionResource{
	Group:    RayGroup,
	Version:  RayVersion,
	Resource: "rayjobs",
}

var RayServiceGVR = schema.GroupVersionResource{
	Group:    RayGroup,
	Version:  RayVersion,
	Resource: "rayservices",
}
