package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// JobStatus is the Ray Job Status. https://docs.ray.io/en/latest/cluster/jobs-package-ref.html#jobstatus
type JobStatus string

const (
	JobStatusPending   JobStatus = "PENDING"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusStopped   JobStatus = "STOPPED"
	JobStatusSucceeded JobStatus = "SUCCEEDED"
	JobStatusFailed    JobStatus = "FAILED"
)

// JobDeploymentStatus indicates RayJob status including RayCluster lifecycle management and Job submission
type JobDeploymentStatus string

const (
	JobDeploymentStatusInitializing                  JobDeploymentStatus = "Initializing"
	JobDeploymentStatusFailedToGetOrCreateRayCluster JobDeploymentStatus = "FailedToGetOrCreateRayCluster"
	JobDeploymentStatusWaitForDashboard              JobDeploymentStatus = "WaitForDashboard"
	JobDeploymentStatusFailedJobDeploy               JobDeploymentStatus = "FailedJobDeploy"
	JobDeploymentStatusRunning                       JobDeploymentStatus = "Running"
	JobDeploymentStatusFailedToGetJobStatus          JobDeploymentStatus = "FailedToGetJobStatus"
	JobDeploymentStatusComplete                      JobDeploymentStatus = "Complete"
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
	// If jobId is not set, a new jobId will be auto-generated.
	JobId string `json:"jobId,omitempty"`
	// ShutdownAfterJobFinishes will determine whether to delete the ray cluster once rayJob succeed or failed.
	ShutdownAfterJobFinishes bool `json:"shutdownAfterJobFinishes,omitempty"`
	// TTLSecondsAfterFinished is the TTL to clean up RayCluster.
	// It's only working when ShutdownAfterJobFinishes set to true.
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`
	// RayClusterSpec is the cluster template to run the job
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
	Message             string              `json:"message,omitempty"`
	// Represents time when the job was acknowledged by the Ray cluster.
	// It is not guaranteed to be set in happens-before order across separate operations.
	// It is represented in RFC3339 form
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// Represents time when the job was ended.
	EndTime          *metav1.Time     `json:"endTime,omitempty"`
	RayClusterStatus RayClusterStatus `json:"rayClusterStatus,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+genclient
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
