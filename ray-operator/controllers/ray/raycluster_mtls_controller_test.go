package ray

import (
	"context"
	"fmt"
	"testing"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
)

func newMTLSTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = rayv1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = certmanagerv1.AddToScheme(s)
	return s
}

func newMTLSTestCluster(name string) *rayv1.RayCluster {
	return &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			UID:       "test-uid-12345678",
		},
		Spec: rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{
				RayStartParams: map[string]string{},
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "ray-head", Image: "rayproject/ray:latest"},
						},
					},
				},
			},
			WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
				{
					GroupName:      "small-group",
					RayStartParams: map[string]string{},
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "ray-worker", Image: "rayproject/ray:latest"},
							},
						},
					},
				},
			},
		},
	}
}

func newMTLSController(t *testing.T, objs ...client.Object) *RayClusterMTLSController {
	t.Helper()
	features.SetFeatureGateDuringTest(t, features.EnhancedSecurityPrimitives, true)
	s := newMTLSTestScheme()
	runtimeObjs := make([]runtime.Object, len(objs))
	for i, obj := range objs {
		runtimeObjs[i] = obj
	}
	fakeClient := clientFake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(runtimeObjs...).
		WithStatusSubresource(&certmanagerv1.Certificate{}).
		Build()
	return &RayClusterMTLSController{
		Client:   fakeClient,
		Scheme:   s,
		Recorder: record.NewFakeRecorder(32),
	}
}

// markMTLSCertificatesReady sets the Ready=True condition on all Certificate objects
// for the given cluster, simulating cert-manager issuing the certificates.
func markMTLSCertificatesReady(ctx context.Context, t *testing.T, r *RayClusterMTLSController, cluster *rayv1.RayCluster) {
	t.Helper()
	certNames := []string{
		fmt.Sprintf("%s-%s", utils.RayHeadCertPrefix, cluster.Name),
		fmt.Sprintf("%s-%s", utils.RayWorkerCertPrefix, cluster.Name),
	}
	for _, name := range certNames {
		cert := &certmanagerv1.Certificate{}
		err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: cluster.Namespace}, cert)
		if errors.IsNotFound(err) {
			continue
		}
		require.NoError(t, err)
		cert.Status.Conditions = []certmanagerv1.CertificateCondition{
			{
				Type:   certmanagerv1.CertificateConditionReady,
				Status: cmmeta.ConditionTrue,
			},
		}
		require.NoError(t, r.Status().Update(ctx, cert))
	}
}

// --- Auto-generate mode tests ---

func TestMTLSController_Disabled(t *testing.T) {
	cluster := newMTLSTestCluster("test-cluster")
	r := newMTLSController(t, cluster)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
	})
	require.NoError(t, err, "Reconcile should be a no-op when mTLS is disabled")
	assert.Equal(t, ctrl.Result{}, result)
}

