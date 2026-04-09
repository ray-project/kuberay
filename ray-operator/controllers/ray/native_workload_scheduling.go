package ray

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha2 "k8s.io/api/scheduling/v1alpha2"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
)

const (
	// Annotation used to opt-in a RayCluster to native workload scheduling.
	NativeWorkloadSchedulingAnnotation = "ray.io/native-workload-scheduling"

	// podGroupProtectionFinalizer is the finalizer added by the Kubernetes scheduler to PodGroups
	// when the GangScheduling feature gate is enabled on the scheduler (alpha in K8s 1.35).
	// We remove it explicitly before deleting PodGroups so that deletion is immediate
	// rather than waiting for the scheduler to process the finalizer removal.
	podGroupProtectionFinalizer = "scheduling.k8s.io/podgroup-protection"
)

// schedulingSkipReason is a typed reason for skipping native workload scheduling.
type schedulingSkipReason string

const (
	skipReasonNone                schedulingSkipReason = ""
	skipReasonDisabled            schedulingSkipReason = "disabled"
	skipReasonBatchScheduler      schedulingSkipReason = "batch scheduler configured"
	skipReasonAutoscaling         schedulingSkipReason = "autoscaling enabled"
	skipReasonTooManyWorkerGroups schedulingSkipReason = "too many worker groups"
)

// Event reasons used for native workload scheduling operations.
const (
	CreatedWorkload               utils.K8sEventType = "CreatedWorkload"
	CreatedPodGroup               utils.K8sEventType = "CreatedPodGroup"
	DeletedWorkload               utils.K8sEventType = "DeletedWorkload"
	DeletedPodGroup               utils.K8sEventType = "DeletedPodGroup"
	FailedToCreateWorkload        utils.K8sEventType = "FailedToCreateWorkload"
	FailedToCreatePodGroup        utils.K8sEventType = "FailedToCreatePodGroup"
	FailedToDeleteWorkload        utils.K8sEventType = "FailedToDeleteWorkload"
	FailedToDeletePodGroup        utils.K8sEventType = "FailedToDeletePodGroup"
	WorkloadSchedulingSkipped     utils.K8sEventType = "WorkloadSchedulingSkipped"
	WorkloadSchedulingInvalidSpec utils.K8sEventType = "WorkloadSchedulingInvalidSpec"
)

