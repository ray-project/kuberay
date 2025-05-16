package schedulerplugins

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/scheduler-plugins/apis/scheduling/v1alpha1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	SchedulerName                 string = "kube-scheduler"
	KubeSchedulerPodGroupLabelKey string = "scheduling.x-k8s.io/pod-group"
)

type KubeScheduler struct {
	cli client.Client
}

type KubeSchedulerFactory struct{}

func GetPluginName() string {
	return SchedulerName
}

func (y *KubeScheduler) Name() string {
	return GetPluginName()
}

func (y *KubeScheduler) DoBatchSchedulingOnSubmission(ctx context.Context, rc *rayv1.RayCluster) error {
	replica := int32(1)
	for _, workerGroup := range rc.Spec.WorkerGroupSpecs {
		if workerGroup.Replicas == nil {
			continue
		}
		replica += *workerGroup.Replicas
	}

	podGroup := &v1alpha1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{},
		Spec: v1alpha1.PodGroupSpec{
			MinMember:    replica,
			MinResources: utils.CalculateMinResources(rc),
		},
	}

	return y.cli.Create(ctx, podGroup)
}

// AddMetadataToPod adds essential labels and annotations to the Ray pods
// the yunikorn scheduler needs these labels and annotations in order to do the scheduling properly
func (y *KubeScheduler) AddMetadataToPod(_ context.Context, app *rayv1.RayCluster, groupName string, pod *corev1.Pod) {
	// when gang scheduling is enabled, extra annotations need to be added to all pods
	if y.isGangSchedulingEnabled(app) {
		// the group name for the head and each of the worker group should be different
		pod.Annotations[KubeSchedulerPodGroupLabelKey] = groupName
	}
}

func (y *KubeScheduler) isGangSchedulingEnabled(app *rayv1.RayCluster) bool {
	_, exist := app.Labels[utils.RayClusterGangSchedulingEnabled]
	return exist
}

func (yf *KubeSchedulerFactory) New(ctx context.Context, c *rest.Config) (schedulerinterface.BatchScheduler, error) {
	scheme := runtime.NewScheme()
	_ = clientscheme.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)
	ccache, err := cache.New(c, cache.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, err
	}
	go func() {
		err := ccache.Start(ctx)
		if err != nil {
			panic(err)
		}
	}()
	if !ccache.WaitForCacheSync(ctx) {
		return nil, fmt.Errorf("failed to sync cache")
	}
	cli, err := client.New(c, client.Options{
		Scheme: scheme,
		Cache: &client.CacheOptions{
			Reader: ccache,
		},
	})
	if err != nil {
		return nil, err
	}
	return &KubeScheduler{
		cli: cli,
	}, nil
}

func (yf *KubeSchedulerFactory) AddToScheme(sche *runtime.Scheme) {
	// No extra scheme needs to be registered
	_ = v1alpha1.AddToScheme(sche)
}

func (yf *KubeSchedulerFactory) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b
}
