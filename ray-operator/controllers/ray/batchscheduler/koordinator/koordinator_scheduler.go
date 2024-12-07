package koordinator

import (
	"context"
	"encoding/json"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	SchedulerName                             string = "koordinator"
	KoordinatorGangAnnotationName             string = "gang.scheduling.koordinator.sh/name"
	KoordinatorGangMinAvailableAnnotationName string = "gang.scheduling.koordinator.sh/min-available"
	KoordinatorGangTotalNumberAnnotationName  string = "gang.scheduling.koordinator.sh/total-number"
	KoordinatorGangModeAnnotationName         string = "gang.scheduling.koordinator.sh/mode"
	KoordinatorGangGroupsAnnotationName       string = "gang.scheduling.koordinator.sh/groups"

	KoordinatorGangModeStrict string = "Strict"
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
	logger := ctrl.LoggerFrom(ctx).WithName(SchedulerName)

	// when gang scheduling is enabled, extra annotations need to be added to all pods
	if y.isGangSchedulingEnabled(app) {

		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}

		// set the pod group name based on the head or worker group name
		// the group name for the head and each of the worker group should be different
		// the api is define here https://koordinator.sh/docs/designs/gang-scheduling/#annotation-way

		gangGroups, minMemberMap := analyzeGangGroupsFromApp(app)

		pod.Annotations[KoordinatorGangAnnotationName] = getAppPodGroupName(app, groupName)
		pod.Annotations[KoordinatorGangMinAvailableAnnotationName] = strconv.Itoa(int(minMemberMap[groupName].MinReplicas))
		pod.Annotations[KoordinatorGangTotalNumberAnnotationName] = strconv.Itoa(int(minMemberMap[groupName].Replicas))
		pod.Annotations[KoordinatorGangModeAnnotationName] = KoordinatorGangModeStrict

		gangGroupAnnotationValueBytes, err := json.Marshal(gangGroups)
		if err != nil {
			logger.Error(err, "failed to add gang group scheduling related annotations to pod, "+
				"gang scheduling will not be enabled for this workload",
				"name", pod.Name, "namespace", pod.Namespace)
			return
		}

		gangGroupAnnotationValue := string(gangGroupAnnotationValueBytes)
		logger.Info("add task groups info to pod's annotation",
			"key", KoordinatorGangGroupsAnnotationName,
			"value", gangGroupAnnotationValue,
			"group", pod.Annotations[KoordinatorGangAnnotationName],
			"min-available", pod.Annotations[KoordinatorGangMinAvailableAnnotationName])

		pod.Annotations[KoordinatorGangGroupsAnnotationName] = gangGroupAnnotationValue

		logger.Info("Gang Group Scheduling enabled for RayCluster")
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

func getAppPodGroupName(app *rayv1.RayCluster, groupName string) string {
	return "ray-" + app.Name + "-" + groupName
}
