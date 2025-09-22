package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// JobStatus is the Ray Job Status.
type JobStatus string

// https://docs.ray.io/en/latest/cluster/running-applications/job-submission/jobs-package-ref.html#jobstatus
//
// NOTICE: [AllJobStatuses] should be kept in sync with all job statuses below.
const (
	JobStatusNew       JobStatus = ""
	JobStatusPending   JobStatus = "PENDING"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusStopped   JobStatus = "STOPPED"
	JobStatusSucceeded JobStatus = "SUCCEEDED"
	JobStatusFailed    JobStatus = "FAILED"
)

var AllJobStatuses = []JobStatus{
	JobStatusNew,
	JobStatusPending,
	JobStatusRunning,
	JobStatusStopped,
	JobStatusSucceeded,
	JobStatusFailed,
}

// This function should be synchronized with the function `is_terminal()` in Ray Job.
func IsJobTerminal(status JobStatus) bool {
	terminalStatusSet := map[JobStatus]struct{}{
		JobStatusStopped: {}, JobStatusSucceeded: {}, JobStatusFailed: {},
	}
	_, ok := terminalStatusSet[status]
	return ok
}

// JobDeploymentStatus indicates RayJob status including RayCluster lifecycle management and Job submission
type JobDeploymentStatus string

const (
	JobDeploymentStatusNew              JobDeploymentStatus = ""
	JobDeploymentStatusInitializing     JobDeploymentStatus = "Initializing"
	JobDeploymentStatusRunning          JobDeploymentStatus = "Running"
	JobDeploymentStatusComplete         JobDeploymentStatus = "Complete"
	JobDeploymentStatusFailed           JobDeploymentStatus = "Failed"
	JobDeploymentStatusValidationFailed JobDeploymentStatus = "ValidationFailed"
	JobDeploymentStatusSuspending       JobDeploymentStatus = "Suspending"
	JobDeploymentStatusSuspended        JobDeploymentStatus = "Suspended"
	JobDeploymentStatusRetrying         JobDeploymentStatus = "Retrying"
	JobDeploymentStatusWaiting          JobDeploymentStatus = "Waiting"
)

// IsJobDeploymentTerminal returns true if the given JobDeploymentStatus
// is in a terminal state. Terminal states are either Complete or Failed.
func IsJobDeploymentTerminal(status JobDeploymentStatus) bool {
	terminalStatusSet := map[JobDeploymentStatus]struct{}{
		JobDeploymentStatusComplete: {}, JobDeploymentStatusFailed: {},
	}
	_, ok := terminalStatusSet[status]
	return ok
}

// JobFailedReason indicates the reason the RayJob changes its JobDeploymentStatus to 'Failed'
type JobFailedReason string

const (
	SubmissionFailed                                 JobFailedReason = "SubmissionFailed"
	DeadlineExceeded                                 JobFailedReason = "DeadlineExceeded"
	AppFailed                                        JobFailedReason = "AppFailed"
	JobDeploymentStatusTransitionGracePeriodExceeded JobFailedReason = "JobDeploymentStatusTransitionGracePeriodExceeded"
	ValidationFailed                                 JobFailedReason = "ValidationFailed"
)

type JobSubmissionMode string

const (
	K8sJobMode      JobSubmissionMode = "K8sJobMode"      // Submit job via Kubernetes Job
	HTTPMode        JobSubmissionMode = "HTTPMode"        // Submit job via HTTP request
	InteractiveMode JobSubmissionMode = "InteractiveMode" // Don't submit job in KubeRay. Instead, wait for user to submit job and provide the job submission ID.
	SidecarMode     JobSubmissionMode = "SidecarMode"     // Submit job via a sidecar container in the Ray head Pod
)

type DeletionPolicyType string

type DeletionStrategy struct {
	OnSuccess DeletionPolicy `json:"onSuccess"`
	OnFailure DeletionPolicy `json:"onFailure"`
}

type DeletionPolicy struct {
	// Valid values are 'DeleteCluster', 'DeleteWorkers', 'DeleteSelf' or 'DeleteNone'.
	// +kubebuilder:validation:XValidation:rule="self in ['DeleteCluster', 'DeleteWorkers', 'DeleteSelf', 'DeleteNone']",message="the policy field value must be either 'DeleteCluster', 'DeleteWorkers', 'DeleteSelf', or 'DeleteNone'"
	Policy *DeletionPolicyType `json:"policy"`
}

const (
	DeleteCluster DeletionPolicyType = "DeleteCluster" // To delete the entire RayCluster custom resource on job completion.
	DeleteWorkers DeletionPolicyType = "DeleteWorkers" // To delete only the workers on job completion.
	DeleteSelf    DeletionPolicyType = "DeleteSelf"    // To delete the RayJob custom resource (and all associated resources) on job completion.
	DeleteNone    DeletionPolicyType = "DeleteNone"    // To delete no resources on job completion.
)

