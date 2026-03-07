package ray

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

const (
	// Requeue intervals aligned with ODH mTLS controller.
	mtlsDefaultRequeueDuration = 30 * time.Second
	mtlsPeriodicCheckDuration  = 1 * time.Minute

	// mtlsFinalizer prevents the RayCluster from being deleted before the mTLS
	// controller has cleaned up auto-generated cert-manager secrets.
	mtlsFinalizer = "ray.io/mtls-cleanup"
)

// RayClusterMTLSController manages mTLS certificates for RayClusters using cert-manager.
// Runs as a separate controller from the main RayClusterReconciler so that certificate
// creation happens independently of pod lifecycle, avoiding the SAN race condition.
type RayClusterMTLSController struct {
	client.Client
	Scheme   *k8sruntime.Scheme
	Recorder record.EventRecorder
}

// NewRayClusterMTLSController creates a new mTLS controller instance.
func NewRayClusterMTLSController(mgr ctrl.Manager) *RayClusterMTLSController {
	return &RayClusterMTLSController{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("raycluster-mtls-controller"),
	}
}

// +kubebuilder:rbac:groups=ray.io,resources=rayclusters,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=ray.io,resources=rayclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=cert-manager.io,resources=issuers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

// Reconcile handles mTLS certificate lifecycle for a RayCluster.
// Cert-manager Issuers and Certificates are owned by the RayCluster via controller
// references, so Kubernetes garbage collection handles their deletion automatically.
// Secrets are cleaned up explicitly because cert-manager may not set owner references
// on them (depends on the --enable-certificate-owner-ref flag).
func (r *RayClusterMTLSController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	instance := &rayv1.RayCluster{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion first, regardless of whether mTLS is currently enabled.
	// A user may have disabled mTLS (set enableMTLS=false) before deleting the cluster.
	// Without handling deletion before the enableMTLS check, the finalizer would
	// never be removed and the RayCluster would be stuck in Terminating.
	if instance.DeletionTimestamp != nil && !instance.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(instance, mtlsFinalizer) {
			// Only delete secrets in auto-generate mode; BYOC secrets belong to the user.
			if !utils.IsMTLSBYOC(&instance.Spec) {
				if err := r.deleteTLSSecrets(ctx, instance); err != nil {
					logger.Error(err, "Failed to delete TLS secrets during RayCluster deletion")
					r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.MTLSFailedToCleanupSecrets),
						"Failed to clean up mTLS secrets: %v", err)
					return ctrl.Result{RequeueAfter: mtlsDefaultRequeueDuration}, err
				}
				r.Recorder.Event(instance, corev1.EventTypeNormal, string(utils.MTLSSecretsCleanedUp),
					"Auto-generated mTLS secrets deleted")
			}
			// Remove the finalizer to unblock deletion.
			controllerutil.RemoveFinalizer(instance, mtlsFinalizer)
			if err := r.Update(ctx, instance); err != nil {
				logger.Error(err, "Failed to remove mTLS finalizer")
				return ctrl.Result{}, err
			}
			logger.Info("Removed mTLS finalizer after secret cleanup")
		}
		return ctrl.Result{}, nil
	}

	// mTLS not enabled: nothing to do.
	if !utils.IsMTLSEnabled(&instance.Spec) {
		return ctrl.Result{}, nil
	}

	// Ensure the finalizer is present so we get a chance to clean up secrets before deletion.
	// Only needed for auto-generate mode; BYOC secrets are user-managed and never deleted.
	if !utils.IsMTLSBYOC(&instance.Spec) {
		if !controllerutil.ContainsFinalizer(instance, mtlsFinalizer) {
			controllerutil.AddFinalizer(instance, mtlsFinalizer)
			if err := r.Update(ctx, instance); err != nil {
				logger.Error(err, "Failed to add mTLS finalizer")
				return ctrl.Result{}, err
			}
			logger.Info("Added mTLS finalizer for auto-generated secret cleanup")
			// Re-reconcile after the finalizer update takes effect.
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// BYOC mode: user provides their own certificate secret. No cert-manager PKI needed.
	if utils.IsMTLSBYOC(&instance.Spec) {
		return r.handleBYOC(ctx, instance, logger)
	}

	// Auto-generate mode: create full cert-manager PKI chain.
	return r.handleAutoGenerate(ctx, instance, logger)
}

// handleAutoGenerate creates the full cert-manager PKI chain (self-signed issuer,
// CA certificate, CA issuer, head/worker leaf certificates) and monitors readiness.
func (r *RayClusterMTLSController) handleAutoGenerate(ctx context.Context, instance *rayv1.RayCluster, logger logr.Logger) (ctrl.Result, error) {
	logger.Info("Reconciling auto-generated mTLS for RayCluster", "cluster", instance.Name)

	steps := []struct {
		fn   func(context.Context, *rayv1.RayCluster) error
		name string
	}{
		{r.reconcileSelfSignedIssuer, "self-signed issuer"},
		{r.reconcileCACertificate, "CA certificate"},
		{r.reconcileCAIssuer, "CA issuer"},
		{r.reconcileHeadCertificate, "head certificate"},
		{r.reconcileWorkerCertificate, "worker certificate"},
	}

	for _, step := range steps {
		if err := step.fn(ctx, instance); err != nil {
			logger.Error(err, "Failed to reconcile TLS resource", "resource", step.name)
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.MTLSFailedToReconcile),
				"Failed to reconcile mTLS %s: %v", step.name, err)
			return ctrl.Result{RequeueAfter: mtlsDefaultRequeueDuration}, err
		}
	}

	// Verify certificates are ready before declaring success.
	ready, err := r.checkTLSCertificatesReady(ctx, instance)
	if err != nil {
		return ctrl.Result{RequeueAfter: mtlsDefaultRequeueDuration}, err
	}
	if !ready {
		logger.Info("TLS certificates not ready yet, will requeue")
		r.Recorder.Event(instance, corev1.EventTypeNormal, string(utils.MTLSCertsNotReady),
			"Waiting for cert-manager to issue mTLS certificates")
		return ctrl.Result{RequeueAfter: mtlsDefaultRequeueDuration}, nil
	}

	r.Recorder.Event(instance, corev1.EventTypeNormal, string(utils.MTLSPKIReady),
		"mTLS PKI is ready: CA, head, and worker certificates issued")

	r.checkTLSCertificateExpiry(ctx, instance)

	return ctrl.Result{RequeueAfter: mtlsPeriodicCheckDuration}, nil
}

