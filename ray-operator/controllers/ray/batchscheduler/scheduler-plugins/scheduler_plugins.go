package schedulerplugins

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktypes "k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/scheduler-plugins/apis/scheduling/v1alpha1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	schedulerName                 string = "scheduler-plugins"
	kubeSchedulerPodGroupLabelKey string = "scheduling.x-k8s.io/pod-group"
)

type KubeScheduler struct {
	cli client.Client
}

type KubeSchedulerFactory struct{}

func GetPluginName() string {
	return schedulerName
}

func (k *KubeScheduler) Name() string {
	return GetPluginName()
}

func createPodGroup(ctx context.Context, app *rayv1.RayCluster) *v1alpha1.PodGroup {
	// TODO(troychiu): Consider the case when autoscaling is enabled.

	podGroup := &v1alpha1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: app.Namespace,
			Name:      app.Name,
			OwnerReferences: []metav1.OwnerReference{
				{
					Name:       app.Name,
					UID:        app.UID,
					APIVersion: app.APIVersion,
					Kind:       app.Kind,
				},
			},
		},
		Spec: v1alpha1.PodGroupSpec{
			MinMember:    utils.CalculateDesiredReplicas(ctx, app) + 1, // +1 for the head pod
			MinResources: utils.CalculateDesiredResources(app),
		},
	}
	return podGroup
}

func (k *KubeScheduler) DoBatchSchedulingOnSubmission(ctx context.Context, app *rayv1.RayCluster) error {
	if !k.isGangSchedulingEnabled(app) {
		return nil
	}
	podGroup := &v1alpha1.PodGroup{}
	if err := k.cli.Get(ctx, ktypes.NamespacedName{Namespace: app.Namespace, Name: app.Name}, podGroup); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		podGroup = createPodGroup(ctx, app)
		if err := k.cli.Create(ctx, podGroup); err != nil {
			if errors.IsAlreadyExists(err) {
				return nil
			}
			return fmt.Errorf("failed to create PodGroup: %w", err)
		}
	}
	return nil
}

// AddMetadataToPod adds essential labels and annotations to the Ray pods
// the scheduler needs these labels and annotations in order to do the scheduling properly
func (k *KubeScheduler) AddMetadataToPod(_ context.Context, app *rayv1.RayCluster, _ string, pod *corev1.Pod) {
	// when gang scheduling is enabled, extra labels need to be added to all pods
	if k.isGangSchedulingEnabled(app) {
		pod.Labels[kubeSchedulerPodGroupLabelKey] = app.Name
	}
	pod.Spec.SchedulerName = k.Name()
}

func (k *KubeScheduler) isGangSchedulingEnabled(app *rayv1.RayCluster) bool {
	_, exist := app.Labels[utils.RayClusterGangSchedulingEnabled]
	return exist
}

func (kf *KubeSchedulerFactory) New(_ context.Context, _ *rest.Config, cli client.Client) (schedulerinterface.BatchScheduler, error) {
	if err := v1alpha1.AddToScheme(cli.Scheme()); err != nil {
		return nil, err
	}
	return &KubeScheduler{
		cli: cli,
	}, nil
}

func (kf *KubeSchedulerFactory) AddToScheme(sche *runtime.Scheme) {
	utilruntime.Must(v1alpha1.AddToScheme(sche))
}

func (kf *KubeSchedulerFactory) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b
}
