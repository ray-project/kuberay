package historyserver

import (
	"context"
	"testing"
	"time"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		svcInfoCache: NewTTLCache[ServiceInfo](svcInfoCacheTTL),
	}

	token, err := clientManager.GetAuthTokenForRayCluster(context.Background(), namespace, clusterName)
	assert.NoError(t, err)
	assert.Equal(t, secretKey, token)

	// Second call should be served from cache (fake client still has same secret, result unchanged)
	token, err = clientManager.GetAuthTokenForRayCluster(context.Background(), namespace, clusterName)
	assert.NoError(t, err)
	assert.Equal(t, secretKey, token)

	// Expired cache entry should trigger a re-fetch from K8s
	clientManager.tokenCache.SetWithExpiry(cacheKey, "stale-token", time.Now().Add(-1*time.Second))
	token, err = clientManager.GetAuthTokenForRayCluster(context.Background(), namespace, clusterName)
	assert.NoError(t, err)
	assert.Equal(t, secretKey, token)

	// Auth disabled returns empty token without error
	rcAuthDisabled := rc.DeepCopy()
	rcAuthDisabled.Spec.AuthOptions = &rayv1.AuthOptions{Mode: rayv1.AuthModeDisabled}
	require.NoError(t, fakeClient.Update(context.Background(), rcAuthDisabled))
	token, err = clientManager.GetAuthTokenForRayCluster(context.Background(), namespace, clusterName)
	assert.NoError(t, err)
	assert.Equal(t, "", token)

	// Non-existent cluster should error (spec is read fresh from K8s)
	_, err = clientManager.GetAuthTokenForRayCluster(context.Background(), namespace, "not-exists")
	assert.Error(t, err)
}

func TestGetSvcInfo(t *testing.T) {
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
		svcInfoCache: NewTTLCache[ServiceInfo](svcInfoCacheTTL),
	}

	// First call should fetch from K8s and populate cache.
	svcInfo, err := clientManager.GetSvcInfo(clusterName, namespace)
	assert.NoError(t, err)
	assert.Equal(t, serviceName, svcInfo.ServiceName)
	assert.Equal(t, namespace, svcInfo.Namespace)
	assert.Equal(t, int(portalPort), svcInfo.Port)

	// Second call should be served from cache.
	svcInfo2, err := clientManager.GetSvcInfo(clusterName, namespace)
	assert.NoError(t, err)
	assert.Equal(t, svcInfo, svcInfo2)

	// Expired cache entry should trigger a re-fetch.
	clientManager.svcInfoCache.SetWithExpiry(cacheKey, ServiceInfo{
		ServiceName: "stale-svc",
		Namespace:   namespace,
		Port:        int(portalPort),
	}, time.Now().Add(-1*time.Second))

	svcInfo3, err := clientManager.GetSvcInfo(clusterName, namespace)
	assert.NoError(t, err)
	assert.Equal(t, serviceName, svcInfo3.ServiceName)
	assert.Equal(t, int(portalPort), svcInfo3.Port)

	// Non-existent cluster should error.
	_, err = clientManager.GetSvcInfo("not-exists", namespace)
	assert.Error(t, err)

	// No clients should error.
	emptyMgr := &ClientManager{
		clients:      []client.Client{},
		svcInfoCache: NewTTLCache[ServiceInfo](svcInfoCacheTTL),
	}
	_, err = emptyMgr.GetSvcInfo(clusterName, namespace)
	assert.Error(t, err)
}
