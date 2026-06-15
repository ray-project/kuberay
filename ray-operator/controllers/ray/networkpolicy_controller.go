package ray

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

// NetworkPolicyController is an independent controller that watches RayCluster
// resources and manages NetworkPolicies for them.
type NetworkPolicyController struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	OperatorNamespace string
}

// NewNetworkPolicyController creates a new independent NetworkPolicy controller.
// The operator's namespace is resolved from the in-cluster service account token.
func NewNetworkPolicyController(mgr manager.Manager) (*NetworkPolicyController, error) {
	ns, err := getOperatorNamespace()
	if err != nil {
		return nil, err
	}
	return &NetworkPolicyController{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Recorder:          mgr.GetEventRecorderFor("networkpolicy-controller"),
		OperatorNamespace: ns,
	}, nil
}

// getOperatorNamespace returns the namespace the operator is running in.
// It first checks the OPERATOR_NAMESPACE env var (useful for local dev and tests),
// then falls back to the in-cluster service account namespace file.
func getOperatorNamespace() (string, error) {
	if ns := os.Getenv("OPERATOR_NAMESPACE"); ns != "" {
		return ns, nil
	}
	ns, err := os.ReadFile(utils.InClusterNamespacePath)
	if err != nil {
		return "", fmt.Errorf("unable to determine operator namespace from service account at %s: %w", utils.InClusterNamespacePath, err)
	}
	return strings.TrimSpace(string(ns)), nil
}

// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters,verbs=get;list;watch

// Reconcile handles RayCluster resources and creates/manages NetworkPolicies
func (r *NetworkPolicyController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	// Fetch the RayCluster instance
	instance := &rayv1.RayCluster{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			// RayCluster was deleted - NetworkPolicies will be garbage collected automatically
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Check if RayCluster is being deleted
	if instance.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	if manager := utils.ManagedByExternalController(instance.Spec.ManagedBy); manager != nil {
		logger.Info("Skipping RayCluster managed by a custom controller", "managed-by", manager)
		return ctrl.Result{}, nil
	}

	// Check if NetworkIsolation is configured
	if instance.Spec.NetworkIsolation == nil {
		logger.V(1).Info("NetworkIsolation not configured for RayCluster", "cluster", instance.Name, "namespace", instance.Namespace)
		// If NetworkPolicies exist but NetworkIsolation is removed, clean them up
		return r.cleanupNetworkPoliciesIfNeeded(ctx, instance)
	}

	logger.Info("Reconciling NetworkPolicies for RayCluster", "cluster", instance.Name, "namespace", instance.Namespace)

	// Determine mode (default to DenyAll)
	mode := rayv1.NetworkIsolationDenyAll
	if instance.Spec.NetworkIsolation.Mode != nil {
		mode = *instance.Spec.NetworkIsolation.Mode
	}

	// Create or update head NetworkPolicy
	headNetworkPolicy := r.buildHeadNetworkPolicy(instance, mode)
	if err := r.createOrUpdateNetworkPolicy(ctx, instance, headNetworkPolicy); err != nil {
		return ctrl.Result{}, err
	}

	// Create or update worker NetworkPolicy
	workerNetworkPolicy := r.buildWorkerNetworkPolicy(instance, mode)
	if err := r.createOrUpdateNetworkPolicy(ctx, instance, workerNetworkPolicy); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled NetworkPolicies for RayCluster", "cluster", instance.Name, "namespace", instance.Namespace)
	return ctrl.Result{}, nil
}

// createOrUpdateNetworkPolicy creates or updates a NetworkPolicy
func (r *NetworkPolicyController) createOrUpdateNetworkPolicy(ctx context.Context, instance *rayv1.RayCluster, networkPolicy *networkingv1.NetworkPolicy) error {
	logger := ctrl.LoggerFrom(ctx)

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(instance, networkPolicy, r.Scheme); err != nil {
		return err
	}

	// Try to create the NetworkPolicy
	if err := r.Create(ctx, networkPolicy); err != nil {
		if errors.IsAlreadyExists(err) {
			existing := &networkingv1.NetworkPolicy{}
			if err := r.Get(ctx, client.ObjectKeyFromObject(networkPolicy), existing); err != nil {
				return err
			}

			if !metav1.IsControlledBy(existing, instance) {
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.NetworkPolicyNameCollision),
					"NetworkPolicy %s/%s already exists and is not owned by this RayCluster, network isolation will not be applied. "+
						"Rename the existing NetworkPolicy or use a different RayCluster name to avoid the collision",
					networkPolicy.Namespace, networkPolicy.Name)
				return fmt.Errorf("NetworkPolicy %s/%s already exists and is not owned by this RayCluster", networkPolicy.Namespace, networkPolicy.Name)
			}

			if reflect.DeepEqual(existing.Spec, networkPolicy.Spec) && reflect.DeepEqual(existing.Labels, networkPolicy.Labels) {
				logger.V(1).Info("NetworkPolicy already up to date, skipping update", "name", networkPolicy.Name)
				return nil
			}

			existing.Spec = networkPolicy.Spec
			existing.Labels = networkPolicy.Labels

			if err := r.Update(ctx, existing); err != nil {
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToUpdateNetworkPolicy),
					"Failed to update NetworkPolicy %s/%s: %v", networkPolicy.Namespace, networkPolicy.Name, err)
				return err
			}

			logger.Info("Successfully updated NetworkPolicy", "name", networkPolicy.Name)
			r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.UpdatedNetworkPolicy),
				"Updated NetworkPolicy %s/%s", networkPolicy.Namespace, networkPolicy.Name)
		} else {
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToCreateNetworkPolicy),
				"Failed to create NetworkPolicy %s/%s: %v", networkPolicy.Namespace, networkPolicy.Name, err)
			return err
		}
	} else {
		logger.Info("Successfully created NetworkPolicy", "name", networkPolicy.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.CreatedNetworkPolicy),
			"Created NetworkPolicy %s/%s", networkPolicy.Namespace, networkPolicy.Name)
	}

	return nil
}

