package v1alpha2

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha2 "k8s.io/api/scheduling/v1alpha2"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	kuberneteswas "github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler/kubernetes-was"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	podGroupProtectionFinalizer = "scheduling.k8s.io/podgroup-protection"
	// clusterPodGroupTemplateName is the name of the single PodGroupTemplate that
	// gang schedules the entire RayCluster (head + all worker groups) together.
	clusterPodGroupTemplateName = "cluster"
)

type skipReason string

const (
	skipReasonNone                   skipReason = ""
	skipReasonGangSchedulingDisabled skipReason = "gang scheduling not enabled on RayCluster"
	skipReasonAutoscaling            skipReason = "autoscaling enabled"
)

type KubernetesWASV1Alpha2Scheduler struct {
	cli client.Client
}

// Provider implements kuberneteswas.Provider for scheduling.k8s.io/v1alpha2.
type Provider struct{}

func init() {
	kuberneteswas.RegisterProvider(&Provider{})
}

func (k *KubernetesWASV1Alpha2Scheduler) Name() string { return kuberneteswas.GetPluginName() }

func (k *KubernetesWASV1Alpha2Scheduler) DoBatchSchedulingOnSubmission(ctx context.Context, object metav1.Object) error {
	rayCluster, ok := object.(*rayv1.RayCluster)
	if !ok {
		return nil
	}

	if reason := schedulingSkipReason(rayCluster); reason != skipReasonNone {
		ctrl.LoggerFrom(ctx).WithName(kuberneteswas.GetPluginName()).Info("Skipping Kubernetes workload-aware scheduling", "reason", string(reason))
		_, err := k.CleanupOnCompletion(ctx, rayCluster)
		return err
	}

	return k.syncSchedulingResources(ctx, rayCluster)
}

func (k *KubernetesWASV1Alpha2Scheduler) AddMetadataToChildResource(_ context.Context, parent metav1.Object, child metav1.Object, _ string) {
	setDefaultSchedulerName(child)

	rayCluster, ok := parent.(*rayv1.RayCluster)
	if !ok || schedulingSkipReason(rayCluster) != skipReasonNone {
		return
	}
	// The entire RayCluster (head + every worker group) is gang scheduled as a
	// single PodGroup, so all pods reference the same PodGroup regardless of group.
	setSchedulingGroup(child, clusterPodGroupName(rayCluster.Name))
}

func (k *KubernetesWASV1Alpha2Scheduler) CleanupOnCompletion(ctx context.Context, object metav1.Object) (bool, error) {
	rayCluster, ok := object.(*rayv1.RayCluster)
	if !ok {
		return false, nil
	}
	return k.deleteSchedulingResources(ctx, rayCluster)
}

// The methods below adapt this package to kuberneteswas.Provider.

func (p *Provider) GroupVersion() schema.GroupVersion {
	return schedulingv1alpha2.SchemeGroupVersion
}

func (p *Provider) Available(config *rest.Config) error {
	return schedulingV1alpha2Available(config)
}

func (p *Provider) AddToScheme(scheme *runtime.Scheme) {
	utilruntime.Must(schedulingv1alpha2.AddToScheme(scheme))
}

func (p *Provider) NewScheduler(cli client.Client) schedulerinterface.BatchScheduler {
	return &KubernetesWASV1Alpha2Scheduler{cli: cli}
}

func (p *Provider) ConfigureReconciler(b *builder.Builder) *builder.Builder {
	return b.Owns(&schedulingv1alpha2.Workload{}).
		Owns(&schedulingv1alpha2.PodGroup{})
}

type podGroupSpec struct {
	templateName     string
	schedulingPolicy schedulingv1alpha2.PodGroupSchedulingPolicy
}

