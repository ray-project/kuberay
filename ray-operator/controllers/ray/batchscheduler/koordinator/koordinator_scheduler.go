package koordinator

import (
	"context"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	SchedulerName                             string = "koordinator"
	KoordinatorGangMinAvailableAnnotationName string = "gang.scheduling.koordinator.sh/min-available"
	KoordinatorGangAnnotationName             string = "gang.scheduling.koordinator.sh/name"
)

type KoodinatorScheduler struct{}

type KoordinatorSchedulerFactory struct{}

func GetPluginName() string {
	return SchedulerName
}

func (y *KoodinatorScheduler) Name() string {
	return GetPluginName()
}

func (y *KoodinatorScheduler) DoBatchSchedulingOnSubmission(_ context.Context, _ *rayv1.RayCluster) error {
	// koordinator doesn't require any resources to be created upfront
	// this is a no-opt for this implementation
	return nil
}

// AddMetadataToPod adds essential labels and annotations to the Ray pods
// the koordinator scheduler needs these labels and annotations in order to do the scheduling properly
func (y *KoodinatorScheduler) AddMetadataToPod(ctx context.Context, app *rayv1.RayCluster, groupName string, pod *corev1.Pod) {

	// when gang scheduling is enabled, extra annotations need to be added to all pods
	if y.isGangSchedulingEnabled(app) {
		// set the task group name based on the head or worker group name
		// the group name for the head and each of the worker group should be different
		pod.Annotations[KoordinatorGangAnnotationName] = getAppPodGroupName(app)
		pod.Annotations[KoordinatorGangMinAvailableAnnotationName] = strconv.Itoa(int(getMinAvailable(app)))
	}
}

func (y *KoodinatorScheduler) isGangSchedulingEnabled(app *rayv1.RayCluster) bool {
	_, exist := app.Labels[utils.RayClusterGangSchedulingEnabled]
	return exist
}

func (yf *KoordinatorSchedulerFactory) New(_ *rest.Config) (schedulerinterface.BatchScheduler, error) {
	return &KoodinatorScheduler{}, nil
}

func (yf *KoordinatorSchedulerFactory) AddToScheme(_ *runtime.Scheme) {
	// No extra scheme needs to be registered
}

func (yf *KoordinatorSchedulerFactory) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b
}

func getAppPodGroupName(app *rayv1.RayCluster) string {
	return fmt.Sprintf("ray-%s-pg", app.Name)
}
func getMinAvailable(app *rayv1.RayCluster) int32 {

	var minAvailable int32
	for _, workerGroupSpec := range app.Spec.WorkerGroupSpecs {
		minAvailable += *workerGroupSpec.MinReplicas
	}
	return minAvailable + 1

}
