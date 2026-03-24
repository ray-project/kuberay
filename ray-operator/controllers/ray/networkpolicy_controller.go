package ray

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

// NetworkPolicyController is an independent controller that watches RayCluster
// resources and manages NetworkPolicies for them.
type NetworkPolicyController struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// NewNetworkPolicyController creates a new independent NetworkPolicy controller
func NewNetworkPolicyController(mgr manager.Manager) *NetworkPolicyController {
	return &NetworkPolicyController{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("networkpolicy-controller"),
	}
}

// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters,verbs=get;list;watch

// Reconcile handles RayCluster resources and creates/manages NetworkPolicies
func (r *NetworkPolicyController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("networkpolicy-controller")

	// Fetch the RayCluster instance
	instance := &rayv1.RayCluster{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			// RayCluster was deleted - NetworkPolicies will be garbage collected automatically
			logger.Info("RayCluster not found, NetworkPolicies will be garbage collected")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Check if RayCluster is being deleted
	if instance.DeletionTimestamp != nil {
		logger.Info("RayCluster is being deleted, NetworkPolicies will be garbage collected")
		return ctrl.Result{}, nil
	}

	// Check if NetworkIsolation is configured
	if instance.Spec.NetworkIsolation == nil {
		logger.V(1).Info("NetworkIsolation not configured for RayCluster", "cluster", instance.Name)
		// If NetworkPolicies exist but NetworkIsolation is removed, clean them up
		return r.cleanupNetworkPoliciesIfNeeded(ctx, instance)
	}

	logger.Info("Reconciling NetworkPolicies for RayCluster", "cluster", instance.Name)

	// Determine mode (default to denyAll)
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

	logger.Info("Successfully reconciled NetworkPolicies for RayCluster", "cluster", instance.Name)
	return ctrl.Result{}, nil
}

// createOrUpdateNetworkPolicy creates or updates a NetworkPolicy
func (r *NetworkPolicyController) createOrUpdateNetworkPolicy(ctx context.Context, instance *rayv1.RayCluster, networkPolicy *networkingv1.NetworkPolicy) error {
	logger := ctrl.LoggerFrom(ctx).WithName("networkpolicy-controller")

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(instance, networkPolicy, r.Scheme); err != nil {
		return err
	}

	// Try to create the NetworkPolicy
	if err := r.Create(ctx, networkPolicy); err != nil {
		if errors.IsAlreadyExists(err) {
			// NetworkPolicy exists, update it
			existing := &networkingv1.NetworkPolicy{}
			if err := r.Get(ctx, client.ObjectKeyFromObject(networkPolicy), existing); err != nil {
				return err
			}

			// Ensure controller owner reference is set
			if err := controllerutil.SetControllerReference(instance, existing, r.Scheme); err != nil {
				return err
			}

			// Update the existing NetworkPolicy
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
func (r *NetworkPolicyController) buildHeadNetworkPolicy(instance *rayv1.RayCluster, mode string) *networkingv1.NetworkPolicy {
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

		// Auto-allow RayJob submitter pod if RayCluster is owned by a RayJob
		if rayJobPeer := r.buildRayJobPeer(instance); rayJobPeer != nil {
			ingressRules = append(ingressRules, networkingv1.NetworkPolicyIngressRule{
				From: []networkingv1.NetworkPolicyPeer{*rayJobPeer},
			})
		}
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
func (r *NetworkPolicyController) buildWorkerNetworkPolicy(instance *rayv1.RayCluster, mode string) *networkingv1.NetworkPolicy {
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
// NetworkPolicy: intra-cluster communication plus operator and same-namespace
// access to dashboard and client ports.
func (r *NetworkPolicyController) buildHeadIngressRules(instance *rayv1.RayCluster) []networkingv1.NetworkPolicyIngressRule {
	tcpProtocol := corev1.ProtocolTCP
	dashboardPort := intstr.FromInt32(r.getHeadPort(instance, utils.DashboardPortName, utils.DefaultDashboardPort))
	clientPort := intstr.FromInt32(r.getHeadPort(instance, utils.ClientPortName, utils.DefaultClientPort))

	rules := r.buildBaseIngressRules(instance)
	rules = append(rules,
		// KubeRay operator access to dashboard and client ports.
		// Only app.kubernetes.io/component is used because app.kubernetes.io/name
		// differs between deployment methods (kustomize sets "kuberay", Helm sets
		// "kuberay-operator"), whereas component is "kuberay-operator" in both.
		// NamespaceSelector is intentionally empty (matches all namespaces) so that
		// the operator pod is allowed regardless of which namespace it runs in.
		networkingv1.NetworkPolicyIngressRule{
			From: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/component": utils.ComponentName,
						},
					},
					NamespaceSelector: &metav1.LabelSelector{},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &tcpProtocol, Port: &dashboardPort},
				{Protocol: &tcpProtocol, Port: &clientPort},
			},
		},
		// Same-namespace access to dashboard and client ports.
		// An empty PodSelector matches all pods in the same namespace, which
		// covers RayJob submitter pods targeting this cluster via clusterSelector
		// (where no ownerReference exists on the RayCluster).
		networkingv1.NetworkPolicyIngressRule{
			From: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &tcpProtocol, Port: &dashboardPort},
				{Protocol: &tcpProtocol, Port: &clientPort},
			},
		},
	)
	return rules
}

