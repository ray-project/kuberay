package runtime

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/kube"
)

func TestResolve(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(scheme)

	trueVar := true

	tests := []struct {
		name           string
		clusterName    string
		namespace      string
		existingObject *rayv1.RayCluster
		expectedKind   string
		expectedName   string
		expectErr      bool
	}{
		{
			name:        "RayJob owner",
			clusterName: "my-cluster",
			namespace:   "default",
			existingObject: &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cluster",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind:       "RayJob",
							Name:       "my-job",
							Controller: &trueVar,
						},
					},
				},
			},
			expectedKind: "RayJob",
			expectedName: "my-job",
			expectErr:    false,
		},
		{
			name:        "RayService owner",
			clusterName: "my-service-cluster",
			namespace:   "default",
			existingObject: &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-service-cluster",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind:       "RayService",
							Name:       "my-service",
							Controller: &trueVar,
						},
					},
				},
			},
			expectedKind: "RayService",
			expectedName: "my-service",
			expectErr:    false,
		},
		{
			name:        "No owner",
			clusterName: "standalone-cluster",
			namespace:   "default",
			existingObject: &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "standalone-cluster",
					Namespace: "default",
				},
			},
			expectedKind: "",
			expectedName: "",
			expectErr:    false,
		},
		{
			name:        "Cluster not found",
			clusterName: "non-existent",
			namespace:   "default",
			expectErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.existingObject != nil {
				builder = builder.WithObjects(tt.existingObject)
			}
			fakeClient := builder.Build()
			kubeClient := kube.NewKubeClient(fakeClient)

			resolver := NewLogCollectorOwnerResolver(kubeClient)

			kind, name, err := resolver.Resolve(context.Background(), tt.namespace, tt.clusterName)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Resolve expected error but got none")
				}
			} else {
				if err != nil {
					t.Fatalf("Resolve failed unexpectedly: %v", err)
				}
				if kind != tt.expectedKind {
					t.Errorf("Expected kind %q, got %q", tt.expectedKind, kind)
				}
				if name != tt.expectedName {
					t.Errorf("Expected name %q, got %q", tt.expectedName, name)
				}
			}
		})
	}
}
