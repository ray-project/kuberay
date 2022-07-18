package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type JobStatus string

const (
	JobStatusPending   JobStatus = "PENDING"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusStopped   JobStatus = "STOPPED"
	JobStatusSucceeded JobStatus = "SUCCEEDED"
	JobStatusFailed    JobStatus = "FAILED"
)

type JobDeploymentStatus string

const (
	JobDeploymentStatusInitializing                  JobDeploymentStatus = "Initializing"
	JobDeploymentStatusFailedToGetOrCreateRayCluster JobDeploymentStatus = "FailedToGetOrCreateRayCluster"
	JobDeploymentStatusWaitForDashboard              JobDeploymentStatus = "WaitForDashboard"
	JobDeploymentStatusFailedJobDeploy               JobDeploymentStatus = "FailedJobDeploy"
	JobDeploymentStatusRunning                       JobDeploymentStatus = "Running"
	JobDeploymentStatusFailedToGetJobStatus          JobDeploymentStatus = "FailedToGetJobStatus"
)

// RayJobSpec defines the desired state of RayJob
type RayJobSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Entrypoint string `json:"entrypoint"`
	// Metadata is data to store along with this job.
	Metadata map[string]string `json:"metadata,omitempty"`
	// RuntimeEnv is base64 encoded.
	RuntimeEnv string `json:"runtimeEnv,omitempty"`
	// TODO: If set to true, the rayCluster will be deleted after the rayJob finishes
	ShutdownAfterJobFinishes bool `json:"shutdownAfterJobFinishes,omitempty"`
	// If jobId is not set, a new jobId will be auto-generated.
	JobId          string         `json:"jobId,omitempty"`
	RayClusterSpec RayClusterSpec `json:"rayClusterSpec,omitempty"`
	// clusterSelector is used to select running rayclusters by labels
	ClusterSelector map[string]string `json:"clusterSelector,omitempty"`
}

// RayJobStatus defines the observed state of RayJob
type RayJobStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	JobId               string              `json:"jobId,omitempty"`
	RayClusterName      string              `json:"rayClusterName,omitempty"`
	DashboardURL        string              `json:"dashboardURL,omitempty"`
	JobStatus           JobStatus           `json:"jobStatus,omitempty"`
	JobDeploymentStatus JobDeploymentStatus `json:"jobDeploymentStatus,omitempty"`
	RayClusterStatus    RayClusterStatus    `json:"rayClusterStatus,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// RayJob is the Schema for the rayjobs API
type RayJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RayJobSpec   `json:"spec,omitempty"`
	Status RayJobStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RayJobList contains a list of RayJob
type RayJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RayJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RayJob{}, &RayJobList{})
}
