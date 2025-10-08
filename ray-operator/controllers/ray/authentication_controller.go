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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

type AuthenticationMode string

const (
	ModeIntegratedOAuth AuthenticationMode = "IntegratedOAuth"
	ModeOIDC            AuthenticationMode = "OIDC"
	ModeNone            AuthenticationMode = "None"
)

// Backoff and retry constants for OIDC rollout coordination
const (
	// MediumRequeueDelay for ongoing rollouts
	MediumRequeueDelay = 10 * time.Second

	// OAuth proxy constants
	oauthProxyContainerName = "oauth-proxy"
	oauthProxyVolumeName    = "proxy-tls-secret"
	oauthProxyPort          = 8443
	oauthProxyPortName      = "oauth-proxy"
	oauthConfigVolumeName   = "oauth-config"

	// OAuth proxy image (OpenShift 4.x)
	oauthProxyImage = "registry.redhat.io/openshift4/ose-oauth-proxy:latest"
)

//
//// RolloutState represents the current state of an OIDC/OAuth rollout
//type RolloutState string
//
//const (
//	RolloutStateUnknown     RolloutState = "Unknown"
//	RolloutStateProgressing RolloutState = "Progressing"
//	RolloutStateCompleted   RolloutState = "Completed"
//	RolloutStateDegraded    RolloutState = "Degraded"
//	RolloutStateAvailable   RolloutState = "Available"
//)

// AuthenticationStatus represents the overall authentication configuration status
//type AuthenticationStatus struct {
//	OIDCRollout     RolloutState
//	OAuthRollout    RolloutState
//	OverallState    RolloutState
//	CookieSalt      string
//	ActiveProviders []string
//}

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

//type AuthenticationControllerReconcilerOptions struct {
//	IsOpenShift bool
//}

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

	// Detect the authentication mode configured in the cluster
	authMode := r.detectAuthenticationMode(ctx)

	logger.Info("Detected authentication mode", "mode", authMode)

	// Handle authentication based on detected mode
	switch authMode {
	case ModeIntegratedOAuth:
		logger.Info("Cluster is configured with Integrated OAuth")
		// Handle OAuth configuration for RayClusters
		if err := r.handleOAuthConfiguration(ctx, req, logger); err != nil {
			logger.Error(err, "Failed to handle OAuth configuration")
			return ctrl.Result{RequeueAfter: MediumRequeueDelay}, err
		}

	case ModeOIDC:
		logger.Info("Cluster is configured with OIDC")
		// Handle OIDC configuration
	case ModeNone:
		logger.Info("No authentication configured")
		// Handle no authentication
	}

	logger.Info("Successfully reconciled authentication resource")
	return ctrl.Result{RequeueAfter: MediumRequeueDelay}, nil
}

// detectAuthenticationMode determines whether the cluster is using OAuth or OIDC
func (r *AuthenticationController) detectAuthenticationMode(ctx context.Context) AuthenticationMode {
	// First, check if we're on OpenShift
	if !r.options.IsOpenShift {
		return ModeNone
	}

	// Check for OIDC configuration in the Authentication resource
	auth := &configv1.Authentication{}
	oidcConfigured := false
	if err := r.Get(ctx, client.ObjectKey{Name: "cluster"}, auth); err == nil {
		// Check if OIDC providers are configured
		if len(auth.Spec.OIDCProviders) > 0 {
			return ModeOIDC
		}
		oidcConfigured = true // Successfully retrieved auth resource, OIDC not configured
	}

	// Check for Integrated OAuth configuration in the OAuth resource
	oauth := &configv1.OAuth{}
	if err := r.Get(ctx, client.ObjectKey{Name: "cluster"}, oauth); err == nil {
		// Check if OAuth identity providers are configured
		if len(oauth.Spec.IdentityProviders) > 0 {
			return ModeIntegratedOAuth
		}
		// If OAuth spec exists but is empty/null, and OIDC is not configured,
		// assume OAuth is the default authentication method on OpenShift
		if oidcConfigured {
			return ModeIntegratedOAuth
		}
	}

	// No authentication configured
	return ModeNone
}

