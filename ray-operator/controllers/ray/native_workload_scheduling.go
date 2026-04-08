package ray

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha2 "k8s.io/api/scheduling/v1alpha2"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
)

const (
	// Annotation used to opt-in a RayCluster to native workload scheduling.
	NativeWorkloadSchedulingAnnotation = "ray.io/native-workload-scheduling"
)

// Event reasons used for native workload scheduling operations.
const (
	CreatedWorkload               utils.K8sEventType = "CreatedWorkload"
	CreatedPodGroup               utils.K8sEventType = "CreatedPodGroup"
	FailedToCreateWorkload        utils.K8sEventType = "FailedToCreateWorkload"
	FailedToCreatePodGroup        utils.K8sEventType = "FailedToCreatePodGroup"
	WorkloadSchedulingSkipped     utils.K8sEventType = "WorkloadSchedulingSkipped"
	WorkloadSchedulingInvalidSpec utils.K8sEventType = "WorkloadSchedulingInvalidSpec"
)

// reconcileNativeWorkloadScheduling creates Workload and PodGroup resources for a RayCluster
// when the NativeWorkloadScheduling feature gate is enabled and the cluster has the opt-in annotation.
// This must be called before pod creation so that pods can reference the PodGroups via schedulingGroup.
func (r *RayClusterReconciler) reconcileNativeWorkloadScheduling(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := ctrl.LoggerFrom(ctx)

	if !isNativeWorkloadSchedulingEnabled(instance) {
		return nil
	}

	// Native workload scheduling is mutually exclusive with batch schedulers.
	if r.options.BatchSchedulerManager != nil {
		if scheduler, err := r.options.BatchSchedulerManager.GetScheduler(); err == nil && scheduler != nil && scheduler.Name() != "default" {
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(WorkloadSchedulingSkipped),
				"Native workload scheduling is mutually exclusive with batch scheduler %q; skipping native scheduling", scheduler.Name())
			return nil
		}
	}

	// Native workload scheduling does not support autoscaling.
	if utils.IsAutoscalingEnabled(&instance.Spec) {
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(WorkloadSchedulingSkipped),
			"Native workload scheduling is not supported with autoscaling enabled; skipping native scheduling")
		return nil
	}

	// The v1alpha2 API limits Workloads to WorkloadMaxPodGroupTemplates (8) PodGroupTemplates.
	// 1 template is reserved for the head group, leaving 7 for worker groups.
	// This limit is expected to be lifted when the API graduates to beta.
	maxWorkerGroups := schedulingv1alpha2.WorkloadMaxPodGroupTemplates - 1
	if len(instance.Spec.WorkerGroupSpecs) > maxWorkerGroups {
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(WorkloadSchedulingInvalidSpec),
			"RayCluster has %d worker groups, but native workload scheduling supports at most %d (%d PodGroupTemplates total, 1 reserved for head)",
			len(instance.Spec.WorkerGroupSpecs), maxWorkerGroups, schedulingv1alpha2.WorkloadMaxPodGroupTemplates)
		return fmt.Errorf("RayCluster %s/%s has %d worker groups, exceeding the maximum of %d for native workload scheduling",
			instance.Namespace, instance.Name, len(instance.Spec.WorkerGroupSpecs), maxWorkerGroups)
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
		logger.Info("Workload already exists", "name", workload.Name)
	} else {
		logger.Info("Created Workload for RayCluster", "name", workload.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(CreatedWorkload),
			"Created Workload %s/%s", workload.Namespace, workload.Name)
	}

	// Build and create PodGroups: one for the head and one per worker group.
	podGroupSpecs := buildPodGroupSpecs(instance)
	for _, pgSpec := range podGroupSpecs {
		if pgSpec.schedulingPolicy.Basic != nil && pgSpec.templateName != "head" {
			logger.Info("Worker group using BasicSchedulingPolicy due to desired replicas being 0", "template", pgSpec.templateName)
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
			logger.Info("PodGroup already exists", "name", pg.Name)
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
	if !isNativeWorkloadSchedulingEnabled(instance) {
		return false
	}
	if r.options.BatchSchedulerManager != nil {
		if scheduler, err := r.options.BatchSchedulerManager.GetScheduler(); err == nil && scheduler != nil && scheduler.Name() != "default" {
			return false
		}
	}
	if utils.IsAutoscalingEnabled(&instance.Spec) {
		return false
	}
	return len(instance.Spec.WorkerGroupSpecs) <= schedulingv1alpha2.WorkloadMaxPodGroupTemplates-1
}

// setSchedulingGroup sets the schedulingGroup field on a pod to link it to a PodGroup.
func setSchedulingGroup(pod *corev1.Pod, pgName string) {
	pod.Spec.SchedulingGroup = &corev1.PodSchedulingGroup{
		PodGroupName: &pgName,
	}
}