func headNetworkPolicyName(clusterName string) string {
	return fmt.Sprintf("%s-head", clusterName)
}

func workerNetworkPolicyName(clusterName string) string {
	return fmt.Sprintf("%s-workers", clusterName)
}

// buildHeadNetworkPolicy creates a NetworkPolicy for Ray head pods
func (r *NetworkPolicyController) buildHeadNetworkPolicy(instance *rayv1.RayCluster, mode rayv1.NetworkIsolationMode) *networkingv1.NetworkPolicy {
	labels := map[string]string{
		utils.RayClusterLabelKey:                instance.Name,
		utils.KubernetesApplicationNameLabelKey: utils.ApplicationName,
		utils.KubernetesCreatedByLabelKey:       utils.ComponentName,
	}

	var policyTypes []networkingv1.PolicyType
	var ingressRules []networkingv1.NetworkPolicyIngressRule
	var egressRules []networkingv1.NetworkPolicyEgressRule

	// Only restrict ingress when mode includes ingress denial.
	// Including PolicyTypeIngress causes K8s to deny all ingress not covered by a rule.
	if mode == rayv1.NetworkIsolationDenyAll || mode == rayv1.NetworkIsolationDenyAllIngress {
		policyTypes = append(policyTypes, networkingv1.PolicyTypeIngress)
		ingressRules = r.buildHeadIngressRules(instance)
		ingressRules = append(ingressRules, instance.Spec.NetworkIsolation.IngressRules...)
	}

	// Only restrict egress when mode includes egress denial.
	if mode == rayv1.NetworkIsolationDenyAll || mode == rayv1.NetworkIsolationDenyAllEgress {
		policyTypes = append(policyTypes, networkingv1.PolicyTypeEgress)
		egressRules = r.buildBaseEgressRules(instance)
		egressRules = append(egressRules, instance.Spec.NetworkIsolation.EgressRules...)
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      headNetworkPolicyName(instance.Name),
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					utils.RayClusterLabelKey:  instance.Name,
					utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
				},
			},
			PolicyTypes: policyTypes,
			Ingress:     ingressRules,
			Egress:      egressRules,
		},
	}
}

// buildWorkerNetworkPolicy creates a NetworkPolicy for Ray worker pods
func (r *NetworkPolicyController) buildWorkerNetworkPolicy(instance *rayv1.RayCluster, mode rayv1.NetworkIsolationMode) *networkingv1.NetworkPolicy {
	labels := map[string]string{
		utils.RayClusterLabelKey:                instance.Name,
		utils.KubernetesApplicationNameLabelKey: utils.ApplicationName,
		utils.KubernetesCreatedByLabelKey:       utils.ComponentName,
	}

	var policyTypes []networkingv1.PolicyType
	var ingressRules []networkingv1.NetworkPolicyIngressRule
	var egressRules []networkingv1.NetworkPolicyEgressRule

	// Only restrict ingress when mode includes ingress denial.
	if mode == rayv1.NetworkIsolationDenyAll || mode == rayv1.NetworkIsolationDenyAllIngress {
		policyTypes = append(policyTypes, networkingv1.PolicyTypeIngress)
		ingressRules = r.buildBaseIngressRules(instance)
		ingressRules = append(ingressRules, instance.Spec.NetworkIsolation.IngressRules...)
	}

	// Only restrict egress when mode includes egress denial.
	if mode == rayv1.NetworkIsolationDenyAll || mode == rayv1.NetworkIsolationDenyAllEgress {
		policyTypes = append(policyTypes, networkingv1.PolicyTypeEgress)
		egressRules = r.buildBaseEgressRules(instance)
		egressRules = append(egressRules, instance.Spec.NetworkIsolation.EgressRules...)
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workerNetworkPolicyName(instance.Name),
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					utils.RayClusterLabelKey:  instance.Name,
					utils.RayNodeTypeLabelKey: string(rayv1.WorkerNode),
				},
			},
			PolicyTypes: policyTypes,
			Ingress:     ingressRules,
			Egress:      egressRules,
		},
	}
}

