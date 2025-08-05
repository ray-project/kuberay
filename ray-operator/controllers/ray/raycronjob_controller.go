package ray

import (
	"context"
	"fmt"
	"strings"

	// Import Kubernetes API machinery

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"
)

// RayCronJobReconciler reconciles a RayCronJob object
type RayCronJobReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=ray.example.com,resources=raycronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ray.example.com,resources=raycronjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=cronjobs/status,verbs=get
// +kubebuilder:rbac:groups=ray.example.com,resources=rayjobs,verbs=get;list;watch;create;update;patch;delete // RBAC for creating RayJobs

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.2/pkg/reconcile
func (r *RayCronJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// 1. Fetch the RayCronJob instance
	rayCronJob := &rayv1.RayCronJob{}
	if err := r.Get(ctx, req.NamespacedName, rayCronJob); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic, use finalizers.
			log.Info("RayCronJob resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get RayCronJob")
		return ctrl.Result{}, err
	}

	// 2. Construct the desired Kubernetes CronJob object using the new helper function
	desiredCronJob, err := r.buildIntermediateRayJobCreatorCronJob(rayCronJob)
	if err != nil {
		log.Error(err, "Failed to build intermediate Kubernetes CronJob")
		return ctrl.Result{}, err
	}

	// Set RayCronJob instance as the owner and controller
	// This ensures that the Kubernetes CronJob is garbage collected when the RayCronJob is deleted.
	if err := ctrl.SetControllerReference(rayCronJob, desiredCronJob, r.Scheme); err != nil {
		log.Error(err, "Failed to set owner reference for CronJob")
		return ctrl.Result{}, err
	}

	// 3. Create or Update the Kubernetes CronJob
	foundCronJob := &batchv1.CronJob{}
	err = r.Get(ctx, types.NamespacedName{Name: desiredCronJob.Name, Namespace: desiredCronJob.Namespace}, foundCronJob)

	if err != nil && errors.IsNotFound(err) {
		log.Info("Creating a new Kubernetes CronJob to run intermediate job for RayJob", "CronJob.Namespace", desiredCronJob.Namespace, "CronJob.Name", desiredCronJob.Name)
		err = r.Create(ctx, desiredCronJob)
		if err != nil {
			log.Error(err, "Failed to create new CronJob", "CronJob.Namespace", desiredCronJob.Namespace, "CronJob.Name", desiredCronJob.Name)
			return ctrl.Result{}, err
		}
		// CronJob created successfully - don't requeue
		return ctrl.Result{}, nil
	} else if err != nil {
		log.Error(err, "Failed to get CronJob")
		return ctrl.Result{}, err
	}

	// Updating Spec
	log.Info("Updating existing Kubernetes CronJob for RayJob", "CronJob.Namespace", foundCronJob.Namespace, "CronJob.Name", foundCronJob.Name)
	foundCronJob.Spec = desiredCronJob.Spec
	err = r.Update(ctx, foundCronJob)
	if err != nil {
		log.Error(err, "Failed to update existing CronJob", "CronJob.Namespace", foundCronJob.Namespace, "CronJob.Name", foundCronJob.Name)
		return ctrl.Result{}, err
	}

	// 4. Update RayCronJob status (Optional but good practice)
	// TODO

	log.Info("Successfully reconciled RayCronJob", "RayCronJob.Name", rayCronJob.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RayCronJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1.RayCronJob{}).
		Owns(&batchv1.CronJob{}).
		Complete(r)
}

