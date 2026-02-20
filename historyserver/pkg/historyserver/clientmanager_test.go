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

func TestGetAuthToken(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name             string
		clusterName      string
		clusterNamespace string
		objects          []client.Object
		expectedToken    string
		expectError      bool
		errorContains    string
	}{
		{
			name:             "auth enabled with valid token",
			clusterName:      "test-cluster",
			clusterNamespace: "default",
			objects: []client.Object{
				&rayv1.RayCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster",
						Namespace: "default",
					},
					Spec: rayv1.RayClusterSpec{
						AuthOptions: &rayv1.AuthOptions{
							Mode: rayv1.AuthModeToken,
						},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster",
						Namespace: "default",
					},
					Data: map[string][]byte{
						"auth_token": []byte("test-token-123"),
					},
				},
			},
			expectedToken: "test-token-123",
			expectError:   false,
		},
		{
			name:             "auth disabled",
			clusterName:      "test-cluster",
			clusterNamespace: "default",
			objects: []client.Object{
				&rayv1.RayCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster",
						Namespace: "default",
					},
					Spec: rayv1.RayClusterSpec{
						AuthOptions: &rayv1.AuthOptions{
							Mode: rayv1.AuthModeDisabled,
						},
					},
				},
			},
			expectedToken: "",
			expectError:   false,
		},
		{
			name:             "auth enabled but secret not found",
			clusterName:      "test-cluster",
			clusterNamespace: "default",
			objects: []client.Object{
				&rayv1.RayCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster",
						Namespace: "default",
					},
					Spec: rayv1.RayClusterSpec{
						AuthOptions: &rayv1.AuthOptions{
							Mode: rayv1.AuthModeToken,
						},
					},
				},
			},
			expectError:   true,
			errorContains: "failed to get auth secret",
		},
		{
			name:             "cluster not found",
			clusterName:      "non-existent",
			clusterNamespace: "default",
			objects:          []client.Object{},
			expectError:      true,
			errorContains:    "failed to get RayCluster",
		},
		{
			name:             "no auth options specified",
			clusterName:      "test-cluster",
			clusterNamespace: "default",
			objects: []client.Object{
				&rayv1.RayCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cluster",
						Namespace: "default",
					},
					Spec: rayv1.RayClusterSpec{},
				},
			},
			expectedToken: "",
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				Build()

			clientManager := &ClientManager{
				clients: []client.Client{fakeClient},
			}

			token, err := clientManager.GetAuthToken(context.Background(), tt.clusterName, tt.clusterNamespace)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedToken, token)
			}
		})
	}
}