// handleOAuthConfiguration configures OAuth proxy for RayClusters
func (r *AuthenticationController) handleOAuthConfiguration(ctx context.Context, req ctrl.Request, logger logr.Logger) error {
	// Try to get RayCluster
	rayCluster := &rayv1.RayCluster{}
	if err := r.Get(ctx, req.NamespacedName, rayCluster); err != nil {
		// Not a RayCluster or doesn't exist, skip
		return client.IgnoreNotFound(err)
	}

	logger.Info("Handling OAuth configuration for RayCluster", "cluster", rayCluster.Name, "namespace", rayCluster.Namespace)

	// Check if OAuth annotation is present and enabled
	if !r.shouldEnableOAuth() {
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

	logger.Info("Enabling ingress for OAuth-secured cluster", "cluster", cluster.Name)

	// Create a copy and enable ingress
	updatedCluster := cluster.DeepCopy()
	trueValue := true
	updatedCluster.Spec.HeadGroupSpec.EnableIngress = &trueValue

	// Update the cluster
	if err := r.Update(ctx, updatedCluster); err != nil {
		return fmt.Errorf("failed to enable ingress: %w", err)
	}

	logger.Info("Successfully enabled ingress for cluster", "cluster", cluster.Name)
	r.Recorder.Event(cluster, "Normal", "IngressEnabled", "Automatically enabled ingress for OAuth configuration")
	return nil
}

// shouldEnableOAuth checks if OAuth should be enabled for the given RayCluster
func (r *AuthenticationController) shouldEnableOAuth() bool {
	// If ControlledNetworkEnvironment is enabled globally, apply OAuth to all clusters
	if r.options.ControlledNetworkEnvironment {
		return true
	}

	return false
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
	saName := getOAuthServiceAccountName(cluster)
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: cluster.Namespace,
		},
	}

	// Add service account annotation for OAuth
	sa.Annotations = map[string]string{
		"serviceaccounts.openshift.io/oauth-redirectreference.first": fmt.Sprintf(`{"kind":"OAuthRedirectReference","apiVersion":"v1","reference":{"kind":"Route","name":"%s"}}`, utils.GenerateRouteName(cluster.Name)),
	}

	err := r.Get(ctx, client.ObjectKey{Name: saName, Namespace: cluster.Namespace}, sa)
	if err != nil && errors.IsNotFound(err) {
		// Create new service account
		if err := controllerutil.SetControllerReference(cluster, sa, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, sa); err != nil {
			return err
		}
		logger.Info("Created OAuth service account", "name", saName)
	} else if err != nil {
		return err
	}

	return nil
}

// ensureOAuthSecret creates or updates the OAuth cookie secret
func (r *AuthenticationController) ensureOAuthSecret(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	secretName := getOAuthSecretName(cluster)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: cluster.Namespace,
		},
	}

	err := r.Get(ctx, client.ObjectKey{Name: secretName, Namespace: cluster.Namespace}, secret)
	if err != nil && errors.IsNotFound(err) {
		// Generate cookie secret
		hasher := sha256.New()
		hasher.Write([]byte(cluster.Name + cluster.Namespace + "oauth-salt"))
		cookieSecret := base64.StdEncoding.EncodeToString(hasher.Sum(nil))

		secret.StringData = map[string]string{
			"cookie_secret": cookieSecret,
		}

		if err := controllerutil.SetControllerReference(cluster, secret, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, secret); err != nil {
			return err
		}
		logger.Info("Created OAuth secret", "name", secretName)
	} else if err != nil {
		return err
	}

	return nil
}