// reconcileNativeWorkloadScheduling creates Workload and PodGroup resources for a RayCluster
// when the NativeWorkloadScheduling feature gate is enabled and the cluster has the opt-in annotation.
// This must be called before pod creation so that pods can reference the PodGroups via schedulingGroup.
func (r *RayClusterReconciler) reconcileNativeWorkloadScheduling(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)

	if skipReason := r.nativeSchedulingSkipReason(instance); skipReason != skipReasonNone {
		if skipReason == skipReasonDisabled {
			return nil
		}
		if skipReason == skipReasonTooManyWorkerGroups {
			maxWorkerGroups := schedulingv1alpha2.WorkloadMaxPodGroupTemplates - 1
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(WorkloadSchedulingInvalidSpec),
				"RayCluster has %d worker groups, but native workload scheduling supports at most %d (%d PodGroupTemplates total, 1 reserved for head)",
				len(instance.Spec.WorkerGroupSpecs), maxWorkerGroups, schedulingv1alpha2.WorkloadMaxPodGroupTemplates)
			return fmt.Errorf("RayCluster %s/%s has %d worker groups, exceeding the maximum of %d for native workload scheduling",
				instance.Namespace, instance.Name, len(instance.Spec.WorkerGroupSpecs), maxWorkerGroups)
		}
		// All other reasons (batch scheduler, autoscaling) are warnings.
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(WorkloadSchedulingSkipped),
			"Skipping native workload scheduling: %s", string(skipReason))
		return nil
	}

	// Build and create the Workload.
	workload, err := r.buildWorkload(instance)
	if err != nil {
		return fmt.Errorf("failed to build Workload for RayCluster %s/%s: %w", instance.Namespace, instance.Name, err)
	}
	if err := r.Create(ctx, workload); err != nil {
		if !errors.IsAlreadyExists(err) {
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(FailedToCreateWorkload),
				"Failed to create Workload %s/%s: %v", workload.Namespace, workload.Name, err)
			return err
		}

		// Workload already exists — check if it is stale and needs to be recreated.
		// Note: the recreate path below may itself hit AlreadyExists if the old Workload has
		// a finalizer delaying its deletion. In that case the error propagates and the
		// reconciler requeues, which is the correct self-healing behaviour.
		existing := &schedulingv1alpha2.Workload{}
		if err := r.Get(ctx, types.NamespacedName{Name: workload.Name, Namespace: workload.Namespace}, existing); err != nil {
			return fmt.Errorf("failed to get existing Workload %s/%s: %w", workload.Namespace, workload.Name, err)
		}
		if isWorkloadStale(existing, instance) {
			logger.Info("Workload is stale, deleting and recreating", "name", workload.Name)
			if err := r.deleteNativeWorkloadSchedulingResources(ctx, instance); err != nil {
				return err
			}
			// Rebuild to get a clean object — the original may have stale metadata from a failed Create.
			workload, err = r.buildWorkload(instance)
			if err != nil {
				return fmt.Errorf("failed to rebuild Workload for RayCluster %s/%s: %w", instance.Namespace, instance.Name, err)
			}
			if err := r.Create(ctx, workload); err != nil {
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(FailedToCreateWorkload),
					"Failed to recreate Workload %s/%s: %v", workload.Namespace, workload.Name, err)
				return err
			}
			logger.Info("Recreated Workload for RayCluster", "name", workload.Name)
			r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(CreatedWorkload),
				"Recreated Workload %s/%s due to spec drift", workload.Namespace, workload.Name)
		} else {
			logger.V(1).Info("Workload already exists and is up-to-date", "name", workload.Name)
		}
	} else {
		logger.Info("Created Workload for RayCluster", "name", workload.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(CreatedWorkload),
			"Created Workload %s/%s", workload.Namespace, workload.Name)
	}

	// Build and create PodGroups: one for the head and one per worker group.
	podGroupSpecs := buildPodGroupSpecs(instance)
	for _, pgSpec := range podGroupSpecs {
		if pgSpec.schedulingPolicy.Basic != nil && pgSpec.templateName != "head" {
			// A worker group with 0 desired replicas uses BasicSchedulingPolicy (trivially satisfied).
			// The scheduler evaluates each PodGroup independently, so this does not block scheduling
			// of the other PodGroups.
			logger.V(1).Info("Worker group using BasicSchedulingPolicy due to desired replicas being 0", "template", pgSpec.templateName)
		}

		pg, err := r.buildPodGroup(instance, pgSpec.templateName, pgSpec.schedulingPolicy)
		if err != nil {
			return fmt.Errorf("failed to build PodGroup %s for RayCluster %s/%s: %w",
				pgSpec.templateName, instance.Namespace, instance.Name, err)
		}

		if err := r.Create(ctx, pg); err != nil {
			if !errors.IsAlreadyExists(err) {
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(FailedToCreatePodGroup),
					"Failed to create PodGroup %s/%s: %v", pg.Namespace, pg.Name, err)
				return err
			}
			// If the existing PodGroup is being deleted (e.g. finalizer-delayed after stale detection),
			// return an error so the reconciler requeues and retries once deletion completes.
			existing := &schedulingv1alpha2.PodGroup{}
			if err := r.Get(ctx, types.NamespacedName{Name: pg.Name, Namespace: pg.Namespace}, existing); err != nil {
				return fmt.Errorf("failed to get existing PodGroup %s/%s: %w", pg.Namespace, pg.Name, err)
			}
			if existing.DeletionTimestamp != nil {
				return fmt.Errorf("PodGroup %s/%s is being deleted (finalizer pending), will retry", pg.Namespace, pg.Name)
			}
			logger.V(1).Info("PodGroup already exists", "name", pg.Name)
		} else {
			logger.Info("Created PodGroup for RayCluster", "name", pg.Name)
			r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(CreatedPodGroup),
				"Created PodGroup %s/%s", pg.Namespace, pg.Name)
		}
	}

	return nil
}

// podGroupSpec holds the parameters needed to build a PodGroup.
type podGroupSpec struct {
	templateName     string
	schedulingPolicy schedulingv1alpha2.PodGroupSchedulingPolicy
}

// buildPodGroupSpecs returns the list of PodGroup specifications for the given RayCluster:
// one for the head (BasicSchedulingPolicy) and one per worker group (GangSchedulingPolicy or
// BasicSchedulingPolicy if the desired replica count is 0).
func buildPodGroupSpecs(instance *rayv1.RayCluster) []podGroupSpec {
	specs := make([]podGroupSpec, 0, 1+len(instance.Spec.WorkerGroupSpecs))

	// Head group always uses BasicSchedulingPolicy (single pod, no gang constraint needed).
	specs = append(specs, podGroupSpec{
		templateName: "head",
		schedulingPolicy: schedulingv1alpha2.PodGroupSchedulingPolicy{
			Basic: &schedulingv1alpha2.BasicSchedulingPolicy{},
		},
	})

	for _, wg := range instance.Spec.WorkerGroupSpecs {
		minCount := utils.GetWorkerGroupDesiredReplicas(wg)
		templateName := "worker-" + wg.GroupName

		var policy schedulingv1alpha2.PodGroupSchedulingPolicy
		if minCount == 0 {
			// Use BasicSchedulingPolicy — trivially satisfied, no gang constraint.
			policy = schedulingv1alpha2.PodGroupSchedulingPolicy{
				Basic: &schedulingv1alpha2.BasicSchedulingPolicy{},
			}
		} else {
			policy = schedulingv1alpha2.PodGroupSchedulingPolicy{
				Gang: &schedulingv1alpha2.GangSchedulingPolicy{
					MinCount: minCount,
				},
			}
		}

		specs = append(specs, podGroupSpec{
			templateName:     templateName,
			schedulingPolicy: policy,
		})
	}

	return specs
}

