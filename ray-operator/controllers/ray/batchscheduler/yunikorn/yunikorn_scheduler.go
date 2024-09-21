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
)

const (
	SchedulerName                       string = "yunikorn"
	YuniKornPodApplicationIDLabelName   string = "applicationId"
	YuniKornPodQueueLabelName           string = "queue"
	RayClusterApplicationIDLabelName    string = "yunikorn.apache.org/application-id"
	RayClusterQueueLabelName            string = "yunikorn.apache.org/queue-name"
	RayClusterGangSchedulingLabelName   string = "yunikorn.apache.org/gang-scheduling"
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

func (y *YuniKornScheduler) DoBatchSchedulingOnSubmission(ctx context.Context, app *rayv1.RayCluster) error {
	return nil
}

// populatePodLabels is a helper function that copies RayCluster's label to the given pod based on the label key
func (y *YuniKornScheduler) populatePodLabels(app *rayv1.RayCluster, pod *corev1.Pod, sourceKey string, targetKey string) {
	// check labels
	if value, exist := app.Labels[sourceKey]; exist {
		y.log.Info("Updating pod label based on RayCluster labels",
			"sourceKey", sourceKey, "targetKey", targetKey, "value", value)
		pod.Labels[targetKey] = value
	}
}

// populatePodAnnotations is a helper function that copies RayCluster's annotation to the given pod based on the annotation key
func (y *YuniKornScheduler) populatePodAnnotations(app *rayv1.RayCluster, pod *corev1.Pod, sourceKey string, targetKey string) {
	// check labels
	if value, exist := app.Annotations[sourceKey]; exist {
		y.log.Info("Updating pod annotations based on RayCluster annotations",
			"sourceKey", sourceKey, "targetKey", targetKey, "value", value)
		pod.Annotations[targetKey] = value
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
	if value, exist := app.Labels[RayClusterGangSchedulingLabelName]; exist {
		if value == "true" {
			return true
		}
	}
	return false
}

func (y *YuniKornScheduler) populateTaskGroupsAnnotationToPod(app *rayv1.RayCluster, pod *corev1.Pod) {
	y.log.Info("Gang Scheduling enabled for RayCluster",
		"RayCluster", app.Name, "Namespace", app.Namespace)

	taskGroups := newTaskGroupsFromApp(app)
	taskGroupsAnnotationValue, err := taskGroups.marshal()
	if err != nil {
		y.log.Error(err, "failed to marshal task groups info")
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
