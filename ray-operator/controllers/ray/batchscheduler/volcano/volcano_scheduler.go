package volcano

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	quotav1 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	volcanobatchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	volcanoschedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	PodGroupName      = "podgroups.scheduling.volcano.sh"
	QueueNameLabelKey = "volcano.sh/queue-name"
)

type VolcanoBatchScheduler struct {
	cli client.Client
}

type VolcanoBatchSchedulerFactory struct{}

func GetPluginName() string {
	return "volcano"
}

func (v *VolcanoBatchScheduler) Name() string {
	return GetPluginName()
}

func (v *VolcanoBatchScheduler) DoBatchSchedulingOnSubmission(ctx context.Context, object client.Object) error {
	switch obj := object.(type) {
	case *rayv1.RayCluster:
		return v.handleRayCluster(ctx, obj)
	case *rayv1.RayJob:
		return v.handleRayJob(ctx, obj)
	default:
		return fmt.Errorf("unsupported object type %T, only RayCluster and RayJob are supported", object)
	}
}

func getAppPodGroupName(object client.Object) string {
	// If the object is a RayCluster created by a RayJob, use the RayJob's name
	if labels := object.GetLabels(); labels != nil {
		if labels[utils.RayOriginatedFromCRDLabelKey] == utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD) {
			if rayJobName, ok := labels[utils.RayOriginatedFromCRNameLabelKey]; ok {
				return fmt.Sprintf("ray-%s-pg", rayJobName)
			}
		}
	}
	return fmt.Sprintf("ray-%s-pg", object.GetName())
}

// copySchedulingLabels copies scheduling-related labels from source to target labels map.
func (v *VolcanoBatchScheduler) copySchedulingLabels(source client.Object, targetLabels map[string]string) {
	if queue, ok := source.GetLabels()[QueueNameLabelKey]; ok {
		targetLabels[QueueNameLabelKey] = queue
	}
	if priorityClassName, ok := source.GetLabels()[utils.RayPriorityClassName]; ok {
		targetLabels[utils.RayPriorityClassName] = priorityClassName
	}
}

// syncPodGroup ensures a Volcano PodGroup exists/updated for the given object
// with the provided size (MinMember) and total resources.
func (v *VolcanoBatchScheduler) syncPodGroup(ctx context.Context, owner client.Object, size int32, totalResource corev1.ResourceList) error {
	logger := ctrl.LoggerFrom(ctx).WithName(v.Name())

	podGroupName := getAppPodGroupName(owner)
	podGroup := volcanoschedulingv1beta1.PodGroup{}
	if err := v.cli.Get(ctx, types.NamespacedName{Namespace: owner.GetNamespace(), Name: podGroupName}, &podGroup); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		podGroup := createPodGroup(owner, podGroupName, size, totalResource)
		if err := v.cli.Create(ctx, &podGroup); err != nil {
			if errors.IsAlreadyExists(err) {
				logger.Info("pod group already exists, no need to create")
				return nil
			}

			logger.Error(err, "Pod group CREATE error!", "PodGroup.Error", err)
			return err
		}
	} else {
		if podGroup.Spec.MinMember != size || !quotav1.Equals(*podGroup.Spec.MinResources, totalResource) {
			podGroup.Spec.MinMember = size
			podGroup.Spec.MinResources = &totalResource
			if err := v.cli.Update(ctx, &podGroup); err != nil {
				logger.Error(err, "Pod group UPDATE error!", "podGroup", podGroupName)
				return err
			}
		}
	}
	return nil
}

// calculatePodGroupParams calculates MinMember and MinResources for a RayCluster spec
func (v *VolcanoBatchScheduler) calculatePodGroupParams(ctx context.Context, rayClusterSpec *rayv1.RayClusterSpec) (int32, corev1.ResourceList) {
	rayCluster := &rayv1.RayCluster{Spec: *rayClusterSpec}

	if !utils.IsAutoscalingEnabled(rayClusterSpec) {
		return utils.CalculateDesiredReplicas(ctx, rayCluster) + 1, utils.CalculateDesiredResources(rayCluster)
	}
	return utils.CalculateMinReplicas(rayCluster) + 1, utils.CalculateMinResources(rayCluster)
}

// handleRayCluster calculates the PodGroup MinMember and MinResources for a RayCluster
// and creates/updates the corresponding PodGroup unless the cluster originated from a RayJob.
func (v *VolcanoBatchScheduler) handleRayCluster(ctx context.Context, raycluster *rayv1.RayCluster) error {
	// Check if this RayCluster is created by a RayJob, if so, skip PodGroup creation
	if crdType, ok := raycluster.Labels[utils.RayOriginatedFromCRDLabelKey]; ok && crdType == utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD) {
		return nil
	}

	minMember, totalResource := v.calculatePodGroupParams(ctx, &raycluster.Spec)

	return v.syncPodGroup(ctx, raycluster, minMember, totalResource)
}

