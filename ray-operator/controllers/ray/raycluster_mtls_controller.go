package ray

import (
	"context"
	"fmt"
	"strings"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	configapi "github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
)

const (
	caSecretName            = "ray-ca-secret"
	caIssuerName            = "ray-ca-issuer"
	raySelfSignedIssuerName = "ray-selfsigned-issuer"
	caCertificateName       = "ray-ca-certificate"
	rayHeadCertName         = "ray-head-cert"
	// #nosec G101 -- this is a Kubernetes Secret resource name, not a credential
	rayHeadSecretName = "ray-head-secret"

	// Worker node certificate constants
	rayWorkerCertName = "ray-worker-cert"
	// #nosec G101 -- this is a Kubernetes Secret resource name, not a credential
	rayWorkerSecretName = "ray-worker-secret"

	RayClusterMTLSDefaultRequeueDuration = 30 * time.Second
	RayClusterMTLSPeriodicCheckDuration  = 5 * time.Minute
)

// toSet converts a slice of strings into a set for order-insensitive comparisons
func toSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, v := range values {
		result[v] = struct{}{}
	}
	return result
}

// setsEqual compares two string slices as sets (order-insensitive)
func setsEqual(a, b []string) bool {
	as := toSet(a)
	bs := toSet(b)
	if len(as) != len(bs) {
		return false
	}
	for k := range as {
		if _, ok := bs[k]; !ok {
			return false
		}
	}
	return true
}

// uniqueStrings de-duplicates while preserving order
func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// normalizeIPs returns a unique set of IPs plus 127.0.0.1, preserving order
func normalizeIPs(podIPs []string) []string {
	seen := make(map[string]struct{}, len(podIPs)+1)
	out := make([]string, 0, len(podIPs)+1)
	for _, ip := range podIPs {
		if ip == "" {
			continue
		}
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		out = append(out, ip)
	}
	if _, ok := seen["127.0.0.1"]; !ok {
		out = append(out, "127.0.0.1")
	}
	return out
}

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

	// Reconcile RootCA
	if err := r.reconcileRootCA(ctx, instance); err != nil {
		logger.Error(err, "Failed to reconcile RootCA")
		return ctrl.Result{RequeueAfter: RayClusterMTLSDefaultRequeueDuration}, err
	}

	// Reconcile CA issuer (uses the CA secret created by the CA certificate)
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

func (r *RayClusterMTLSController) cleanupMTLSResources(ctx context.Context, namespace, clusterName string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Cleaning up MTLS resources for deleted RayCluster", "namespace", namespace, "clusterName", clusterName)

	// List of resources to clean up
	var errors []error

	// Clean up CA Issuer
	caIssuerName := fmt.Sprintf("%s-%s", caIssuerName, clusterName)
	if err := r.deleteIssuer(ctx, namespace, caIssuerName); err != nil {
		logger.Error(err, "Failed to delete CA issuer", "issuer", caIssuerName)
		errors = append(errors, err)
	}

	// Clean up SelfSigned Issuer
	selfSignedIssuerName := fmt.Sprintf("%s-%s", raySelfSignedIssuerName, clusterName)
	if err := r.deleteIssuer(ctx, namespace, selfSignedIssuerName); err != nil {
		logger.Error(err, "Failed to delete SelfSigned issuer", "issuer", selfSignedIssuerName)
		errors = append(errors, err)
	}

	// Clean up Ray head certificate
	headCertName := fmt.Sprintf("%s-%s", rayHeadCertName, clusterName)
	if err := r.deleteCertificate(ctx, namespace, headCertName); err != nil {
		logger.Error(err, "Failed to delete head certificate", "certificate", headCertName)
		errors = append(errors, err)
	}

	// Clean up Ray worker certificate
	workerCertName := fmt.Sprintf("%s-%s", rayWorkerCertName, clusterName)
	if err := r.deleteCertificate(ctx, namespace, workerCertName); err != nil {
		logger.Error(err, "Failed to delete worker certificate", "certificate", workerCertName)
		errors = append(errors, err)
	}

	// Clean up associated secrets (they should be cleaned up automatically by cert-manager, but let's be explicit)
	headSecretName := fmt.Sprintf("%s-%s", rayHeadSecretName, clusterName)
	if err := r.deleteSecret(ctx, namespace, headSecretName); err != nil {
		logger.Error(err, "Failed to delete head secret", "secret", headSecretName)
		errors = append(errors, err)
	}

	workerSecretName := fmt.Sprintf("%s-%s", rayWorkerSecretName, clusterName)
	if err := r.deleteSecret(ctx, namespace, workerSecretName); err != nil {
		logger.Error(err, "Failed to delete worker secret", "secret", workerSecretName)
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		logger.Info("Some cleanup operations failed, will retry", "errorCount", len(errors))
		return ctrl.Result{RequeueAfter: RayClusterMTLSDefaultRequeueDuration}, errors[0]
	}

	logger.Info("Successfully cleaned up MTLS resources", "namespace", namespace, "clusterName", clusterName)
	return ctrl.Result{}, nil
}

