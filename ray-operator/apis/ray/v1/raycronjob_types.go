package v1

import (
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

// RayCronJobSpec describes how the job execution will look like and when it will actually run.
type RayCronJobSpec struct {
	// The schedule in Cron format, see https://en.wikipedia.org/wiki/Cron.
	Schedule string `json:"schedule"`

	// Specifies the job that will be created when executing a CronJob.
	RayJobTemplate RayJobTemplate `json:"rayJobTemplate"`
}

// CronJobStatus represents the current state of a cron job.
type RayCronJobStatus struct {
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:categories=all
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +genclient
// RayCronJob represents the configuration of a single ray cron job.
// It will currently schedule and run one ray job at the correct time based on a cron string
type RayCronJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              RayCronJobSpec   `json:"spec,omitempty"`
	Status            RayCronJobStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RayCronJobList is a collection of cron jobs.
type RayCronJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RayCronJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RayCronJob{}, &RayCronJobList{})
}
