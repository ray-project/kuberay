package v1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RayJobTemplate struct {
	// Standard object's metadata of the jobs created from this template.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the desired behavior of the job.
	// +optional
	Spec RayJobSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
}

// CronJobSpec describes how the job execution will look like and when it will actually run.
type RayCronJobSpec struct {
	// The schedule in Cron format, see https://en.wikipedia.org/wiki/Cron.
	Schedule string `json:"schedule"`

	// Specifies how to treat concurrent executions of a Job.
	// Valid values are:
	//
	// - "Allow" (default): allows CronJobs to run concurrently;
	// - "Forbid": forbids concurrent runs, skipping next run if previous run hasn't finished yet;
	// - "Replace": cancels currently running job and replaces it with a new one
	// +optional
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`

	// This flag tells the controller to suspend subsequent executions, it does
	// not apply to already started executions. Defaults to false.
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// Specifies the job that will be created when executing a CronJob.
	RayJobTemplate RayJobTemplate `json:"rayJobTemplate"`
}

// ConcurrencyPolicy describes how the job will be handled.
// Only one of the following concurrent policies may be specified.
// If none of the following policies is specified, the default one
// is AllowConcurrent.
type ConcurrencyPolicy string

const (
	// AllowConcurrent allows CronJobs to run concurrently.
	AllowConcurrent ConcurrencyPolicy = "Allow"

	// ForbidConcurrent forbids concurrent runs, skipping next run if previous
	// hasn't finished yet.
	ForbidConcurrent ConcurrencyPolicy = "Forbid"

	// ReplaceConcurrent cancels currently running job and replaces it with a new one.
	ReplaceConcurrent ConcurrencyPolicy = "Replace"
)

// CronJobStatus represents the current state of a cron job.
type RayCronJobStatus struct {
	LastScheduleTime   *metav1.Time         `json:"lastScheduleTime,omitempty"`
	LastSuccessfulTime *metav1.Time         `json:"lastSuccessfulTime,omitempty"`
	Active             []v1.ObjectReference `json:"active,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:categories=all
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +genclient
// CronJob represents the configuration of a single cron job.
type RayCronJob struct {
	Status            RayCronJobStatus `json:"status,omitempty"`
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              RayCronJobSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// CronJobList is a collection of cron jobs.
type RayCronJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RayCronJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RayCronJob{}, &RayCronJobList{})
}
