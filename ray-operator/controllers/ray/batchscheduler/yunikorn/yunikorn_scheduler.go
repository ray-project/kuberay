package yunikorn

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
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
	RayApplicationIDLabelName           string = "yunikorn.apache.org/app-id"
	RayApplicationQueueLabelName        string = "yunikorn.apache.org/queue"
	YuniKornTaskGroupNameAnnotationName string = "yunikorn.apache.org/task-group-name"
	YuniKornTaskGroupsAnnotationName    string = "yunikorn.apache.org/task-groups"
)

type YuniKornScheduler struct{}

type YuniKornSchedulerFactory struct{}

func GetPluginName() string {
	return SchedulerName
}

func (y *YuniKornScheduler) Name() string {
	return GetPluginName()
}

func (y *YuniKornScheduler) DoBatchSchedulingOnSubmission(_ context.Context, _ client.Object) error {
	// yunikorn doesn't require any resources to be created upfront
	// this is a no-opt for this implementation
	return nil
}

// propagateTaskGroupsAnnotation is a helper function thatpropagates the task groups annotation to the child
// if the parent has the task groups annotation, it will be copied to the child
// if the parent doesn't have the task groups annotation, a new one will be created
func propagateTaskGroupsAnnotation(parent client.Object, child client.Object) error {
	if parentAnnotations, exist := parent.GetAnnotations()[YuniKornTaskGroupsAnnotationName]; exist && parentAnnotations != "" {
		annotations := child.GetAnnotations()
		if annotations == nil {
			child.SetAnnotations(make(map[string]string))
		}
		annotations[YuniKornTaskGroupsAnnotationName] = parentAnnotations
		child.SetAnnotations(annotations)
		return nil
	}
	taskGroupsAnnotationValue, err := getTaskGroupsAnnotationValue(parent)
	if err != nil {
		return err
	}
	if job, ok := child.(*batchv1.Job); ok {
		if job.Spec.Template.Annotations == nil {
			job.Spec.Template.Annotations = make(map[string]string)
		}
		job.Spec.Template.Annotations[YuniKornTaskGroupsAnnotationName] = taskGroupsAnnotationValue
		return nil
	}
	annotations := child.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[YuniKornTaskGroupsAnnotationName] = taskGroupsAnnotationValue
	child.SetAnnotations(annotations)
	return nil
}

func populateLabelsFromObject(parent client.Object, child client.Object, sourceKey string, targetKey string) {
	// If obj is a job, we need to inject the labels to the job template
	if job, ok := child.(*batchv1.Job); ok {
		if job.Spec.Template.Labels == nil {
			job.Spec.Template.Labels = make(map[string]string)
		}
		job.Spec.Template.Labels[targetKey] = parent.GetLabels()[sourceKey]
		return
	}
	labels := child.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	if parent.GetLabels() != nil && parent.GetLabels()[sourceKey] != "" {
		labels[targetKey] = parent.GetLabels()[sourceKey]
		child.SetLabels(labels)
	}
}

func addSchedulerNameToObject(obj client.Object, schedulerName string) {
	switch obj := obj.(type) {
	case *corev1.Pod:
		obj.Spec.SchedulerName = schedulerName
	case *batchv1.Job:
		obj.Spec.Template.Spec.SchedulerName = schedulerName
	}
}

func getTaskGroupsAnnotationValue(obj client.Object) (string, error) {
	taskGroups := newTaskGroups()
	switch obj := obj.(type) {
	case *rayv1.RayCluster:
		taskGroups = newTaskGroupsFromRayCluster(obj)
	case *rayv1.RayJob:
		taskGroups = newTaskGroupsFromRayJob(obj)
	}
	taskGroupsAnnotationValue, err := taskGroups.marshal()
	if err != nil {
		return "", err
	}
	return taskGroupsAnnotationValue, nil
}