// buildBaseIngressRules returns the intra-cluster ingress rule shared by both
// head and worker NetworkPolicies: allow all ports from pods in the same RayCluster.
func (r *NetworkPolicyController) buildBaseIngressRules(instance *rayv1.RayCluster) []networkingv1.NetworkPolicyIngressRule {
	return []networkingv1.NetworkPolicyIngressRule{
		{
			From: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							utils.RayClusterLabelKey: instance.Name,
						},
					},
				},
			},
		},
	}
}

// buildHeadIngressRules returns the full set of base ingress rules for the head
// NetworkPolicy: intra-cluster communication and KubeRay operator access.
// For RayJob-owned clusters using K8sJobMode, a per-job submitter rule is added via buildRayJobPeer.
// For RayClusters that are NOT owned by a RayJob (e.g. clusterSelector use cases),
// users must allow their submitter pods explicitly via NetworkIsolation.IngressRules
// (e.g. a podSelector matching the submitterPodTemplate labels). Users who need any
// other external access must likewise add explicit IngressRules in the spec.
func (r *NetworkPolicyController) buildHeadIngressRules(instance *rayv1.RayCluster) []networkingv1.NetworkPolicyIngressRule {
	tcpProtocol := corev1.ProtocolTCP
	headContainer := &instance.Spec.HeadGroupSpec.Template.Spec.Containers[utils.RayContainerIndex]
	dashboardPort := intstr.FromInt32(utils.FindContainerPort(headContainer, utils.DashboardPortName, utils.DefaultDashboardPort))

	rules := r.buildBaseIngressRules(instance)
	rules = append(rules,
		// KubeRay operator access to dashboard port.
		// Only app.kubernetes.io/component is used because app.kubernetes.io/name
		// differs between deployment methods (kustomize sets "kuberay", Helm sets
		// "kuberay-operator"), whereas component is "kuberay-operator" in both.
		// NamespaceSelector restricts to the operator's namespace using the
		// immutable kubernetes.io/metadata.name label (available since K8s 1.21)
		// to prevent label-spoofing from user-supplied RayCluster pod labels.
		networkingv1.NetworkPolicyIngressRule{
			From: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							utils.KubernetesComponentLabelKey: utils.ComponentName,
						},
					},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							corev1.LabelMetadataName: r.OperatorNamespace,
						},
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &tcpProtocol, Port: &dashboardPort},
			},
		},
	)

	if peer := r.buildRayJobPeer(instance); peer != nil {
		// Owned by a RayJob: allow the specific submitter pod.
		rules = append(rules, networkingv1.NetworkPolicyIngressRule{
			From: []networkingv1.NetworkPolicyPeer{*peer},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &tcpProtocol, Port: &dashboardPort},
			},
		})
	}

	return rules
}

// buildRayJobPeer returns a NetworkPolicyPeer matching the submitter pod for
// the RayJob that owns this RayCluster, or nil if not applicable.
//
// Only K8sJobMode creates a standalone submitter Job pod that must reach the head
// dashboard from outside the cluster. Other submission modes (Sidecar, Interactive,
// HTTP) have no such pod — Sidecar runs inside the head pod (covered by the
// intra-cluster rule) and the others submit out-of-band — so no submitter ingress
// rule is needed. The submission mode label is stamped on the RayCluster by the
// RayJob controller (see constructRayClusterForRayJob).
func (r *NetworkPolicyController) buildRayJobPeer(instance *rayv1.RayCluster) *networkingv1.NetworkPolicyPeer {
	if instance.Labels[utils.RayJobSubmissionModeLabelKey] != string(rayv1.K8sJobMode) {
		return nil
	}
	for _, ownerRef := range instance.OwnerReferences {
		if ownerRef.Kind == "RayJob" &&
			ownerRef.Controller != nil && *ownerRef.Controller &&
			strings.HasPrefix(ownerRef.APIVersion, "ray.io/") {
			return &networkingv1.NetworkPolicyPeer{
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						utils.RayOriginatedFromCRNameLabelKey: ownerRef.Name,
						utils.RayOriginatedFromCRDLabelKey:    utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD),
					},
				},
			}
		}
	}
	return nil
}

