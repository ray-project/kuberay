package ray

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"reflect"
	"strconv"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"
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
func NewNetworkPolicyController(mgr manager.Manager) *NetworkPolicyController {
	return &NetworkPolicyController{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Recorder:          mgr.GetEventRecorderFor("networkpolicy-controller"),
		OperatorNamespace: getOperatorNamespace(),
	}
}

func getAllowAllRayJobSubmitters() bool {
	return os.Getenv(utils.ALLOW_ALL_RAYJOB_SUBMITTERS) == "true"
}

// getOperatorNamespace returns the namespace the operator is running in.
// It reads from the in-cluster service account namespace file first, then
// falls back to "default" which matches both the Helm and Kustomize deployment defaults.
func getOperatorNamespace() string {
	if ns, err := os.ReadFile(utils.InClusterNamespacePath); err == nil {
		return strings.TrimSpace(string(ns))
	}
	ctrl.Log.WithName("controllers").WithName("NetworkPolicy").Info(
		"Unable to determine operator namespace from service account, falling back to 'default'")
	return "default"
}

// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;delete;patch
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
		if utils.IsAutoscalingEnabled(&instance.Spec) {
			egressRules = append(egressRules, r.buildKubeAPIServerEgressRule()...)
		}
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
// For RayJob-owned clusters, a per-job submitter rule is added via buildRayJobPeer.
// For RayClusters that are NOT owned by a RayJob (e.g. clusterSelector use cases),
// the ALLOW_ALL_RAYJOB_SUBMITTERS env var can be set to true to add a broad rule
// allowing all KubeRay-created RayJob submitters in the namespace. This env var
// has no effect on RayJob-owned RayClusters.
// Users who need other external access must add explicit IngressRules in the
// NetworkIsolation spec.
func (r *NetworkPolicyController) buildHeadIngressRules(instance *rayv1.RayCluster) []networkingv1.NetworkPolicyIngressRule {
	tcpProtocol := corev1.ProtocolTCP
	dashboardPort := intstr.FromInt32(r.getHeadPort(instance, "dashboard-port", utils.DefaultDashboardPort))
	clientPort := intstr.FromInt32(r.getHeadPort(instance, "ray-client-server-port", utils.DefaultClientPort))

	rules := r.buildBaseIngressRules(instance)
	rules = append(rules,
		// KubeRay operator access to dashboard and client ports.
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
				{Protocol: &tcpProtocol, Port: &clientPort},
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
	} else if getAllowAllRayJobSubmitters() {
		// Not owned by a RayJob: opt-in broad rule for clusterSelector use cases.
		rules = append(rules, networkingv1.NetworkPolicyIngressRule{
			From: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							utils.RayOriginatedFromCRDLabelKey: utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD),
						},
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &tcpProtocol, Port: &dashboardPort},
			},
		})
	}

	return rules
}

// buildRayJobPeer returns a NetworkPolicyPeer matching the submitter pod for
// the RayJob that owns this RayCluster, or nil if not owned by a RayJob.
func (r *NetworkPolicyController) buildRayJobPeer(instance *rayv1.RayCluster) *networkingv1.NetworkPolicyPeer {
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

// getHeadPort returns the port number for the given rayStartParams key,
// falling back to defaultPort if the key is absent or not a valid integer.
func (r *NetworkPolicyController) getHeadPort(instance *rayv1.RayCluster, rayStartParamKey string, defaultPort int32) int32 {
	if portStr, ok := instance.Spec.HeadGroupSpec.RayStartParams[rayStartParamKey]; ok {
		if port, err := strconv.ParseInt(portStr, 10, 32); err == nil {
			return int32(port)
		}
	}
	return defaultPort
}

// buildBaseEgressRules creates base egress rules (intra-cluster + DNS)
func (r *NetworkPolicyController) buildBaseEgressRules(instance *rayv1.RayCluster) []networkingv1.NetworkPolicyEgressRule {
	udpProtocol := corev1.ProtocolUDP
	tcpProtocol := corev1.ProtocolTCP
	dnsPort := intstr.FromInt32(53)

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
		// Rule 2: DNS egress (port 53) to any destination.
		// Not namespace-restricted so clusters with non-standard DNS deployments
		// (e.g. custom CoreDNS, cloud-provider DNS) are not broken.
		{
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

// buildKubeAPIServerEgressRule returns an egress rule allowing the head pod to
// reach the Kubernetes API server. This is needed when in-tree autoscaling is
// enabled, as the autoscaler sidecar must patch the RayCluster CR via the API.
func (r *NetworkPolicyController) buildKubeAPIServerEgressRule() []networkingv1.NetworkPolicyEgressRule {
	apiServerHost := os.Getenv("KUBERNETES_SERVICE_HOST")
	apiServerPort := os.Getenv("KUBERNETES_SERVICE_PORT")
	if apiServerHost == "" || apiServerPort == "" {
		ctrl.Log.WithName("controllers").WithName("NetworkPolicy").Info(
			"KUBERNETES_SERVICE_HOST or KUBERNETES_SERVICE_PORT not set, skipping API server egress rule")
		return nil
	}

	port, err := strconv.ParseInt(apiServerPort, 10, 32)
	if err != nil {
		ctrl.Log.WithName("controllers").WithName("NetworkPolicy").Info(
			"Failed to parse KUBERNETES_SERVICE_PORT, skipping API server egress rule", "port", apiServerPort)
		return nil
	}

	tcpProtocol := corev1.ProtocolTCP
	apiPort := intstr.FromInt32(int32(port))

	addr, err := netip.ParseAddr(apiServerHost)
	if err != nil {
		ctrl.Log.WithName("controllers").WithName("NetworkPolicy").Info(
			"Failed to parse KUBERNETES_SERVICE_HOST as IP, skipping API server egress rule", "host", apiServerHost)
		return nil
	}

	prefix := netip.PrefixFrom(addr, addr.BitLen())
	cidr := prefix.String()

	return []networkingv1.NetworkPolicyEgressRule{
		{
			To: []networkingv1.NetworkPolicyPeer{
				{
					IPBlock: &networkingv1.IPBlock{
						CIDR: cidr,
					},
				},
			},
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &tcpProtocol, Port: &apiPort},
			},
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
func (r *NetworkPolicyController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rayv1.RayCluster{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Named("networkpolicy").
		WithOptions(controller.Options{
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
