package server

import (
	"context"

	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	"github.com/ray-project/kuberay/apiserver/pkg/model"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ComputeTemplateServerOptions struct {
	CollectMetrics bool
}

// implements `type ComputeTemplateServiceServer interface` in runtime_grpc.pb.go
// ComputeTemplateServer is the server API for ClusterRuntimeService.
type ComputeTemplateServer struct {
	resourceManager *manager.ResourceManager
	options         *ComputeTemplateServerOptions
	api.UnimplementedComputeTemplateServiceServer
}

func (s *ComputeTemplateServer) CreateComputeTemplate(ctx context.Context, request *api.CreateComputeTemplateRequest) (*api.ComputeTemplate, error) {
	if err := ValidateCreateComputeTemplateRequest(request); err != nil {
		return nil, util.Wrap(err, "Validate compute template runtime request failed.")
	}

	runtime, err := s.resourceManager.CreateComputeTemplate(ctx, request.ComputeTemplate)
	if err != nil {
		return nil, util.Wrap(err, "Create compute template Runtime failed.")
	}

	return model.FromKubeToAPIComputeTemplate(runtime), nil
}

func (s *ComputeTemplateServer) GetComputeTemplate(ctx context.Context, request *api.GetComputeTemplateRequest) (*api.ComputeTemplate, error) {
	runtime, err := s.resourceManager.GetComputeTemplate(ctx, request.Name, request.Namespace)
	if err != nil {
		return nil, util.Wrap(err, "Get compute template runtime failed.")
	}

	return model.FromKubeToAPIComputeTemplate(runtime), nil
}

func (s *ComputeTemplateServer) ListComputeTemplates(ctx context.Context, request *api.ListComputeTemplatesRequest) (*api.ListComputeTemplatesResponse, error) {
	runtimes, err := s.resourceManager.ListComputeTemplates(ctx, request.Namespace)
	if err != nil {
		return nil, util.Wrap(err, "List compute templates runtime failed.")
	}

	return &api.ListComputeTemplatesResponse{
		ComputeTemplates: model.FromKubeToAPIComputeTemplates(runtimes),
	}, nil
}

func (s *ComputeTemplateServer) DeleteComputeTemplate(ctx context.Context, request *api.DeleteComputeTemplateRequest) (*emptypb.Empty, error) {
	if err := s.resourceManager.DeleteComputeTemplate(ctx, request.Name, request.Namespace); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func ValidateCreateComputeTemplateRequest(request *api.CreateComputeTemplateRequest) error {
	if request.ComputeTemplate.Name == "" {
		return util.NewInvalidInputError("Compute template name is empty. Please specify a valid value.")
	}

	if request.ComputeTemplate.Namespace == "" {
		return util.NewInvalidInputError("Compute template namespace is empty. Please specify a valid value.")
	}

	if request.ComputeTemplate.Cpu == 0 {
		return util.NewInvalidInputError("Cpu amount is zero. Please specify a valid value.")
	}

	if request.ComputeTemplate.Memory == 0 {
		return util.NewInvalidInputError("Memory amount is zero. Please specify a valid value.")
	}

	return nil
}

func NewComputeTemplateServer(resourceManager *manager.ResourceManager, options *ComputeTemplateServerOptions) *ComputeTemplateServer {
	return &ComputeTemplateServer{resourceManager: resourceManager, options: options}
}
