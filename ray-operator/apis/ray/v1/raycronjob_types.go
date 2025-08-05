package v1

import (
	batchv1 "k8s.io/api/batch/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type CronJobStatus string

type RayCronJobSpec struct {

	// The schedule in Cron format, see https://en.wikipedia.org/wiki/Cron.
	Schedule string `json:"schedule" protobuf:"bytes,1,opt,name=schedule"`

	// The time zone name for the given schedule, see https://en.wikipedia.org/wiki/List_of_tz_database_time_zones.
	// If not specified, this will default to the time zone of the kube-controller-manager process.
	// The set of valid time zone names and the time zone offset is loaded from the system-wide time zone
	// database by the API server during CronJob validation and the controller manager during execution.
	// If no system-wide time zone database can be found a bundled version of the database is used instead.
	// If the time zone name becomes invalid during the lifetime of a CronJob or due to a change in host
	// configuration, the controller will stop creating new new Jobs and will create a system event with the
	// reason UnknownTimeZone.
	// More information can be found in https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/#time-zones
	// +optional
	TimeZone *string `json:"timeZone,omitempty" protobuf:"bytes,8,opt,name=timeZone"`

	// Optional deadline in seconds for starting the job if it misses scheduled
	// time for any reason.  Missed jobs executions will be counted as failed ones.
	// +optional
	StartingDeadlineSeconds *int64 `json:"startingDeadlineSeconds,omitempty" protobuf:"varint,2,opt,name=startingDeadlineSeconds"`

	// Specifies how to treat concurrent executions of a Job.
	// Valid values are:
	//
	// - "Allow" (default): allows CronJobs to run concurrently;
	// - "Forbid": forbids concurrent runs, skipping next run if previous run hasn't finished yet;
	// - "Replace": cancels currently running job and replaces it with a new one
	// +optional
	ConcurrencyPolicy batchv1.ConcurrencyPolicy `json:"concurrencyPolicy,omitempty" protobuf:"bytes,3,opt,name=concurrencyPolicy,casttype=ConcurrencyPolicy"`

	// This flag tells the controller to suspend subsequent executions, it does
	// not apply to already started executions.  Defaults to false.
	// +optional
	Suspend *bool `json:"suspend,omitempty" protobuf:"varint,4,opt,name=suspend"`

	// Specifies the job that will be created when executing a CronJob.
	RayJobTemplate RayJobSpec `json:"rayJobTemplate" protobuf:"bytes,5,opt,name=jobTemplate"`

	// The number of successful finished jobs to retain.
	// This is a pointer to distinguish between explicit zero and not specified.
	// Defaults to 3.
	// +optional
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit,omitempty" protobuf:"varint,6,opt,name=successfulJobsHistoryLimit"`

	// The number of failed finished jobs to retain.
	// This is a pointer to distinguish between explicit zero and not specified.
	// Defaults to 1.
	// +optional
	FailedJobsHistoryLimit *int32 `json:"failedJobsHistoryLimit,omitempty" protobuf:"varint,7,opt,name=failedJobsHistoryLimit"`
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

type RayCronJobStatus struct {
	// lastScheduledTime is the last time the job was successfully scheduled.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
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
type RayCronJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec RayCronJobSpec `json:"spec,omitempty"`
	// +optional
	Status RayCronJobStatus `json:"status,omitempty"`
}

// DeepCopyObject implements runtime.Object.
func (r *RayCronJob) DeepCopyObject() runtime.Object {
	panic("unimplemented")
}

// GetObjectKind implements runtime.Object.
// Subtle: this method shadows the method (TypeMeta).GetObjectKind of RayCronJob.TypeMeta.
func (r *RayCronJob) GetObjectKind() schema.ObjectKind {
	panic("unimplemented")
}