// deleteIssuer deletes an issuer if it exists
func (r *RayClusterMTLSController) deleteIssuer(ctx context.Context, namespace, name string) error {
	issuer := &certmanagerv1.Issuer{}
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, issuer)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil // Already deleted
		}
		return err
	}
	return r.Delete(ctx, issuer)
}

// deleteCertificate deletes a certificate if it exists
func (r *RayClusterMTLSController) deleteCertificate(ctx context.Context, namespace, name string) error {
	cert := &certmanagerv1.Certificate{}
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, cert)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil // Already deleted
		}
		return err
	}
	return r.Delete(ctx, cert)
}

// deleteSecret deletes a secret if it exists
func (r *RayClusterMTLSController) deleteSecret(ctx context.Context, namespace, name string) error {
	secret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, secret)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil // Already deleted
		}
		return err
	}
	return r.Delete(ctx, secret)
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
func (r *RayClusterMTLSController) shouldUpdateCertificate(cert *certmanagerv1.Certificate, currentPodIPs []string) bool {
	desired := normalizeIPs(currentPodIPs)
	current := append([]string(nil), cert.Spec.IPAddresses...)
	return !setsEqual(current, desired)
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
	needsUpdate := r.shouldUpdateCertificate(cert, podIPs)
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
		headSvcName := fmt.Sprintf("%s-head-svc", instance.Name)
		newDNSNames = append(newDNSNames,
			headSvcName,
			"localhost",
			fmt.Sprintf("%s.%s.svc", headSvcName, instance.Namespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", headSvcName, instance.Namespace),
		)
	} else {
		// Worker certificate
		workerSvcName := fmt.Sprintf("%s-worker-svc", instance.Name)
		newDNSNames = append(newDNSNames,
			workerSvcName,
			"localhost",
			fmt.Sprintf("%s.%s.svc", workerSvcName, instance.Namespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", workerSvcName, instance.Namespace),
			fmt.Sprintf("*.%s.%s.svc", workerSvcName, instance.Namespace),
			fmt.Sprintf("*.%s.%s.svc.cluster.local", workerSvcName, instance.Namespace),
			fmt.Sprintf("*-worker-*.%s.svc", instance.Namespace),
			fmt.Sprintf("*-worker-*.%s.svc.cluster.local", instance.Namespace),
		)
	}

	// Update certificate
	desiredDNS := uniqueStrings(newDNSNames)
	desiredIPs := normalizeIPs(podIPs)

	changed := false
	if !setsEqual(cert.Spec.DNSNames, desiredDNS) {
		cert.Spec.DNSNames = desiredDNS
		changed = true
	}
	if !setsEqual(cert.Spec.IPAddresses, desiredIPs) {
		cert.Spec.IPAddresses = desiredIPs
		changed = true
	}

	if !changed {
		return nil
	}

	if err := r.Update(ctx, cert); err != nil {
		logger.Error(err, "Failed to update certificate SANs")
		return err
	}
	logger.Info("Updated certificate SANs", "certificate", certName, "dnsNames", desiredDNS, "ipAddresses", desiredIPs)
	return nil
}

// reconcileRootCA reconciles the RootCA for the RayCluster
func (r *RayClusterMTLSController) reconcileRootCA(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := log.FromContext(ctx)
	caCertificate := &certmanagerv1.Certificate{}
	err := r.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("%s-%s", caCertificateName, instance.Name), Namespace: instance.Namespace}, caCertificate)
	if err == nil {
		// CA certificate exists, do nothing
		return nil
	} else if errors.IsNotFound(err) {
		// CA certificate does not exist, create it
		logger.Info("Creating CA certificate for RayCluster", "rayCluster", instance.Name)
		return r.createCACertificate(ctx, instance)
	}
	return err
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

