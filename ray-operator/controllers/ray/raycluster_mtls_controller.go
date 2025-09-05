package ray

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	configapi "github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
	corev1 "k8s.io/api/core/v1"
)

const (
	caSecretName          = "ray-ca-secret"
	caCertKey             = "ca.crt"
	caKeyKey              = "ca.key"
	caIssuerName          = "ray-ca-issuer"
	caCertificateName     = "ray-ca-certificate"
	rayHeadCertName       = "ray-head-cert"
	rayHeadSecretName     = "ray-head-secret"
	rayInternalIssuerName = "ray-internal-issuer"

	// Worker node certificate constants
	rayWorkerCertName   = "ray-worker-cert"
	rayWorkerSecretName = "ray-worker-secret"

	RayClusterMTLSDefaultRequeueDuration = 30 * time.Second
	RayClusterMTLSWaitForIPsDuration     = 10 * time.Second
	RayClusterMTLSPeriodicCheckDuration  = 5 * time.Minute
)

// RayClusterMTLSController manages CA certificates for MTLS-enabled Ray clusters using cert-manager
// +kubebuilder:rbac:groups=cert-manager.io,resources=issuers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
type RayClusterMTLSController struct {
	client.Client
	Scheme *runtime.Scheme
	Config *configapi.Configuration
}

// NewRayClusterMTLSController creates a new MTLS controller instance
func NewRayClusterMTLSController(client client.Client, scheme *runtime.Scheme, config *configapi.Configuration) *RayClusterMTLSController {
	return &RayClusterMTLSController{
		Client: client,
		Scheme: scheme,
		Config: config,
	}
}

// Reconcile handles the reconciliation of CA certificates using cert-manager
func (r *RayClusterMTLSController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !features.Enabled(features.MTLS) {
		return ctrl.Result{}, nil
	}

	// Get the RayCluster instance
	instance := &rayv1.RayCluster{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			// RayCluster not found, clean up related resources
			return r.cleanupMTLSResources(ctx, req.Namespace, req.Name)
		}
		logger.Error(err, "Failed to get RayCluster")
		return ctrl.Result{}, err
	}

	// Reconcile CA issuer first
	if err := r.reconcileCAIssuer(ctx, instance); err != nil {
		logger.Error(err, "Failed to reconcile CA issuer")
		return ctrl.Result{RequeueAfter: RayClusterMTLSDefaultRequeueDuration}, err
	}

	// Wait for pod IPs to be available before creating certificates
	podIPs, err := r.getPodIPs(ctx, instance)
	if err != nil {
		logger.Error(err, "Failed to get pod IPs")
		return ctrl.Result{RequeueAfter: RayClusterMTLSDefaultRequeueDuration}, err
	}

	// Check if we have enough pod IPs (at least head pod)
	if len(podIPs) == 0 {
		logger.Info("Waiting for pod IPs to be available", "rayCluster", instance.Name)
		return ctrl.Result{RequeueAfter: RayClusterMTLSWaitForIPsDuration}, nil
	}

	// Check if certificates need to be created or updated with new pod IPs
	headCertName := fmt.Sprintf("%s-%s", rayHeadCertName, instance.Name)
	workerCertName := fmt.Sprintf("%s-%s", rayWorkerCertName, instance.Name)

	// Check if certificates exist
	headCertExists := r.certificateExists(ctx, instance.Namespace, headCertName)
	workerCertExists := r.certificateExists(ctx, instance.Namespace, workerCertName)

	// Create or update certificates with current pod IPs
	if !headCertExists {
		if err := r.createRayHeadCertificate(ctx, instance, podIPs); err != nil {
			logger.Error(err, "Failed to create Ray head certificate")
			return ctrl.Result{RequeueAfter: RayClusterMTLSDefaultRequeueDuration}, err
		}
	} else {
		// Update existing certificate if pod IPs have changed
		if err := r.updateCertificateWithPodIPs(ctx, instance, headCertName, podIPs); err != nil {
			logger.Error(err, "Failed to update head certificate with pod IPs")
			return ctrl.Result{RequeueAfter: RayClusterMTLSDefaultRequeueDuration}, err
		}
	}

	if !workerCertExists {
		if err := r.createRayWorkerCertificate(ctx, instance, podIPs); err != nil {
			logger.Error(err, "Failed to create Ray worker certificate")
			return ctrl.Result{RequeueAfter: RayClusterMTLSDefaultRequeueDuration}, err
		}
	} else {
		// Update existing certificate if pod IPs have changed
		if err := r.updateCertificateWithPodIPs(ctx, instance, workerCertName, podIPs); err != nil {
			logger.Error(err, "Failed to update worker certificate with pod IPs")
			return ctrl.Result{RequeueAfter: RayClusterMTLSDefaultRequeueDuration}, err
		}
	}

	// Check if all certificates are ready
	if ready, err := r.checkCertificatesReady(ctx, instance); err != nil {
		logger.Error(err, "Failed to check certificate readiness")
		return ctrl.Result{RequeueAfter: RayClusterMTLSDefaultRequeueDuration}, err
	} else if !ready {
		logger.Info("One or more certificates are not ready, requeuing")
		return ctrl.Result{RequeueAfter: RayClusterMTLSDefaultRequeueDuration}, nil
	}

	// Check certificate expiry
	if err := r.checkCertificateExpiry(ctx, instance); err != nil {
		logger.Error(err, "Failed to check certificate expiry")
		return ctrl.Result{RequeueAfter: RayClusterMTLSDefaultRequeueDuration}, err
	}

	logger.Info("All mTLS certificates are ready", "rayCluster", instance.Name, "podIPs", podIPs)
	return ctrl.Result{RequeueAfter: RayClusterMTLSPeriodicCheckDuration}, nil
}