// getHeadPort returns the port number for the named port in the head container,
// falling back to defaultPort if the name is not found or no containers are defined.
func (r *NetworkPolicyController) getHeadPort(instance *rayv1.RayCluster, portName string, defaultPort int32) int32 {
	containers := instance.Spec.HeadGroupSpec.Template.Spec.Containers
	if len(containers) == 0 {
		return defaultPort
	}
	return utils.FindContainerPort(&containers[utils.RayContainerIndex], portName, defaultPort)
}

// buildBaseEgressRules creates base egress rules (intra-cluster + DNS)
func (r *NetworkPolicyController) buildBaseEgressRules(instance *rayv1.RayCluster) []networkingv1.NetworkPolicyEgressRule {
	udpProtocol := corev1.ProtocolUDP
	tcpProtocol := corev1.ProtocolTCP
	dnsPort := intstr.FromInt(53)

	rules := []networkingv1.NetworkPolicyEgressRule{
		// Rule 1: Interpod egress (all ports)
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
		// Rule 2: DNS egress to kube-system
		{
			To: []networkingv1.NetworkPolicyPeer{
				{
					NamespaceSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      corev1.LabelMetadataName,
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"kube-system", "openshift-dns"},
							},
						},
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{
					Protocol: &udpProtocol,
					Port:     &dnsPort,
				},
				{
					Protocol: &tcpProtocol,
					Port:     &dnsPort,
				},
			},
		},
	}

	return rules
}

// cleanupNetworkPoliciesIfNeeded removes NetworkPolicies if they exist but NetworkIsolation is disabled
func (r *NetworkPolicyController) cleanupNetworkPoliciesIfNeeded(ctx context.Context, instance *rayv1.RayCluster) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("networkpolicy-controller")

	// Try to delete head NetworkPolicy if it exists
	headNetworkPolicy := &networkingv1.NetworkPolicy{}
	headName := headNetworkPolicyName(instance.Name)
	headKey := client.ObjectKey{Namespace: instance.Namespace, Name: headName}

	if err := r.Get(ctx, headKey, headNetworkPolicy); err == nil {
		// NetworkPolicy exists, delete it
		if err := r.Delete(ctx, headNetworkPolicy); err != nil {
			logger.Error(err, "Failed to delete head NetworkPolicy", "name", headName)
			return ctrl.Result{}, err
		}
		logger.Info("Deleted head NetworkPolicy", "name", headName)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.DeletedNetworkPolicy),
			"Deleted NetworkPolicy %s/%s", instance.Namespace, headName)
	} else if !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	// Try to delete worker NetworkPolicy if it exists
	workerNetworkPolicy := &networkingv1.NetworkPolicy{}
	workerName := workerNetworkPolicyName(instance.Name)
	workerKey := client.ObjectKey{Namespace: instance.Namespace, Name: workerName}

	if err := r.Get(ctx, workerKey, workerNetworkPolicy); err == nil {
		// NetworkPolicy exists, delete it
		if err := r.Delete(ctx, workerNetworkPolicy); err != nil {
			logger.Error(err, "Failed to delete worker NetworkPolicy", "name", workerName)
			return ctrl.Result{}, err
		}
		logger.Info("Deleted worker NetworkPolicy", "name", workerName)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.DeletedNetworkPolicy),
			"Deleted NetworkPolicy %s/%s", instance.Namespace, workerName)
	} else if !errors.IsNotFound(err) {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// buildRayJobPeer returns a NetworkPolicyPeer for the RayJob submitter pod
// if the RayCluster is owned by a RayJob. Returns nil otherwise.
func (r *NetworkPolicyController) buildRayJobPeer(instance *rayv1.RayCluster) *networkingv1.NetworkPolicyPeer {
	for _, ownerRef := range instance.OwnerReferences {
		if ownerRef.Kind == "RayJob" {
			return &networkingv1.NetworkPolicyPeer{
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"batch.kubernetes.io/job-name": ownerRef.Name,
					},
				},
			}
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *NetworkPolicyController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1.RayCluster{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Named("networkpolicy").
		Complete(r)
}
