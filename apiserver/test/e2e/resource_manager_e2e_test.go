package e2e

import (
	"context"
	"testing"

	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	api "github.com/ray-project/kuberay/proto/go_client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPopulateComputeTemplate(t *testing.T) {
	ctx := context.Background()
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	tCtx.CreateComputeTemplateCustomName(t, "worker-1")
	tCtx.CreateComputeTemplateCustomName(t, "worker-2")
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
		tCtx.DeleteComputeTemplateCustomName(t, "worker-1")
		tCtx.DeleteComputeTemplateCustomName(t, "worker-2")
	})
	clusterSpec := &api.ClusterSpec{
		HeadGroupSpec: &api.HeadGroupSpec{
			ComputeTemplate: tCtx.GetComputeTemplateName(),
		},

		WorkerGroupSpec: []*api.WorkerGroupSpec{
			{
				ComputeTemplate: "worker-1",
			},
			{
				ComputeTemplate: "worker-2",
			},
		},
	}

	clientManager := manager.NewClientManager()
	resourceManager := manager.NewResourceManager(&clientManager)

	// Call the method
	computeTemplates, err := resourceManager.PopulateComputeTemplate(ctx, clusterSpec, tCtx.namespaceName)

	// Assertions
	require.NoError(t, err)
	assert.Len(t, computeTemplates, 3)
	assert.Contains(t, computeTemplates, tCtx.GetComputeTemplateName())
	assert.Contains(t, computeTemplates, "worker-1")
	assert.Contains(t, computeTemplates, "worker-2")
}