// getPodIPs collects all pod IPs for the RayCluster
func (r *RayClusterMTLSController) getPodIPs(ctx context.Context, instance *rayv1.RayCluster) ([]string, error) {
	logger := log.FromContext(ctx)
	var podIPs []string

	// Get all pods for this RayCluster
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(instance.Namespace),
		client.MatchingLabels(map[string]string{
			"ray.io/cluster": instance.Name,
		})); err != nil {
		return nil, err
	}

	// Collect pod IPs
	for _, pod := range pods.Items {
		if pod.Status.PodIP != "" {
			podIPs = append(podIPs, pod.Status.PodIP)
			logger.Info("Found pod IP", "pod", pod.Name, "ip", pod.Status.PodIP)
		}
	}

	return podIPs, nil
}

// certificateExists checks if a certificate exists
func (r *RayClusterMTLSController) certificateExists(ctx context.Context, namespace, certName string) bool {
	cert := &certmanagerv1.Certificate{}
	err := r.Get(ctx, client.ObjectKey{Name: certName, Namespace: namespace}, cert)
	return err == nil
}

// shouldUpdateCertificate checks if certificate needs updating with new pod IPs
func (r *RayClusterMTLSController) shouldUpdateCertificate(cert *certmanagerv1.Certificate, currentPodIPs []string) (bool, error) {
	// Check if all current pod IPs are in the certificate
	for _, ip := range currentPodIPs {
		found := false
		for _, dnsName := range cert.Spec.DNSNames {
			if dnsName == ip {
				found = true
				break
			}
		}
		if !found {
			return true, nil // Need to update
		}
	}

	return false, nil
}

