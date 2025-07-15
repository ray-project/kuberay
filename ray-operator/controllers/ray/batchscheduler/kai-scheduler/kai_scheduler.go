package kaischeduler

// This KAI plugin relies on KAI-Scheduler's
// built-in PodGrouper to create PodGroups at
// runtime, so the plugin itself only needs to:
//   1. expose the scheduler name,
//   2. stamp pods with schedulerName + queue label.
// No PodGroup create/patch logic is included.

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
)

const (
	QueueLabelName = "kai.scheduler/queue"
)

type KaiScheduler struct{}

type KaiSchedulerFactory struct{}

func GetPluginName() string { return "kai-scheduler" }

func (k *KaiScheduler) Name() string { return GetPluginName() }

func (k *KaiScheduler) DoBatchSchedulingOnSubmission(_ context.Context, _ *rayv1.RayCluster) error {
	return nil
}

func (k *KaiScheduler) AddMetadataToPod(ctx context.Context, app *rayv1.RayCluster, _ string, pod *corev1.Pod) {
	queue, ok := app.Labels[QueueLabelName]
	if !ok || queue == "" {
		logger := ctrl.LoggerFrom(ctx).WithName("kai-scheduler")
		logger.Error(nil, "Queue label missing from RayCluster; pods will remain pending",
			"requiredLabel", QueueLabelName,
			"rayCluster", app.Name)
	} else {
		if pod.Labels == nil {
			pod.Labels = make(map[string]string)
		}
		pod.Labels[QueueLabelName] = queue
	}
	pod.Spec.SchedulerName = k.Name()
}

func (kf *KaiSchedulerFactory) New(_ context.Context, _ *rest.Config) (schedulerinterface.BatchScheduler, error) {
	return &KaiScheduler{}, nil
}

func (kf *KaiSchedulerFactory) AddToScheme(_ *runtime.Scheme) {
}

func (kf *KaiSchedulerFactory) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b
}