// handleRayJob calculates the PodGroup MinMember and MinResources for a RayJob
// based on its embedded RayCluster spec and creates/updates the corresponding PodGroup.
// Note: We intentionally do NOT include the submitter pod in MinMember since the RayCluster
// may not be ready yet.
func (v *VolcanoBatchScheduler) handleRayJob(ctx context.Context, rayJob *rayv1.RayJob) error {
	// For RayJob, we need to calculate resources based on the RayClusterSpec
	// Not support using existing RayCluster
	if rayJob.Spec.RayClusterSpec == nil {
		return fmt.Errorf("RayJob %s/%s does not have RayClusterSpec defined", rayJob.Namespace, rayJob.Name)
	}

	minMember, totalResource := v.calculatePodGroupParams(ctx, rayJob.Spec.RayClusterSpec)

	return v.syncPodGroup(ctx, rayJob, minMember, totalResource)
}

// createPodGroup builds a Volcano PodGroup owned by the provided owner object.
func createPodGroup(owner client.Object, podGroupName string, size int32, totalResource corev1.ResourceList) volcanoschedulingv1beta1.PodGroup {
	var ownerRef metav1.OwnerReference
	switch obj := owner.(type) {
	case *rayv1.RayCluster:
		ownerRef = *metav1.NewControllerRef(obj, rayv1.SchemeGroupVersion.WithKind("RayCluster"))
	case *rayv1.RayJob:
		ownerRef = *metav1.NewControllerRef(obj, rayv1.SchemeGroupVersion.WithKind("RayJob"))
	}

	podGroup := volcanoschedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       owner.GetNamespace(),
			Name:            podGroupName,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: volcanoschedulingv1beta1.PodGroupSpec{
			MinMember:    size,
			MinResources: &totalResource,
		},
		Status: volcanoschedulingv1beta1.PodGroupStatus{
			Phase: volcanoschedulingv1beta1.PodGroupPending,
		},
	}

	if queue, ok := owner.GetLabels()[QueueNameLabelKey]; ok {
		podGroup.Spec.Queue = queue
	}

	if priorityClassName, ok := owner.GetLabels()[utils.RayPriorityClassName]; ok {
		podGroup.Spec.PriorityClassName = priorityClassName
	}

	return podGroup
}

// AddMetadataToChildResource enriches child resource with metadata necessary to tie it to the scheduler.
// For example, setting labels for queues / priority, and setting schedulerName.
func (v *VolcanoBatchScheduler) AddMetadataToChildResource(ctx context.Context, parent client.Object, groupName string, child client.Object) {
	switch parentObj := parent.(type) {
	case *rayv1.RayCluster:
		v.AddMetadataToPod(ctx, parentObj, groupName, child.(*corev1.Pod))
	case *rayv1.RayJob:
		switch childObj := child.(type) {
		case *batchv1.Job:
			v.addMetadataToSubmitterPod(ctx, parent.(*rayv1.RayJob), groupName, childObj)
		}
	}
}

func (v *VolcanoBatchScheduler) AddMetadataToPod(_ context.Context, app *rayv1.RayCluster, groupName string, pod *corev1.Pod) {
	podGroupName := getAppPodGroupName(app)

	pod.Annotations[volcanoschedulingv1beta1.KubeGroupNameAnnotationKey] = podGroupName
	pod.Annotations[volcanobatchv1alpha1.TaskSpecKey] = groupName

	v.copySchedulingLabels(app, pod.Labels)
	pod.Spec.SchedulerName = v.Name()
}

// addMetadataToSubmitterPod sets Volcano-related metadata on the submitter pod.
func (v *VolcanoBatchScheduler) addMetadataToSubmitterPod(_ context.Context, app *rayv1.RayJob, _ string, job *batchv1.Job) {
	submitterTemplate := &job.Spec.Template
	if submitterTemplate.Labels == nil {
		submitterTemplate.Labels = make(map[string]string)
	}
	if submitterTemplate.Annotations == nil {
		submitterTemplate.Annotations = make(map[string]string)
	}

	submitterTemplate.Annotations[volcanoschedulingv1beta1.KubeGroupNameAnnotationKey] = getAppPodGroupName(app)
	submitterTemplate.Annotations[volcanobatchv1alpha1.TaskSpecKey] = utils.RayNodeSubmitterGroupLabelValue

	v.copySchedulingLabels(app, submitterTemplate.Labels)
	submitterTemplate.Spec.SchedulerName = v.Name()
}

func (vf *VolcanoBatchSchedulerFactory) New(_ context.Context, _ *rest.Config, cli client.Client) (schedulerinterface.BatchScheduler, error) {
	if err := volcanoschedulingv1beta1.AddToScheme(cli.Scheme()); err != nil {
		return nil, fmt.Errorf("failed to add volcano to scheme with error %w", err)
	}
	return &VolcanoBatchScheduler{
		cli: cli,
	}, nil
}

func (vf *VolcanoBatchSchedulerFactory) AddToScheme(scheme *runtime.Scheme) {
	utilruntime.Must(volcanoschedulingv1beta1.AddToScheme(scheme))
}

func (vf *VolcanoBatchSchedulerFactory) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b.Owns(&volcanoschedulingv1beta1.PodGroup{})
}
