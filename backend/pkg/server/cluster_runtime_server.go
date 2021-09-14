package server

import (
	"context"

	api "github.com/ray-project/kuberay/api/go_client"
	"github.com/ray-project/kuberay/backend/pkg/manager"
	"github.com/ray-project/kuberay/backend/pkg/model/converter"
	"github.com/ray-project/kuberay/backend/pkg/util"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ClusterRuntimeServerOptions struct {
	CollectMetrics bool
}

// implements `type ClusterRuntimeServiceServer interface` in runtime_grpc.pb.go
// ClusterRuntimeServer is the server API for ClusterRuntimeService.
type ClusterRuntimeServer struct {
	resourceManager *manager.ResourceManager
	options         *ClusterRuntimeServerOptions
	api.UnimplementedClusterRuntimeServiceServer
}

func (s ClusterRuntimeServer) CreateClusterRuntime(ctx context.Context, request *api.CreateClusterRuntimeRequest) (*api.ClusterRuntime, error) {
	if err := ValidateCreateClusterRuntimeRequest(request); err != nil {
		return nil, util.Wrap(err, "Validate cluster runtime request failed.")
	}

	runtime, err := s.resourceManager.CreateClusterRuntime(ctx, request.ClusterRuntime)
	if err != nil {
		return nil, util.Wrap(err, "Create ClusterRuntime failed.")
	}

	return converter.FromKubeToAPIClusterRuntime(runtime), nil
}

func (s ClusterRuntimeServer) GetClusterRuntime(ctx context.Context, request *api.GetClusterRuntimeRequest) (*api.ClusterRuntime, error) {
	runtime, err := s.resourceManager.GetClusterRuntime(ctx, request.Id, request.Name)
	if err != nil {
		return nil, util.Wrap(err, "Get cluster runtime failed.")
	}

	return converter.FromKubeToAPIClusterRuntime(runtime), nil
}

func (s ClusterRuntimeServer) ListClusterRuntimes(ctx context.Context, request *api.ListClusterRuntimesRequest) (*api.ListClusterRuntimesResponse, error) {
	runtimes, err := s.resourceManager.ListClusterRuntimes(ctx)
	if err != nil {
		return nil, util.Wrap(err, "List cluster runtime failed.")
	}

	return &api.ListClusterRuntimesResponse{
		Runtimes: converter.FromKubeToAPIClusterRuntimes(runtimes),
	}, nil
}

func (s ClusterRuntimeServer) DeleteClusterRuntime(ctx context.Context, request *api.DeleteClusterRuntimeRequest) (*emptypb.Empty, error) {
	if err := s.resourceManager.DeleteClusterRuntime(ctx, request.Id, request.Name); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func ValidateCreateClusterRuntimeRequest(request *api.CreateClusterRuntimeRequest) error {
	if request.ClusterRuntime.Name == "" || request.ClusterRuntime.BaseImage == "" {
		return util.NewInvalidInputError("ClusterRuntime name or baseImage is empty. Please specify a valid value.")
	}

	if len(request.ClusterRuntime.Image) != 0 {
		return util.NewInvalidInputError("ClusterRuntime image field is a read-only field, please do not specify it.")
	}
	return nil
}

func NewClusterRuntimeServer(resourceManager *manager.ResourceManager, options *ClusterRuntimeServerOptions) *ClusterRuntimeServer {
	return &ClusterRuntimeServer{resourceManager: resourceManager, options: options}
}