// buildIntermediateRayJobCreatorCronJob constructs the Kubernetes CronJob
// that will create the RayJob when it runs.
func (r *RayCronJobReconciler) buildIntermediateRayJobCreatorCronJob(rayCronJob *rayv1.RayCronJob) (*batchv1.CronJob, error) {
	// Marshal the RayJobTemplate.Spec into a YAML string to be passed to the intermediate job
	rayJobSpecYAML, err := yaml.Marshal(rayCronJob.Spec.RayJobTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal RayJobTemplate.Spec to YAML: %w", err)
	}

	// Labels for the intermediate Kubernetes Job (not the RayJob it creates)
	// This ensures the intermediate job is linked back to the RayCronJob
	intermediateJobLabels := map[string]string{
		"raycronjob.example.com/name": rayCronJob.Name,
	}

	// Prepare labels for the RayJob metadata that will be created by the intermediate job
	// These labels will be applied to the *RayJob* resource itself.
	rayJobMetadataLabels := make(map[string]string)
	// Add the specific label for linking the created RayJob back to the RayCronJob
	rayJobMetadataLabels["raycronjob.example.com/name"] = rayCronJob.Name

	rayJobMetadataLabelsYAML := ""
	if len(rayJobMetadataLabels) > 0 {
		labelsBuilder := strings.Builder{}
		labelsBuilder.WriteString("  labels:\n")
		for k, v := range rayJobMetadataLabels {
			labelsBuilder.WriteString(fmt.Sprintf("    %s: \"%s\"\n", k, v))
		}
		rayJobMetadataLabelsYAML = labelsBuilder.String()
	}

	// Indent the marshaled YAML to fit under 'spec:' in the RayJob manifest
	indentedRayJobSpecYAML := indentYAML(string(rayJobSpecYAML), "  ")

	desiredCronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rayCronJob.Name,
			Namespace: rayCronJob.Namespace,
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   rayCronJob.Spec.Schedule,
			ConcurrencyPolicy:          rayCronJob.Spec.ConcurrencyPolicy,
			Suspend:                    rayCronJob.Spec.Suspend,
			StartingDeadlineSeconds:    rayCronJob.Spec.StartingDeadlineSeconds,
			SuccessfulJobsHistoryLimit: rayCronJob.Spec.SuccessfulJobsHistoryLimit,
			FailedJobsHistoryLimit:     rayCronJob.Spec.FailedJobsHistoryLimit,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      intermediateJobLabels,
					Annotations: map[string]string{},
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyOnFailure,
							Containers: []corev1.Container{
								{
									Name:  "rayjob-creator",
									Image: "bitnami/kubectl:1.29.3",
									Command: []string{
										"/bin/sh",
										"-c",
										// The intermediate job will construct the RayJob YAML and apply it.
										// We embed the RayJobTemplate.Spec directly into the script.
										// A more robust solution would involve a custom binary that takes
										// the RayJob spec as an argument or from a mounted ConfigMap.
										fmt.Sprintf(`
										set -euo pipefail
										RAYJOB_NAME="%s-rayjob-$(date +%%s)"
										RAYJOB_NAMESPACE="%s"

										cat <<EOF | kubectl apply -f -
apiVersion: ray.io/v1alpha1
kind: RayJob
metadata:
  name: ${RAYJOB_NAME}
  namespace: ${RAYJOB_NAMESPACE}
%s # Labels for the RayJob metadata
spec:
%s
EOF
										`,
											rayCronJob.Name,
											rayCronJob.Namespace,
											rayJobMetadataLabelsYAML,
											indentedRayJobSpecYAML,
										),
									},
								},
							},
							// Ensure the ServiceAccount has permissions to create RayJobs
							ServiceAccountName: "default", // TODO
						},
					},
				},
			},
		},
	}
	return desiredCronJob, nil
}

// Helper function to indent YAML string
func indentYAML(yamlStr, indent string) string {
	lines := strings.Split(yamlStr, "\n")
	var indentedLines []string
	for _, line := range lines {
		if len(strings.TrimSpace(line)) > 0 { // Only indent non-empty lines
			indentedLines = append(indentedLines, indent+line)
		}
	}
	return strings.Join(indentedLines, "\n")
}

// Helper function to split string by newline (no longer strictly needed with strings.Split)
// Keeping it for consistency if other parts of the code still rely on it.
func splitLines(s string) []string {
	return strings.Split(s, "\n")
}

// +kubebuilder:docs-gen:collapse=RayCronJob
// +kubebuilder:docs-gen:collapse=RayCronJobList
// +kubebuilder:docs-gen:collapse=RayJob
// +kubebuilder:docs-gen:collapse=RayJobList
