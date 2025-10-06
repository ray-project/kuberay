package ray

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

// Backoff and retry constants for OIDC rollout coordination
const (
	// MediumRequeueDelay for ongoing rollouts
	MediumRequeueDelay = 10 * time.Second

	// OAuth proxy constants
	oauthProxyContainerName = "oauth-proxy"
	oauthProxyVolumeName    = "proxy-tls-secret"
	authProxyPort           = 8443
	oauthProxyPortName      = "oauth-proxy"

	oauthConfigVolumeName = "oauth-config"

	oidcProxyContainerName  = "kube-rbac-proxy"
	oidcProxyPortName       = "https"
	oauthProxyImage         = "registry.redhat.io/openshift4/ose-oauth-proxy:latest"
	oidcProxyContainerImage = "registry.redhat.io/openshift4/ose-kube-rbac-proxy-rhel9@sha256:784c4667a867abdbec6d31a4bbde52676a0f37f8e448eaae37568a46fcdeace7"
)

// AuthenticationController is a completely independent controller that watches authentication-related
// resources (ConfigMaps, ServiceAccounts, OpenShift Routes) and manages authentication configurations for Ray clusters on Openshift.
type AuthenticationController struct {
	client.Client
	Recorder record.EventRecorder
	// configClient configclient.Interface
	Scheme *runtime.Scheme
	// routeClient  *routev1client.RouteV1Client
	options RayClusterReconcilerOptions
}

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=kubeapiservers,verbs=get;list;watch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=kubeapiservers/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=config.openshift.io,resources=authentications,verbs=get;list;watch
// +kubebuilder:rbac:groups=config.openshift.io,resources=authentications/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=config.openshift.io,resources=oauths,verbs=get;list;watch
// +kubebuilder:rbac:groups=config.openshift.io,resources=oauths/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters,verbs=get;list;watch;update;patch
// NewAuthenticationController creates a new authentication controller
func NewAuthenticationController(mgr manager.Manager, options RayClusterReconcilerOptions) *AuthenticationController {
	return &AuthenticationController{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("authentication-controller"),
		options:  options,
	}
}

// Reconcile handles authentication-related resources and manages OAuth sidecar injection
func (r *AuthenticationController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx).WithName("authentication-controller")
	logger.Info("Reconciling Authentication", "namespacedName", req.NamespacedName)

	// Get the RayCluster - this should always be a RayCluster now due to proper mapping
	rayCluster := &rayv1.RayCluster{}
	if err := r.Get(ctx, req.NamespacedName, rayCluster); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("RayCluster not found, likely deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get RayCluster")
		return ctrl.Result{RequeueAfter: MediumRequeueDelay}, err
	}

	// Skip if managed by external controller
	if manager := utils.ManagedByExternalController(rayCluster.Spec.ManagedBy); manager != nil {
		logger.Info("Skipping RayCluster managed by external controller", "managed-by", manager)
		return ctrl.Result{}, nil
	}

	// Detect the authentication mode configured in the cluster
	authMode := utils.DetectAuthenticationMode(ctx, r.Client, r.options.IsOpenShift)
	logger.Info("Detected authentication mode", "mode", authMode, "cluster", rayCluster.Name)

	// Handle authentication based on detected mode
	var err error
	switch authMode {
	case utils.ModeIntegratedOAuth:
		logger.Info("Handling Integrated OAuth", "cluster", rayCluster.Name)
		err = r.handleOAuthConfiguration(ctx, req, authMode, logger)

	case utils.ModeOIDC:
		logger.Info("Handling OIDC", "cluster", rayCluster.Name)
		err = r.handleOIDCConfiguration(ctx, req, authMode, logger)
	}

	if err != nil {
		logger.Error(err, "Failed to handle authentication configuration")
		return ctrl.Result{RequeueAfter: MediumRequeueDelay}, err
	}

	logger.Info("Successfully reconciled authentication", "cluster", rayCluster.Name)
	// Don't requeue on success - let watches trigger next reconciliation
	return ctrl.Result{}, nil
}

