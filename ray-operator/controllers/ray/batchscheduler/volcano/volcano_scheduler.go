package volcano

import (
	"context"
	"fmt"
	"maps"
	"strconv"

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
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	pluginName                                = "volcano"
	QueueNameLabelKey                         = "volcano.sh/queue-name"
	NetworkTopologyModeLabelKey               = "volcano.sh/network-topology-mode"
	NetworkTopologyHighestTierAllowedLabelKey = "volcano.sh/network-topology-highest-tier-allowed"
)

type VolcanoBatchScheduler struct {
	cli client.Client
}

type VolcanoBatchSchedulerFactory struct{}

func GetPluginName() string { return pluginName }

func (v *VolcanoBatchScheduler) Name() string {
	return GetPluginName()
}

func (v *VolcanoBatchScheduler) DoBatchSchedulingOnSubmission(ctx context.Context, object metav1.Object) error {
	switch obj := object.(type) {
	case *rayv1.RayCluster:
		return v.handleRayCluster(ctx, obj)
	case *rayv1.RayJob:
		return v.handleRayJob(ctx, obj)
	default:
		return fmt.Errorf("unsupported object type %T, only RayCluster and RayJob are supported", object)
	}
}

// handleRayCluster calculates the PodGroup MinMember and MinResources for a RayCluster
func (v *VolcanoBatchScheduler) handleRayCluster(ctx context.Context, raycluster *rayv1.RayCluster) error {
	// Check if this RayCluster is created by a RayJob, if so, skip PodGroup creation
	if crdType, ok := raycluster.Labels[utils.RayOriginatedFromCRDLabelKey]; ok && crdType == utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD) {
		return nil
	}

	minMember, totalResource := v.calculatePodGroupParams(ctx, &raycluster.Spec)

	return v.syncPodGroup(ctx, raycluster, minMember, totalResource)
}

// handleRayJob calculates the PodGroup MinMember and MinResources for a RayJob
func (v *VolcanoBatchScheduler) handleRayJob(ctx context.Context, rayJob *rayv1.RayJob) error {
	if rayJob.Spec.RayClusterSpec == nil {
		return fmt.Errorf("gang scheduling does not support RayJob %s/%s referencing an existing RayCluster", rayJob.Namespace, rayJob.Name)
	}

	var totalResourceList []corev1.ResourceList
	minMember, totalResource := v.calculatePodGroupParams(ctx, rayJob.Spec.RayClusterSpec)
	totalResourceList = append(totalResourceList, totalResource)

	// MinMember intentionally excludes the submitter pod to avoid a startup deadlock
	// (submitter waits for cluster; gang would wait for submitter). We still add the
	// submitter's resource requests into MinResources so capacity is reserved.
	submitterResource := getSubmitterResource(rayJob)
	totalResourceList = append(totalResourceList, submitterResource)
	return v.syncPodGroup(ctx, rayJob, minMember, utils.SumResourceList(totalResourceList))
}

func getSubmitterResource(rayJob *rayv1.RayJob) corev1.ResourceList {
	switch rayJob.Spec.SubmissionMode {
	case rayv1.K8sJobMode:
		submitterTemplate := common.GetSubmitterTemplate(&rayJob.Spec, rayJob.Spec.RayClusterSpec)
		return utils.CalculatePodResource(submitterTemplate.Spec)
	case rayv1.SidecarMode:
		submitterContainer := common.GetDefaultSubmitterContainer(rayJob.Spec.RayClusterSpec)
		containerResource := submitterContainer.Resources.Requests
		for name, quantity := range submitterContainer.Resources.Limits {
			if _, ok := containerResource[name]; !ok {
				containerResource[name] = quantity
			}
		}
		return containerResource
	default:
		return corev1.ResourceList{}
	}
}

func getAppPodGroupName(object metav1.Object) string {
	// Prefer the RayJob name if this object originated from a RayJob
	name := object.GetName()
	if labels := object.GetLabels(); labels != nil &&
		labels[utils.RayOriginatedFromCRDLabelKey] == utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD) {
		if rayJobName := labels[utils.RayOriginatedFromCRNameLabelKey]; rayJobName != "" {
			name = rayJobName
		}
	}
	return fmt.Sprintf("ray-%s-pg", name)
}

func addSchedulerName(obj metav1.Object, schedulerName string) {
	switch obj := obj.(type) {
	case *corev1.Pod:
		obj.Spec.SchedulerName = schedulerName
	case *corev1.PodTemplateSpec:
		obj.Spec.SchedulerName = schedulerName
	}
}

func populateAnnotations(parent metav1.Object, child metav1.Object, groupName string) {
	annotations := child.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[volcanoschedulingv1beta1.KubeGroupNameAnnotationKey] = getAppPodGroupName(parent)
	annotations[volcanobatchv1alpha1.TaskSpecKey] = groupName
	child.SetAnnotations(annotations)
}

func populateLabelsFromObject(parent metav1.Object, child metav1.Object, key string) {
	labels := child.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	if parentLabel, exist := parent.GetLabels()[key]; exist && parentLabel != "" {
		labels[key] = parentLabel
	}
	child.SetLabels(labels)
}