// buildWorkload constructs a Workload object for the given RayCluster.
func (r *RayClusterReconciler) buildWorkload(instance *rayv1.RayCluster) (*schedulingv1alpha2.Workload, error) {
	podGroupSpecs := buildPodGroupSpecs(instance)

	templates := make([]schedulingv1alpha2.PodGroupTemplate, 0, len(podGroupSpecs))
	for _, pgSpec := range podGroupSpecs {
		templates = append(templates, schedulingv1alpha2.PodGroupTemplate{
			Name:             pgSpec.templateName,
			SchedulingPolicy: pgSpec.schedulingPolicy,
		})
	}

	workload := &schedulingv1alpha2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
			Labels: map[string]string{
				utils.RayClusterLabelKey: instance.Name,
			},
		},
		Spec: schedulingv1alpha2.WorkloadSpec{
			ControllerRef: &schedulingv1alpha2.TypedLocalObjectReference{
				APIGroup: rayv1.GroupVersion.Group,
				Kind:     "RayCluster",
				Name:     instance.Name,
			},
			PodGroupTemplates: templates,
		},
	}

	if err := ctrl.SetControllerReference(instance, workload, r.Scheme); err != nil {
		return nil, err
	}

	return workload, nil
}

// buildPodGroup constructs a PodGroup object for the given RayCluster and template.
func (r *RayClusterReconciler) buildPodGroup(instance *rayv1.RayCluster, templateName string, policy schedulingv1alpha2.PodGroupSchedulingPolicy) (*schedulingv1alpha2.PodGroup, error) {
	pg := &schedulingv1alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podGroupName(instance.Name, templateName),
			Namespace: instance.Namespace,
			Labels: map[string]string{
				utils.RayClusterLabelKey: instance.Name,
			},
		},
		Spec: schedulingv1alpha2.PodGroupSpec{
			PodGroupTemplateRef: &schedulingv1alpha2.PodGroupTemplateReference{
				Workload: &schedulingv1alpha2.WorkloadPodGroupTemplateReference{
					WorkloadName:         instance.Name,
					PodGroupTemplateName: templateName,
				},
			},
			SchedulingPolicy: policy,
		},
	}

	if err := ctrl.SetControllerReference(instance, pg, r.Scheme); err != nil {
		return nil, err
	}

	return pg, nil
}

// podGroupName returns the deterministic name for a PodGroup: <clusterName>-<templateName>.
func podGroupName(clusterName, templateName string) string {
	return clusterName + "-" + templateName
}

// isNativeWorkloadSchedulingEnabled returns true when both the feature gate and the per-cluster
// opt-in annotation are set.
func isNativeWorkloadSchedulingEnabled(instance *rayv1.RayCluster) bool {
	return features.Enabled(features.NativeWorkloadScheduling) &&
		instance.Annotations[NativeWorkloadSchedulingAnnotation] == "true"
}

// shouldSetSchedulingGroup returns true when native scheduling is active and
// Workload/PodGroup resources will actually be created (i.e. no skip conditions).
func (r *RayClusterReconciler) shouldSetSchedulingGroup(instance *rayv1.RayCluster) bool {
	return r.nativeSchedulingSkipReason(instance) == skipReasonNone
}

// nativeSchedulingSkipReason returns a schedulingSkipReason indicating why native workload
// scheduling should be skipped for this RayCluster, or skipReasonNone if it should proceed.
// It is used by both reconcileNativeWorkloadScheduling and shouldSetSchedulingGroup.
func (r *RayClusterReconciler) nativeSchedulingSkipReason(instance *rayv1.RayCluster) schedulingSkipReason {
	if !isNativeWorkloadSchedulingEnabled(instance) {
		return skipReasonDisabled
	}
	if r.options.BatchSchedulerManager != nil {
		if scheduler, err := r.options.BatchSchedulerManager.GetScheduler(); err == nil && scheduler != nil && scheduler.Name() != "default" {
			return skipReasonBatchScheduler
		}
	}
	if utils.IsAutoscalingEnabled(&instance.Spec) {
		return skipReasonAutoscaling
	}
	if len(instance.Spec.WorkerGroupSpecs) > schedulingv1alpha2.WorkloadMaxPodGroupTemplates-1 {
		return skipReasonTooManyWorkerGroups
	}
	return skipReasonNone
}