// handleOIDCConfiguration configures OIDC for RayClusters
func (r *AuthenticationController) handleOIDCConfiguration(ctx context.Context, req ctrl.Request, authMode utils.AuthenticationMode, logger logr.Logger) error {
	// Try to get RayCluster
	rayCluster := &rayv1.RayCluster{}
	if err := r.Get(ctx, req.NamespacedName, rayCluster); err != nil {
		// Not a RayCluster or doesn't exist, skip
		return client.IgnoreNotFound(err)
	}

	if !utils.ShouldEnableOIDC(r.options.ControlledNetworkEnvironment, authMode) {
		logger.Info("OIDC not requested for this cluster", "cluster", rayCluster.Name)
		return r.cleanupOIDCResources(ctx, rayCluster, logger)
	}

	// Ensure ingress is enabled for httproute creation
	if err := r.ensureIngressEnabled(ctx, rayCluster, logger); err != nil {
		return fmt.Errorf("failed to ensure ingress enabled: %w", err)
	}

	// Ensure OIDC resources exist
	if err := r.ensureOIDCResources(ctx, rayCluster, logger); err != nil {
		return fmt.Errorf("failed to ensure OIDC resources: %w", err)
	}

	r.Recorder.Event(rayCluster, "Normal", "OIDCConfigured", "OIDC proxy configured for RayCluster")
	return nil
}

// handleOAuthConfiguration configures OAuth proxy for RayClusters
func (r *AuthenticationController) handleOAuthConfiguration(ctx context.Context, req ctrl.Request, authMode utils.AuthenticationMode, logger logr.Logger) error {
	// Try to get RayCluster
	rayCluster := &rayv1.RayCluster{}
	if err := r.Get(ctx, req.NamespacedName, rayCluster); err != nil {
		// Not a RayCluster or doesn't exist, skip
		return client.IgnoreNotFound(err)
	}

	logger.Info("Handling OAuth configuration for RayCluster", "cluster", rayCluster.Name, "namespace", rayCluster.Namespace)

	// Check if OAuth should be enabled
	if !utils.ShouldEnableOAuth(r.options.ControlledNetworkEnvironment, authMode) {
		logger.Info("OAuth not requested for this cluster", "cluster", rayCluster.Name)
		return r.cleanupOAuthResources(ctx, rayCluster, logger)
	}

	// Ensure ingress is enabled for route creation
	if err := r.ensureIngressEnabled(ctx, rayCluster, logger); err != nil {
		return fmt.Errorf("failed to ensure ingress enabled: %w", err)
	}

	// Ensure OAuth resources exist
	if err := r.ensureOAuthResources(ctx, rayCluster, logger); err != nil {
		return fmt.Errorf("failed to ensure OAuth resources: %w", err)
	}

	// Update head service to include OAuth proxy port
	if err := r.updateHeadServiceForOAuth(ctx, rayCluster, logger); err != nil {
		logger.Info("Failed to update head service for OAuth", "error", err)
		// Don't fail reconciliation if service update fails
	}

	// NOTE: We do NOT try to retrofit existing pods with OAuth sidecar
	// Kubernetes pods are immutable - you cannot add containers to running pods
	// The OAuth sidecar is injected during pod creation by the RayCluster controller
	// For existing clusters without OAuth, the user must delete and recreate the RayCluster
	// New clusters will automatically get OAuth sidecar if shouldEnableOAuth() returns true
	logger.Info("OAuth resources ready. New pods will automatically get OAuth sidecar.",
		"cluster", rayCluster.Name,
		"note", "Existing pods require recreation to get OAuth protection")

	// Update route to use OAuth proxy if it exists
	if err := r.updateRouteForOAuth(ctx, rayCluster, logger); err != nil {
		logger.Info("Failed to update route for OAuth", "error", err)
		// Don't fail reconciliation if route update fails
	}

	r.Recorder.Event(rayCluster, "Normal", "OAuthConfigured", "OAuth proxy configured for RayCluster")
	return nil
}