func (k *KubernetesWASV1Alpha2Scheduler) syncSchedulingResources(ctx context.Context, rayCluster *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx).WithName(kuberneteswas.GetPluginName())

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
		// Workload PodGroupTemplates are immutable, so a spec change (e.g. a new
		// MinCount) requires deleting and recreating the Workload and its PodGroup.
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

	podGroupSpec := buildClusterPodGroupSpec(rayCluster)
	podGroup, err := k.buildPodGroup(rayCluster, podGroupSpec.templateName, podGroupSpec.schedulingPolicy)
	if err != nil {
		return fmt.Errorf("failed to build PodGroup for RayCluster %s/%s: %w", rayCluster.Namespace, rayCluster.Name, err)
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
		// PodGroup SchedulingPolicy is immutable, so if the existing PodGroup drifted
		// from the desired spec, delete and recreate it (and the Workload).
		if isPodGroupStale(existing, podGroupSpec.schedulingPolicy) {
			logger.Info("PodGroup is stale, deleting and recreating", "name", podGroup.Name)
			if _, err := k.deleteSchedulingResources(ctx, rayCluster); err != nil {
				return err
			}
			return k.syncSchedulingResources(ctx, rayCluster)
		}
	}

	return nil
}

// buildClusterPodGroupSpec builds the single PodGroupTemplate spec that gang
// schedules the entire RayCluster. MinCount is the head pod plus the desired
// number of worker replicas (accounting for NumOfHosts) across all worker groups.
func buildClusterPodGroupSpec(rayCluster *rayv1.RayCluster) podGroupSpec {
	minCount := int32(1) + utils.CalculateDesiredReplicas(rayCluster)
	return podGroupSpec{
		templateName: clusterPodGroupTemplateName,
		schedulingPolicy: schedulingv1alpha2.PodGroupSchedulingPolicy{
			Gang: &schedulingv1alpha2.GangSchedulingPolicy{MinCount: minCount},
		},
	}
}

func (k *KubernetesWASV1Alpha2Scheduler) buildWorkload(rayCluster *rayv1.RayCluster) (*schedulingv1alpha2.Workload, error) {
	podGroupSpec := buildClusterPodGroupSpec(rayCluster)
	templates := []schedulingv1alpha2.PodGroupTemplate{
		{
			Name:             podGroupSpec.templateName,
			SchedulingPolicy: podGroupSpec.schedulingPolicy,
		},
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
			Name:      clusterPodGroupName(rayCluster.Name),
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
		// The scheduler adds a podgroup-protection finalizer; remove it so the
		// PodGroup can be deleted as part of the RayCluster lifecycle.
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

func schedulingSkipReason(rayCluster *rayv1.RayCluster) skipReason {
	// Gang scheduling is opt-in per RayCluster via the gang-scheduling label.
	if _, ok := rayCluster.GetLabels()[utils.RayGangSchedulingEnabled]; !ok {
		return skipReasonGangSchedulingDisabled
	}
	// TODO: support the Ray autoscaler with workload-aware scheduling.
	if utils.IsAutoscalingEnabled(&rayCluster.Spec) {
		return skipReasonAutoscaling
	}
	return skipReasonNone
}

func isWorkloadStale(existing *schedulingv1alpha2.Workload, rayCluster *rayv1.RayCluster) bool {
	desired := buildClusterPodGroupSpec(rayCluster)
	if len(existing.Spec.PodGroupTemplates) != 1 {
		return true
	}

	existingTemplate := existing.Spec.PodGroupTemplates[0]
	if existingTemplate.Name != desired.templateName {
		return true
	}
	return !schedulingPoliciesMatch(existingTemplate.SchedulingPolicy, desired.schedulingPolicy)
}

func isPodGroupStale(existing *schedulingv1alpha2.PodGroup, desired schedulingv1alpha2.PodGroupSchedulingPolicy) bool {
	return !schedulingPoliciesMatch(existing.Spec.SchedulingPolicy, desired)
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

func clusterPodGroupName(clusterName string) string {
	return clusterName + "-" + clusterPodGroupTemplateName
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