// handleBYOC validates the user-provided certificate secret(s) exist and contain the required keys.
// When WorkerCertificateSecretName is set, both head and worker secrets are validated.
// No cert-manager resources are created. The user's secrets are never deleted by the operator.
func (r *RayClusterMTLSController) handleBYOC(ctx context.Context, instance *rayv1.RayCluster, logger logr.Logger) (ctrl.Result, error) {
	// Build the list of secrets to validate: head (always) + worker (if separate).
	secretNames := []string{*instance.Spec.MTLSOptions.CertificateSecretName}
	if w := instance.Spec.MTLSOptions.WorkerCertificateSecretName; w != nil && *w != "" {
		secretNames = append(secretNames, *w)
	}

	for _, secretName := range secretNames {
		logger.Info("BYOC mTLS mode: verifying user-provided secret", "secret", secretName)

		secret := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Name: secretName, Namespace: instance.Namespace}, secret); err != nil {
			if errors.IsNotFound(err) {
				logger.Info("BYOC secret not found, will requeue", "secret", secretName)
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.MTLSBYOCSecretNotFound),
					"BYOC mTLS secret %q not found; waiting for it to be created", secretName)
				return ctrl.Result{RequeueAfter: mtlsDefaultRequeueDuration}, nil
			}
			return ctrl.Result{}, err
		}

		for _, key := range []string{"tls.crt", "tls.key", "ca.crt"} {
			if _, ok := secret.Data[key]; !ok {
				logger.Info("BYOC secret missing required key", "secret", secretName, "key", key)
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, string(utils.MTLSBYOCSecretInvalid),
					"BYOC mTLS secret %q is missing required key %q", secretName, key)
				return ctrl.Result{RequeueAfter: mtlsDefaultRequeueDuration}, nil
			}
		}
	}

	logger.Info("BYOC mTLS secret(s) valid", "secrets", secretNames)
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.MTLSBYOCSecretValid),
		"BYOC mTLS secret(s) %v are valid", secretNames)
	return ctrl.Result{RequeueAfter: mtlsPeriodicCheckDuration}, nil
}