// ensureIngressEnabled ensures that ingress is enabled for the RayCluster
func (r *AuthenticationController) ensureIngressEnabled(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	// Check if ingress is already enabled
	if cluster.Spec.HeadGroupSpec.EnableIngress != nil && *cluster.Spec.HeadGroupSpec.EnableIngress {
		logger.Info("Ingress already enabled for cluster", "cluster", cluster.Name)
		return nil
	}

	logger.Info("Enabling ingress for Auth-secured cluster", "cluster", cluster.Name)

	// Create a copy and enable ingress
	updatedCluster := cluster.DeepCopy()
	trueValue := true
	updatedCluster.Spec.HeadGroupSpec.EnableIngress = &trueValue

	// Update the cluster
	if err := r.Update(ctx, updatedCluster); err != nil {
		return fmt.Errorf("failed to enable ingress: %w", err)
	}

	logger.Info("Successfully enabled ingress for cluster", "cluster", cluster.Name)
	r.Recorder.Event(cluster, "Normal", "IngressEnabled", "Automatically enabled ingress for Auth configuration")
	return nil
}

func (r *AuthenticationController) ensureOIDCResources(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	if err := r.ensureOAuthServiceAccount(ctx, cluster, logger); err != nil {
		return fmt.Errorf("failed to ensure service account: %w", err)
	}

	// Create HttpRoute
	if err := r.ensureHttpRoute(ctx, cluster, logger); err != nil {
		return fmt.Errorf("failed to ensure HttpRoute: %w", err)
	}

	// Create ConfigMap
	if err := r.ensureOIDCConfigMap(ctx, cluster, logger); err != nil {
		return fmt.Errorf("failed to ensure ConfigMap: %w", err)
	}

	return nil
}

func (r *AuthenticationController) cleanupOIDCResources(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	namer := utils.NewResourceNamer(cluster)

	// Remove OIDC ConfigMap
	configMap := &corev1.ConfigMap{}
	configMapName := namer.ConfigMapName()
	if err := r.Get(ctx, client.ObjectKey{Name: configMapName, Namespace: cluster.Namespace}, configMap); err == nil {
		if err := r.Delete(ctx, configMap); err != nil {
			logger.Info("Failed to delete OIDC ConfigMap", "error", err)
		} else {
			logger.Info("Deleted OIDC ConfigMap", "configMap", configMapName)
		}
	}

	// Remove HTTPRoute
	httpRoute := &gatewayv1.HTTPRoute{}
	httpRouteName := cluster.Name
	if err := r.Get(ctx, client.ObjectKey{Name: httpRouteName, Namespace: cluster.Namespace}, httpRoute); err == nil {
		if err := r.Delete(ctx, httpRoute); err != nil {
			logger.Info("Failed to delete HTTPRoute", "error", err)
		} else {
			logger.Info("Deleted HTTPRoute", "httpRoute", httpRouteName)
		}
	}

	// Remove service account (OIDC uses the same service account creation as OAuth)
	sa := &corev1.ServiceAccount{}
	saName := namer.ServiceAccountName(utils.ModeOIDC)
	if err := r.Get(ctx, client.ObjectKey{Name: saName, Namespace: cluster.Namespace}, sa); err == nil {
		if err := r.Delete(ctx, sa); err != nil {
			logger.Info("Failed to delete OIDC service account", "error", err)
		} else {
			logger.Info("Deleted OIDC service account", "serviceAccount", saName)
		}
	}

	return nil
}

