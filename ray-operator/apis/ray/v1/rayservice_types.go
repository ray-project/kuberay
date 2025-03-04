package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type ServiceStatus string

const (
	// `Running` means the RayService is ready to serve requests. `NotRunning` means it is not ready.
	// The naming is a bit confusing, but to maintain backward compatibility, we use `Running` instead of `Ready`.
	// Since KubeRay v1.3.0, `ServiceStatus` is equivalent to the `RayServiceReady` condition.
	// `ServiceStatus` is deprecated - please use conditions instead.
	Running    ServiceStatus = "Running"
	NotRunning ServiceStatus = ""
)

type RayServiceUpgradeType string

const (
	// During upgrade, IncrementalUpgrade strategy will create an upgraded cluster to gradually scale
	// and migrate traffic to using Gateway API.
	IncrementalUpgrade RayServiceUpgradeType = "IncrementalUpgrade"
	// During upgrade, NewCluster strategy will create new upgraded cluster and switch to it when it becomes ready
	NewCluster RayServiceUpgradeType = "NewCluster"
	// No new cluster will be created while the strategy is set to None
	None RayServiceUpgradeType = "None"
)

// These statuses should match Ray Serve's application statuses
// See `enum ApplicationStatus` in https://sourcegraph.com/github.com/ray-project/ray/-/blob/src/ray/protobuf/serve.proto for more details.
var ApplicationStatusEnum = struct {
	NOT_STARTED   string
	DEPLOYING     string
	RUNNING       string
	DEPLOY_FAILED string
	DELETING      string
	UNHEALTHY     string
}{
	NOT_STARTED:   "NOT_STARTED",
	DEPLOYING:     "DEPLOYING",
	RUNNING:       "RUNNING",
	DEPLOY_FAILED: "DEPLOY_FAILED",
	DELETING:      "DELETING",
	UNHEALTHY:     "UNHEALTHY",
}

// These statuses should match Ray Serve's deployment statuses
var DeploymentStatusEnum = struct {
	UPDATING  string
	HEALTHY   string
	UNHEALTHY string
}{
	UPDATING:  "UPDATING",
	HEALTHY:   "HEALTHY",
	UNHEALTHY: "UNHEALTHY",
}

type IncrementalUpgradeOptions struct {
	// The capacity of serve requests the upgraded cluster should scale to handle each interval.
	// Defaults to 100%.
	// +kubebuilder:default:=100
	MaxSurgePercent *int32 `json:"maxSurgePercent,omitempty"`
	// The percentage of traffic to switch to the upgraded RayCluster at a set interval after scaling by MaxSurgePercent.
	StepSizePercent *int32 `json:"stepSizePercent"`
	// The interval in seconds between transferring StepSize traffic from the old to new RayCluster.
	IntervalSeconds *int32 `json:"intervalSeconds"`
	// The name of the Gateway Class installed by the Kubernetes Cluster admin.
	GatewayClassName string `json:"gatewayClassName"`
}

type RayServiceUpgradeStrategy struct {
	// Type represents the strategy used when upgrading the RayService. Currently supports `NewCluster` and `None`.
	// +optional
	Type *RayServiceUpgradeType `json:"type,omitempty"`
	// IncrementalUpgradeOptions defines the behavior of an IncrementalUpgrade.
	IncrementalUpgradeOptions *IncrementalUpgradeOptions `json:"incrementalUpgradeOptions,omitempty"`
}

