package ray

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

// NetworkPolicyController is a completely independent controller that watches RayCluster
// resources and manages NetworkPolicies for them.
type NetworkPolicyController struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters,verbs=get;list;watch

// NewNetworkPolicyController creates a new independent NetworkPolicy controller
func NewNetworkPolicyController(mgr manager.Manager) *NetworkPolicyController {
	return &NetworkPolicyController{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("networkpolicy-controller"),
	}
}

// Reconcile handles RayCluster resources and creates/manages NetworkPolicies
func (r *NetworkPolicyController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("networkpolicy-controller")

	// Fetch the RayCluster instance
	instance := &rayv1.RayCluster{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			// RayCluster was deleted - NetworkPolicy will be garbage collected automatically
			logger.Info("RayCluster not found, NetworkPolicy will be garbage collected")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Check if RayCluster is being deleted
	if instance.DeletionTimestamp != nil {
		logger.Info("RayCluster is being deleted, NetworkPolicy will be garbage collected")
		return ctrl.Result{}, nil
	}

	logger.Info("Reconciling NetworkPolicy for RayCluster", "cluster", instance.Name)

	// Create or update NetworkPolicy
	networkPolicy := r.buildNetworkPolicy(instance)

	// Set owner reference for garbage collection
	if err := controllerutil.SetControllerReference(instance, networkPolicy, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	// Create the NetworkPolicy
	if err := r.Create(ctx, networkPolicy); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("NetworkPolicy already exists")
			return ctrl.Result{}, nil
		}
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.FailedToCreateNetworkPolicy),
			"Failed to create NetworkPolicy %s/%s: %v", networkPolicy.Namespace, networkPolicy.Name, err)
		return ctrl.Result{}, err
	}

	logger.Info("Successfully created NetworkPolicy", "name", networkPolicy.Name)
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.CreatedNetworkPolicy),
		"Created NetworkPolicy %s/%s", networkPolicy.Namespace, networkPolicy.Name)

	return ctrl.Result{}, nil
}

// buildNetworkPolicy creates a NetworkPolicy for the given RayCluster
func (r *NetworkPolicyController) buildNetworkPolicy(instance *rayv1.RayCluster) *networkingv1.NetworkPolicy {
	operatorNamespace := os.Getenv("POD_NAMESPACE")
	if operatorNamespace == "" {
		operatorNamespace = "ray-system" // fallback
	}

	labels := map[string]string{
		utils.RayClusterLabelKey:                instance.Name,
		utils.KubernetesApplicationNameLabelKey: utils.ApplicationName,
		utils.KubernetesCreatedByLabelKey:       utils.ComponentName,
	}

	// Build base peers
	peers := []networkingv1.NetworkPolicyPeer{
		// Allow intra-cluster communication
		{
			PodSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					utils.RayClusterLabelKey: instance.Name,
				},
			},
		},
		// Allow KubeRay operator communication
		{
			PodSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					utils.KubernetesApplicationNameLabelKey: utils.ApplicationName,
				},
			},
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kubernetes.io/metadata.name": operatorNamespace,
				},
			},
		},
	}

	// Add RayJob submitter peer if RayCluster is owned by RayJob
	if rayJobPeer := r.buildRayJobPeer(instance); rayJobPeer != nil {
		peers = append(peers, *rayJobPeer)
	}

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-default-deny", instance.Name),
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					utils.RayClusterLabelKey: instance.Name,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: peers,
				},
			},
		},
	}
}

// buildRayJobPeer creates a NetworkPolicy peer for RayJob submitter pods
// Returns nil if RayCluster is not owned by RayJob
func (r *NetworkPolicyController) buildRayJobPeer(instance *rayv1.RayCluster) *networkingv1.NetworkPolicyPeer {
	// Check if RayCluster is owned by RayJob
	for _, ownerRef := range instance.OwnerReferences {
		if ownerRef.Kind == "RayJob" {
			// Return peer for RayJob submitter pods
			return &networkingv1.NetworkPolicyPeer{
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"batch.kubernetes.io/job-name": ownerRef.Name,
					},
				},
			}
		}
	}
	// No RayJob owner = no RayJob submitter pods to allow
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