func (r *AuthenticationController) ensureHttpRoute(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	serviceName, err := utils.GenerateHeadServiceName("RayCluster", cluster.Spec, cluster.Name)
	if err != nil {
		return err
	}

	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
	}

	opResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, httpRoute, func() error {
		// Set controller reference
		if err := controllerutil.SetControllerReference(cluster, httpRoute, r.Scheme); err != nil {
			return err
		}

		// Helper variables for pointer fields
		group := gatewayv1.Group("gateway.networking.k8s.io")
		kind := gatewayv1.Kind("Gateway")
		gatewayName := gatewayv1.ObjectName("data-science-gateway")
		namespace := gatewayv1.Namespace("openshift-ingress")
		serviceGroup := gatewayv1.Group("")
		serviceKind := gatewayv1.Kind("Service")
		weight := int32(1)
		pathExact := gatewayv1.PathMatchExact
		pathPrefix := gatewayv1.PathMatchPathPrefix
		pathValue := "/"
		prefixValue := fmt.Sprintf("/ray/%s/%s", cluster.Namespace, cluster.Name)

		// Update the HTTPRoute spec
		httpRoute.Spec = gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Group:     &group,
						Kind:      &kind,
						Name:      gatewayName,
						Namespace: &namespace,
					},
				},
			},
			Rules: []gatewayv1.HTTPRouteRule{
				// Rule 1: Exact match for root path - redirect to #/
				// This handles the case when users access /ray/{namespace}/{cluster} without the trailing hash
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Path: &gatewayv1.HTTPPathMatch{
								Type:  &pathExact,
								Value: &prefixValue,
							},
						},
					},
					Filters: []gatewayv1.HTTPRouteFilter{
						{
							Type: gatewayv1.HTTPRouteFilterRequestRedirect,
							RequestRedirect: &gatewayv1.HTTPRequestRedirectFilter{
								Path: &gatewayv1.HTTPPathModifier{
									Type:            gatewayv1.FullPathHTTPPathModifier,
									ReplaceFullPath: ptr.To(prefixValue + "/#/"),
								},
								StatusCode: ptr.To(302), // Temporary redirect
							},
						},
					},
				},
				// Rule 2: Prefix match for all other paths (including #/)
				// This handles all sub-paths and rewrites them to the backend
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Path: &gatewayv1.HTTPPathMatch{
								Type:  &pathPrefix,
								Value: &prefixValue,
							},
						},
					},
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Group: &serviceGroup,
									Kind:  &serviceKind,
									Name:  gatewayv1.ObjectName(serviceName),
									Port:  (*gatewayv1.PortNumber)(ptr.To(int32(8265))),
								},
								Weight: &weight,
							},
						},
					},
					Filters: []gatewayv1.HTTPRouteFilter{
						{
							Type: gatewayv1.HTTPRouteFilterURLRewrite,
							URLRewrite: &gatewayv1.HTTPURLRewriteFilter{
								Path: &gatewayv1.HTTPPathModifier{
									Type:               gatewayv1.PrefixMatchHTTPPathModifier,
									ReplacePrefixMatch: &pathValue,
								},
							},
						},
					},
				},
			},
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update HTTPRoute: %w", err)
	}

	if opResult != controllerutil.OperationResultNone {
		logger.Info("HTTPRoute reconciled", "name", httpRoute.Name, "operation", opResult)
	}

	return nil
}

func (r *AuthenticationController) ensureOIDCConfigMap(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	namer := utils.NewResourceNamer(cluster)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namer.ConfigMapName(),
			Namespace: cluster.Namespace,
		},
	}

	opResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		// Set controller reference
		if err := controllerutil.SetControllerReference(cluster, configMap, r.Scheme); err != nil {
			return err
		}

		// Build ConfigMap data dynamically
		configYAML := fmt.Sprintf(`
authorization:
  resourceAttributes:
    # For an incoming request, the proxy will check if the user
    # has the "get" verb on the "services" resource.
    verb: "get"
    resource: "services"
    # The API group and resource name should match the target Service.
    apiGroup: ""
    resourceName: "%s"
`, cluster.Name+"-head-svc")

		// Set labels and data
		if configMap.Labels == nil {
			configMap.Labels = make(map[string]string)
		}
		configMap.Labels["app"] = "kube-rbac-proxy"

		if configMap.Data == nil {
			configMap.Data = make(map[string]string)
		}
		configMap.Data["config.yaml"] = configYAML

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update OIDC ConfigMap: %w", err)
	}

	if opResult != controllerutil.OperationResultNone {
		logger.Info("OIDC ConfigMap reconciled", "name", configMap.Name, "operation", opResult)
	}

	return nil
}