// deleteTLSSecrets deletes the auto-generated cert-manager TLS Secrets for this RayCluster.
// Issuers and Certificates cascade-delete via owner references, but cert-manager may not
// set owner references on Secrets (depends on --enable-certificate-owner-ref flag), so we
// delete them explicitly. Only targets the operator's auto-generated secret names (CA, head,
// worker); user-provided BYOC secrets with custom names are never touched.
func (r *RayClusterMTLSController) deleteTLSSecrets(ctx context.Context, instance *rayv1.RayCluster) error {
	ns := instance.Namespace
	name := instance.Name
	secrets := []string{
		utils.GetCASecretName(name, instance.UID),
		fmt.Sprintf("%s-%s", utils.RayHeadSecretPrefix, name),
		fmt.Sprintf("%s-%s", utils.RayWorkerSecretPrefix, name),
	}
	for _, secretName := range secrets {
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: ns}}
		if err := r.Delete(ctx, secret); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// reconcileSelfSignedIssuer creates or updates the self-signed issuer used to bootstrap the CA.
func (r *RayClusterMTLSController) reconcileSelfSignedIssuer(ctx context.Context, instance *rayv1.RayCluster) error {
	issuerName := fmt.Sprintf("%s-%s", utils.RaySelfSignedIssuerPrefix, instance.Name)
	desiredLabels := tlsResourceLabels(instance.Name, "selfsigned-issuer")
	desiredSpec := certmanagerv1.IssuerSpec{
		IssuerConfig: certmanagerv1.IssuerConfig{
			SelfSigned: &certmanagerv1.SelfSignedIssuer{},
		},
	}

	existing := &certmanagerv1.Issuer{}
	err := r.Get(ctx, client.ObjectKey{Name: issuerName, Namespace: instance.Namespace}, existing)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		issuer := &certmanagerv1.Issuer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      issuerName,
				Namespace: instance.Namespace,
				Labels:    desiredLabels,
			},
			Spec: desiredSpec,
		}
		if err := controllerutil.SetControllerReference(instance, issuer, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, issuer)
	}

	if !reflect.DeepEqual(existing.Labels, desiredLabels) || !reflect.DeepEqual(existing.Spec, desiredSpec) {
		existing.Labels = desiredLabels
		existing.Spec = desiredSpec
		if err := controllerutil.SetControllerReference(instance, existing, r.Scheme); err != nil {
			return err
		}
		return r.Update(ctx, existing)
	}
	return nil
}

// reconcileCACertificate creates or updates the root CA certificate signed by the self-signed issuer.
func (r *RayClusterMTLSController) reconcileCACertificate(ctx context.Context, instance *rayv1.RayCluster) error {
	certName := fmt.Sprintf("%s-%s", utils.RayCACertificatePrefix, instance.Name)
	caSecretName := utils.GetCASecretName(instance.Name, instance.UID)
	desiredLabels := tlsResourceLabels(instance.Name, "ca-certificate")
	desiredSpec := certmanagerv1.CertificateSpec{
		SecretName: caSecretName,
		IsCA:       true,
		CommonName: fmt.Sprintf("ray-ca-%s", instance.Name),
		PrivateKey: &certmanagerv1.CertificatePrivateKey{
			Algorithm: certmanagerv1.RSAKeyAlgorithm,
			Size:      4096,
		},
		Duration: &metav1.Duration{
			Duration: 24 * time.Hour * 3650, // ~10 years
		},
		RenewBefore: &metav1.Duration{
			Duration: 24 * time.Hour * 30, // 30 days before expiry
		},
		Usages: []certmanagerv1.KeyUsage{
			certmanagerv1.UsageCertSign,
			certmanagerv1.UsageCRLSign,
		},
		IssuerRef: cmmeta.ObjectReference{
			Name:  fmt.Sprintf("%s-%s", utils.RaySelfSignedIssuerPrefix, instance.Name),
			Kind:  "Issuer",
			Group: "cert-manager.io",
		},
	}

	existing := &certmanagerv1.Certificate{}
	err := r.Get(ctx, client.ObjectKey{Name: certName, Namespace: instance.Namespace}, existing)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		cert := &certmanagerv1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certName,
				Namespace: instance.Namespace,
				Labels:    desiredLabels,
			},
			Spec: desiredSpec,
		}
		if err := controllerutil.SetControllerReference(instance, cert, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, cert)
	}

	if !reflect.DeepEqual(existing.Labels, desiredLabels) || !reflect.DeepEqual(existing.Spec, desiredSpec) {
		existing.Labels = desiredLabels
		existing.Spec = desiredSpec
		if err := controllerutil.SetControllerReference(instance, existing, r.Scheme); err != nil {
			return err
		}
		return r.Update(ctx, existing)
	}
	return nil
}