func TestMTLSController_AutoGenerate_CreatesFullPKI(t *testing.T) {
	cluster := newMTLSTestCluster("test-cluster")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{}

	r := newMTLSController(t, cluster)
	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}}

	// First call adds the mTLS finalizer and requeues.
	result, err := r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.True(t, result.Requeue, "should requeue after adding finalizer")

	// Second call creates resources but certs aren't ready yet.
	result, err = r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.NotZero(t, result.RequeueAfter, "should requeue when certs not ready")

	// Simulate cert-manager marking certificates as ready.
	markMTLSCertificatesReady(ctx, t, r, cluster)

	result, err = r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, mtlsPeriodicCheckDuration, result.RequeueAfter, "should schedule periodic recheck")

	// Verify self-signed issuer was created.
	issuer := &certmanagerv1.Issuer{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-%s", utils.RaySelfSignedIssuerPrefix, cluster.Name),
		Namespace: "default",
	}, issuer)
	require.NoError(t, err, "self-signed issuer should be created")
	assert.NotNil(t, issuer.Spec.SelfSigned)

	// Verify CA certificate was created.
	caCert := &certmanagerv1.Certificate{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-%s", utils.RayCACertificatePrefix, cluster.Name),
		Namespace: "default",
	}, caCert)
	require.NoError(t, err, "CA certificate should be created")
	assert.True(t, caCert.Spec.IsCA)
	assert.Equal(t, 4096, caCert.Spec.PrivateKey.Size)

	// Verify CA issuer was created.
	caIssuer := &certmanagerv1.Issuer{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-%s", utils.RayCAIssuerPrefix, cluster.Name),
		Namespace: "default",
	}, caIssuer)
	require.NoError(t, err, "CA issuer should be created")
	assert.NotNil(t, caIssuer.Spec.CA)

	// Verify head certificate was created with correct DNS names.
	headCert := &certmanagerv1.Certificate{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-%s", utils.RayHeadCertPrefix, cluster.Name),
		Namespace: "default",
	}, headCert)
	require.NoError(t, err, "head certificate should be created")
	assert.Contains(t, headCert.Spec.DNSNames, "localhost")
	assert.Contains(t, headCert.Spec.DNSNames, fmt.Sprintf("%s-head-svc", cluster.Name))
	assert.Contains(t, headCert.Spec.IPAddresses, "127.0.0.1")

	// Verify worker certificate was created with correct DNS names.
	workerCert := &certmanagerv1.Certificate{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-%s", utils.RayWorkerCertPrefix, cluster.Name),
		Namespace: "default",
	}, workerCert)
	require.NoError(t, err, "worker certificate should be created")
	assert.Contains(t, workerCert.Spec.DNSNames, "localhost")
	assert.Contains(t, workerCert.Spec.IPAddresses, "127.0.0.1")

	// Worker cert should include per-worker-group DNS names.
	for _, wg := range cluster.Spec.WorkerGroupSpecs {
		groupSvc := fmt.Sprintf("%s-%s", cluster.Name, wg.GroupName)
		assert.Contains(t, workerCert.Spec.DNSNames, groupSvc,
			"worker cert should include worker group %s DNS name", wg.GroupName)
	}

	// Verify all resources have correct labels.
	assert.Equal(t, cluster.Name, issuer.Labels[utils.RayClusterLabelKey])
	assert.Equal(t, cluster.Name, caCert.Labels[utils.RayClusterLabelKey])
	assert.Equal(t, cluster.Name, caIssuer.Labels[utils.RayClusterLabelKey])
	assert.Equal(t, cluster.Name, headCert.Labels[utils.RayClusterLabelKey])
	assert.Equal(t, cluster.Name, workerCert.Labels[utils.RayClusterLabelKey])
}

func TestMTLSController_AutoGenerate_Idempotent(t *testing.T) {
	cluster := newMTLSTestCluster("test-cluster")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{}

	r := newMTLSController(t, cluster)
	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}}

	// First reconcile creates resources.
	_, err := r.Reconcile(ctx, req)
	require.NoError(t, err)

	markMTLSCertificatesReady(ctx, t, r, cluster)

	// Second and third reconciles should be idempotent.
	_, err = r.Reconcile(ctx, req)
	require.NoError(t, err)
	_, err = r.Reconcile(ctx, req)
	require.NoError(t, err)

	// Verify exactly one of each resource type.
	issuerList := &certmanagerv1.IssuerList{}
	err = r.List(ctx, issuerList, client.InNamespace("default"))
	require.NoError(t, err)
	assert.Len(t, issuerList.Items, 2, "should have 2 issuers: self-signed + CA")

	certList := &certmanagerv1.CertificateList{}
	err = r.List(ctx, certList, client.InNamespace("default"))
	require.NoError(t, err)
	assert.Len(t, certList.Items, 3, "should have 3 certificates: CA + head + worker")
}