// updateCertificateWithPodIPs updates existing certificate with new pod IPs
func (r *RayClusterMTLSController) updateCertificateWithPodIPs(ctx context.Context, instance *rayv1.RayCluster, certName string, podIPs []string) error {
	logger := log.FromContext(ctx)

	// Get the certificate
	cert := &certmanagerv1.Certificate{}
	if err := r.Get(ctx, client.ObjectKey{Name: certName, Namespace: instance.Namespace}, cert); err != nil {
		return err
	}

	// Check if update is needed
	needsUpdate, err := r.shouldUpdateCertificate(cert, podIPs)
	if err != nil {
		return err
	}

	if !needsUpdate {
		return nil
	}

	// Build new DNS names
	newDNSNames := []string{}

	// Keep existing service names (non-IP entries)
	for _, dnsName := range cert.Spec.DNSNames {
		if !strings.Contains(dnsName, ".") && !strings.Contains(dnsName, ":") {
			// This is likely a service name, keep it
			newDNSNames = append(newDNSNames, dnsName)
		}
	}

	// Add service names based on certificate type
	if strings.Contains(certName, rayHeadCertName) {
		newDNSNames = append(newDNSNames,
			"ray-head-svc",
			fmt.Sprintf("ray-head-svc.%s.svc", instance.Namespace),
			fmt.Sprintf("ray-head-svc.%s.svc.cluster.local", instance.Namespace),
		)
	} else {
		// Worker certificate
		newDNSNames = append(newDNSNames,
			"ray-worker-svc",
			fmt.Sprintf("ray-worker-svc.%s.svc", instance.Namespace),
			fmt.Sprintf("ray-worker-svc.%s.svc.cluster.local", instance.Namespace),
			fmt.Sprintf("*.ray-worker-svc.%s.svc", instance.Namespace),
			fmt.Sprintf("*.ray-worker-svc.%s.svc.cluster.local", instance.Namespace),
			fmt.Sprintf("*-worker-*.%s.svc", instance.Namespace),
			fmt.Sprintf("*-worker-*.%s.svc.cluster.local", instance.Namespace),
		)
	}

	// Update certificate
	cert.Spec.DNSNames = newDNSNames
	cert.Spec.IPAddresses = podIPs
	cert.Spec.IPAddresses = append(cert.Spec.IPAddresses, "127.0.0.1")

	if err := r.Update(ctx, cert); err != nil {
		logger.Error(err, "Failed to update certificate with pod IPs")
		return err
	}

	logger.Info("Updated certificate with pod IPs", "certificate", certName, "podIPs", podIPs)
	return nil
}

// reconcileCAIssuer reconciles the CA issuer for the RayCluster
func (r *RayClusterMTLSController) reconcileCAIssuer(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := log.FromContext(ctx)

	caIssuer := &certmanagerv1.Issuer{}
	err := r.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-%s", caIssuerName, instance.Name), Namespace: instance.Namespace}, caIssuer)

	if err == nil {
		// CA issuer exists, do nothing
		return nil
	} else if errors.IsNotFound(err) {
		// CA issuer does not exist, create it
		logger.Info("Creating CA issuer for RayCluster", "rayCluster", instance.Name)
		return r.createCAIssuer(ctx, instance)
	}
	return err
}

// createCAIssuer creates a new self-signed CA issuer
func (r *RayClusterMTLSController) createCAIssuer(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := log.FromContext(ctx)

	issuer := &certmanagerv1.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", caIssuerName, instance.Name),
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "ray-mtls-ca",
				"app.kubernetes.io/component": "ca-issuer",
				"ray.io/cluster-name":         instance.Name,
			},
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				SelfSigned: &certmanagerv1.SelfSignedIssuer{},
			},
		},
	}

	// Set owner reference
	if err := ctrl.SetControllerReference(instance, issuer, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, issuer); err != nil {
		logger.Error(err, "Failed to create CA issuer")
		return err
	}

	logger.Info("CA issuer created successfully", "rayCluster", instance.Name)
	return nil
}