// reconcileCAIssuer creates or updates the issuer backed by the generated CA certificate's secret.
func (r *RayClusterMTLSController) reconcileCAIssuer(ctx context.Context, instance *rayv1.RayCluster) error {
	issuerName := fmt.Sprintf("%s-%s", utils.RayCAIssuerPrefix, instance.Name)
	caSecretName := utils.GetCASecretName(instance.Name, instance.UID)
	desiredLabels := tlsResourceLabels(instance.Name, "ca-issuer")
	desiredSpec := certmanagerv1.IssuerSpec{
		IssuerConfig: certmanagerv1.IssuerConfig{
			CA: &certmanagerv1.CAIssuer{
				SecretName: caSecretName,
			},
		},
	}

	existing := &certmanagerv1.Issuer{}
	err := r.Get(ctx, client.ObjectKey{Name: issuerName, Namespace: instance.Namespace}, existing)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		issuer := &certmanagerv1.Issuer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      issuerName,
				Namespace: instance.Namespace,
				Labels:    desiredLabels,
			},
			Spec: desiredSpec,
		}
		if err := controllerutil.SetControllerReference(instance, issuer, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, issuer)
	}

	if !reflect.DeepEqual(existing.Labels, desiredLabels) || !reflect.DeepEqual(existing.Spec, desiredSpec) {
		existing.Labels = desiredLabels
		existing.Spec = desiredSpec
		if err := controllerutil.SetControllerReference(instance, existing, r.Scheme); err != nil {
			return err
		}
		return r.Update(ctx, existing)
	}
	return nil
}

// reconcileHeadCertificate creates or updates the head node leaf certificate.
// Pod IPs are collected and injected into the certificate SANs so Ray's gRPC
// TLS hostname verification succeeds when workers connect to the head.
func (r *RayClusterMTLSController) reconcileHeadCertificate(ctx context.Context, instance *rayv1.RayCluster) error {
	certName := fmt.Sprintf("%s-%s", utils.RayHeadCertPrefix, instance.Name)
	secretName := fmt.Sprintf("%s-%s", utils.RayHeadSecretPrefix, instance.Name)
	headSvcName := fmt.Sprintf("%s-head-svc", instance.Name)

	podIPs, err := r.getPodIPs(ctx, instance)
	if err != nil {
		return err
	}

	desiredLabels := tlsResourceLabels(instance.Name, "head-certificate")
	desiredDNSNames := uniqueStrings([]string{
		headSvcName,
		"localhost",
		fmt.Sprintf("%s.%s.svc", headSvcName, instance.Namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", headSvcName, instance.Namespace),
	})
	sort.Strings(desiredDNSNames)
	desiredIPAddresses := normalizeIPs(podIPs)

	desiredSpec := certmanagerv1.CertificateSpec{
		SecretName:  secretName,
		Duration:    &metav1.Duration{Duration: 2160 * time.Hour},
		RenewBefore: &metav1.Duration{Duration: 360 * time.Hour},
		DNSNames:    desiredDNSNames,
		IPAddresses: desiredIPAddresses,
		Usages:      leafCertKeyUsages(),
		IssuerRef: cmmeta.ObjectReference{
			Name:  fmt.Sprintf("%s-%s", utils.RayCAIssuerPrefix, instance.Name),
			Kind:  "Issuer",
			Group: "cert-manager.io",
		},
	}

	existing := &certmanagerv1.Certificate{}
	err = r.Get(ctx, client.ObjectKey{Name: certName, Namespace: instance.Namespace}, existing)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		cert := &certmanagerv1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certName,
				Namespace: instance.Namespace,
				Labels:    desiredLabels,
			},
			Spec: desiredSpec,
		}
		if err := controllerutil.SetControllerReference(instance, cert, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, cert)
	}

	if !reflect.DeepEqual(existing.Labels, desiredLabels) || certificateNeedsUpdate(&existing.Spec, &desiredSpec) {
		existing.Labels = desiredLabels
		existing.Spec = desiredSpec
		if err := controllerutil.SetControllerReference(instance, existing, r.Scheme); err != nil {
			return err
		}
		logger := ctrl.LoggerFrom(ctx)
		logger.Info("Updating head certificate", "certificate", certName, "ipAddresses", desiredIPAddresses)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.MTLSCertificatesUpdated),
			"Updated head certificate SANs (IPs: %v)", desiredIPAddresses)
		return r.Update(ctx, existing)
	}
	return nil
}