func TestMTLSController_AutoGenerate_UpdatesIPAddresses(t *testing.T) {
	cluster := newMTLSTestCluster("test-cluster")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-head-0",
			Namespace: "default",
			Labels:    map[string]string{utils.RayClusterLabelKey: cluster.Name},
		},
		Status: corev1.PodStatus{PodIP: "10.244.0.5"},
	}

	r := newMTLSController(t, cluster, pod)
	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}}

	_, err := r.Reconcile(ctx, req)
	require.NoError(t, err)

	markMTLSCertificatesReady(ctx, t, r, cluster)
	_, err = r.Reconcile(ctx, req)
	require.NoError(t, err)

	// Verify head certificate includes the pod IP.
	headCert := &certmanagerv1.Certificate{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-%s", utils.RayHeadCertPrefix, cluster.Name),
		Namespace: "default",
	}, headCert)
	require.NoError(t, err)
	assert.Contains(t, headCert.Spec.IPAddresses, "10.244.0.5")
	assert.Contains(t, headCert.Spec.IPAddresses, "127.0.0.1")

	// Simulate scale-up: add a second pod.
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-worker-0",
			Namespace: "default",
			Labels:    map[string]string{utils.RayClusterLabelKey: cluster.Name},
		},
		Status: corev1.PodStatus{PodIP: "10.244.0.6"},
	}
	require.NoError(t, r.Create(ctx, pod2))

	_, err = r.Reconcile(ctx, req)
	require.NoError(t, err)

	err = r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-%s", utils.RayHeadCertPrefix, cluster.Name),
		Namespace: "default",
	}, headCert)
	require.NoError(t, err)
	assert.Contains(t, headCert.Spec.IPAddresses, "10.244.0.5")
	assert.Contains(t, headCert.Spec.IPAddresses, "10.244.0.6",
		"head cert should be updated with the new pod IP after scale-up")
}

func TestMTLSController_Disabled_IsNoOp(t *testing.T) {
	cluster := newMTLSTestCluster("disabled-cluster")
	cluster.Spec.TLSOptions = nil

	// Pre-existing resources should NOT be touched when mTLS is disabled.
	// The controller simply returns without requeuing.
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: utils.GetCASecretName(cluster.Name, cluster.UID), Namespace: "default"},
	}

	r := newMTLSController(t, cluster, caSecret)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
	})
	require.NoError(t, err)
	assert.Zero(t, result.RequeueAfter, "disabled mTLS should not requeue")

	// The CA secret should still exist (no cleanup on disable).
	err = r.Get(ctx, types.NamespacedName{Name: caSecret.Name, Namespace: "default"}, &corev1.Secret{})
	require.NoError(t, err, "resources should not be deleted when mTLS is simply disabled")
}

func TestMTLSController_DeleteTLSSecrets(t *testing.T) {
	cluster := newMTLSTestCluster("del-cluster")
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: utils.GetCASecretName(cluster.Name, cluster.UID), Namespace: "default"},
	}
	headSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-%s", utils.RayHeadSecretPrefix, cluster.Name), Namespace: "default"},
	}
	workerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-%s", utils.RayWorkerSecretPrefix, cluster.Name), Namespace: "default"},
	}

	r := newMTLSController(t, cluster, caSecret, headSecret, workerSecret)
	ctx := context.Background()

	err := r.deleteTLSSecrets(ctx, cluster)
	require.NoError(t, err)

	for _, secret := range []*corev1.Secret{caSecret, headSecret, workerSecret} {
		err = r.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, &corev1.Secret{})
		require.Error(t, err, "secret %s should be deleted", secret.Name)
		assert.True(t, errors.IsNotFound(err), "secret %s should be deleted", secret.Name)
	}
}

func TestMTLSController_WorkerCertHasWildcardDNS(t *testing.T) {
	cluster := newMTLSTestCluster("wc-cluster")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{}

	r := newMTLSController(t, cluster)
	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}}

	_, err := r.Reconcile(ctx, req)
	require.NoError(t, err)

	markMTLSCertificatesReady(ctx, t, r, cluster)
	_, err = r.Reconcile(ctx, req)
	require.NoError(t, err)

	workerCert := &certmanagerv1.Certificate{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-%s", utils.RayWorkerCertPrefix, cluster.Name),
		Namespace: "default",
	}, workerCert)
	require.NoError(t, err)

	workerSvcName := fmt.Sprintf("%s-worker-svc", cluster.Name)
	assert.Contains(t, workerCert.Spec.DNSNames, workerSvcName)
	assert.Contains(t, workerCert.Spec.DNSNames,
		fmt.Sprintf("*.%s.%s.svc", workerSvcName, cluster.Namespace))
	assert.Contains(t, workerCert.Spec.DNSNames,
		fmt.Sprintf("*-worker-*.%s.svc", cluster.Namespace))
}