// buildBaseEgressRules creates the base egress rule allowing intra-cluster
// pod-to-pod communication. DNS egress is intentionally NOT baked in: the
// operator does not assume how the cluster's DNS is deployed.
//
// IMPORTANT: under DenyAll/DenyAllEgress this denies DNS by default. Ray workers
// reach the head via its service FQDN (see GenerateFQDNServiceName), so users
// MUST add a DNS egress rule via NetworkIsolation.EgressRules or the cluster will
// fail to start. See the network-isolation-deny-all sample.
func (r *NetworkPolicyController) buildBaseEgressRules(instance *rayv1.RayCluster) []networkingv1.NetworkPolicyEgressRule {
	return []networkingv1.NetworkPolicyEgressRule{
		// Intra-cluster egress (all ports) to pods in the same RayCluster.
		{
			To: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							utils.RayClusterLabelKey: instance.Name,
						},
					},
				},
			},
			// No Ports specified = allow all ports
		},
	}
}

// cleanupNetworkPoliciesIfNeeded removes NetworkPolicies if they exist but NetworkIsolation is disabled
func (r *NetworkPolicyController) cleanupNetworkPoliciesIfNeeded(ctx context.Context, instance *rayv1.RayCluster) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	// Try to delete head NetworkPolicy if it exists
	headNetworkPolicy := &networkingv1.NetworkPolicy{}
	headName := headNetworkPolicyName(instance.Name)
	headKey := client.ObjectKey{Namespace: instance.Namespace, Name: headName}

	if err := r.Get(ctx, headKey, headNetworkPolicy); err == nil {
		if !metav1.IsControlledBy(headNetworkPolicy, instance) {
			logger.V(1).Info("Head NetworkPolicy exists but is not owned by this RayCluster, skipping deletion", "name", headName)
		} else if err := r.Delete(ctx, headNetworkPolicy); err != nil {
			logger.Error(err, "Failed to delete head NetworkPolicy", "name", headName)
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToDeleteNetworkPolicy),
				"Failed to delete NetworkPolicy %s/%s: %v", instance.Namespace, headName, err)
			return ctrl.Result{}, err
		} else {
			logger.Info("Deleted head NetworkPolicy", "name", headName)
			r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.DeletedNetworkPolicy),
				"Deleted NetworkPolicy %s/%s", instance.Namespace, headName)
		}
	} else if !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	// Try to delete worker NetworkPolicy if it exists
	workerNetworkPolicy := &networkingv1.NetworkPolicy{}
	workerName := workerNetworkPolicyName(instance.Name)
	workerKey := client.ObjectKey{Namespace: instance.Namespace, Name: workerName}

	if err := r.Get(ctx, workerKey, workerNetworkPolicy); err == nil {
		if !metav1.IsControlledBy(workerNetworkPolicy, instance) {
			logger.V(1).Info("Worker NetworkPolicy exists but is not owned by this RayCluster, skipping deletion", "name", workerName)
		} else if err := r.Delete(ctx, workerNetworkPolicy); err != nil {
			logger.Error(err, "Failed to delete worker NetworkPolicy", "name", workerName)
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToDeleteNetworkPolicy),
				"Failed to delete NetworkPolicy %s/%s: %v", instance.Namespace, workerName, err)
			return ctrl.Result{}, err
		} else {
			logger.Info("Deleted worker NetworkPolicy", "name", workerName)
			r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.DeletedNetworkPolicy),
				"Deleted NetworkPolicy %s/%s", instance.Namespace, workerName)
		}
	} else if !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *NetworkPolicyController) SetupWithManager(mgr ctrl.Manager, reconcileConcurrency int) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1.RayCluster{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&networkingv1.NetworkPolicy{}).
		Named("networkpolicy").
		WithOptions(controller.Options{
			MaxConcurrentReconciles: reconcileConcurrency,
			LogConstructor: func(request *reconcile.Request) logr.Logger {
				logger := ctrl.Log.WithName("controllers").WithName("NetworkPolicy")
				if request != nil {
					logger = logger.WithValues("RayCluster", request.NamespacedName)
				}
				return logger
			},
		}).
		Complete(r)
}