// reconcileWorkerCertificate creates or updates the worker node leaf certificate.
// All worker pods share this single certificate. Pod IPs are included in the SANs
// so Ray's gRPC TLS hostname verification succeeds for inter-node communication.
func (r *RayClusterMTLSController) reconcileWorkerCertificate(ctx context.Context, instance *rayv1.RayCluster) error {
	certName := fmt.Sprintf("%s-%s", utils.RayWorkerCertPrefix, instance.Name)
	secretName := fmt.Sprintf("%s-%s", utils.RayWorkerSecretPrefix, instance.Name)

	podIPs, err := r.getPodIPs(ctx, instance)
	if err != nil {
		return err
	}

	// Build DNS names including per-worker-group service names and wildcards
	// for dynamic worker discovery (aligned with ODH mTLS controller).
	workerSvcName := fmt.Sprintf("%s-worker-svc", instance.Name)
	dnsNames := []string{
		workerSvcName,
		"localhost",
		fmt.Sprintf("%s.%s.svc", workerSvcName, instance.Namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", workerSvcName, instance.Namespace),
	}
	for _, wg := range instance.Spec.WorkerGroupSpecs {
		groupSvc := fmt.Sprintf("%s-%s", instance.Name, wg.GroupName)
		dnsNames = append(dnsNames,
			groupSvc,
			fmt.Sprintf("%s.%s.svc", groupSvc, instance.Namespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", groupSvc, instance.Namespace),
		)
	}
	// Wildcard patterns for dynamic worker services.
	dnsNames = append(dnsNames,
		fmt.Sprintf("*.%s.%s.svc", workerSvcName, instance.Namespace),
		fmt.Sprintf("*.%s.%s.svc.cluster.local", workerSvcName, instance.Namespace),
		fmt.Sprintf("*-worker-*.%s.svc", instance.Namespace),
		fmt.Sprintf("*-worker-*.%s.svc.cluster.local", instance.Namespace),
	)

	desiredLabels := tlsResourceLabels(instance.Name, "worker-certificate")
	desiredDNSNames := uniqueStrings(dnsNames)
	sort.Strings(desiredDNSNames)
	desiredIPAddresses := normalizeIPs(podIPs)

	desiredSpec := certmanagerv1.CertificateSpec{
		SecretName:  secretName,
		Duration:    &metav1.Duration{Duration: 2160 * time.Hour},
		RenewBefore: &metav1.Duration{Duration: 360 * time.Hour},
		DNSNames:    desiredDNSNames,
		IPAddresses: desiredIPAddresses,
		Usages:      leafCertKeyUsages(),
		IssuerRef: cmmeta.ObjectReference{
			Name:  fmt.Sprintf("%s-%s", utils.RayCAIssuerPrefix, instance.Name),
			Kind:  "Issuer",
			Group: "cert-manager.io",
		},
	}

	existing := &certmanagerv1.Certificate{}
	err = r.Get(ctx, client.ObjectKey{Name: certName, Namespace: instance.Namespace}, existing)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		cert := &certmanagerv1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certName,
				Namespace: instance.Namespace,
				Labels:    desiredLabels,
			},
			Spec: desiredSpec,
		}
		if err := controllerutil.SetControllerReference(instance, cert, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, cert)
	}

	if !reflect.DeepEqual(existing.Labels, desiredLabels) || certificateNeedsUpdate(&existing.Spec, &desiredSpec) {
		existing.Labels = desiredLabels
		existing.Spec = desiredSpec
		if err := controllerutil.SetControllerReference(instance, existing, r.Scheme); err != nil {
			return err
		}
		logger := ctrl.LoggerFrom(ctx)
		logger.Info("Updating worker certificate", "certificate", certName, "ipAddresses", desiredIPAddresses)
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, string(utils.MTLSCertificatesUpdated),
			"Updated worker certificate SANs (IPs: %v)", desiredIPAddresses)
		return r.Update(ctx, existing)
	}
	return nil
}

// getPodIPs collects all pod IPs for the RayCluster by listing pods
// with the ray.io/cluster label matching the cluster name.
func (r *RayClusterMTLSController) getPodIPs(ctx context.Context, instance *rayv1.RayCluster) ([]string, error) {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(instance.Namespace),
		client.MatchingLabels(map[string]string{
			utils.RayClusterLabelKey: instance.Name,
		})); err != nil {
		return nil, err
	}
	var podIPs []string
	for _, pod := range pods.Items {
		if pod.Status.PodIP != "" {
			podIPs = append(podIPs, pod.Status.PodIP)
		}
	}
	return podIPs, nil
}