func TestMTLSController_CertReadinessBlocksReconciliation(t *testing.T) {
	cluster := newMTLSTestCluster("ready-cluster")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{}

	r := newMTLSController(t, cluster)
	ctx := context.Background()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}}

	// First reconcile adds the finalizer.
	result, err := r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.True(t, result.Requeue, "should requeue after adding finalizer")

	// Second reconcile creates resources but should requeue (certs not ready).
	result, err = r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, mtlsDefaultRequeueDuration, result.RequeueAfter, "should requeue when certs not ready")

	// Resources should still be created.
	headCert := &certmanagerv1.Certificate{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-%s", utils.RayHeadCertPrefix, cluster.Name),
		Namespace: "default",
	}, headCert)
	require.NoError(t, err, "head certificate should be created")

	// Mark only head cert ready; should still requeue (worker not ready).
	headCert.Status.Conditions = []certmanagerv1.CertificateCondition{
		{Type: certmanagerv1.CertificateConditionReady, Status: cmmeta.ConditionTrue},
	}
	require.NoError(t, r.Status().Update(ctx, headCert))

	result, err = r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, mtlsDefaultRequeueDuration, result.RequeueAfter, "should requeue when only head cert is ready")

	// Mark both certs ready; should now succeed with periodic check.
	markMTLSCertificatesReady(ctx, t, r, cluster)
	result, err = r.Reconcile(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, mtlsPeriodicCheckDuration, result.RequeueAfter, "should schedule periodic check when all ready")
}

// --- BYOC mode tests ---

func TestMTLSController_BYOC_ValidSecret(t *testing.T) {
	cluster := newMTLSTestCluster("byoc-cluster")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{
		CertificateSecretName: ptr.To("my-tls-secret"),
	}

	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-tls-secret", Namespace: "default"},
		Data: map[string][]byte{
			"tls.crt": []byte("cert-data"),
			"tls.key": []byte("key-data"),
			"ca.crt":  []byte("ca-data"),
		},
	}

	r := newMTLSController(t, cluster, userSecret)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
	})
	require.NoError(t, err)
	assert.Equal(t, mtlsPeriodicCheckDuration, result.RequeueAfter, "should schedule periodic check for BYOC")

	// Verify no cert-manager resources were created.
	issuerList := &certmanagerv1.IssuerList{}
	require.NoError(t, r.List(ctx, issuerList, client.InNamespace("default")))
	assert.Empty(t, issuerList.Items, "BYOC mode should not create cert-manager issuers")

	certList := &certmanagerv1.CertificateList{}
	require.NoError(t, r.List(ctx, certList, client.InNamespace("default")))
	assert.Empty(t, certList.Items, "BYOC mode should not create cert-manager certificates")
}

func TestMTLSController_BYOC_MissingSecret(t *testing.T) {
	cluster := newMTLSTestCluster("byoc-missing")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{
		CertificateSecretName: ptr.To("missing-secret"),
	}

	r := newMTLSController(t, cluster)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
	})
	require.NoError(t, err)
	assert.Equal(t, mtlsDefaultRequeueDuration, result.RequeueAfter, "should requeue when BYOC secret missing")
}

func TestMTLSController_BYOC_SecretMissingKey(t *testing.T) {
	cluster := newMTLSTestCluster("byoc-partial")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{
		CertificateSecretName: ptr.To("partial-secret"),
	}

	partialSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "partial-secret", Namespace: "default"},
		Data: map[string][]byte{
			"tls.crt": []byte("cert-data"),
			"tls.key": []byte("key-data"),
			// missing ca.crt
		},
	}

	r := newMTLSController(t, cluster, partialSecret)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
	})
	require.NoError(t, err)
	assert.Equal(t, mtlsDefaultRequeueDuration, result.RequeueAfter, "should requeue when BYOC secret missing keys")
}

