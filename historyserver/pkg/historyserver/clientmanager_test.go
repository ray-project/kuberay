package historyserver

import (
	"context"
	"testing"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetAuthTokenForCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	rc := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: rayv1.RayClusterSpec{
			AuthOptions: &rayv1.AuthOptions{Mode: rayv1.AuthModeToken},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Data: map[string][]byte{"auth_token": []byte("test-token-123")},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(rc, secret).
		Build()

	clientManager := &ClientManager{clients: []client.Client{fakeClient}}

	token, err := clientManager.GetAuthTokenForRayCluster(context.Background(), rc)
	assert.NoError(t, err)
	assert.Equal(t, "test-token-123", token)

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
