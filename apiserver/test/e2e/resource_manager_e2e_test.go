package e2e

import (
	"context"
	"testing"

	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"

	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

func TestCreateCluster(t *testing.T) {
	ctx := context.Background()
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})

	// Create cluster
	cluster := &api.Cluster{
		Name:      tCtx.GetNextName(),
		Namespace: tCtx.GetNamespaceName(),
		ClusterSpec: &api.ClusterSpec{
			HeadGroupSpec: &api.HeadGroupSpec{
				ComputeTemplate: tCtx.GetComputeTemplateName(),
				Image:           tCtx.GetRayImage(),
				RayStartParams: map[string]string{
					"dashboard-host":      "0.0.0.0",
					"metrics-export-port": "8080",
				},
			},
			WorkerGroupSpec: []*api.WorkerGroupSpec{
				{
					GroupName:       "small-wg",
					ComputeTemplate: tCtx.GetComputeTemplateName(),
					RayStartParams: map[string]string{
						"node-ip-address": "$MY_POD_IP",
					},
				},
			},
		},
	}

	clientManager := manager.NewClientManager()
	resourceManager := manager.NewResourceManager(&clientManager)
	rayCluster, err := resourceManager.CreateCluster(ctx, cluster)
	require.NoError(t, err)

	assert.Equal(t, cluster.Name, rayCluster.ObjectMeta.Name)
	assert.Equal(t, tCtx.GetNamespaceName(), rayCluster.Namespace)
	assert.Equal(t, tCtx.GetComputeTemplateName(), rayCluster.Spec.HeadGroupSpec.Template.ObjectMeta.Annotations[util.RayClusterComputeTemplateAnnotationKey])
	assert.Equal(t, "ray-head", rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Name)
	assert.True(t, rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Cpu().Equal(resource.MustParse("2")))
	assert.True(t, rayCluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Resources.Requests.Memory().Equal(resource.MustParse("4Gi")))

	assert.Equal(t, "ray-worker", rayCluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Name)
	assert.Equal(t, resource.MustParse("2"), *rayCluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Cpu())
	assert.Equal(t, resource.MustParse("4Gi"), *rayCluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Resources.Requests.Memory())
}

func TestListClusters(t *testing.T) {
	ctx := context.Background()
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})

	clusterSpec := &api.ClusterSpec{
		HeadGroupSpec: &api.HeadGroupSpec{
			ComputeTemplate: tCtx.GetComputeTemplateName(),
			Image:           tCtx.GetRayImage(),
			RayStartParams: map[string]string{
				"dashboard-host":      "0.0.0.0",
				"metrics-export-port": "8080",
			},
		},
	}

	// Create cluster
	// TODO: need to add compute template string here to prevent panic
	clustersList := []*api.Cluster{
		{
			Name:        tCtx.GetNextName(),
			Namespace:   tCtx.GetNamespaceName(),
			ClusterSpec: clusterSpec,
		},
		{
			Name:        tCtx.GetNextName(),
			Namespace:   tCtx.GetNamespaceName(),
			ClusterSpec: clusterSpec,
		},
		{
			Name:        tCtx.GetNextName(),
			Namespace:   tCtx.GetNamespaceName(),
			ClusterSpec: clusterSpec,
		},
		{
			Name:        tCtx.GetNextName(),
			Namespace:   tCtx.GetNamespaceName(),
			ClusterSpec: clusterSpec,
		},
	}

	clientManager := manager.NewClientManager()
	resourceManager := manager.NewResourceManager(&clientManager)
	for _, cluster := range clustersList {
		_, err := resourceManager.CreateCluster(ctx, cluster)
		require.NoError(t, err)
	}

	// Verify returning 2 clusters each time using continue token
	resRayClusters, continueToken, err := resourceManager.ListClusters(ctx, tCtx.GetNamespaceName(), "", 2)
	require.NoError(t, err)
	assert.Len(t, resRayClusters, 2)

	resRayClusters, _, err = resourceManager.ListClusters(ctx, tCtx.GetNamespaceName(), continueToken, 2)
	require.NoError(t, err)
	assert.Len(t, resRayClusters, 2)
}

func TestResourceManagerDeleteCluster(t *testing.T) {
	ctx := context.Background()
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})

	tCtx.CreateRayClusterWithConfigMaps(t, map[string]string{
		"counter_sample.py": ReadFileAsString(t, "resources/counter_sample.py"),
		"fail_fast.py":      ReadFileAsString(t, "resources/fail_fast_sample.py"),
	}, []rayv1api.RayClusterConditionType{})

	clientManager := manager.NewClientManager()
	resourceManager := manager.NewResourceManager(&clientManager)

	type clusterInfo struct {
		clusterName string
		namespace   string
	}

	tests := []struct {
		name                string
		input               *clusterInfo
		expectedErrorString string
	}{
		{
			name: "Delete an existing cluster",
			input: &clusterInfo{
				clusterName: tCtx.GetRayClusterName(),
				namespace:   tCtx.GetNamespaceName(),
			},
			expectedErrorString: "",
		},
		{
			name: "Delete a non existing cluster",
			input: &clusterInfo{
				clusterName: "bogus-cluster-name",
				namespace:   tCtx.GetNamespaceName(),
			},
			expectedErrorString: "NotFoundError",
		},
		{
			name: "Delete a cluster with no namespace",
			input: &clusterInfo{
				clusterName: "bogus-cluster-name",
				namespace:   "",
			},
			expectedErrorString: "NotFoundError",
		},
		{
			name: "Delete a cluster with no name",
			input: &clusterInfo{
				clusterName: "",
				namespace:   tCtx.GetNamespaceName(),
			},
			expectedErrorString: "resource name may not be empty",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := resourceManager.DeleteCluster(ctx, tc.input.clusterName, tc.input.namespace)
			if tc.expectedErrorString == "" {
				require.NoError(t, err, "No error expected")
			} else {
				require.ErrorContains(t, err, tc.expectedErrorString)
			}
		})
	}
}