func TestMTLSController_BYOC_DeletionDoesNotDeleteUserSecret(t *testing.T) {
	now := metav1.Now()
	cluster := newMTLSTestCluster("byoc-del")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{
		CertificateSecretName: ptr.To("my-company-cert"),
	}
	cluster.DeletionTimestamp = &now
	cluster.Finalizers = []string{"test-finalizer"}

	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-company-cert", Namespace: "default"},
		Data: map[string][]byte{
			"tls.crt": []byte("cert"), "tls.key": []byte("key"), "ca.crt": []byte("ca"),
		},
	}

	r := newMTLSController(t, cluster, userSecret)
	ctx := context.Background()

	_, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
	})
	require.NoError(t, err)

	// User's secret must still exist after reconciliation during deletion.
	err = r.Get(ctx, types.NamespacedName{Name: "my-company-cert", Namespace: "default"}, &corev1.Secret{})
	require.NoError(t, err, "BYOC secret should NOT be deleted when RayCluster is deleted")
}

func TestMTLSController_BYOC_SeparateHeadWorkerSecrets(t *testing.T) {
	cluster := newMTLSTestCluster("byoc-separate")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{
		CertificateSecretName:       ptr.To("my-head-secret"),
		WorkerCertificateSecretName: ptr.To("my-worker-secret"),
	}

	headSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-head-secret", Namespace: "default"},
		Data: map[string][]byte{
			"tls.crt": []byte("head-cert"), "tls.key": []byte("head-key"), "ca.crt": []byte("ca"),
		},
	}
	workerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-worker-secret", Namespace: "default"},
		Data: map[string][]byte{
			"tls.crt": []byte("worker-cert"), "tls.key": []byte("worker-key"), "ca.crt": []byte("ca"),
		},
	}

	r := newMTLSController(t, cluster, headSecret, workerSecret)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
	})
	require.NoError(t, err)
	assert.Equal(t, mtlsPeriodicCheckDuration, result.RequeueAfter, "should schedule periodic check for BYOC with separate secrets")
}

func TestMTLSController_BYOC_SeparateSecrets_WorkerMissing(t *testing.T) {
	cluster := newMTLSTestCluster("byoc-worker-missing")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{
		CertificateSecretName:       ptr.To("my-head-secret"),
		WorkerCertificateSecretName: ptr.To("my-worker-secret"),
	}

	// Only create the head secret; worker secret is missing.
	headSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-head-secret", Namespace: "default"},
		Data: map[string][]byte{
			"tls.crt": []byte("head-cert"), "tls.key": []byte("head-key"), "ca.crt": []byte("ca"),
		},
	}

	r := newMTLSController(t, cluster, headSecret)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
	})
	require.NoError(t, err)
	assert.Equal(t, mtlsDefaultRequeueDuration, result.RequeueAfter, "should requeue when BYOC worker secret is missing")
}

func TestMTLSController_DeletionWithDisabledMTLS_RemovesFinalizer(t *testing.T) {
	now := metav1.Now()
	cluster := newMTLSTestCluster("disabled-del")
	// mTLS was previously enabled (finalizer present) but user disabled it before deleting.
	cluster.Spec.TLSOptions = nil
	cluster.DeletionTimestamp = &now
	cluster.Finalizers = []string{mtlsFinalizer}

	r := newMTLSController(t, cluster)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
	})
	require.NoError(t, err)
	assert.Zero(t, result.RequeueAfter, "should not requeue after finalizer removal")

	// The cluster should be fully deleted (fake client removes it when last finalizer is removed
	// and DeletionTimestamp is set).
	updatedCluster := &rayv1.RayCluster{}
	err = r.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, updatedCluster)
	assert.True(t, errors.IsNotFound(err), "cluster should be fully deleted after finalizer removal with disabled mTLS")
}

