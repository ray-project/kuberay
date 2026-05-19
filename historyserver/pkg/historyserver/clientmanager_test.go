package historyserver

import (
	"context"
	"testing"
	"time"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetAuthTokenForCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	clusterName := "test-cluster"
	namespace := "default"
	secretKey := "test-token-123"
	cacheKey := namespace + "/" + clusterName

	rc := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: rayv1.RayClusterSpec{
			AuthOptions: &rayv1.AuthOptions{Mode: rayv1.AuthModeToken},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Data: map[string][]byte{AuthTokenSecretKey: []byte(secretKey)},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(rc, secret).
		Build()

	clientManager := &ClientManager{
		clients:      []client.Client{fakeClient},
		tokenCache:   NewTTLCache[string](authTokenCacheTTL),
		svcInfoCache: NewTTLCache[svcInfoEntry](svcInfoCacheTTL),
	}

	token, err := clientManager.GetAuthTokenForRayCluster(context.Background(), rc)
	assert.NoError(t, err)
	assert.Equal(t, secretKey, token)

	// Second call should be served from cache (fake client still has same secret, result unchanged)
	token, err = clientManager.GetAuthTokenForRayCluster(context.Background(), rc)
	assert.NoError(t, err)
	assert.Equal(t, secretKey, token)

	// Expired cache entry should trigger a re-fetch from K8s
	clientManager.tokenCache.SetWithExpiry(cacheKey, "stale-token", time.Now().Add(-1*time.Second))
	token, err = clientManager.GetAuthTokenForRayCluster(context.Background(), rc)
	assert.NoError(t, err)
	assert.Equal(t, secretKey, token)

	// Auth disabled returns empty token without error
	rcAuthDisabled := rc.DeepCopy()
	rcAuthDisabled.Spec.AuthOptions = &rayv1.AuthOptions{Mode: rayv1.AuthModeDisabled}
	token, err = clientManager.GetAuthTokenForRayCluster(context.Background(), rcAuthDisabled)
	assert.NoError(t, err)
	assert.Equal(t, "", token)

	// Nil RayCluster should error
	_, err = clientManager.GetAuthTokenForRayCluster(context.Background(), nil)
	assert.Error(t, err)
}

func TestGetClusterAndSvcInfo(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	clusterName := "test-cluster"
	namespace := "default"
	serviceName := "test-cluster-head-svc"
	cacheKey := namespace + "/" + clusterName

	portalPort := int32(8265)

	rc := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Status: rayv1.RayClusterStatus{
			Head: rayv1.HeadInfo{
				ServiceName: serviceName,
			},
		},
	}
	headSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       DashboardPortName,
					Port:       portalPort,
					TargetPort: intstr.FromInt32(portalPort),
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(rc, headSvc).
		WithStatusSubresource(rc).
		Build()

	// Set the status on the fake client (some builders require this separately).
	_ = fakeClient.Status().Update(context.Background(), rc)

	clientManager := &ClientManager{
		clients:      []client.Client{fakeClient},
		tokenCache:   NewTTLCache[string](authTokenCacheTTL),
		svcInfoCache: NewTTLCache[svcInfoEntry](svcInfoCacheTTL),
	}

	// First call should fetch from K8s and populate cache.
	svcInfo, retRC, err := clientManager.GetClusterAndSvcInfo(clusterName, namespace)
	assert.NoError(t, err)
	assert.Equal(t, serviceName, svcInfo.ServiceName)
	assert.Equal(t, namespace, svcInfo.Namespace)
	assert.Equal(t, int(portalPort), svcInfo.Port)
	assert.Equal(t, clusterName, retRC.Name)

	// Second call should be served from cache.
	svcInfo2, retRC2, err := clientManager.GetClusterAndSvcInfo(clusterName, namespace)
	assert.NoError(t, err)
	assert.Equal(t, svcInfo, svcInfo2)
	assert.Equal(t, retRC.Name, retRC2.Name)

	// Expired cache entry should trigger a re-fetch.
	clientManager.svcInfoCache.SetWithExpiry(cacheKey, svcInfoEntry{
		svcInfo: ServiceInfo{
			ServiceName: "stale-svc",
			Namespace:   namespace,
			Port:        int(portalPort),
		},
		rc: rc,
	}, time.Now().Add(-1*time.Second))

	svcInfo3, _, err := clientManager.GetClusterAndSvcInfo(clusterName, namespace)
	assert.NoError(t, err)
	assert.Equal(t, serviceName, svcInfo3.ServiceName)
	assert.Equal(t, int(portalPort), svcInfo3.Port)

	// Non-existent cluster should error.
	_, _, err = clientManager.GetClusterAndSvcInfo("not-exists", namespace)
	assert.Error(t, err)

	// No clients should error.
	emptyMgr := &ClientManager{
		clients:      []client.Client{},
		svcInfoCache: NewTTLCache[svcInfoEntry](svcInfoCacheTTL),
	}
	_, _, err = emptyMgr.GetClusterAndSvcInfo(clusterName, namespace)
	assert.Error(t, err)
}
