package server

import (
	"context"

	api "github.com/ray-project/kuberay/api/go_client"
	"github.com/ray-project/kuberay/backend/pkg/manager"
	"github.com/ray-project/kuberay/backend/pkg/model/converter"
	"github.com/ray-project/kuberay/backend/pkg/util"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ComputeRuntimeServerOptions struct {
	CollectMetrics bool
}

// implements `type ComputeRuntimeServiceServer interface` in runtime_grpc.pb.go
// ComputeRuntimeServer is the server API for ClusterRuntimeService.
type ComputeRuntimeServer struct {
	resourceManager *manager.ResourceManager
	options         *ComputeRuntimeServerOptions
	api.UnimplementedComputeRuntimeServiceServer
}

func (s ComputeRuntimeServer) CreateComputeRuntime(ctx context.Context, request *api.CreateComputeRuntimeRequest) (*api.ComputeRuntime, error) {
	if err := ValidateCreateComputeRuntimeRequest(request); err != nil {
		return nil, util.Wrap(err, "Validate compute runtime request failed.")
	}

	runtime, err := s.resourceManager.CreateComputeRuntime(ctx, request.ComputeRuntime)
	if err != nil {
		return nil, util.Wrap(err, "Create Cluster failed.")
	}

	return converter.FromKubeToAPIComputeRuntime(runtime), nil
}

func (s ComputeRuntimeServer) GetComputeRuntime(ctx context.Context, request *api.GetComputeRuntimeRequest) (*api.ComputeRuntime, error) {
	runtime, err := s.resourceManager.GetComputeRuntime(ctx, request.Id, request.Name)
	if err != nil {
		return nil, util.Wrap(err, "Get cluster runtime failed.")
	}

	return converter.FromKubeToAPIComputeRuntime(runtime), nil
}

func (s ComputeRuntimeServer) ListComputeRuntimes(ctx context.Context, request *api.ListComputeRuntimesRequest) (*api.ListComputeRuntimesResponse, error) {
	runtimes, err := s.resourceManager.ListComputeRuntimes(ctx)
	if err != nil {
		return nil, util.Wrap(err, "List cluster runtime failed.")
	}

	return &api.ListComputeRuntimesResponse{
		Runtimes: converter.FromKubeToAPIComputeRuntimes(runtimes),
	}, nil
}

func (s ComputeRuntimeServer) DeleteComputeRuntime(ctx context.Context, request *api.DeleteComputeRuntimeRequest) (*emptypb.Empty, error) {
	if err := s.resourceManager.DeleteComputeRuntime(ctx, request.Id, request.Name); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func ValidateCreateComputeRuntimeRequest(request *api.CreateComputeRuntimeRequest) error {
	if request.ComputeRuntime.Name == "" {
		return util.NewInvalidInputError("Cluster name is empty. Please specify a valid value.")
	}

	if request.ComputeRuntime.HeadGroupSpec == nil {
		return util.NewInvalidInputError("HeadGroupSpec is empty. Please specify a valid value.")
	}

	for index, spec := range request.ComputeRuntime.WorkerGroupSepc {
		if spec.GroupName == "" {
			return util.NewInvalidInputError("WorkerNodeSpec %d group name is empty. Please specify a valid value.", index)
		}
		if spec.MaxReplicas == 0 {
			return util.NewInvalidInputError("WorkerNodeSpec %d MaxReplicas can not be 0. Please specify a valid value.", index)
		}
		if spec.MinReplicas > spec.MaxReplicas {
			return util.NewInvalidInputError("WorkerNodeSpec %d MinReplica > MaxReplicas. Please specify a valid value.", index)
		}
	}

	return nil
}

func NewComputeRuntimeServer(resourceManager *manager.ResourceManager, options *ComputeRuntimeServerOptions) *ComputeRuntimeServer {
	return &ComputeRuntimeServer{resourceManager: resourceManager, options: options}
}