// RayServiceSpec defines the desired state of RayService
type RayServiceSpec struct {
	// RayClusterDeletionDelaySeconds specifies the delay, in seconds, before deleting old RayClusters.
	// The default value is 60 seconds.
	// +kubebuilder:validation:Minimum=0
	// +optional
	RayClusterDeletionDelaySeconds *int32 `json:"rayClusterDeletionDelaySeconds,omitempty"`
	// Deprecated: This field is not used anymore. ref: https://github.com/ray-project/kuberay/issues/1685
	// +optional
	ServiceUnhealthySecondThreshold *int32 `json:"serviceUnhealthySecondThreshold,omitempty"`
	// Deprecated: This field is not used anymore. ref: https://github.com/ray-project/kuberay/issues/1685
	// +optional
	DeploymentUnhealthySecondThreshold *int32 `json:"deploymentUnhealthySecondThreshold,omitempty"`
	// ServeService is the Kubernetes service for head node and worker nodes who have healthy http proxy to serve traffics.
	// +optional
	ServeService *corev1.Service `json:"serveService,omitempty"`
	// Gateway is the Gateway object for the RayService to serve traffics during an IncrementalUpgrade.
	Gateway *gwv1.Gateway `json:"gateway,omitempty"`
	// HTTPRoute is the HTTPRoute object for the RayService to split traffics during an IncrementalUpgrade.
	HTTPRoute *gwv1.HTTPRoute `json:"httpRoute,omitempty"`
	// UpgradeStrategy defines the scaling policy used when upgrading the RayService.
	// +optional
	UpgradeStrategy *RayServiceUpgradeStrategy `json:"upgradeStrategy,omitempty"`
	// Important: Run "make" to regenerate code after modifying this file
	// Defines the applications and deployments to deploy, should be a YAML multi-line scalar string.
	// +optional
	ServeConfigV2  string         `json:"serveConfigV2,omitempty"`
	RayClusterSpec RayClusterSpec `json:"rayClusterConfig"`
	// If the field is set to true, the value of the label `ray.io/serve` on the head Pod should always be false.
	// Therefore, the head Pod's endpoint will not be added to the Kubernetes Serve service.
	// +optional
	ExcludeHeadPodFromServeSvc bool `json:"excludeHeadPodFromServeSvc,omitempty"`
}

// RayServiceStatuses defines the observed state of RayService
type RayServiceStatuses struct {
	LastUpdateTime       *metav1.Time       `json:"lastUpdateTime,omitempty"`
	ServiceStatus        ServiceStatus      `json:"serviceStatus,omitempty"`
	Conditions           []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
	ActiveServiceStatus  RayServiceStatus   `json:"activeServiceStatus,omitempty"`
	PendingServiceStatus RayServiceStatus   `json:"pendingServiceStatus,omitempty"`
	ObservedGeneration   int64              `json:"observedGeneration,omitempty"`
	NumServeEndpoints    int32              `json:"numServeEndpoints,omitempty"`
}

type RayServiceStatus struct {
	Applications            map[string]AppStatus `json:"applicationStatuses,omitempty"`
	TargetCapacity          *int32               `json:"targetCapacity,omitempty"`
	TrafficRoutedPercent    *int32               `json:"trafficRoutedPercent,omitempty"`
	LastTrafficMigratedTime *metav1.Time         `json:"lastTrafficMigratedTime,omitempty"`
	RayClusterName          string               `json:"rayClusterName,omitempty"`
	RayClusterStatus        RayClusterStatus     `json:"rayClusterStatus,omitempty"`
}

type AppStatus struct {
	// +optional
	Deployments map[string]ServeDeploymentStatus `json:"serveDeploymentStatuses,omitempty"`
	// +optional
	Status string `json:"status,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
}

// ServeDeploymentStatus defines the current state of a Serve deployment
type ServeDeploymentStatus struct {
	// +optional
	Status string `json:"status,omitempty"`
	// +optional
	Message string `json:"message,omitempty"`
}

type (
	RayServiceConditionType   string
	RayServiceConditionReason string
)

const (
	// RayServiceReady means users can send requests to the underlying cluster and the number of serve endpoints is greater than 0.
	RayServiceReady RayServiceConditionType = "Ready"
	// UpgradeInProgress means the RayService is currently performing a zero-downtime upgrade.
	UpgradeInProgress RayServiceConditionType = "UpgradeInProgress"
)

const (
	RayServiceInitializing         RayServiceConditionReason = "Initializing"
	ZeroServeEndpoints             RayServiceConditionReason = "ZeroServeEndpoints"
	NonZeroServeEndpoints          RayServiceConditionReason = "NonZeroServeEndpoints"
	BothActivePendingClustersExist RayServiceConditionReason = "BothActivePendingClustersExist"
	NoPendingCluster               RayServiceConditionReason = "NoPendingCluster"
	NoActiveCluster                RayServiceConditionReason = "NoActiveCluster"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:categories=all
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="service status",type=string,JSONPath=".status.serviceStatus"
// +kubebuilder:printcolumn:name="num serve endpoints",type=string,JSONPath=".status.numServeEndpoints"
// +genclient
// RayService is the Schema for the rayservices API
type RayService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              RayServiceSpec     `json:"spec,omitempty"`
	Status            RayServiceStatuses `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RayServiceList contains a list of RayService
type RayServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RayService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RayService{}, &RayServiceList{})
}