// ensureOAuthTLSSecret creates or updates the TLS secret for OAuth proxy
func (r *AuthenticationController) ensureOAuthTLSSecret(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	secretName := getOAuthTLSSecretName(cluster)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: cluster.Namespace,
		},
	}

	err := r.Get(ctx, client.ObjectKey{Name: secretName, Namespace: cluster.Namespace}, secret)
	if err != nil && errors.IsNotFound(err) {
		// Generate self-signed certificate
		cert, key, err := generateSelfSignedCert(cluster)
		if err != nil {
			return fmt.Errorf("failed to generate certificate: %w", err)
		}

		secret.Type = corev1.SecretTypeTLS
		secret.Data = map[string][]byte{
			"tls.crt": cert,
			"tls.key": key,
		}

		// Add service serving cert annotation
		secret.Annotations = map[string]string{
			"service.beta.openshift.io/serving-cert-secret-name": secretName,
		}

		if err := controllerutil.SetControllerReference(cluster, secret, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, secret); err != nil {
			return err
		}
		logger.Info("Created OAuth TLS secret", "name", secretName)
	} else if err != nil {
		return err
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

	// Check if OAuth proxy port already exists
	oauthPortExists := false
	for _, port := range service.Spec.Ports {
		if port.Name == oauthProxyPortName {
			oauthPortExists = true
			break
		}
	}

	if !oauthPortExists {
		logger.Info("Adding OAuth proxy port to head service", "service", serviceName)

		// Add OAuth proxy port
		oauthPort := corev1.ServicePort{
			Name:     oauthProxyPortName,
			Port:     oauthProxyPort,
			Protocol: corev1.ProtocolTCP,
		}
		service.Spec.Ports = append(service.Spec.Ports, oauthPort)

		if err := r.Update(ctx, service); err != nil {
			return err
		}
		logger.Info("Added OAuth proxy port to head service", "service", serviceName, "port", oauthProxyPort)
	}

	return nil
}

// updateRouteForOAuth updates the RayCluster route to use OAuth proxy
func (r *AuthenticationController) updateRouteForOAuth(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	routeName := utils.GenerateRouteName(cluster.Name)
	route := &routev1.Route{}

	if err := r.Get(ctx, client.ObjectKey{Name: routeName, Namespace: cluster.Namespace}, route); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Route not found, will be created with OAuth proxy configuration", "route", routeName)
			return nil
		}
		return err
	}

	// Update route to use OAuth proxy port
	updated := false
	if route.Spec.Port == nil || route.Spec.Port.TargetPort.String() != oauthProxyPortName {
		route.Spec.Port = &routev1.RoutePort{
			TargetPort: intstr.FromString(oauthProxyPortName),
		}
		updated = true
	}

	// Ensure TLS is configured for re-encrypt
	if route.Spec.TLS == nil || route.Spec.TLS.Termination != routev1.TLSTerminationReencrypt {
		route.Spec.TLS = &routev1.TLSConfig{
			Termination:                   routev1.TLSTerminationPassthrough,
			InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
		}
		updated = true
	}

	if updated {
		if err := r.Update(ctx, route); err != nil {
			return err
		}
		logger.Info("Updated route for OAuth proxy", "route", routeName)
	}

	return nil
}

