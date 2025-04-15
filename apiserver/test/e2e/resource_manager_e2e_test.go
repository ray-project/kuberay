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
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	clusterSpec := &api.ClusterSpec{
		HeadGroupSpec: &api.HeadGroupSpec{
			ComputeTemplate: tCtx.GetComputeTemplateName(),
		},
	}

	clientManager := manager.NewClientManager()
	resourceManager := manager.NewResourceManager(&clientManager)

	// Call the method
	computeTemplates, err := resourceManager.PopulateComputeTemplate(ctx, clusterSpec, tCtx.namespaceName)

	// Assertions
	require.NoError(t, err)
	// assert.NoError(t, err)
	assert.Len(t, computeTemplates, 1)
	assert.Contains(t, computeTemplates, tCtx.GetComputeTemplateName())
}