func addTaskGroupNameAnnotation(obj client.Object, groupName string) {
	// if the child is a job, we need to inject the annotation to the job template
	if job, ok := obj.(*batchv1.Job); ok {
		if job.Spec.Template.Annotations == nil {
			job.Spec.Template.Annotations = make(map[string]string)
		}
		job.Spec.Template.Annotations[YuniKornTaskGroupNameAnnotationName] = groupName
		return
	}

	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[YuniKornTaskGroupNameAnnotationName] = groupName
	obj.SetAnnotations(annotations)
}

func (y *YuniKornScheduler) isGangSchedulingEnabled(obj client.Object) bool {
	switch obj := obj.(type) {
	case *rayv1.RayCluster:
		_, exist := obj.Labels[utils.RayGangSchedulingEnabled]
		return exist
	case *rayv1.RayJob:
		_, exist := obj.Labels[utils.RayGangSchedulingEnabled]
		return exist
	default:
		return false
	}
}

// AddMetadataToPod adds essential labels and annotations to the Ray pod
// the yunikorn scheduler needs these labels and annotations in order to do the scheduling properly
func (y *YuniKornScheduler) AddMetadataToPod(ctx context.Context, rayCluster *rayv1.RayCluster, groupName string, pod *corev1.Pod) {
	logger := ctrl.LoggerFrom(ctx).WithName(SchedulerName)
	// the applicationID and queue name must be provided in the labels
	populateLabelsFromObject(rayCluster, pod, RayApplicationIDLabelName, YuniKornPodApplicationIDLabelName)
	populateLabelsFromObject(rayCluster, pod, RayApplicationQueueLabelName, YuniKornPodQueueLabelName)
	addSchedulerNameToObject(pod, y.Name())

	// when gang scheduling is enabled, extra annotations need to be added to all pods
	if y.isGangSchedulingEnabled(rayCluster) {
		// populate the taskGroups info to each pod
		err := propagateTaskGroupsAnnotation(rayCluster, pod)
		if err != nil {
			logger.Error(err, "failed to add gang scheduling related annotations to pod, "+
				"gang scheduling will not be enabled for this workload",
				"name", pod.Name, "namespace", pod.Namespace)
			return
		}

		// set the task group name based on the head or worker group name
		// the group name for the head and each of the worker group should be different
		pod.Annotations[YuniKornTaskGroupNameAnnotationName] = groupName
	}
}

func (y *YuniKornScheduler) AddMetadataToChildResource(ctx context.Context, parent client.Object, child client.Object, groupName string) {
	logger := ctrl.LoggerFrom(ctx).WithName(SchedulerName)

	populateLabelsFromObject(parent, child, RayApplicationIDLabelName, YuniKornPodApplicationIDLabelName)
	populateLabelsFromObject(parent, child, RayApplicationQueueLabelName, YuniKornPodQueueLabelName)
	addSchedulerNameToObject(child, y.Name())

	if y.isGangSchedulingEnabled(parent) {
		logger.Info("gang scheduling is enabled, propagating task groups annotation to child", "name", child.GetName(), "namespace", child.GetNamespace())

		err := propagateTaskGroupsAnnotation(parent, child)
		if err != nil {
			logger.Error(err, "failed to add gang scheduling related annotations to object, "+
				"gang scheduling will not be enabled for this workload",
				"name", child.GetName(), "namespace", child.GetNamespace())
			return
		}
		if groupName != "" {
			addTaskGroupNameAnnotation(child, groupName)
		}
	}
}

func (yf *YuniKornSchedulerFactory) New(_ context.Context, _ *rest.Config, _ client.Client) (schedulerinterface.BatchScheduler, error) {
	return &YuniKornScheduler{}, nil
}

func (yf *YuniKornSchedulerFactory) AddToScheme(_ *runtime.Scheme) {
	// No extra scheme needs to be registered
}

func (yf *YuniKornSchedulerFactory) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b
}