// checkTLSCertificatesReady verifies that both head and worker certificates
// have been issued and are in Ready state.
func (r *RayClusterMTLSController) checkTLSCertificatesReady(ctx context.Context, instance *rayv1.RayCluster) (bool, error) {
	certNames := []string{
		fmt.Sprintf("%s-%s", utils.RayHeadCertPrefix, instance.Name),
		fmt.Sprintf("%s-%s", utils.RayWorkerCertPrefix, instance.Name),
	}
	for _, name := range certNames {
		cert := &certmanagerv1.Certificate{}
		if err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: instance.Namespace}, cert); err != nil {
			return false, err
		}
		if !isCertificateReady(cert) {
			ctrl.LoggerFrom(ctx).Info("TLS certificate not ready yet", "certificate", name)
			return false, nil
		}
	}
	return true, nil
}

// checkTLSCertificateExpiry logs warnings for certificates approaching expiry.
func (r *RayClusterMTLSController) checkTLSCertificateExpiry(ctx context.Context, instance *rayv1.RayCluster) {
	logger := ctrl.LoggerFrom(ctx)
	certNames := []string{
		fmt.Sprintf("%s-%s", utils.RayHeadCertPrefix, instance.Name),
		fmt.Sprintf("%s-%s", utils.RayWorkerCertPrefix, instance.Name),
		fmt.Sprintf("%s-%s", utils.RayCACertificatePrefix, instance.Name),
	}
	for _, name := range certNames {
		cert := &certmanagerv1.Certificate{}
		if err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: instance.Namespace}, cert); err != nil {
			continue
		}
		if cert.Status.NotAfter != nil {
			remaining := time.Until(cert.Status.NotAfter.Time)
			if remaining < 24*time.Hour {
				logger.Info("TLS certificate expiring soon",
					"certificate", name,
					"expiresAt", cert.Status.NotAfter.Time,
					"remainingHours", remaining.Hours(),
				)
			}
		}
	}
}

// SetupWithManager registers the mTLS controller with the manager.
func (r *RayClusterMTLSController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("raycluster-mtls").
		For(&rayv1.RayCluster{}).
		Owns(&certmanagerv1.Issuer{}).
		Owns(&certmanagerv1.Certificate{}).
		WithOptions(controller.Options{
			LogConstructor: func(request *reconcile.Request) logr.Logger {
				logger := ctrl.Log.WithName("controllers").WithName("RayClusterMTLS")
				if request != nil {
					logger = logger.WithValues("RayCluster", request.NamespacedName)
				}
				return logger
			},
		}).
		Complete(r)
}

// --- Shared helpers (also used by tests) ---

// leafCertKeyUsages returns the key usages for head and worker leaf certificates.
func leafCertKeyUsages() []certmanagerv1.KeyUsage {
	return []certmanagerv1.KeyUsage{
		certmanagerv1.UsageDigitalSignature,
		certmanagerv1.UsageKeyEncipherment,
		certmanagerv1.UsageServerAuth,
		certmanagerv1.UsageClientAuth,
	}
}

// isCertificateReady returns true if the certificate has a Ready=True condition.
func isCertificateReady(cert *certmanagerv1.Certificate) bool {
	for _, cond := range cert.Status.Conditions {
		if cond.Type == certmanagerv1.CertificateConditionReady {
			return cond.Status == cmmeta.ConditionTrue
		}
	}
	return false
}

// setsEqual compares two string slices as sets (order-insensitive).
func setsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, v := range a {
		set[v] = struct{}{}
	}
	for _, v := range b {
		if _, ok := set[v]; !ok {
			return false
		}
	}
	return true
}

// certificateNeedsUpdate checks if a certificate's DNS names or IP addresses
// differ from the desired values using set-based comparison.
func certificateNeedsUpdate(existing, desired *certmanagerv1.CertificateSpec) bool {
	return !setsEqual(existing.DNSNames, desired.DNSNames) ||
		!setsEqual(existing.IPAddresses, desired.IPAddresses)
}

// normalizeIPs returns a sorted, de-duplicated set of IPs. Always includes 127.0.0.1.
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
	sort.Strings(out)
	return out
}

// uniqueStrings returns the input with duplicates removed, preserving order.
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

// tlsResourceLabels returns standard labels for cert-manager resources owned by a RayCluster.
func tlsResourceLabels(clusterName, component string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "ray-tls",
		"app.kubernetes.io/component": component,
		utils.RayClusterLabelKey:      clusterName,
	}
}
