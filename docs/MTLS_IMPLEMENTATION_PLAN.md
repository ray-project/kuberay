# MTLS Implementation Plan for KubeRay

## Overview

This document outlines the comprehensive plan for adding Mutual TLS (MTLS) support to KubeRay by implementing a webhook-based approach that automatically injects CA certificates and generates self-signed certificates for Ray clusters when `enableMTLS: true` is set in the operator configuration.

## Current State Analysis

### KubeRay Operator

- Has basic webhook infrastructure but only validation webhooks
- Has cert-manager integration for webhook server certificates
- No MTLS support currently
- Configuration API exists but limited

### CodeFlare Operator (Reference Implementation)

- Full MTLS implementation with webhook-based injection
- CA certificate generation and management
- Init container injection for certificate creation
- Environment variable injection for Ray TLS configuration
- Volume and volume mount management

## Implementation Steps

### 1. Extend KubeRay Configuration API

**File:** `kuberay/ray-operator/apis/config/v1alpha1/configuration_types.go`

Add new MTLS-related configuration fields:

```go
type Configuration struct {
    // ... existing fields ...

    // EnableMTLS enables mutual TLS support for Ray clusters.
    // When enabled, the operator will automatically generate CA certificates
    // and inject them into Ray head and worker pods.
    // +optional
    EnableMTLS *bool `json:"enableMTLS,omitempty"`

    // CertGeneratorImage specifies the container image to use for generating
    // self-signed certificates in Ray pods. Must contain OpenSSL.
    // +optional
    CertGeneratorImage string `json:"certGeneratorImage,omitempty"`

    // MTLSSecretNamespace specifies the namespace where CA secrets should be stored.
    // Defaults to the operator namespace if not specified.
    // +optional
    MTLSSecretNamespace string `json:"mtlsSecretNamespace,omitempty"`
}
```

**File:** `kuberay/ray-operator/apis/config/v1alpha1/defaults.go`

Add default values:

```go
const (
    // ... existing constants ...
    DefaultEnableMTLS = false
    DefaultCertGeneratorImage = "registry.redhat.io/ubi9@sha256:770cf07083e1c85ae69c25181a205b7cdef63c11b794c89b3b487d4670b4c328"
)

func SetDefaults_Configuration(cfg *Configuration) {
    // ... existing defaults ...

    if cfg.EnableMTLS == nil {
        cfg.EnableMTLS = ptr.To(DefaultEnableMTLS)
    }

    if cfg.CertGeneratorImage == "" {
        cfg.CertGeneratorImage = DefaultCertGeneratorImage
    }
}
```

### 2. Create MTLS Webhook Implementation

**File:** `kuberay/ray-operator/pkg/webhooks/v1/raycluster_mtls_webhook.go`

Create a new mutating webhook that handles MTLS injection:

```go
package v1

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
    "k8s.io/apimachinery/pkg/runtime"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/webhook"
    "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

    rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
    configapi "github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
)

type RayClusterMTLSWebhook struct {
    Config *configapi.Configuration
}

var _ webhook.CustomDefaulter = &RayClusterMTLSWebhook{}

// Default implements webhook.Defaulter
func (w *RayClusterMTLSWebhook) Default(ctx context.Context, obj runtime.Object) error {
    if !ptr.Deref(w.Config.EnableMTLS, false) {
        return nil
    }

    rayCluster := obj.(*rayv1.RayCluster)
    logger := ctrl.LoggerFrom(ctx)

    logger.Info("Injecting MTLS configuration", "cluster", rayCluster.Name)

    // Inject MTLS configuration into head group
    w.injectMTLSIntoHeadGroup(rayCluster)

    // Inject MTLS configuration into worker groups
    w.injectMTLSIntoWorkerGroups(rayCluster)

    return nil
}

func (w *RayClusterMTLSWebhook) injectMTLSIntoHeadGroup(rayCluster *rayv1.RayCluster) {
    // Add environment variables for Ray TLS
    w.addTLSEnvironmentVariables(&rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[0])

    // Add init container for certificate generation
    w.addCertInitContainer(&rayCluster.Spec.HeadGroupSpec.Template.Spec, true)

    // Add CA volumes
    w.addCAVolumes(&rayCluster.Spec.HeadGroupSpec.Template.Spec)

    // Add certificate volume mounts
    w.addCertVolumeMounts(&rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[0])
}

func (w *RayClusterMTLSWebhook) injectMTLSIntoWorkerGroups(rayCluster *rayv1.RayCluster) {
    for i := range rayCluster.Spec.WorkerGroupSpecs {
        workerSpec := &rayCluster.Spec.WorkerGroupSpecs[i]

        // Add environment variables for Ray TLS
        w.addTLSEnvironmentVariables(&workerSpec.Template.Spec.Containers[0])

        // Add init container for certificate generation
        w.addCertInitContainer(&workerSpec.Template.Spec, false)

        // Add CA volumes
        w.addCAVolumes(&workerSpec.Template.Spec)

        // Add certificate volume mounts
        w.addCertVolumeMounts(&workerSpec.Template.Spec.Containers[0])
    }
}

func (w *RayClusterMTLSWebhook) addTLSEnvironmentVariables(container *corev1.Container) {
    tlsEnvVars := []corev1.EnvVar{
        {
            Name: "MY_POD_IP",
            ValueFrom: &corev1.EnvVarSource{
                FieldRef: &corev1.ObjectFieldSelector{
                    FieldPath: "status.podIP",
                },
            },
        },
        {Name: "RAY_USE_TLS", Value: "1"},
        {Name: "RAY_TLS_SERVER_CERT", Value: "/home/ray/workspace/tls/server.crt"},
        {Name: "RAY_TLS_SERVER_KEY", Value: "/home/ray/workspace/tls/server.key"},
        {Name: "RAY_TLS_CA_CERT", Value: "/home/ray/workspace/tls/ca.crt"},
    }

    container.Env = append(container.Env, tlsEnvVars...)
}

func (w *RayClusterMTLSWebhook) addCertInitContainer(podSpec *corev1.PodSpec, isHead bool) {
    initContainer := corev1.Container{
        Name:  "create-cert",
        Image: w.Config.CertGeneratorImage,
        Command: []string{"sh", "-c", w.generateCertCommand(isHead)},
        VolumeMounts: w.getCertVolumeMounts(),
    }

    podSpec.InitContainers = append(podSpec.InitContainers, initContainer)
}

func (w *RayClusterMTLSWebhook) generateCertCommand(isHead bool) string {
    if isHead {
        return `cd /home/ray/workspace/tls &&
                openssl req -nodes -newkey rsa:2048 -keyout server.key -out server.csr -subj '/CN=ray-head' &&
                printf "authorityKeyIdentifier=keyid,issuer\nbasicConstraints=CA:FALSE\nsubjectAltName = @alt_names\n[alt_names]\nDNS.1 = 127.0.0.1\nDNS.2 = localhost\nDNS.3 = ${FQ_RAY_IP}\nDNS.4 = $(awk 'END{print $1}' /etc/hosts)">./domain.ext &&
                cp /home/ray/workspace/ca/* . &&
                openssl x509 -req -CA ca.crt -CAkey ca.key -in server.csr -out server.crt -days 365 -CAcreateserial -extfile domain.ext`
    }

    return `cd /home/ray/workspace/tls &&
            openssl req -nodes -newkey rsa:2048 -keyout server.key -out server.csr -subj '/CN=ray-worker' &&
            printf "authorityKeyIdentifier=keyid,issuer\nbasicConstraints=CA:FALSE\nsubjectAltName = @alt_names\n[alt_names]\nDNS.1 = 127.0.0.1\nDNS.2 = localhost\nDNS.3 = ${FQ_RAY_IP}\nDNS.4 = $(awk 'END{print $1}' /etc/hosts)">./domain.ext &&
            cp /home/ray/workspace/ca/* . &&
            openssl x509 -req -CA ca.crt -CAkey ca.key -in server.csr -out server.crt -days 365 -CAcreateserial -extfile domain.ext`
}

func (w *RayClusterMTLSWebhook) addCAVolumes(podSpec *corev1.PodSpec) {
    caVolumes := []corev1.Volume{
        {
            Name: "ca-vol",
            VolumeSource: corev1.VolumeSource{
                Secret: &corev1.SecretVolumeSource{
                    SecretName: w.getCASecretName(),
                },
            },
        },
        {
            Name: "server-cert",
            VolumeSource: corev1.VolumeSource{
                EmptyDir: &corev1.EmptyDirVolumeSource{},
            },
        },
    }

    podSpec.Volumes = append(podSpec.Volumes, caVolumes...)
}

func (w *RayClusterMTLSWebhook) addCertVolumeMounts(container *corev1.Container) {
    volumeMounts := []corev1.VolumeMount{
        {
            Name:      "ca-vol",
            MountPath: "/home/ray/workspace/ca",
            ReadOnly:  true,
        },
        {
            Name:      "server-cert",
            MountPath: "/home/ray/workspace/tls",
            ReadOnly:  false,
        },
    }

    container.VolumeMounts = append(container.VolumeMounts, volumeMounts...)
}

func (w *RayClusterMTLSWebhook) getCertVolumeMounts() []corev1.VolumeMount {
    return []corev1.VolumeMount{
        {
            Name:      "ca-vol",
            MountPath: "/home/ray/workspace/ca",
            ReadOnly:  true,
        },
        {
            Name:      "server-cert",
            MountPath: "/home/ray/workspace/tls",
            ReadOnly:  false,
        },
    }
}

func (w *RayClusterMTLSWebhook) getCASecretName() string {
    // This will be managed by the controller
    return "ray-ca-secret"
}
```

### 3. Create MTLS Controller for CA Management

**File:** `kuberay/ray-operator/controllers/ray/raycluster_mtls_controller.go`

Create a controller that manages CA certificates:

```go
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
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/log"

    rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
    configapi "github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
)

const (
    caSecretName = "ray-ca-secret"
    caCertKey    = "ca.crt"
    caKeyKey     = "ca.key"
)

type RayClusterMTLSController struct {
    client.Client
    Scheme *runtime.Scheme
    Config *configapi.Configuration
}

func NewRayClusterMTLSController(client client.Client, scheme *runtime.Scheme, config *configapi.Configuration) *RayClusterMTLSController {
    return &RayClusterMTLSController{
        Client: client,
        Scheme: scheme,
        Config:  config,
    }
}

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
                "app.kubernetes.io/name": "ray-mtls-ca",
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

func (r *RayClusterMTLSController) generateCACertificate() ([]byte, []byte, error) {
    // Generate private key
    privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        return nil, nil, err
    }

    // Create CA certificate
    serialNumber := big.NewInt(rand.Int63())
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

func (r *RayClusterMTLSController) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&rayv1.RayCluster{}).
        Complete(r)
}
```

### 4. Update Main Webhook Setup

**File:** `kuberay/ray-operator/pkg/webhooks/v1/raycluster_webhook.go`

Modify the existing webhook to include MTLS support:

```go
// Update the existing webhook to include MTLS functionality
func SetupRayClusterWebhookWithManager(mgr ctrl.Manager, config *configapi.Configuration) error {
    // Create MTLS webhook instance
    mtlsWebhook := &RayClusterMTLSWebhook{
        Config: config,
    }

    // Setup validation webhook
    if err := ctrl.NewWebhookManagedBy(mgr).
        For(&rayv1.RayCluster{}).
        WithValidator(&RayClusterWebhook{}).
        Complete(); err != nil {
        return err
    }

    // Setup MTLS mutating webhook
    if ptr.Deref(config.EnableMTLS, false) {
        if err := ctrl.NewWebhookManagedBy(mgr).
            For(&rayv1.RayCluster{}).
            WithDefaulter(mtlsWebhook).
            Complete(); err != nil {
            return err
        }
    }

    return nil
}
```

### 5. Update Main Function

**File:** `kuberay/ray-operator/main.go`

Update the main function to include MTLS webhook setup:

```go
// In the main function, update the webhook setup
if os.Getenv("ENABLE_WEBHOOKS") == "true" {
    exitOnError(webhooks.SetupRayClusterWebhookWithManager(mgr, &config),
        "unable to create webhook", "webhook", "RayCluster")
}
```

### 6. Add RBAC Permissions

**File:** `kuberay/ray-operator/config/rbac/role.yaml`

Add necessary RBAC permissions:

```yaml
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ray-operator-role
rules:
  # ... existing rules ...

  # MTLS-related permissions
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]

  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]

  - apiGroups: ["ray.io"]
    resources: ["rayclusters"]
    verbs: ["get", "list", "watch", "update", "patch"]
```

### 7. Update Configuration Files

**File:** `kuberay/ray-operator/config/samples/ray-operator-config.yaml`

Add MTLS configuration example:

```yaml
apiVersion: config.ray.io/v1alpha1
kind: Configuration
metadata:
  name: ray-operator-config
spec:
  metricsAddr: ":8080"
  probeAddr: ":8082"
  enableLeaderElection: true
  reconcileConcurrency: 1

  # MTLS Configuration
  enableMTLS: true
  certGeneratorImage: "registry.redhat.io/ubi9@sha256:770cf07083e1c85ae69c25181a205b7cdef63c11b794c89b3b487d4670b4c328"
  mtlsSecretNamespace: "ray-system"
```

### 8. Add Validation Webhook

**File:** `kuberay/ray-operator/pkg/webhooks/v1/raycluster_webhook.go`

Add validation for MTLS-related fields:

```go
func (w *RayClusterWebhook) validateRayCluster(rayCluster *rayv1.RayCluster) error {
    var allErrs field.ErrorList

    // ... existing validation ...

    // Add MTLS validation if enabled
    if ptr.Deref(w.Config.EnableMTLS, false) {
        allErrs = append(allErrs, w.validateMTLSConfiguration(rayCluster)...)
    }

    if len(allErrs) == 0 {
        return nil
    }

    return apierrors.NewInvalid(
        schema.GroupKind{Group: "ray.io", Kind: "RayCluster"},
        rayCluster.Name, allErrs)
}

func (w *RayClusterWebhook) validateMTLSConfiguration(rayCluster *rayv1.RayCluster) field.ErrorList {
    var allErrs field.ErrorList

    // Validate that required volumes are present
    if err := w.validateRequiredVolumes(rayCluster); err != nil {
        allErrs = append(allErrs, err)
    }

    // Validate that required environment variables are present
    if err := w.validateRequiredEnvVars(rayCluster); err != nil {
        allErrs = append(allErrs, err)
    }

    return allErrs
}
```

### 9. Create Sample MTLS RayCluster

**File:** `kuberay/ray-operator/config/samples/ray-cluster.mtls.yaml`

Create a sample RayCluster with MTLS enabled:

```yaml
apiVersion: ray.io/v1
kind: RayCluster
metadata:
  name: raycluster-mtls-sample
spec:
  rayVersion: '2.8.0'
  headGroupSpec:
    serviceType: ClusterIP
    template:
      spec:
        containers:
        - name: ray-head
          image: rayproject/ray:2.8.0
          ports:
          - containerPort: 6379
            name: gcs
          - containerPort: 8265
            name: dashboard
          - containerPort: 10001
            name: client
          env:
          - name: RAY_DISABLE_DOCKER_CPU_WARNING
            value: "1"
          resources:
            limits:
              cpu: "1"
              memory: "2Gi"
            requests:
              cpu: "500m"
              memory: "1Gi"
  workerGroupSpecs:
  - groupName: small-group
    replicas: 1
    template:
      spec:
        containers:
        - name: ray-worker
          image: rayproject/ray:2.8.0
          env:
          - name: RAY_DISABLE_DOCKER_CPU_WARNING
            value: "1"
          resources:
            limits:
              cpu: "1"
              memory: "2Gi"
            requests:
              cpu: "500m"
              memory: "1Gi"
```

## Testing Strategy

### 1. Unit Tests

- Test webhook logic for certificate injection
- Test certificate generation functions
- Test validation logic for MTLS configuration

### 2. Integration Tests

- Test webhook registration and certificate injection
- Test CA secret creation and management
- Test certificate rotation logic

### 3. E2E Tests

- Test complete MTLS flow from RayCluster creation to secure communication
- Test certificate injection in both head and worker pods
- Test TLS communication between Ray nodes

### 4. Security Tests

- Verify certificate rotation and proper secret management
- Test certificate validation and expiration handling
- Verify proper RBAC permissions

## Migration Considerations

- **Existing RayClusters**: Will not be affected unless explicitly updated
- **Feature Flag**: MTLS can be enabled/disabled per operator instance
- **No Breaking Changes**: No changes to existing APIs
- **Gradual Rollout**: Possible through feature flag configuration

## Prerequisites

- **cert-manager**: Must be installed in the cluster
- **RBAC Permissions**: KubeRay operator must have permissions to manage secrets
- **OpenSSL**: Must be available in the cert generator image
- **Webhook Infrastructure**: Must be properly configured

## Security Features

- **Automatic CA Rotation**: CA certificates rotate every year
- **Individual Certificates**: Each pod gets its own certificate signed by the CA
- **Secure Storage**: Certificates stored in Kubernetes secrets
- **No Manual Management**: Fully automated certificate lifecycle

## Troubleshooting

### Common Issues

1. **CA Secret Not Found**
   - Ensure cert-manager is running
   - Verify operator has proper RBAC permissions
   - Check operator logs for errors

2. **Certificate Generation Failed**
   - Verify cert generator image contains OpenSSL
   - Check init container logs
   - Ensure volumes are properly mounted

3. **TLS Connection Failed**
   - Verify Ray TLS environment variables are set
   - Check certificate files exist in pods
   - Verify CA certificate is valid

### Debugging

Enable debug logging for the operator:

```bash
kubectl logs -n ray-system deployment/ray-operator --v=2
```

Check webhook status:

```bash
kubectl get validatingwebhookconfigurations
kubectl get mutatingwebhookconfigurationsI
```

## Conclusion

This implementation provides a robust, production-ready MTLS solution that:

- Follows Kubernetes best practices
- Integrates seamlessly with existing KubeRay architecture
- Provides automatic certificate management
- Ensures secure communication between Ray nodes
- Maintains backward compatibility
- Supports gradual rollout and configuration

The solution leverages the proven patterns from the CodeFlare operator while adapting them to KubeRay's specific architecture and requirements.