// setSchedulingGroup sets the schedulingGroup field on a pod to link it to a PodGroup.
func setSchedulingGroup(pod *corev1.Pod, pgName string) {
	pod.Spec.SchedulingGroup = &corev1.PodSchedulingGroup{
		PodGroupName: &pgName,
	}
}

// isWorkloadStale returns true if the existing Workload's PodGroupTemplates no longer match
// the current RayCluster spec (e.g., worker groups added/removed/renamed, replica count changed,
// or suspension state changed).
func isWorkloadStale(existing *schedulingv1alpha2.Workload, instance *rayv1.RayCluster) bool {
	desired := buildPodGroupSpecs(instance)

	// Number of templates must match: 1 (head) + len(WorkerGroupSpecs).
	if len(existing.Spec.PodGroupTemplates) != len(desired) {
		return true
	}

	// Build a map of existing templates by name for efficient lookup.
	existingByName := make(map[string]schedulingv1alpha2.PodGroupTemplate, len(existing.Spec.PodGroupTemplates))
	for _, tmpl := range existing.Spec.PodGroupTemplates {
		existingByName[tmpl.Name] = tmpl
	}

	for _, d := range desired {
		e, ok := existingByName[d.templateName]
		if !ok {
			// Template name not found — worker group added or renamed.
			return true
		}
		if !schedulingPoliciesMatch(e.SchedulingPolicy, d.schedulingPolicy) {
			return true
		}
	}

	return false
}

// schedulingPoliciesMatch returns true if two PodGroupSchedulingPolicy values are equivalent.
func schedulingPoliciesMatch(a, b schedulingv1alpha2.PodGroupSchedulingPolicy) bool {
	// Both unset — structurally equal.
	if a.Basic == nil && a.Gang == nil && b.Basic == nil && b.Gang == nil {
		return true
	}
	// Both Basic
	if a.Basic != nil && b.Basic != nil {
		return true
	}
	// Both Gang with same MinCount
	if a.Gang != nil && b.Gang != nil {
		return a.Gang.MinCount == b.Gang.MinCount
	}
	// One is Basic and the other is Gang (or one is nil and the other set)
	return false
}

// deleteNativeWorkloadSchedulingResources deletes the Workload and all PodGroups owned by
// the given RayCluster. NotFound errors are treated as no-ops.
func (r *RayClusterReconciler) deleteNativeWorkloadSchedulingResources(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)

	// Delete all PodGroups owned by this RayCluster.
	var podGroupList schedulingv1alpha2.PodGroupList
	if err := r.List(ctx, &podGroupList,
		client.InNamespace(instance.Namespace),
		client.MatchingLabels{utils.RayClusterLabelKey: instance.Name},
	); err != nil {
		return fmt.Errorf("failed to list PodGroups for RayCluster %s/%s: %w", instance.Namespace, instance.Name, err)
	}
	for i := range podGroupList.Items {
		pg := &podGroupList.Items[i]
		// Remove the scheduler's PodGroup protection finalizer before deletion so that
		// the PodGroup is deleted immediately rather than waiting for the scheduler to
		// process the finalizer removal.
		if controllerutil.RemoveFinalizer(pg, podGroupProtectionFinalizer) {
			if err := r.Update(ctx, pg); err != nil {
				if !errors.IsNotFound(err) {
					return fmt.Errorf("failed to remove finalizer from PodGroup %s/%s: %w", pg.Namespace, pg.Name, err)
				}
			}
		}
		if err := r.Delete(ctx, pg); err != nil {
			if !errors.IsNotFound(err) {
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(FailedToDeletePodGroup),
					"Failed to delete PodGroup %s/%s: %v", pg.Namespace, pg.Name, err)
				return err
			}
		} else {
			logger.Info("Deleted PodGroup", "name", pg.Name)
			r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(DeletedPodGroup),
				"Deleted PodGroup %s/%s", pg.Namespace, pg.Name)
		}
	}

	// Delete the Workload.
	workload := &schedulingv1alpha2.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}
	if err := r.Delete(ctx, workload); err != nil {
		if !errors.IsNotFound(err) {
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(FailedToDeleteWorkload),
				"Failed to delete Workload %s/%s: %v", workload.Namespace, workload.Name, err)
			return err
		}
	} else {
		logger.Info("Deleted Workload", "name", workload.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(DeletedWorkload),
			"Deleted Workload %s/%s", workload.Namespace, workload.Name)
	}

	return nil
}
