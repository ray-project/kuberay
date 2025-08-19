package kaischeduler

// This KAI plugin relies on KAI-Scheduler's
// built-in PodGrouper to create PodGroups at
// runtime, so the plugin itself only needs to:
//   1. expose the scheduler name,
//   2. stamp pods with schedulerName + queue label.
// No PodGroup create/patch logic is included.

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
)

const (
	QueueLabelName = "kai.scheduler/queue"
)

type KaiScheduler struct {
	log logr.Logger
}

type KaiSchedulerFactory struct{}

func GetPluginName() string { return "kai-scheduler" }

func (k *KaiScheduler) Name() string { return GetPluginName() }

func (k *KaiScheduler) DoBatchSchedulingOnSubmission(_ context.Context, object client.Object) error {
	_, ok := object.(*rayv1.RayCluster)
	if !ok {
		return fmt.Errorf("currently only RayCluster is supported, got %T", object)
	}
	// yunikorn doesn't require any resources to be created upfront
	// this is a no-opt for this implementation
	return nil
}

func (k *KaiScheduler) AddMetadataToPod(_ context.Context, app *rayv1.RayCluster, _ string, pod *corev1.Pod) {
	pod.Spec.SchedulerName = k.Name()

	queue, ok := app.Labels[QueueLabelName]
	if !ok || queue == "" {
		k.log.Info("Queue label missing from RayCluster; pods will remain pending",
			"requiredLabel", QueueLabelName,
			"rayCluster", app.Name)
		return
	}
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[QueueLabelName] = queue
}

func (k *KaiScheduler) AddMetadataToChildResource(_ context.Context, _ client.Object, _ string, _ client.Object) {
}

func (k *KaiScheduler) AddMetadataToChildResourceFromRayJob(_ context.Context, _ *rayv1.RayJob, _ *rayv1.RayCluster, _ *corev1.PodTemplateSpec) {
}

func (kf *KaiSchedulerFactory) New(_ context.Context, _ *rest.Config, _ client.Client) (schedulerinterface.BatchScheduler, error) {
	return &KaiScheduler{
		log: logf.Log.WithName("kai-scheduler"),
	}, nil
}

func (kf *KaiSchedulerFactory) AddToScheme(_ *runtime.Scheme) {
}

func (kf *KaiSchedulerFactory) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b
}