type SubmitterConfig struct {
	// BackoffLimit of the submitter k8s job.
	// +optional
	BackoffLimit *int32 `json:"backoffLimit,omitempty"`
}

// `RayJobStatusInfo` is a subset of `RayJobInfo` from `dashboard_httpclient.py`.
// This subset is used to store information in the CR status.
//
// TODO(kevin85421): We can consider exposing the whole `RayJobInfo` in the CR status
// after careful consideration. In that case, we can remove `RayJobStatusInfo`.
type RayJobStatusInfo struct {
	StartTime *metav1.Time `json:"startTime,omitempty"`
	EndTime   *metav1.Time `json:"endTime,omitempty"`
}

// RayJobSpec defines the desired state of RayJob
type RayJobSpec struct {
	ActiveDeadlineSeconds           *int32                  `json:"activeDeadlineSeconds,omitempty"`
	BackoffLimit                    *int32                  `json:"backoffLimit,omitempty"`
	RayClusterSpec                  *RayClusterSpec         `json:"rayClusterSpec,omitempty"`
	SubmitterPodTemplate            *corev1.PodTemplateSpec `json:"submitterPodTemplate,omitempty"`
	Metadata                        map[string]string       `json:"metadata,omitempty"`
	ClusterSelector                 map[string]string       `json:"clusterSelector,omitempty"`
	SubmitterConfig                 *SubmitterConfig        `json:"submitterConfig,omitempty"`
	ManagedBy                       *string                 `json:"managedBy,omitempty"`
	DeletionStrategy                *DeletionStrategy       `json:"deletionStrategy,omitempty"`
	SubmitterFinishedTimeoutSeconds *int32                  `json:"submitterFinishedTimeoutSeconds,omitempty"`
	RuntimeEnvYAML                  string                  `json:"runtimeEnvYAML,omitempty"`
	JobId                           string                  `json:"jobId,omitempty"`
	SubmissionMode                  JobSubmissionMode       `json:"submissionMode,omitempty"`
	EntrypointResources             string                  `json:"entrypointResources,omitempty"`
	Entrypoint                      string                  `json:"entrypoint,omitempty"`
	EntrypointNumCpus               float32                 `json:"entrypointNumCpus,omitempty"`
	EntrypointNumGpus               float32                 `json:"entrypointNumGpus,omitempty"`
	TTLSecondsAfterFinished         int32                   `json:"ttlSecondsAfterFinished,omitempty"`
	ShutdownAfterJobFinishes        bool                    `json:"shutdownAfterJobFinishes,omitempty"`
	Suspend                         bool                    `json:"suspend,omitempty"`
}

// RayJobStatus defines the observed state of RayJob
type RayJobStatus struct {
	RayJobStatusInfo      RayJobStatusInfo    `json:"rayJobInfo,omitempty"`
	StartTime             *metav1.Time        `json:"startTime,omitempty"`
	SubmitterFinishedTime *metav1.Time        `json:"submitterFinishedTime,omitempty"`
	Failed                *int32              `json:"failed,omitempty"`
	Succeeded             *int32              `json:"succeeded,omitempty"`
	EndTime               *metav1.Time        `json:"endTime,omitempty"`
	DashboardURL          string              `json:"dashboardURL,omitempty"`
	Message               string              `json:"message,omitempty"`
	Reason                JobFailedReason     `json:"reason,omitempty"`
	JobDeploymentStatus   JobDeploymentStatus `json:"jobDeploymentStatus,omitempty"`
	JobStatus             JobStatus           `json:"jobStatus,omitempty"`
	RayClusterName        string              `json:"rayClusterName,omitempty"`
	JobId                 string              `json:"jobId,omitempty"`
	RayClusterStatus      RayClusterStatus    `json:"rayClusterStatus,omitempty"`
	ObservedGeneration    int64               `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:categories=all
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="job status",type=string,JSONPath=".status.jobStatus",priority=0
// +kubebuilder:printcolumn:name="deployment status",type=string,JSONPath=".status.jobDeploymentStatus",priority=0
// +kubebuilder:printcolumn:name="ray cluster name",type="string",JSONPath=".status.rayClusterName",priority=0
// +kubebuilder:printcolumn:name="start time",type=string,JSONPath=".status.startTime",priority=0
// +kubebuilder:printcolumn:name="end time",type=string,JSONPath=".status.endTime",priority=0
// +kubebuilder:printcolumn:name="age",type="date",JSONPath=".metadata.creationTimestamp",priority=0
// +genclient
// RayJob is the Schema for the rayjobs API
type RayJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              RayJobSpec   `json:"spec,omitempty"`
	Status            RayJobStatus `json:"status,omitempty"`
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
