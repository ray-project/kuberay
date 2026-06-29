package kuberneteswasv1alpha2

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha2 "k8s.io/api/scheduling/v1alpha2"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	schedulerinterface "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/interface"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	pluginName                  = "kubernetes-was-v1alpha2"
	podGroupProtectionFinalizer = "scheduling.k8s.io/podgroup-protection"
)

type schedulingSkipReason string

const (
	skipReasonNone                schedulingSkipReason = ""
	skipReasonAutoscaling         schedulingSkipReason = "autoscaling enabled"
	skipReasonTooManyWorkerGroups schedulingSkipReason = "too many worker groups"
)

type KubernetesWASV1Alpha2Scheduler struct {
	cli client.Client
}

type KubernetesWASV1Alpha2SchedulerFactory struct{}

func GetPluginName() string { return pluginName }

func (k *KubernetesWASV1Alpha2Scheduler) Name() string { return GetPluginName() }

func (k *KubernetesWASV1Alpha2Scheduler) DoBatchSchedulingOnSubmission(ctx context.Context, object metav1.Object) error {
	rayCluster, ok := object.(*rayv1.RayCluster)
	if !ok {
		return nil
	}

	if skipReason := nativeSchedulingSkipReason(rayCluster); skipReason != skipReasonNone {
		ctrl.LoggerFrom(ctx).WithName(pluginName).Info("Skipping Kubernetes workload-aware scheduling", "reason", string(skipReason))
		_, err := k.CleanupOnCompletion(ctx, rayCluster)
		return err
	}

	return k.syncSchedulingResources(ctx, rayCluster)
}

func (k *KubernetesWASV1Alpha2Scheduler) AddMetadataToChildResource(_ context.Context, parent metav1.Object, child metav1.Object, groupName string) {
	setDefaultSchedulerName(child)

	rayCluster, ok := parent.(*rayv1.RayCluster)
	if !ok || nativeSchedulingSkipReason(rayCluster) != skipReasonNone {
		return
	}
	setSchedulingGroup(child, podGroupName(rayCluster.Name, podGroupTemplateName(groupName)))
}

func (k *KubernetesWASV1Alpha2Scheduler) CleanupOnCompletion(ctx context.Context, object metav1.Object) (bool, error) {
	rayCluster, ok := object.(*rayv1.RayCluster)
	if !ok {
		return false, nil
	}
	return k.deleteSchedulingResources(ctx, rayCluster)
}

func (kf *KubernetesWASV1Alpha2SchedulerFactory) New(_ context.Context, config *rest.Config, cli client.Client) (schedulerinterface.BatchScheduler, error) {
	if err := schedulingV1alpha2Available(config); err != nil {
		return nil, err
	}
	return &KubernetesWASV1Alpha2Scheduler{cli: cli}, nil
}

func (kf *KubernetesWASV1Alpha2SchedulerFactory) AddToScheme(scheme *runtime.Scheme) {
	utilruntime.Must(schedulingv1alpha2.AddToScheme(scheme))
}

func (kf *KubernetesWASV1Alpha2SchedulerFactory) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b.Owns(&schedulingv1alpha2.Workload{}).
		Owns(&schedulingv1alpha2.PodGroup{})
}

type podGroupSpec struct {
	templateName     string
	schedulingPolicy schedulingv1alpha2.PodGroupSchedulingPolicy
}

func (k *KubernetesWASV1Alpha2Scheduler) syncSchedulingResources(ctx context.Context, rayCluster *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx).WithName(pluginName)

	workload, err := k.buildWorkload(rayCluster)
	if err != nil {
		return fmt.Errorf("failed to build Workload for RayCluster %s/%s: %w", rayCluster.Namespace, rayCluster.Name, err)
	}
	if err := k.cli.Create(ctx, workload); err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create Workload %s/%s: %w", workload.Namespace, workload.Name, err)
		}

		existing := &schedulingv1alpha2.Workload{}
		if err := k.cli.Get(ctx, types.NamespacedName{Name: workload.Name, Namespace: workload.Namespace}, existing); err != nil {
			return fmt.Errorf("failed to get existing Workload %s/%s: %w", workload.Namespace, workload.Name, err)
		}
		if isWorkloadStale(existing, rayCluster) {
			logger.Info("Workload is stale, deleting and recreating", "name", workload.Name)
			if _, err := k.deleteSchedulingResources(ctx, rayCluster); err != nil {
				return err
			}
			workload, err = k.buildWorkload(rayCluster)
			if err != nil {
				return fmt.Errorf("failed to rebuild Workload for RayCluster %s/%s: %w", rayCluster.Namespace, rayCluster.Name, err)
			}
			if err := k.cli.Create(ctx, workload); err != nil {
				return fmt.Errorf("failed to recreate Workload %s/%s: %w", workload.Namespace, workload.Name, err)
			}
		}
	}

	for _, podGroupSpec := range buildPodGroupSpecs(rayCluster) {
		podGroup, err := k.buildPodGroup(rayCluster, podGroupSpec.templateName, podGroupSpec.schedulingPolicy)
		if err != nil {
			return fmt.Errorf("failed to build PodGroup %s for RayCluster %s/%s: %w", podGroupSpec.templateName, rayCluster.Namespace, rayCluster.Name, err)
		}

		if err := k.cli.Create(ctx, podGroup); err != nil {
			if !errors.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create PodGroup %s/%s: %w", podGroup.Namespace, podGroup.Name, err)
			}
			existing := &schedulingv1alpha2.PodGroup{}
			if err := k.cli.Get(ctx, types.NamespacedName{Name: podGroup.Name, Namespace: podGroup.Namespace}, existing); err != nil {
				return fmt.Errorf("failed to get existing PodGroup %s/%s: %w", podGroup.Namespace, podGroup.Name, err)
			}
			if existing.DeletionTimestamp != nil {
				return fmt.Errorf("PodGroup %s/%s is being deleted (finalizer pending), will retry", podGroup.Namespace, podGroup.Name)
			}
		}
	}

	return nil
}

