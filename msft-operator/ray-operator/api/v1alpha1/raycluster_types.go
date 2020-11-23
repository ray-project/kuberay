package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//	appsv1 "k8s.io/api/apps/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
//var app appsv1.Deployment{}
// RayClusterSpec defines the desired state of RayCluster
type RayClusterSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// HeadService is service to abstract the head pod. it will be used by the workers to connect to the head pod
	HeadService v1.Service `json:"headService"`
	// HeadGroupSpecs are the spec for the head pod
	HeadGroupSpec HeadGroupSpec `json:"headGroupSpec"`
	// WorkerGroupSpecs are the specs for the worker pods
	WorkerGroupsSpec []WorkerGroupSpec `json:"workerGroupsSpec,omitempty"`
	// RayVersion is the version of ray being used. this affects the command used to start ray
	RayVersion string `json:"rayVersion,omitempty"`
}

// HeadGroupSpec are the spec for the head pod
type HeadGroupSpec struct {
	// Number of desired pods in this pod group. This is a pointer to distinguish between explicit
	// zero and not specified. Defaults to 1.
	Replicas *int32 `json:"replicas"`
	// RayStartParams are the params of the start command: node-manager-port, object-store-memory, ...
	RayStartParams map[string]string `json:"rayStartParams"`
	// Template is the eaxct pod template used in K8s depoyments, statefulsets, etc.
	Template v1.PodTemplateSpec `json:"template"`
}

// WorkerGroupSpec are the specs for the worker pods
type WorkerGroupSpec struct {
	// we can have multiple worker groups, we distinguish them by name
	GroupName string `json:"groupName"`
	// Replicas Number of desired pods in this pod group. This is a pointer to distinguish between explicit
	// zero and not specified. Defaults to 1.
	Replicas *int32 `json:"replicas"`
	// MinReplicas defaults to 1
	MinReplicas *int32 `json:"minReplicas"`
	// MaxReplicas defaults to maxInt32
	MaxReplicas *int32 `json:"maxReplicas"`
	// RayStartParams are the params of the start command: address, object-store-memory, ...
	RayStartParams map[string]string `json:"rayStartParams"`
	// Template a pod template for the worker
	Template v1.PodTemplateSpec `json:"template"`
	//ScaleStrategy defines which pods to remove
	ScaleStrategy ScaleStrategy `json:"scaleStrategy,omitempty"`
}

// ScaleStrategy to remove workers
type ScaleStrategy struct {
	// WorkersToDelete workers to be deleted
	WorkersToDelete []string `json:"workersToDelete,omitempty"`
}

// RayClusterStatus defines the observed state of RayCluster
type RayClusterStatus struct {

	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// Status reflects the status of the cluster
	AvailableReplicas int32 `json:"availableReplicas,omitempty"`
	// +nullable
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
}

// RayNodeType  the type of a ray node: head/worker
type RayNodeType string

const (
	// HeadNode means that this pod will be ray cluster head
	HeadNode RayNodeType = "head"
	// WorkerNode means that this pod will be ray cluster worker
	WorkerNode RayNodeType = "worker"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// +kubebuilder:object:root=true

// RayCluster is the Schema for the RayClusters API
type RayCluster struct {
	// Standard object metadata.
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Specification of the desired behavior of the RayCluster.
	Spec   RayClusterSpec   `json:"spec,omitempty"`
	Status RayClusterStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RayClusterList contains a list of RayCluster
type RayClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RayCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RayCluster{}, &RayClusterList{})
}