// createRayHeadCertificate creates a new Ray head service certificate using cert-manager
func (r *RayClusterMTLSController) createRayHeadCertificate(ctx context.Context, instance *rayv1.RayCluster, podIPs []string) error {
	logger := log.FromContext(ctx)

	// Build DNS names
	dnsNames := []string{
		"raycluster-kuberay-head-svc.default.svc.cluster.local",
		"raycluster-kuberay-head-svc",
	}

	certificate := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", rayHeadCertName, instance.Name),
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "ray-mtls",
				"app.kubernetes.io/component": "head-certificate",
				"ray.io/cluster-name":         instance.Name,
			},
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: fmt.Sprintf("%s-%s", rayHeadSecretName, instance.Name),
			CommonName: "raycluster-kuberay-head-svc",
			Duration: &metav1.Duration{
				Duration: 2160 * time.Hour, // 90 days
			},
			RenewBefore: &metav1.Duration{
				Duration: 360 * time.Hour, // 15 days
			},
			DNSNames:    dnsNames,
			IPAddresses: podIPs,
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageDigitalSignature,
				certmanagerv1.UsageKeyEncipherment,
				certmanagerv1.UsageServerAuth,
				certmanagerv1.UsageClientAuth,
			},
			IssuerRef: cmmeta.ObjectReference{
				Name:  fmt.Sprintf("%s-%s", caIssuerName, instance.Name),
				Kind:  "Issuer",
				Group: "cert-manager.io",
			},
		},
	}

	// Set owner reference
	if err := ctrl.SetControllerReference(instance, certificate, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, certificate); err != nil {
		logger.Error(err, "Failed to create Ray head certificate")
		return err
	}

	logger.Info("Ray head certificate created successfully", "rayCluster", instance.Name, "podIPs", podIPs)
	return nil
}

// createRayWorkerCertificate creates a new Ray worker service certificate using cert-manager
func (r *RayClusterMTLSController) createRayWorkerCertificate(ctx context.Context, instance *rayv1.RayCluster, podIPs []string) error {
	logger := log.FromContext(ctx)

	// Build DNS names including pod IPs
	dnsNames := []string{
		"ray-worker-svc",
		"*.svc:",
	}

	// Add DNS names for each worker group
	for _, workerGroup := range instance.Spec.WorkerGroupSpecs {
		groupDNSNames := []string{
			fmt.Sprintf("%s-%s", instance.Name, workerGroup.GroupName),
			fmt.Sprintf("%s-%s.%s.svc", instance.Name, workerGroup.GroupName, instance.Namespace),
			fmt.Sprintf("%s-%s.%s.svc.cluster.local", instance.Name, workerGroup.GroupName, instance.Namespace),
		}
		dnsNames = append(dnsNames, groupDNSNames...)
	}

	// Add wildcard patterns for dynamic worker services
	dnsNames = append(dnsNames,
		fmt.Sprintf("*.ray-worker-svc.%s.svc", instance.Namespace),
		fmt.Sprintf("*.ray-worker-svc.%s.svc.cluster.local", instance.Namespace),
		fmt.Sprintf("*-worker-*.%s.svc", instance.Namespace),
		fmt.Sprintf("*-worker-*.%s.svc.cluster.local", instance.Namespace),
	)

	certificate := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", rayWorkerCertName, instance.Name),
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "ray-mtls",
				"app.kubernetes.io/component": "worker-certificate",
				"ray.io/cluster-name":         instance.Name,
			},
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: fmt.Sprintf("%s-%s", rayWorkerSecretName, instance.Name),
			CommonName: "ray-worker-svc",
			Duration: &metav1.Duration{
				Duration: 2160 * time.Hour, // 90 days
			},
			RenewBefore: &metav1.Duration{
				Duration: 360 * time.Hour, // 15 days
			},
			DNSNames:    dnsNames,
			IPAddresses: podIPs,
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageDigitalSignature,
				certmanagerv1.UsageKeyEncipherment,
				certmanagerv1.UsageServerAuth,
				certmanagerv1.UsageClientAuth,
			},
			IssuerRef: cmmeta.ObjectReference{
				Name:  fmt.Sprintf("%s-%s", caIssuerName, instance.Name),
				Kind:  "Issuer",
				Group: "cert-manager.io",
			},
		},
	}

	// Set owner reference
	if err := ctrl.SetControllerReference(instance, certificate, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, certificate); err != nil {
		logger.Error(err, "Failed to create Ray worker certificate")
		return err
	}

	logger.Info("Ray worker certificate created successfully", "rayCluster", instance.Name, "podIPs", podIPs)
	return nil
}

