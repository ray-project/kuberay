/*
Copyright 2021 ByteDance Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RayClusterSpec defines the desired state of RayCluster
type RayClusterSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// RayVersion is the version of ray core. Both Head, Worker and Autoscaler will use this ray version.
	RayVersion string `json:"rayVersion,omitempty"`
	// HeadNodeSpec is the spec for the head node
	HeadNodeSpec HeadNodeSpec `json:"headNodeSpec"`
	// WorkerNodeSpec  the specs for the worker node
	WorkerNodeSpec []WorkerNodeSpec `json:"workerNodeSpec,omitempty"`
	// AutoscalingEnabled indicates if we want to enable autoscaling for this cluster. By default it's disabled in v1alpha1
	AutoscalingEnabled *bool `json:"autoscalingEnabled,omitempty"`
	// TODO (Jeffwan@): Add autoscaling support later, autoscaler
}

// HeadNodeSpec is the spec for the ray head pod
type HeadNodeSpec struct {
	// Params indicates Params of the ray start commands used in ray head pod.
	Params map[string]string `json:"params"`
	// ServiceType indicates service type user want to expose head service
	ServiceType v1.ServiceType `json:"serviceType,omitempty"`
	// Template indicates pod template used by ray head pod
	Template v1.PodTemplateSpec `json:"template"`
}

// WorkerNodeSpec are the specs for the worker pods
type WorkerNodeSpec struct {
	// One ray cluster supports multiple node groups, like cpu group, gpu group, etc.
	NodeGroupName string `json:"groupName"`
	// Replicas means desired replicas of nodes in this group.
	Replicas *int32 `json:"replicas"`
	// MinReplicas means minimum size of the node group. This is used by autoscaler.
	MinReplicas *int32 `json:"minReplicas"`
	// MaxReplicas means maximum size of the node group. This is used by autoscaler.
	MaxReplicas *int32 `json:"maxReplicas"`
	// Params are the Params of the ray start command used in ray node group.
	Params map[string]string `json:"params"`
	// Ray node group pod template
	Template v1.PodTemplateSpec `json:"template"`
}

// RayNodeType is the type of a ray node
type RayNodeType string

const (
	// HeadNode means that this pod is ray head
	HeadNode RayNodeType = "head"
	// WorkerNode means that this pod is ray worker
	WorkerNode RayNodeType = "worker"
)

// The overall state of the Ray cluster.
type ClusterState string

const (
	Healthy    ClusterState = "healthy"
	NotHealthy ClusterState = "healthy"
)

// RayClusterStatus defines the observed state of RayCluster
type RayClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The overall state of the Ray cluster.
	State ClusterState `json:"state,omitempty"`
	// AvailableReplicas indicates how many replicas are available in the cluster
	AvailableReplicas int32 `json:"availableReplicas,omitempty"`
	// DesiredReplicas indicates overall desired replicas claimed by the user at the cluster level.
	DesiredReplicas int32 `json:"desiredReplicas,omitempty"`
	// MinReplicas indicates sum of minimum replicas of each node group.
	MinReplicas int32 `json:"minimumReplicas,omitempty"`
	// MaxReplicas indicates sum of maximum replicas of each node group.
	MaxReplicas int32 `json:"maximumReplicas,omitempty"`
	// LastUpdateTime indicates last update timestamp for this cluster status.
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// RayCluster is the Schema for the rayclusters API
type RayCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RayClusterSpec   `json:"spec,omitempty"`
	Status RayClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RayClusterList contains a list of RayCluster
type RayClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RayCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RayCluster{}, &RayClusterList{})
}