// ensureOAuthResources creates or updates OAuth resources for a RayCluster
func (r *AuthenticationController) ensureOAuthResources(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	// Create service account
	if err := r.ensureOAuthServiceAccount(ctx, cluster, logger); err != nil {
		return fmt.Errorf("failed to ensure service account: %w", err)
	}

	// Create OAuth cookie secret
	if err := r.ensureOAuthSecret(ctx, cluster, logger); err != nil {
		return fmt.Errorf("failed to ensure OAuth secret: %w", err)
	}

	// Create TLS secret for OAuth proxy
	if err := r.ensureOAuthTLSSecret(ctx, cluster, logger); err != nil {
		return fmt.Errorf("failed to ensure TLS secret: %w", err)
	}

	return nil
}

// ensureOAuthServiceAccount creates or updates the OAuth service account
func (r *AuthenticationController) ensureOAuthServiceAccount(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	namer := utils.NewResourceNamer(cluster)
	saName := namer.ServiceAccountName(utils.ModeIntegratedOAuth)
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: cluster.Namespace,
		},
	}

	opResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
		// Set controller reference
		if err := controllerutil.SetControllerReference(cluster, sa, r.Scheme); err != nil {
			return err
		}

		// Add service account annotation for OAuth
		if sa.Annotations == nil {
			sa.Annotations = make(map[string]string)
		}
		sa.Annotations["serviceaccounts.openshift.io/oauth-redirectreference.first"] = fmt.Sprintf(
			`{"kind":"OAuthRedirectReference","apiVersion":"v1","reference":{"kind":"Route","name":"%s"}}`,
			utils.GenerateRouteName(cluster.Name),
		)

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update OAuth service account: %w", err)
	}

	if opResult != controllerutil.OperationResultNone {
		logger.Info("OAuth service account reconciled", "name", saName, "operation", opResult)
	}

	return nil
}

// ensureOAuthSecret creates or updates the OAuth cookie secret
func (r *AuthenticationController) ensureOAuthSecret(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	namer := utils.NewResourceNamer(cluster)
	secretName := namer.SecretName(utils.ModeIntegratedOAuth)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: cluster.Namespace,
		},
	}

	opResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		// Set controller reference
		if err := controllerutil.SetControllerReference(cluster, secret, r.Scheme); err != nil {
			return err
		}

		// Only generate cookie secret if it doesn't exist
		// Don't overwrite existing secrets to maintain session continuity
		if secret.Data == nil || len(secret.Data["cookie_secret"]) == 0 {
			hasher := sha256.New()
			hasher.Write([]byte(cluster.Name + cluster.Namespace + "oauth-salt"))
			cookieSecret := base64.StdEncoding.EncodeToString(hasher.Sum(nil))

			if secret.Data == nil {
				secret.Data = make(map[string][]byte)
			}
			secret.Data["cookie_secret"] = []byte(cookieSecret)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update OAuth secret: %w", err)
	}

	if opResult != controllerutil.OperationResultNone {
		logger.Info("OAuth secret reconciled", "name", secretName, "operation", opResult)
	}

	return nil
}

// ensureOAuthTLSSecret creates or updates the TLS secret for OAuth proxy
func (r *AuthenticationController) ensureOAuthTLSSecret(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	namer := utils.NewResourceNamer(cluster)
	secretName := namer.TLSSecretName(utils.ModeIntegratedOAuth)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: cluster.Namespace,
		},
	}

	opResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		// Set controller reference
		if err := controllerutil.SetControllerReference(cluster, secret, r.Scheme); err != nil {
			return err
		}

		// Only generate certificate if it doesn't exist
		// Don't overwrite existing certificates to avoid connection disruptions
		if secret.Data == nil || len(secret.Data["tls.crt"]) == 0 || len(secret.Data["tls.key"]) == 0 {
			cert, key, err := generateSelfSignedCert(cluster)
			if err != nil {
				return fmt.Errorf("failed to generate certificate: %w", err)
			}

			if secret.Data == nil {
				secret.Data = make(map[string][]byte)
			}
			secret.Data["tls.crt"] = cert
			secret.Data["tls.key"] = key
		}

		// Set secret type and annotations
		secret.Type = corev1.SecretTypeTLS
		if secret.Annotations == nil {
			secret.Annotations = make(map[string]string)
		}
		secret.Annotations["service.beta.openshift.io/serving-cert-secret-name"] = secretName

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update OAuth TLS secret: %w", err)
	}

	if opResult != controllerutil.OperationResultNone {
		logger.Info("OAuth TLS secret reconciled", "name", secretName, "operation", opResult)
	}

	return nil
}