// syncPodGroup ensures a Volcano PodGroup exists/updated for the given object
// with the provided size (MinMember) and total resources.
func (v *VolcanoBatchScheduler) syncPodGroup(ctx context.Context, owner metav1.Object, size int32, totalResource corev1.ResourceList) error {
	logger := ctrl.LoggerFrom(ctx).WithName(pluginName)

	podGroupName := getAppPodGroupName(owner)
	podGroup := volcanoschedulingv1beta1.PodGroup{}
	if err := v.cli.Get(ctx, types.NamespacedName{Namespace: owner.GetNamespace(), Name: podGroupName}, &podGroup); err != nil {
		if !errors.IsNotFound(err) {
			logger.Error(err, "failed to get PodGroup", "podGroupName", podGroupName, "ownerKind", utils.GetCRDType(owner.GetLabels()[utils.RayOriginatedFromCRDLabelKey]), "ownerName", owner.GetName(), "ownerNamespace", owner.GetNamespace())
			return err
		}

		podGroup, err := createPodGroup(owner, podGroupName, size, totalResource)
		if err != nil {
			logger.Error(err, "Failed to create pod group specification", "PodGroup.Error", err)
			return err
		}
		if err := v.cli.Create(ctx, &podGroup); err != nil {
			if errors.IsAlreadyExists(err) {
				logger.Info("podGroup already exists, no need to create", "name", podGroupName)
				return nil
			}

			logger.Error(err, "failed to create PodGroup", "name", podGroupName, "ownerKind", utils.GetCRDType(owner.GetLabels()[utils.RayOriginatedFromCRDLabelKey]), "ownerName", owner.GetName(), "ownerNamespace", owner.GetNamespace())
			return err
		}
	} else {
		if podGroup.Spec.MinMember != size || podGroup.Spec.MinResources == nil || !quotav1.Equals(*podGroup.Spec.MinResources, totalResource) {
			podGroup.Spec.MinMember = size
			podGroup.Spec.MinResources = &totalResource
			if err := v.cli.Update(ctx, &podGroup); err != nil {
				logger.Error(err, "failed to update PodGroup", "name", podGroupName, "ownerKind", utils.GetCRDType(owner.GetLabels()[utils.RayOriginatedFromCRDLabelKey]), "ownerName", owner.GetName(), "ownerNamespace", owner.GetNamespace())
				return err
			}
		}
	}
	return nil
}

func (v *VolcanoBatchScheduler) calculatePodGroupParams(ctx context.Context, rayClusterSpec *rayv1.RayClusterSpec) (int32, corev1.ResourceList) {
	rayCluster := &rayv1.RayCluster{Spec: *rayClusterSpec}

	if !utils.IsAutoscalingEnabled(rayClusterSpec) {
		return utils.CalculateDesiredReplicas(ctx, rayCluster) + 1, utils.CalculateDesiredResources(rayCluster)
	}
	return utils.CalculateMinReplicas(rayCluster) + 1, utils.CalculateMinResources(rayCluster)
}

func createPodGroup(owner metav1.Object, podGroupName string, size int32, totalResource corev1.ResourceList) (volcanoschedulingv1beta1.PodGroup, error) {
	var ownerRef metav1.OwnerReference
	switch obj := owner.(type) {
	case *rayv1.RayCluster:
		ownerRef = *metav1.NewControllerRef(obj, rayv1.SchemeGroupVersion.WithKind("RayCluster"))
	case *rayv1.RayJob:
		ownerRef = *metav1.NewControllerRef(obj, rayv1.SchemeGroupVersion.WithKind("RayJob"))
	}

	annotations := make(map[string]string, len(owner.GetAnnotations()))
	maps.Copy(annotations, owner.GetAnnotations())

	podGroup := volcanoschedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       owner.GetNamespace(),
			Name:            podGroupName,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
			Annotations:     annotations,
		},
		Spec: volcanoschedulingv1beta1.PodGroupSpec{
			MinMember:    size,
			MinResources: &totalResource,
		},
		Status: volcanoschedulingv1beta1.PodGroupStatus{
			Phase: volcanoschedulingv1beta1.PodGroupPending,
		},
	}

	// Handle network topology configuration
	mode, modeOk := owner.GetLabels()[NetworkTopologyModeLabelKey]
	if modeOk {
		podGroup.Spec.NetworkTopology = &volcanoschedulingv1beta1.NetworkTopologySpec{
			Mode: volcanoschedulingv1beta1.NetworkTopologyMode(mode),
		}
		highestTier, tierOk := owner.GetLabels()[NetworkTopologyHighestTierAllowedLabelKey]
		if tierOk {
			highestTierInt, err := strconv.Atoi(highestTier)
			if err != nil {
				return podGroup, fmt.Errorf("failed to convert %s label to int: %w for podgroup %s in namespace %s", NetworkTopologyHighestTierAllowedLabelKey, err, podGroupName, owner.GetNamespace())
			}
			podGroup.Spec.NetworkTopology.HighestTierAllowed = &highestTierInt
		}
	}

	if queue, ok := owner.GetLabels()[QueueNameLabelKey]; ok {
		podGroup.Spec.Queue = queue
	}
	if priorityClassName, ok := owner.GetLabels()[utils.RayPriorityClassName]; ok {
		podGroup.Spec.PriorityClassName = priorityClassName
	}

	return podGroup, nil
}

func (v *VolcanoBatchScheduler) AddMetadataToChildResource(_ context.Context, parent metav1.Object, child metav1.Object, groupName string) {
	populateLabelsFromObject(parent, child, QueueNameLabelKey)
	populateLabelsFromObject(parent, child, utils.RayPriorityClassName)
	populateAnnotations(parent, child, groupName)
	addSchedulerName(child, v.Name())
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
