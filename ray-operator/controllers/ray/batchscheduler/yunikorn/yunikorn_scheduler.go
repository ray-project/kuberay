package yunikorn

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	logger logr.Logger
}

type YuniKornSchedulerFactory struct{}

func GetPluginName() string {
	return SchedulerName
}

func (y *YuniKornScheduler) Name() string {
	return GetPluginName()
}

func (y *YuniKornScheduler) DoBatchSchedulingOnSubmission(_ context.Context, object client.Object) error {
	switch obj := object.(type) {
	case *rayv1.RayCluster, *rayv1.RayJob:
		// Supported types
	default:
		return fmt.Errorf("currently only RayCluster and RayJob are supported, got %T", obj)
	}
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
func (y *YuniKornScheduler) populatePodLabelsFromRayCluster(_ context.Context, app *rayv1.RayCluster, pod *corev1.Pod, sourceKey string, targetKey string) {
	// check labels
	if value, exist := app.Labels[sourceKey]; exist {
		y.logger.Info("Updating pod label based on RayCluster labels",
			"sourceKey", sourceKey, "targetKey", targetKey, "value", value)
		pod.Labels[targetKey] = value
	}
}

func (y *YuniKornScheduler) populateRayClusterLabelsFromRayJob(_ context.Context, rayJob *rayv1.RayJob, rayCluster *rayv1.RayCluster, sourceKey string, targetKey string) {
	if value, exist := rayJob.Labels[sourceKey]; exist {
		y.logger.Info("Updating RayCluster label based on RayJob labels",
			"sourceKey", sourceKey, "targetKey", targetKey, "value", value)
		rayCluster.Labels[targetKey] = value
	}
}

func (y *YuniKornScheduler) populateSubmitterPodTemplateLabelsFromRayJob(_ context.Context, rayJob *rayv1.RayJob, submitterTemplate *corev1.PodTemplateSpec, sourceKey string, targetKey string) {
	if value, exist := rayJob.Labels[sourceKey]; exist {
		y.logger.Info("Updating submitter pod template label based on RayJob labels",
			"sourceKey", sourceKey, "targetKey", targetKey, "value", value)
		submitterTemplate.Labels[targetKey] = value
	}
}

// AddMetadataToChildResourceFromRayCluster adds essential labels and annotations to the Ray pods
// the yunikorn scheduler needs these labels and annotations in order to do the scheduling properly
func (y *YuniKornScheduler) AddMetadataToChildResourceFromRayCluster(ctx context.Context, rayCluster *rayv1.RayCluster, groupName string, pod *corev1.Pod) {
	// the applicationID and queue name must be provided in the labels
	y.populatePodLabelsFromRayCluster(ctx, rayCluster, pod, RayClusterApplicationIDLabelName, YuniKornPodApplicationIDLabelName)
	y.populatePodLabelsFromRayCluster(ctx, rayCluster, pod, RayClusterQueueLabelName, YuniKornPodQueueLabelName)
	pod.Spec.SchedulerName = y.Name()

	// when gang scheduling is enabled, extra annotations need to be added to all pods
	if y.isGangSchedulingEnabled(rayCluster) {
		// populate the taskGroups info to each pod
		y.populateTaskGroupsAnnotationToPod(ctx, rayCluster, pod)

		// set the task group name based on the head or worker group name
		// the group name for the head and each of the worker group should be different
		pod.Annotations[YuniKornTaskGroupNameAnnotationName] = groupName
	}
}

func (y *YuniKornScheduler) AddMetadataToChildResourceFromRayJob(ctx context.Context, rayJob *rayv1.RayJob, rayCluster *rayv1.RayCluster, submitterTemplate *corev1.PodTemplateSpec) {
	// the applicationID and queue name must be provided in the labels
	y.populateRayClusterLabelsFromRayJob(ctx, rayJob, rayCluster, RayClusterApplicationIDLabelName, YuniKornPodApplicationIDLabelName)
	y.populateRayClusterLabelsFromRayJob(ctx, rayJob, rayCluster, RayClusterQueueLabelName, YuniKornPodQueueLabelName)
	if submitterTemplate.Labels == nil {
		submitterTemplate.Labels = make(map[string]string)
	}
	y.populateSubmitterPodTemplateLabelsFromRayJob(ctx, rayJob, submitterTemplate, RayClusterApplicationIDLabelName, YuniKornPodApplicationIDLabelName)
	y.populateSubmitterPodTemplateLabelsFromRayJob(ctx, rayJob, submitterTemplate, RayClusterQueueLabelName, YuniKornPodQueueLabelName)

	// when gang scheduling is enabled, extra annotations need to be added to all pods
	if y.isGangSchedulingEnabled(rayJob) {
		// populate the taskGroups info to RayCluster and submitter pod template
		if rayJob.Spec.RayClusterSpec == nil {
			y.logger.Info("RayJob does not have RayClusterSpec, skip adding task groups annotation to RayCluster and submitter pod template")
			return
		}
		y.populateTaskGroupsAnnotationToRayClusterAndSubmitterPodTemplate(ctx, rayJob, rayCluster, submitterTemplate)
		y.logger.Info("Gang Scheduling enabled for RayJob")
		y.logger.Info("Gang Scheduling enabled for submitter pod template")
	}
}

func (y *YuniKornScheduler) isGangSchedulingEnabled(obj client.Object) bool {
	switch obj := obj.(type) {
	case *rayv1.RayCluster:
		_, exist := obj.Labels[utils.RayClusterGangSchedulingEnabled]
		return exist
	case *rayv1.RayJob:
		_, exist := obj.Labels[utils.RayClusterGangSchedulingEnabled]
		return exist
	default:
		return false
	}
}

func (y *YuniKornScheduler) populateTaskGroupsAnnotationToPod(_ context.Context, app *rayv1.RayCluster, pod *corev1.Pod) {
	var taskGroupsAnnotationValue string
	var err error
	if app.Annotations[YuniKornTaskGroupsAnnotationName] != "" {
		taskGroupsAnnotationValue = app.Annotations[YuniKornTaskGroupsAnnotationName]
		y.logger.Info("using existing task groups annotation from RayCluster", "value", taskGroupsAnnotationValue)
	} else {
		taskGroups := newTaskGroupsFromApp(app)
		taskGroupsAnnotationValue, err = taskGroups.marshal()
		if err != nil {
			y.logger.Error(err, "failed to add gang scheduling related annotations to pod, "+
				"gang scheduling will not be enabled for this workload",
				"name", pod.Name, "namespace", pod.Namespace)
			return
		}

		y.logger.Info("add task groups info to pod's annotation",
			"key", YuniKornTaskGroupsAnnotationName,
			"value", taskGroupsAnnotationValue,
			"numOfTaskGroups", taskGroups.size())

	}
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}

	pod.Annotations[YuniKornTaskGroupsAnnotationName] = taskGroupsAnnotationValue
	y.logger.Info("Gang Scheduling enabled for RayCluster")
}

