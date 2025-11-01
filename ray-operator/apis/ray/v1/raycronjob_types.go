package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// RayCronJobSpec defines the desired state of RayCronJob
type RayCronJobSpec struct {
	// JobTemplate defines the job spec that will be created by cron scheduling
	JobTemplate *RayJobSpec `json:"jobTemplate"`
	// Schedule is the cron schedule string
	Schedule string `json:"schedule"`
}

// The overall state of the RayCronJob.
type ScheduleStatus string

const (
	StatusNew              ScheduleStatus = "new"
	StatusScheduled        ScheduleStatus = "scheduled"
	StatusValidationFailed ScheduleStatus = "validationFailed"
)

// RayCronJobStatus defines the observed state of RayCronJob
type RayCronJobStatus struct {
	// LastScheduleTime is the last time the RayJob is being scheduled by this RayCronJob
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
	// +optional
	ScheduleStatus ScheduleStatus `json:"scheduleStatus,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// RayCronJob is the Schema for the raycronjobs API
type RayCronJob struct {
	metav1.TypeMeta   `json:",inline"`
	Spec              RayCronJobSpec   `json:"spec,omitempty"`
	Status            RayCronJobStatus `json:"status,omitempty"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

//+kubebuilder:object:root=true

// RayCronJobList contains a list of RayCronJob
type RayCronJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RayCronJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RayCronJob{}, &RayCronJobList{})
}