// createCAIssuer creates a new CA issuer backed by the generated CA secret
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
				CA: &certmanagerv1.CAIssuer{
					SecretName: caSecretName,
				},
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
	headSvcName := fmt.Sprintf("%s-head-svc", instance.Name)

	// Build DNS names
	dnsNames := []string{
		headSvcName,
		fmt.Sprintf("%s.%s.svc", headSvcName, instance.Namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", headSvcName, instance.Namespace),
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

	workerSvcName := fmt.Sprintf("%s-worker-svc", instance.Name)

	// Build DNS names including pod IPs
	dnsNames := []string{
		workerSvcName,
		"localhost",
		fmt.Sprintf("%s.%s.svc", workerSvcName, instance.Namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", workerSvcName, instance.Namespace),
	}

	// Add DNS names for each worker group
	for _, workerGroup := range instance.Spec.WorkerGroupSpecs {
		groupDNSNames := []string{
			"localhost",
			fmt.Sprintf("%s-%s", instance.Name, workerGroup.GroupName),
			fmt.Sprintf("%s-%s.%s.svc", instance.Name, workerGroup.GroupName, instance.Namespace),
			fmt.Sprintf("%s-%s.%s.svc.cluster.local", instance.Name, workerGroup.GroupName, instance.Namespace),
		}
		dnsNames = append(dnsNames, groupDNSNames...)
	}

	// Add wildcard patterns for dynamic worker services
	dnsNames = append(dnsNames,
		"localhost",
		fmt.Sprintf("*.%s.%s.svc", workerSvcName, instance.Namespace),
		fmt.Sprintf("*.%s.%s.svc.cluster.local", workerSvcName, instance.Namespace),
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
//
//nolint:unparam // keeping error return for possible future use
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

func (r *RayClusterMTLSController) createCACertificate(ctx context.Context, instance *rayv1.RayCluster) error {
	logger := log.FromContext(ctx)

	caCert := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", caCertificateName, instance.Name),
			Namespace: instance.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "ray-mtls-ca",
				"app.kubernetes.io/component": "ca-certificate",
				"ray.io/cluster-name":         instance.Name,
			},
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: caSecretName, // "ray-ca-secret"
			IsCA:       true,
			CommonName: fmt.Sprintf("%s-%s", "ray-root-ca", instance.Name),
			PrivateKey: &certmanagerv1.CertificatePrivateKey{
				Algorithm: certmanagerv1.RSAKeyAlgorithm,
				Size:      4096,
			},
			Duration: &metav1.Duration{
				Duration: 24 * time.Hour * 3650, // ~10 years
			},
			RenewBefore: &metav1.Duration{
				Duration: 24 * time.Hour * 30, // renew 30 days before expiry
			},
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageCertSign,
				certmanagerv1.UsageCRLSign,
			},
			IssuerRef: cmmeta.ObjectReference{
				// Bootstrap with the SelfSigned issuer
				Name:  fmt.Sprintf("%s-%s", raySelfSignedIssuerName, instance.Name),
				Kind:  "Issuer",
				Group: "cert-manager.io",
			},
		},
	}

	if err := ctrl.SetControllerReference(instance, caCert, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, caCert); err != nil {
		logger.Error(err, "Failed to create CA Certificate")
		return err
	}
	logger.Info("CA Certificate created successfully", "rayCluster", instance.Name, "secret", caSecretName)
	return nil
}
