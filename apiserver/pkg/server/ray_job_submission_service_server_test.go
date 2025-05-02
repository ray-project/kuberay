package server

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"

	"github.com/ray-project/kuberay/apiserver/pkg/client"
	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	fakeclientset "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/fake"
)

func TestGetRayClusterURL(t *testing.T) {
	ctx := context.Background()

	namespace := "test-namespace"
	clusterName := "test-raycluster"

	expectedRayCluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
			Labels: map[string]string{
				util.KubernetesManagedByLabelKey: util.ComponentName,
			},
		},
		Status: rayv1.RayClusterStatus{
			// TODO: test case without this, err: not ready
			State: rayv1.Ready,
		},
		// TODO: test case without this `Spec`, will panic
		Spec: rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								// TODO: test case with/without this (set as different value)
								// converter.go line 263 (in PopulateHeadNodeSpec)
								ImagePullPolicy: corev1.PullAlways,
							},
						},
					},
				},
			},
		},
	}

	expectedEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ray-event-1",
			Namespace: namespace,
		},
	}

	getClusterRequest := &api.GetClusterRequest{
		Name:      clusterName,
		Namespace: namespace,
	}

	expectedURL := clusterName + "-head-svc." + namespace + ".svc.cluster.local:8265"

	// create fake ray cluster
	fakeClient := fakeclientset.NewSimpleClientset(expectedRayCluster)
	fakeRayCluster := fakeClient.RayV1().RayClusters(namespace)

	// mock controller
	ctrl := gomock.NewController(t)

	// mocking r.clientManager.ClusterClient().RayClusterClient(namespace)
	mockClientManager := manager.NewMockClientManagerInterface(ctrl)
	mockClusterClient := client.NewMockClusterClientInterface(ctrl)
	mockKubeClient := client.NewMockKubernetesClientInterface(ctrl)
	// mock return of RayClusterClient(namespace)
	mockClusterClient.EXPECT().RayClusterClient(namespace).Return(fakeRayCluster).Times(2)
	// mock return of clientManager.ClusterClient()
	mockClientManager.EXPECT().ClusterClient().Return(mockClusterClient).Times(2)
	mockClientManager.EXPECT().KubernetesClient().Return(mockKubeClient).Times(1)

	// mock config map client
	fakeClientset := kubernetesfake.NewClientset(expectedEvent)
	fakeEvents := fakeClientset.CoreV1().Events(namespace)
	mockKubeClient.EXPECT().EventsClient(namespace).Return(fakeEvents).Times(1)

	resourceManager := manager.NewResourceManager(mockClientManager)

	rayJobSubmissionService := NewRayJobSubmissionServiceServer(
		&ClusterServer{
			resourceManager: resourceManager,
			options:         &ClusterServerOptions{},
		}, &RayJobSubmissionServiceServerOptions{},
	)

	url, err := rayJobSubmissionService.getRayClusterURL(ctx, getClusterRequest)
	require.NoError(t, err)
	assert.Equal(t, expectedURL, *url)
}