// cleanupOAuthResources removes OAuth resources when OAuth is disabled
func (r *AuthenticationController) cleanupOAuthResources(ctx context.Context, cluster *rayv1.RayCluster, logger logr.Logger) error {
	// Remove TLS secret
	tlsSecret := &corev1.Secret{}
	tlsSecretName := getOAuthTLSSecretName(cluster)
	if err := r.Get(ctx, client.ObjectKey{Name: tlsSecretName, Namespace: cluster.Namespace}, tlsSecret); err == nil {
		if err := r.Delete(ctx, tlsSecret); err != nil {
			logger.Info("Failed to delete TLS secret", "error", err)
		} else {
			logger.Info("Deleted OAuth TLS secret", "secret", tlsSecretName)
		}
	}

	// Remove OAuth secret
	oauthSecret := &corev1.Secret{}
	oauthSecretName := getOAuthSecretName(cluster)
	if err := r.Get(ctx, client.ObjectKey{Name: oauthSecretName, Namespace: cluster.Namespace}, oauthSecret); err == nil {
		if err := r.Delete(ctx, oauthSecret); err != nil {
			logger.Info("Failed to delete OAuth secret", "error", err)
		} else {
			logger.Info("Deleted OAuth secret", "secret", oauthSecretName)
		}
	}

	// Remove service account
	sa := &corev1.ServiceAccount{}
	saName := getOAuthServiceAccountName(cluster)
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

// Helper functions for resource naming
func getOAuthServiceAccountName(cluster *rayv1.RayCluster) string {
	return cluster.Name + "-oauth-proxy-sa"
}

func getOAuthSecretName(cluster *rayv1.RayCluster) string {
	return cluster.Name + "-oauth-config"
}

func getOAuthTLSSecretName(cluster *rayv1.RayCluster) string {
	return cluster.Name + "-oauth-tls"
}

// GetOAuthProxySidecar returns the OAuth proxy sidecar container configuration
// This can be used by the RayCluster controller to inject the sidecar
func GetOAuthProxySidecar(cluster *rayv1.RayCluster) corev1.Container {
	return corev1.Container{
		Name:            oauthProxyContainerName,
		Image:           oauthProxyImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: oauthProxyPort,
				Name:          oauthProxyPortName,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Args: []string{
			fmt.Sprintf("--https-address=:%d", oauthProxyPort),
			"--provider=openshift",
			fmt.Sprintf("--openshift-service-account=%s", getOAuthServiceAccountName(cluster)),
			"--upstream=http://localhost:8265",
			"--tls-cert=/etc/tls/private/tls.crt",
			"--tls-key=/etc/tls/private/tls.key",
			"--cookie-secret=$(COOKIE_SECRET)",
			fmt.Sprintf("--openshift-delegate-urls={\"/\":{\"resource\":\"pods\",\"namespace\":\"%s\",\"verb\":\"get\"}}", cluster.Namespace),
			"--skip-provider-button",
		},
		Env: []corev1.EnvVar{
			{
				Name: "COOKIE_SECRET",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: getOAuthSecretName(cluster),
						},
						Key: "cookie_secret",
					},
				},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      oauthProxyVolumeName,
				MountPath: "/etc/tls/private",
				ReadOnly:  true,
			},
		},
		// Add resource limits to prevent excessive resource usage
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("20Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
		},
		// Add liveness probe to detect if OAuth proxy is healthy
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/oauth/healthz",
					Port:   intstr.FromInt(int(oauthProxyPort)),
					Scheme: corev1.URISchemeHTTPS,
				},
			},
			InitialDelaySeconds: 30,
			TimeoutSeconds:      1,
			PeriodSeconds:       10,
			SuccessThreshold:    1,
			FailureThreshold:    3,
		},
		// Add readiness probe to prevent routing traffic before OAuth proxy is ready
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/oauth/healthz",
					Port:   intstr.FromInt(int(oauthProxyPort)),
					Scheme: corev1.URISchemeHTTPS,
				},
			},
			InitialDelaySeconds: 5,
			TimeoutSeconds:      1,
			PeriodSeconds:       5,
			SuccessThreshold:    1,
			FailureThreshold:    3,
		},
	}
}

// GetOAuthProxyVolumes returns the volumes needed for OAuth proxy sidecar
func GetOAuthProxyVolumes(cluster *rayv1.RayCluster) []corev1.Volume {
	return []corev1.Volume{
		{
			Name: oauthConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: getOAuthSecretName(cluster),
				},
			},
		},
		{
			Name: oauthProxyVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: getOAuthTLSSecretName(cluster),
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager
func (r *AuthenticationController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// PRIMARY: Watch RayClusters for OAuth annotation changes
		For(&rayv1.RayCluster{}).
		// Watch authentication resources for informational purposes
		Watches(&configv1.Authentication{}, &handler.EnqueueRequestForObject{}).
		// Watches(&routev1.Route{}, &handler.EnqueueRequestForObject{}).
		// Watches(&operatorv1.KubeAPIServer{}, &handler.EnqueueRequestForObject{}).
		Watches(&configv1.OAuth{}, &handler.EnqueueRequestForObject{}).
		Named("authentication").
		Complete(r)
}
