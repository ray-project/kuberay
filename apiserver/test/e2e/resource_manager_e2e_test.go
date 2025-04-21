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
)

// Ensure the api.ClusterSpec
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
