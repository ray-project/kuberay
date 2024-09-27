package yunikorn

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	SchedulerName                       string = "yunikorn"
	YuniKornPodApplicationIDLabelName   string = "applicationId"
	YuniKornPodQueueLabelName           string = "queue"
	RayClusterApplicationIDLabelName    string = "yunikorn.apache.org/app-id"
	RayClusterQueueLabelName            string = "yunikorn.apache.org/queue"
	YuniKornTaskGroupNameAnnotationName string = "yunikorn.apache.org/task-group-name"
	YuniKornTaskGroupsAnnotationName    string = "yunikorn.apache.org/task-groups"
)

type YuniKornScheduler struct {
	log logr.Logger
}

type YuniKornSchedulerFactory struct{}

func GetPluginName() string {
	return SchedulerName
}

func (y *YuniKornScheduler) Name() string {
	return GetPluginName()
}

func (y *YuniKornScheduler) DoBatchSchedulingOnSubmission(_ context.Context, _ *rayv1.RayCluster) error {
	// yunikorn doesn't require any resources to be created upfront
	// this is a no-opt for this implementation
	return nil
}

// populatePodLabels is a helper function that copies RayCluster's label to the given pod based on the label key
// TODO: remove the legacy labels, i.e "applicationId" and "queue", directly populate labels
// RayClusterApplicationIDLabelName to RayClusterQueueLabelName to pod labels.
// Currently we use this function to translate labels "yunikorn.apache.org/app-id" and "yunikorn.apache.org/queue"
// to legacy labels "applicationId" and "queue", this is for the better compatibilities to support older yunikorn
// versions.
func (y *YuniKornScheduler) populatePodLabels(app *rayv1.RayCluster, pod *corev1.Pod, sourceKey string, targetKey string) {
	// check labels
	if value, exist := app.Labels[sourceKey]; exist {
		y.log.Info("Updating pod label based on RayCluster labels",
			"sourceKey", sourceKey, "targetKey", targetKey, "value", value)
		pod.Labels[targetKey] = value
	}
}

// AddMetadataToPod adds essential labels and annotations to the Ray pods
// the yunikorn scheduler needs these labels and annotations in order to do the scheduling properly
func (y *YuniKornScheduler) AddMetadataToPod(app *rayv1.RayCluster, groupName string, pod *corev1.Pod) {
	// the applicationID and queue name must be provided in the labels
	y.populatePodLabels(app, pod, RayClusterApplicationIDLabelName, YuniKornPodApplicationIDLabelName)
	y.populatePodLabels(app, pod, RayClusterQueueLabelName, YuniKornPodQueueLabelName)
	pod.Spec.SchedulerName = y.Name()

	// when gang scheduling is enabled, extra annotations need to be added to all pods
	if y.isGangSchedulingEnabled(app) {
		// populate the taskGroups info to each pod
		y.populateTaskGroupsAnnotationToPod(app, pod)

		// set the task group name based on the head or worker group name
		// the group name for the head and each of the worker group should be different
		pod.Annotations[YuniKornTaskGroupNameAnnotationName] = groupName
	}
}

func (y *YuniKornScheduler) isGangSchedulingEnabled(app *rayv1.RayCluster) bool {
	_, exist := app.Labels[utils.RayClusterGangSchedulingEnabled]
	return exist
}

func (y *YuniKornScheduler) populateTaskGroupsAnnotationToPod(app *rayv1.RayCluster, pod *corev1.Pod) {
	taskGroups := newTaskGroupsFromApp(app)
	taskGroupsAnnotationValue, err := taskGroups.marshal()
	if err != nil {
		y.log.Error(err, "failed to add gang scheduling related annotations to pod, "+
			"gang scheduling will not be enabled for this workload",
			"rayCluster", app.Name, "name", pod.Name, "namespace", pod.Namespace)
		return
	}

	y.log.Info("add task groups info to pod's annotation",
		"key", YuniKornTaskGroupsAnnotationName,
		"value", taskGroupsAnnotationValue,
		"numOfTaskGroups", taskGroups.size())
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations[YuniKornTaskGroupsAnnotationName] = taskGroupsAnnotationValue

	y.log.Info("Gang Scheduling enabled for RayCluster",
		"RayCluster", app.Name, "Namespace", app.Namespace)
}

func (yf *YuniKornSchedulerFactory) New(_ *rest.Config) (schedulerinterface.BatchScheduler, error) {
	return &YuniKornScheduler{
		log: logf.Log.WithName(SchedulerName),
	}, nil
}

func (yf *YuniKornSchedulerFactory) AddToScheme(_ *runtime.Scheme) {
	// No extra scheme needs to be registered
}

func (yf *YuniKornSchedulerFactory) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b
}