func buildPodGroupSpecs(rayCluster *rayv1.RayCluster) []podGroupSpec {
	specs := make([]podGroupSpec, 0, 1+len(rayCluster.Spec.WorkerGroupSpecs))
	specs = append(specs, podGroupSpec{
		templateName: "head",
		schedulingPolicy: schedulingv1alpha2.PodGroupSchedulingPolicy{
			Basic: &schedulingv1alpha2.BasicSchedulingPolicy{},
		},
	})

	for _, workerGroup := range rayCluster.Spec.WorkerGroupSpecs {
		minCount := utils.GetWorkerGroupDesiredReplicas(workerGroup)
		templateName := podGroupTemplateName(workerGroup.GroupName)

		var policy schedulingv1alpha2.PodGroupSchedulingPolicy
		if minCount == 0 {
			policy = schedulingv1alpha2.PodGroupSchedulingPolicy{Basic: &schedulingv1alpha2.BasicSchedulingPolicy{}}
		} else {
			policy = schedulingv1alpha2.PodGroupSchedulingPolicy{Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: minCount}}
		}

		specs = append(specs, podGroupSpec{templateName: templateName, schedulingPolicy: policy})
	}

	return specs
}

func (k *KubernetesWASV1Alpha2Scheduler) buildWorkload(rayCluster *rayv1.RayCluster) (*schedulingv1alpha2.Workload, error) {
	podGroupSpecs := buildPodGroupSpecs(rayCluster)
	templates := make([]schedulingv1alpha2.PodGroupTemplate, 0, len(podGroupSpecs))
	for _, podGroupSpec := range podGroupSpecs {
		templates = append(templates, schedulingv1alpha2.PodGroupTemplate{
			Name:             podGroupSpec.templateName,
			SchedulingPolicy: podGroupSpec.schedulingPolicy,
		})
	}

	workload := &schedulingv1alpha2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rayCluster.Name,
			Namespace: rayCluster.Namespace,
			Labels: map[string]string{
				utils.RayClusterLabelKey: rayCluster.Name,
			},
		},
		Spec: schedulingv1alpha2.WorkloadSpec{
			ControllerRef: &schedulingv1alpha2.TypedLocalObjectReference{
				APIGroup: rayv1.GroupVersion.Group,
				Kind:     "RayCluster",
				Name:     rayCluster.Name,
			},
			PodGroupTemplates: templates,
		},
	}

	if err := ctrl.SetControllerReference(rayCluster, workload, k.cli.Scheme()); err != nil {
		return nil, err
	}

	return workload, nil
}

func (k *KubernetesWASV1Alpha2Scheduler) buildPodGroup(rayCluster *rayv1.RayCluster, templateName string, policy schedulingv1alpha2.PodGroupSchedulingPolicy) (*schedulingv1alpha2.PodGroup, error) {
	podGroup := &schedulingv1alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podGroupName(rayCluster.Name, templateName),
			Namespace: rayCluster.Namespace,
			Labels: map[string]string{
				utils.RayClusterLabelKey: rayCluster.Name,
			},
		},
		Spec: schedulingv1alpha2.PodGroupSpec{
			PodGroupTemplateRef: &schedulingv1alpha2.PodGroupTemplateReference{
				Workload: &schedulingv1alpha2.WorkloadPodGroupTemplateReference{
					WorkloadName:         rayCluster.Name,
					PodGroupTemplateName: templateName,
				},
			},
			SchedulingPolicy: policy,
		},
	}

	if err := ctrl.SetControllerReference(rayCluster, podGroup, k.cli.Scheme()); err != nil {
		return nil, err
	}

	return podGroup, nil
}