// NOTE: Pod recreation logic removed - Kubernetes pods are immutable
// You cannot add containers to running pods, so we don't try to retrofit existing pods
// OAuth sidecar is automatically injected by RayCluster controller during pod creation
// Users must delete and recreate their RayCluster to enable OAuth on existing clusters

// updateHeadServiceForOAuth updates the head service to include OAuth proxy port
func (r *AuthenticationController) updateHeadServiceForOAuth(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	serviceName, err := utils.GenerateHeadServiceName("RayCluster", cluster.Spec, cluster.Name)
	if err != nil {
		return err
	}

	service := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKey{Name: serviceName, Namespace: cluster.Namespace}, service); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Head service not found yet", "service", serviceName)
			return nil
		}
		return err
	}

	// Update service to include OAuth proxy port if needed
	opResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		// Check if OAuth proxy port already exists
		oauthPortExists := false
		for _, port := range service.Spec.Ports {
			if port.Name == oauthProxyPortName {
				oauthPortExists = true
				break
			}
		}

		// Add OAuth proxy port if it doesn't exist
		if !oauthPortExists {
			oauthPort := corev1.ServicePort{
				Name:     oauthProxyPortName,
				Port:     authProxyPort,
				Protocol: corev1.ProtocolTCP,
			}
			service.Spec.Ports = append(service.Spec.Ports, oauthPort)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to update head service for OAuth: %w", err)
	}

	if opResult != controllerutil.OperationResultNone {
		logger.Info("Head service reconciled for OAuth", "service", serviceName, "operation", opResult)
	}

	return nil
}

// updateRouteForOAuth updates the RayCluster route to use OAuth proxy
func (r *AuthenticationController) updateRouteForOAuth(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	routeName := utils.GenerateRouteName(cluster.Name)
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: cluster.Namespace,
		},
	}

	// Update route to use OAuth proxy port and TLS configuration
	opResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, route, func() error {
		// Check if route exists first
		if route.CreationTimestamp.IsZero() {
			// Route doesn't exist yet, skip for now
			// It will be created with OAuth config by the RayCluster controller
			logger.Info("Route not found, will be created with OAuth proxy configuration", "route", routeName)
			return errors.NewNotFound(routev1.Resource("routes"), routeName)
		}

		// Update route to use OAuth proxy port
		if route.Spec.Port == nil || route.Spec.Port.TargetPort.String() != oauthProxyPortName {
			route.Spec.Port = &routev1.RoutePort{
				TargetPort: intstr.FromString(oauthProxyPortName),
			}
		}

		// Ensure TLS is configured for passthrough
		if route.Spec.TLS == nil || route.Spec.TLS.Termination != routev1.TLSTerminationPassthrough {
			route.Spec.TLS = &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationPassthrough,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			}
		}

		return nil
	})
	if err != nil {
		if errors.IsNotFound(err) {
			// Route doesn't exist yet, that's okay
			return nil
		}
		return fmt.Errorf("failed to update route for OAuth: %w", err)
	}

	if opResult != controllerutil.OperationResultNone {
		logger.Info("Route reconciled for OAuth", "route", routeName, "operation", opResult)
	}

	return nil
}