func TestMTLSController_AutoGenerate_DeletionCleansUpSecrets(t *testing.T) {
	now := metav1.Now()
	cluster := newMTLSTestCluster("auto-del")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{}
	cluster.DeletionTimestamp = &now
	// Simulate that the finalizer was previously added during normal reconciliation.
	cluster.Finalizers = []string{mtlsFinalizer}

	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: utils.GetCASecretName(cluster.Name, cluster.UID), Namespace: "default"},
	}
	headSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-%s", utils.RayHeadSecretPrefix, cluster.Name), Namespace: "default"},
	}
	workerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-%s", utils.RayWorkerSecretPrefix, cluster.Name), Namespace: "default"},
	}

	r := newMTLSController(t, cluster, caSecret, headSecret, workerSecret)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
	})
	require.NoError(t, err)
	assert.Zero(t, result.RequeueAfter, "should not requeue after cleanup")

	// All auto-generated secrets should be deleted.
	for _, secret := range []*corev1.Secret{caSecret, headSecret, workerSecret} {
		err = r.Get(ctx, types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, &corev1.Secret{})
		require.Error(t, err, "secret %s should be deleted", secret.Name)
		assert.True(t, errors.IsNotFound(err), "secret %s should be deleted", secret.Name)
	}

	// After removing the last finalizer with DeletionTimestamp set, Kubernetes (and the
	// fake client) deletes the object. Verify the cluster is gone, which proves the
	// finalizer was removed successfully.
	updatedCluster := &rayv1.RayCluster{}
	err = r.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, updatedCluster)
	assert.True(t, errors.IsNotFound(err), "cluster should be fully deleted after finalizer removal")
}

// --- Utility function tests ---

func TestSetsEqual(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []string
		expected bool
	}{
		{"both empty", nil, nil, true},
		{"equal same order", []string{"a", "b"}, []string{"a", "b"}, true},
		{"equal different order", []string{"b", "a"}, []string{"a", "b"}, true},
		{"different lengths", []string{"a"}, []string{"a", "b"}, false},
		{"different contents", []string{"a", "c"}, []string{"a", "b"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, setsEqual(tc.a, tc.b))
		})
	}
}

func TestCertificateNeedsUpdate(t *testing.T) {
	existing := &certmanagerv1.CertificateSpec{
		DNSNames:    []string{"b.svc", "a.svc"},
		IPAddresses: []string{"10.0.0.2", "10.0.0.1"},
	}
	desired := &certmanagerv1.CertificateSpec{
		DNSNames:    []string{"a.svc", "b.svc"},
		IPAddresses: []string{"10.0.0.1", "10.0.0.2"},
	}
	assert.False(t, certificateNeedsUpdate(existing, desired), "same sets in different order should not need update")

	desired2 := &certmanagerv1.CertificateSpec{
		DNSNames:    []string{"a.svc", "c.svc"},
		IPAddresses: []string{"10.0.0.1", "10.0.0.2"},
	}
	assert.True(t, certificateNeedsUpdate(existing, desired2), "different DNS names should need update")
}

// --- checkMTLSSecretsReady tests (in main reconciler) ---

func TestCheckMTLSSecretsReady_AutoGenerate_SecretsPresent(t *testing.T) {
	cluster := newMTLSTestCluster("test-cluster")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{}

	headSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", utils.RayHeadSecretPrefix, cluster.Name),
			Namespace: "default",
		},
		Data: map[string][]byte{
			"tls.crt": []byte("cert"),
			"tls.key": []byte("key"),
			"ca.crt":  []byte("ca"),
		},
	}
	workerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", utils.RayWorkerSecretPrefix, cluster.Name),
			Namespace: "default",
		},
		Data: map[string][]byte{
			"tls.crt": []byte("cert"),
			"tls.key": []byte("key"),
			"ca.crt":  []byte("ca"),
		},
	}

	s := newMTLSTestScheme()
	fakeClient := clientFake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(cluster, headSecret, workerSecret).
		Build()
	r := &RayClusterReconciler{Client: fakeClient, Scheme: s}

	err := r.checkMTLSSecretsReady(context.Background(), cluster)
	assert.NoError(t, err, "should succeed when all secrets present with correct keys")
}