func (k *KubernetesWASV1Alpha2Scheduler) deleteSchedulingResources(ctx context.Context, rayCluster *rayv1.RayCluster) (bool, error) {
	didDelete := false
	podGroupList := &schedulingv1alpha2.PodGroupList{}
	if err := k.cli.List(ctx, podGroupList, client.InNamespace(rayCluster.Namespace), client.MatchingLabels{utils.RayClusterLabelKey: rayCluster.Name}); err != nil {
		return false, fmt.Errorf("failed to list PodGroups for RayCluster %s/%s: %w", rayCluster.Namespace, rayCluster.Name, err)
	}

	for i := range podGroupList.Items {
		podGroup := &podGroupList.Items[i]
		if controllerutil.RemoveFinalizer(podGroup, podGroupProtectionFinalizer) {
			if err := k.cli.Update(ctx, podGroup); err != nil && !errors.IsNotFound(err) {
				return didDelete, fmt.Errorf("failed to remove finalizer from PodGroup %s/%s: %w", podGroup.Namespace, podGroup.Name, err)
			}
		}
		if err := k.cli.Delete(ctx, podGroup); err != nil {
			if !errors.IsNotFound(err) {
				return didDelete, fmt.Errorf("failed to delete PodGroup %s/%s: %w", podGroup.Namespace, podGroup.Name, err)
			}
		} else {
			didDelete = true
		}
	}

	workload := &schedulingv1alpha2.Workload{ObjectMeta: metav1.ObjectMeta{Name: rayCluster.Name, Namespace: rayCluster.Namespace}}
	if err := k.cli.Delete(ctx, workload); err != nil {
		if !errors.IsNotFound(err) {
			return didDelete, fmt.Errorf("failed to delete Workload %s/%s: %w", workload.Namespace, workload.Name, err)
		}
	} else {
		didDelete = true
	}

	return didDelete, nil
}

func nativeSchedulingSkipReason(rayCluster *rayv1.RayCluster) schedulingSkipReason {
	if utils.IsAutoscalingEnabled(&rayCluster.Spec) {
		return skipReasonAutoscaling
	}
	if len(rayCluster.Spec.WorkerGroupSpecs) > schedulingv1alpha2.WorkloadMaxPodGroupTemplates-1 {
		return skipReasonTooManyWorkerGroups
	}
	return skipReasonNone
}

func isWorkloadStale(existing *schedulingv1alpha2.Workload, rayCluster *rayv1.RayCluster) bool {
	desired := buildPodGroupSpecs(rayCluster)
	if len(existing.Spec.PodGroupTemplates) != len(desired) {
		return true
	}

	existingByName := make(map[string]schedulingv1alpha2.PodGroupTemplate, len(existing.Spec.PodGroupTemplates))
	for _, template := range existing.Spec.PodGroupTemplates {
		existingByName[template.Name] = template
	}

	for _, desiredPodGroup := range desired {
		existingPodGroup, ok := existingByName[desiredPodGroup.templateName]
		if !ok || !schedulingPoliciesMatch(existingPodGroup.SchedulingPolicy, desiredPodGroup.schedulingPolicy) {
			return true
		}
	}

	return false
}

func schedulingPoliciesMatch(a, b schedulingv1alpha2.PodGroupSchedulingPolicy) bool {
	if a.Basic == nil && a.Gang == nil && b.Basic == nil && b.Gang == nil {
		return true
	}
	if a.Basic != nil && b.Basic != nil {
		return true
	}
	if a.Gang != nil && b.Gang != nil {
		return a.Gang.MinCount == b.Gang.MinCount
	}
	return false
}

func podGroupName(clusterName, templateName string) string {
	return clusterName + "-" + templateName
}

func podGroupTemplateName(groupName string) string {
	if groupName == utils.RayNodeHeadGroupLabelValue {
		return "head"
	}
	return "worker-" + groupName
}

func setDefaultSchedulerName(obj metav1.Object) {
	switch obj := obj.(type) {
	case *corev1.Pod:
		obj.Spec.SchedulerName = corev1.DefaultSchedulerName
	case *corev1.PodTemplateSpec:
		obj.Spec.SchedulerName = corev1.DefaultSchedulerName
	}
}

func setSchedulingGroup(obj metav1.Object, podGroupName string) {
	switch obj := obj.(type) {
	case *corev1.Pod:
		obj.Spec.SchedulingGroup = &corev1.PodSchedulingGroup{PodGroupName: &podGroupName}
	case *corev1.PodTemplateSpec:
		obj.Spec.SchedulingGroup = &corev1.PodSchedulingGroup{PodGroupName: &podGroupName}
	}
}

func schedulingV1alpha2Available(config *rest.Config) error {
	if config == nil {
		return nil
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create discovery client: %w", err)
	}
	if _, err := discoveryClient.ServerResourcesForGroupVersion(schedulingv1alpha2.SchemeGroupVersion.String()); err != nil {
		return fmt.Errorf("scheduling.k8s.io/v1alpha2 API is not available: %w", err)
	}
	return nil
}