// cleanupOAuthResources removes OAuth resources when OAuth is disabled
func (r *AuthenticationController) cleanupOAuthResources(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	namer := utils.NewResourceNamer(cluster)

	// Remove TLS secret
	tlsSecret := &corev1.Secret{}
	tlsSecretName := namer.TLSSecretName(utils.ModeIntegratedOAuth)
	if err := r.Get(ctx, client.ObjectKey{Name: tlsSecretName, Namespace: cluster.Namespace}, tlsSecret); err == nil {
		if err := r.Delete(ctx, tlsSecret); err != nil {
			logger.Info("Failed to delete TLS secret", "error", err)
		} else {
			logger.Info("Deleted OAuth TLS secret", "secret", tlsSecretName)
		}
	}

	// Remove OAuth secret
	oauthSecret := &corev1.Secret{}
	oauthSecretName := namer.SecretName(utils.ModeIntegratedOAuth)
	if err := r.Get(ctx, client.ObjectKey{Name: oauthSecretName, Namespace: cluster.Namespace}, oauthSecret); err == nil {
		if err := r.Delete(ctx, oauthSecret); err != nil {
			logger.Info("Failed to delete OAuth secret", "error", err)
		} else {
			logger.Info("Deleted OAuth secret", "secret", oauthSecretName)
		}
	}

	// Remove service account
	sa := &corev1.ServiceAccount{}
	saName := namer.ServiceAccountName(utils.ModeIntegratedOAuth)
	if err := r.Get(ctx, client.ObjectKey{Name: saName, Namespace: cluster.Namespace}, sa); err == nil {
		if err := r.Delete(ctx, sa); err != nil {
			logger.Info("Failed to delete service account", "error", err)
		} else {
			logger.Info("Deleted OAuth service account", "serviceAccount", saName)
		}
	}

	return nil
}

