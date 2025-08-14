package ray

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	configapi "github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

const (
	caSecretName = "ray-ca-secret"
	caCertKey    = "ca.crt"
	caKeyKey     = "ca.key"
)

// RayClusterMTLSController manages CA certificates for MTLS-enabled Ray clusters
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

// Reconcile handles the reconciliation of CA certificates
func (r *RayClusterMTLSController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !ptr.Deref(r.Config.EnableMTLS, false) {
		return ctrl.Result{}, nil
	}

	logger := log.FromContext(ctx)

	// Check if CA secret exists
	caSecret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Name: caSecretName, Namespace: req.Namespace}, caSecret)

	if errors.IsNotFound(err) {
		logger.Info("CA secret not found, creating new one")
		return r.createCASecret(ctx, req.Namespace)
	} else if err != nil {
		logger.Error(err, "Failed to get CA secret")
		return ctrl.Result{}, err
	}

	// Check if CA certificate is expired or about to expire
	if r.isCAExpiringSoon(caSecret) {
		logger.Info("CA certificate expiring soon, regenerating")
		return r.regenerateCASecret(ctx, req.Namespace)
	}

	return ctrl.Result{}, nil
}

// createCASecret creates a new CA secret with generated certificate and private key
func (r *RayClusterMTLSController) createCASecret(ctx context.Context, namespace string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Generate CA certificate and private key
	caKey, caCert, err := r.generateCACertificate()
	if err != nil {
		logger.Error(err, "Failed to generate CA certificate")
		return ctrl.Result{}, err
	}

	// Create secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      caSecretName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "ray-mtls-ca",
				"app.kubernetes.io/component": "ca-secret",
			},
		},
		Data: map[string][]byte{
			caCertKey: caCert,
			caKeyKey:  caKey,
		},
	}

	if err := r.Create(ctx, secret); err != nil {
		logger.Error(err, "Failed to create CA secret")
		return ctrl.Result{}, err
	}

	logger.Info("CA secret created successfully")
	return ctrl.Result{}, nil
}

// generateCACertificate generates a new CA certificate and private key
func (r *RayClusterMTLSController) generateCACertificate() ([]byte, []byte, error) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	// Create CA certificate
	serialNumber := big.NewInt(time.Now().UnixNano())
	cert := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: "ray-ca",
		},
		Issuer: pkix.Name{
			CommonName: "ray-ca",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0), // 1 year validity
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	// Create certificate
	certBytes, err := x509.CreateCertificate(rand.Reader, cert, cert, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, err
	}

	// Encode to PEM
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})

	return privateKeyPEM, certPEM, nil
}

// isCAExpiringSoon checks if the CA certificate expires in less than 30 days
func (r *RayClusterMTLSController) isCAExpiringSoon(secret *corev1.Secret) bool {
	certData := secret.Data[caCertKey]
	if len(certData) == 0 {
		return true
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		return true
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true
	}

	// Check if certificate expires in less than 30 days
	return time.Until(cert.NotAfter) < 30*24*time.Hour
}

// regenerateCASecret deletes the existing CA secret and creates a new one
func (r *RayClusterMTLSController) regenerateCASecret(ctx context.Context, namespace string) (ctrl.Result, error) {
	// Delete existing secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      caSecretName,
			Namespace: namespace,
		},
	}

	if err := r.Delete(ctx, secret); err != nil {
		return ctrl.Result{}, err
	}

	// Create new one
	return r.createCASecret(ctx, namespace)
}

// SetupWithManager sets up the controller with the manager
func (r *RayClusterMTLSController) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("raycluster-mtls").
		For(&rayv1.RayCluster{}).
		Complete(r)
}