func TestCheckMTLSSecretsReady_AutoGenerate_SecretMissing(t *testing.T) {
	cluster := newMTLSTestCluster("test-cluster")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{}

	s := newMTLSTestScheme()
	fakeClient := clientFake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(cluster).
		Build()
	r := &RayClusterReconciler{Client: fakeClient, Scheme: s}

	err := r.checkMTLSSecretsReady(context.Background(), cluster)
	require.Error(t, err, "should fail when secrets are missing")
	assert.Contains(t, err.Error(), "not found")
}

func TestCheckMTLSSecretsReady_AutoGenerate_SecretMissingKey(t *testing.T) {
	cluster := newMTLSTestCluster("test-cluster")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{}

	// Head secret missing ca.crt key.
	headSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", utils.RayHeadSecretPrefix, cluster.Name),
			Namespace: "default",
		},
		Data: map[string][]byte{
			"tls.crt": []byte("cert"),
			"tls.key": []byte("key"),
		},
	}
	workerSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", utils.RayWorkerSecretPrefix, cluster.Name),
			Namespace: "default",
		},
		Data: map[string][]byte{
			"tls.crt": []byte("cert"),
			"tls.key": []byte("key"),
			"ca.crt":  []byte("ca"),
		},
	}

	s := newMTLSTestScheme()
	fakeClient := clientFake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(cluster, headSecret, workerSecret).
		Build()
	r := &RayClusterReconciler{Client: fakeClient, Scheme: s}

	err := r.checkMTLSSecretsReady(context.Background(), cluster)
	require.Error(t, err, "should fail when secret is missing a required key")
	assert.Contains(t, err.Error(), "ca.crt")
}

func TestCheckMTLSSecretsReady_BYOC(t *testing.T) {
	features.SetFeatureGateDuringTest(t, features.EnhancedSecurityPrimitives, true)
	cluster := newMTLSTestCluster("test-cluster")
	cluster.Spec.TLSOptions = &rayv1.TLSOptions{
		CertificateSecretName: ptr.To("my-cert"),
	}

	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-cert", Namespace: "default"},
		Data: map[string][]byte{
			"tls.crt": []byte("cert"),
			"tls.key": []byte("key"),
			"ca.crt":  []byte("ca"),
		},
	}

	s := newMTLSTestScheme()
	fakeClient := clientFake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(cluster, userSecret).
		Build()
	r := &RayClusterReconciler{Client: fakeClient, Scheme: s}

	err := r.checkMTLSSecretsReady(context.Background(), cluster)
	assert.NoError(t, err, "BYOC mode should succeed with valid user-provided secret")
}

// --- podHasMTLSConfiguration tests ---

func TestPodHasMTLSConfiguration_WithConfig(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{Name: utils.RayTLSVolumeName},
			},
			Containers: []corev1.Container{
				{
					Name: "ray-head",
					Env: []corev1.EnvVar{
						{Name: utils.RAY_USE_TLS, Value: "1"},
					},
				},
			},
		},
	}
	assert.True(t, podHasMTLSConfiguration(pod), "should detect mTLS configuration")
}

func TestPodHasMTLSConfiguration_WithoutConfig(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "ray-head"},
			},
		},
	}
	assert.False(t, podHasMTLSConfiguration(pod), "should not detect mTLS without volume and env")
}

func TestPodHasMTLSConfiguration_VolumeOnly(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{Name: utils.RayTLSVolumeName},
			},
			Containers: []corev1.Container{
				{Name: "ray-head"},
			},
		},
	}
	assert.False(t, podHasMTLSConfiguration(pod), "should not detect mTLS with volume only")
}

func TestPodHasMTLSConfiguration_EnvOnly(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "ray-head",
					Env: []corev1.EnvVar{
						{Name: utils.RAY_USE_TLS, Value: "1"},
					},
				},
			},
		},
	}
	assert.False(t, podHasMTLSConfiguration(pod), "should not detect mTLS with env only")
}

// --- Cluster not found test ---

func TestMTLSController_ClusterNotFound(t *testing.T) {
	r := newMTLSController(t)
	ctx := context.Background()

	result, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: "default"},
	})
	require.NoError(t, err, "should not error on not found")
	assert.Equal(t, ctrl.Result{}, result)
}