// generateSelfSignedCert generates a self-signed certificate for OAuth proxy
func generateSelfSignedCert(cluster *rayv1.RayCluster) ([]byte, []byte, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"KubeRay OAuth Proxy"},
			CommonName:   cluster.Name + "-oauth-proxy",
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames: []string{
			"localhost",
			cluster.Name,
			fmt.Sprintf("%s.%s", cluster.Name, cluster.Namespace),
			fmt.Sprintf("%s.%s.svc", cluster.Name, cluster.Namespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", cluster.Name, cluster.Namespace),
		},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

// GetOAuthProxySidecar returns the OAuth proxy sidecar container configuration
// This can be used by the RayCluster controller to inject the sidecar
func GetOAuthProxySidecar(cluster *rayv1.RayCluster) corev1.Container {
	namer := utils.NewResourceNamer(cluster)
	return corev1.Container{
		Name:            oauthProxyContainerName,
		Image:           oauthProxyImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Ports: []corev1.ContainerPort{
			utils.CreateContainerPort(authProxyPort, oauthProxyPortName),
		},
		Args: []string{
			fmt.Sprintf("--https-address=:%d", authProxyPort),
			"--provider=openshift",
			fmt.Sprintf("--openshift-service-account=%s", namer.ServiceAccountName(utils.ModeIntegratedOAuth)),
			"--upstream=http://localhost:8265",
			"--tls-cert=/etc/tls/private/tls.crt",
			"--tls-key=/etc/tls/private/tls.key",
			"--cookie-secret=$(COOKIE_SECRET)",
			fmt.Sprintf("--openshift-delegate-urls=%s", utils.FormatOAuthDelegateURLs(cluster.Namespace)),
			"--skip-provider-button",
		},
		Env: []corev1.EnvVar{
			utils.CreateEnvVarFromSecret("COOKIE_SECRET", namer.SecretName(utils.ModeIntegratedOAuth), "cookie_secret"),
		},
		VolumeMounts: []corev1.VolumeMount{
			utils.CreateVolumeMount(oauthProxyVolumeName, "/etc/tls/private", true),
		},
		// Add resource limits to prevent excessive resource usage
		Resources: utils.StandardProxyResources(),
		// Add liveness probe to detect if OAuth proxy is healthy
		LivenessProbe: utils.CreateProbe(utils.ProbeConfig{
			Path:                "/oauth/healthz",
			Port:                authProxyPort,
			Scheme:              corev1.URISchemeHTTPS,
			InitialDelaySeconds: 30,
			TimeoutSeconds:      1,
			PeriodSeconds:       10,
			SuccessThreshold:    1,
			FailureThreshold:    3,
		}),
		// Add readiness probe to prevent routing traffic before OAuth proxy is ready
		ReadinessProbe: utils.CreateProbe(utils.ProbeConfig{
			Path:                "/oauth/healthz",
			Port:                authProxyPort,
			Scheme:              corev1.URISchemeHTTPS,
			InitialDelaySeconds: 5,
			TimeoutSeconds:      1,
			PeriodSeconds:       5,
			SuccessThreshold:    1,
			FailureThreshold:    3,
		}),
	}
}

// GetOAuthProxyVolumes returns the volumes needed for OAuth proxy sidecar
func GetOAuthProxyVolumes(cluster *rayv1.RayCluster) []corev1.Volume {
	namer := utils.NewResourceNamer(cluster)
	return []corev1.Volume{
		utils.CreateSecretVolume(oauthConfigVolumeName, namer.SecretName(utils.ModeIntegratedOAuth)),
		utils.CreateSecretVolume(oauthProxyVolumeName, namer.TLSSecretName(utils.ModeIntegratedOAuth)),
	}
}

func GetOIDCProxySidecar(cluster *rayv1.RayCluster) corev1.Container {
	namer := utils.NewResourceNamer(cluster)
	configMapName := namer.ConfigMapName()
	return corev1.Container{
		Name:            oidcProxyContainerName,
		Image:           oidcProxyContainerImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Ports: []corev1.ContainerPort{
			utils.CreateContainerPort(authProxyPort, oidcProxyPortName),
		},
		Args: []string{
			fmt.Sprintf("--secure-listen-address=0.0.0.0:%d", authProxyPort),
			"--upstream=http://127.0.0.1:8080/",
			"--config-file=/etc/kube-rbac-proxy/config.yaml",
			"--logtostderr=true",
		},
		VolumeMounts: []corev1.VolumeMount{
			utils.CreateVolumeMount(configMapName, "/etc/kube-rbac-proxy/", true),
		},
	}
}

func GetOIDCProxyVolumes(cluster *rayv1.RayCluster) []corev1.Volume {
	namer := utils.NewResourceNamer(cluster)
	configMapName := namer.ConfigMapName()
	return []corev1.Volume{
		utils.CreateConfigMapVolume(configMapName, configMapName),
	}
}

// SetupWithManager sets up the controller with the Manager
func (r *AuthenticationController) SetupWithManager(mgr ctrl.Manager) error {
	// Predicate to only reconcile RayClusters when relevant changes occur
	rayClusterPredicate := predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.AnnotationChangedPredicate{},
	)

	return ctrl.NewControllerManagedBy(mgr).
		// PRIMARY: Watch RayClusters
		For(&rayv1.RayCluster{}, builder.WithPredicates(rayClusterPredicate)).
		// OWNED: Watch resources owned by RayClusters
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.Service{}).
		Owns(&routev1.Route{}).
		// SECONDARY: Watch cluster-wide auth config and map to all RayClusters
		Watches(
			&configv1.Authentication{},
			handler.EnqueueRequestsFromMapFunc(r.mapAuthResourceToRayClusters),
		).
		Watches(
			&configv1.OAuth{},
			handler.EnqueueRequestsFromMapFunc(r.mapAuthResourceToRayClusters),
		).
		Named("authentication").
		Complete(r)
}

// mapAuthResourceToRayClusters maps cluster-wide auth config changes to all RayClusters
// This ensures all clusters are re-evaluated when authentication mode changes
func (r *AuthenticationController) mapAuthResourceToRayClusters(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := ctrl.LoggerFrom(ctx)

	// List all RayClusters in all namespaces
	rayClusterList := &rayv1.RayClusterList{}
	if err := r.List(ctx, rayClusterList); err != nil {
		logger.Error(err, "Failed to list RayClusters for authentication config mapping")
		return []reconcile.Request{}
	}

	// Create reconcile requests for all clusters
	requests := make([]reconcile.Request, 0)
	for _, cluster := range rayClusterList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			},
		})
	}

	logger.Info("Mapping authentication config change to RayClusters",
		"authResource", obj.GetName(),
		"clusters", len(requests))

	return requests
}