func (y *YuniKornScheduler) populateTaskGroupsAnnotationToRayClusterAndSubmitterPodTemplate(_ context.Context, rayJob *rayv1.RayJob, rayCluster *rayv1.RayCluster, submitterTemplate *corev1.PodTemplateSpec) {
	taskGroups, err := newTaskGroupsFromRayJob(rayJob, submitterTemplate)
	if err != nil {
		y.logger.Error(err, "failed to create task groups from RayJob", "rayJob", rayJob.Name, "namespace", rayJob.Namespace)
		return
	}
	taskGroupsAnnotationValue, err := taskGroups.marshal()
	if err != nil {
		y.logger.Error(err, "failed to add gang scheduling related annotations to RayCluster, "+
			"gang scheduling will not be enabled for this workload",
			"name", rayCluster.Name, "namespace", rayCluster.Namespace)
		return
	}

	y.logger.Info("add task groups info to pod's annotation",
		"key", YuniKornTaskGroupsAnnotationName,
		"value", taskGroupsAnnotationValue,
		"numOfTaskGroups", taskGroups.size())

	if rayCluster.Annotations == nil {
		rayCluster.Annotations = make(map[string]string)
	}

	rayCluster.Annotations[YuniKornTaskGroupsAnnotationName] = taskGroupsAnnotationValue

	if submitterTemplate.Annotations == nil {
		submitterTemplate.Annotations = make(map[string]string)
	}

	if submitterTemplate.Annotations[YuniKornTaskGroupsAnnotationName] == "" {
		submitterTemplate.Annotations[YuniKornTaskGroupsAnnotationName] = taskGroupsAnnotationValue
	}

	y.logger.Info("Gang Scheduling enabled for RayCluster")
	y.logger.Info("Gang Scheduling enabled for submitter pod template")
}

func (yf *YuniKornSchedulerFactory) New(ctx context.Context, _ *rest.Config, _ client.Client) (schedulerinterface.BatchScheduler, error) {
	return &YuniKornScheduler{
		logger: ctrl.LoggerFrom(ctx).WithName(SchedulerName),
	}, nil
}

func (yf *YuniKornSchedulerFactory) AddToScheme(_ *runtime.Scheme) {
	// No extra scheme needs to be registered
}

func (yf *YuniKornSchedulerFactory) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b
}