// checkCertificatesReady checks if all certificates are ready
func (r *RayClusterMTLSController) checkCertificatesReady(ctx context.Context, instance *rayv1.RayCluster) (bool, error) {
	// Check Ray head certificate
	rayHeadCertificate := &certmanagerv1.Certificate{}
	err := r.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-%s", rayHeadCertName, instance.Name), Namespace: instance.Namespace}, rayHeadCertificate)
	if err != nil {
		return false, err
	}

	// Check Ray worker certificate
	rayWorkerCertificate := &certmanagerv1.Certificate{}
	err = r.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-%s", rayWorkerCertName, instance.Name), Namespace: instance.Namespace}, rayWorkerCertificate)
	if err != nil {
		return false, err
	}

	return r.isCertificateReady(rayHeadCertificate) && r.isCertificateReady(rayWorkerCertificate), nil
}

// checkCertificateExpiry checks if certificates are close to expiry and logs warnings
func (r *RayClusterMTLSController) checkCertificateExpiry(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := log.FromContext(ctx)

	// Check head certificate expiry
	headCert := &certmanagerv1.Certificate{}
	if err := r.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-%s", rayHeadCertName, instance.Name), Namespace: instance.Namespace}, headCert); err == nil {
		if headCert.Status.NotAfter != nil {
			timeUntilExpiry := time.Until(headCert.Status.NotAfter.Time)
			if timeUntilExpiry < 24*time.Hour {
				logger.Info("Head certificate expires soon", "expiry", headCert.Status.NotAfter.Time, "timeUntilExpiry", timeUntilExpiry)
			}
		}
	}

	// Check worker certificate expiry
	workerCert := &certmanagerv1.Certificate{}
	if err := r.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-%s", rayWorkerCertName, instance.Name), Namespace: instance.Namespace}, workerCert); err == nil {
		if workerCert.Status.NotAfter != nil {
			timeUntilExpiry := time.Until(workerCert.Status.NotAfter.Time)
			if timeUntilExpiry < 24*time.Hour {
				logger.Info("Worker certificate expires soon", "expiry", workerCert.Status.NotAfter.Time, "timeUntilExpiry", timeUntilExpiry)
			}
		}
	}

	return nil
}

// isCertificateReady checks if the certificate is ready and valid
func (r *RayClusterMTLSController) isCertificateReady(cert *certmanagerv1.Certificate) bool {
	for _, condition := range cert.Status.Conditions {
		if condition.Type == certmanagerv1.CertificateConditionReady {
			return condition.Status == "True"
		}
	}
	return false
}

// GetCACertificate retrieves the CA certificate from the specified namespace
func (r *RayClusterMTLSController) GetCACertificate(ctx context.Context, namespace string) (*corev1.Secret, error) {
	caSecret := &corev1.Secret{}

	err := r.Get(ctx, client.ObjectKey{Name: caSecretName, Namespace: namespace}, caSecret)
	if err != nil {
		return nil, err
	}

	return caSecret, nil
}

// GetCAIssuer retrieves the CA issuer from the specified namespace
func (r *RayClusterMTLSController) GetCAIssuer(ctx context.Context, namespace string) (*certmanagerv1.Issuer, error) {
	caIssuer := &certmanagerv1.Issuer{}

	err := r.Get(ctx, client.ObjectKey{Name: caIssuerName, Namespace: namespace}, caIssuer)
	if err != nil {
		return nil, err
	}

	return caIssuer, nil
}

// SetupWithManager sets up the controller with the manager
func (r *RayClusterMTLSController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("raycluster-mtls").
		For(&rayv1.RayCluster{}).
		Complete(r)
}
