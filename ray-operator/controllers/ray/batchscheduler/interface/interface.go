package schedulerinterface

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

// BatchScheduler manages submitting RayCluster pods to a third-party scheduler.
type BatchScheduler interface {
	// Name corresponds to the schedulerName in Kubernetes:
	// https://kubernetes.io/docs/tasks/extend-kubernetes/configure-multiple-schedulers/
	Name() string

	// DoBatchSchedulingOnSubmission handles submitting the RayCluster/RayJob/RayService to the batch scheduler on creation / update
	// For most batch schedulers, this results in the creation of a PodGroup.
	DoBatchSchedulingOnSubmission(ctx context.Context, object client.Object) error

	// AddMetadataToChildResourceFromRayCluster enriches child resource with metadata necessary to tie it to the scheduler.
	// For example, setting labels for queues / priority, and setting schedulerName.
	AddMetadataToChildResourceFromRayCluster(ctx context.Context, rayCluster *rayv1.RayCluster, groupName string, pod *corev1.Pod)

	// AddMetadataToChildResourceFromRayJob enriches child resource with metadata necessary to tie it to the scheduler.
	// For example, setting labels for queues / priority, and setting schedulerName.
	AddMetadataToChildResourceFromRayJob(ctx context.Context, rayJob *rayv1.RayJob, rayCluster *rayv1.RayCluster, submitterTemplate *corev1.PodTemplateSpec)
}

// BatchSchedulerFactory handles initial setup of the scheduler plugin by registering the
// necessary callbacks with the operator, and the creation of the BatchScheduler itself.
type BatchSchedulerFactory interface {
	// New creates a new BatchScheduler for the scheduler plugin.
	New(ctx context.Context, config *rest.Config, cli client.Client) (BatchScheduler, error)

	// AddToScheme adds the types in this scheduler to the given scheme (runs during init).
	AddToScheme(scheme *runtime.Scheme)

	// ConfigureReconciler configures the RayCluster Reconciler in the process of being built by
	// adding watches for its scheduler-specific custom resource types, and any other needed setup.
	ConfigureReconciler(b *builder.Builder) *builder.Builder
}

type DefaultBatchScheduler struct{}

type DefaultBatchSchedulerFactory struct{}

func GetDefaultPluginName() string {
	return "default"
}

func (d *DefaultBatchScheduler) Name() string {
	return GetDefaultPluginName()
}

func (d *DefaultBatchScheduler) DoBatchSchedulingOnSubmission(_ context.Context, _ client.Object) error {
	return nil
}

func (d *DefaultBatchScheduler) AddMetadataToChildResourceFromRayCluster(_ context.Context, _ *rayv1.RayCluster, _ string, _ *corev1.Pod) {
}

// AddMetadataToChildResourceFromRayJob Add necessary metadata from RayJob to RayCluster and submitter pod template for BatchScheduler
func (d *DefaultBatchScheduler) AddMetadataToChildResourceFromRayJob(_ context.Context, _ *rayv1.RayJob, _ *rayv1.RayCluster, _ /*submitterTemplate*/ *corev1.PodTemplateSpec) {
}

func (df *DefaultBatchSchedulerFactory) New(_ context.Context, _ *rest.Config, _ client.Client) (BatchScheduler, error) {
	return &DefaultBatchScheduler{}, nil
}

func (df *DefaultBatchSchedulerFactory) AddToScheme(_ *runtime.Scheme) {
}

func (df *DefaultBatchSchedulerFactory) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b
}
