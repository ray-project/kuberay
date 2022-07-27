package server

import (
	"context"

	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	"github.com/ray-project/kuberay/apiserver/pkg/model"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
	"google.golang.org/protobuf/types/known/emptypb"
)

type JobServerOptions struct {
	CollectMetrics bool
}

// implements `type RayJobServiceServer interface` in job_grpc.pb.go
// RayJobServer is the server API for RayJobServer service.

func NewRayJobServer(resourceManager *manager.ResourceManager, options *JobServerOptions) *RayJobServer {
	return &RayJobServer{resourceManager: resourceManager, options: options}
}

type RayJobServer struct {
	resourceManager *manager.ResourceManager
	options         *JobServerOptions
	api.UnimplementedRayJobServiceServer
}

// Creates a new Ray Job.
func (s *RayJobServer) CreateRayJob(ctx context.Context, request *api.CreateRayJobRequest) (*api.RayJob, error) {
	// use the namespace in the request to override the namespace in the job definition
	request.Job.Namespace = request.Namespace

	job, err := s.resourceManager.CreateJob(ctx, request.Job)
	if err != nil {
		return nil, util.Wrap(err, "Create Job failed.")
	}

	return model.FromCrdToApiJob(job), nil
}

// Finds a specific Job by job name.
func (s *RayJobServer) GetRayJob(ctx context.Context, request *api.GetRayJobRequest) (*api.RayJob, error) {
	if request.Name == "" {
		return nil, util.NewInvalidInputError("job name is empty. Please specify a valid value.")
	}

	if request.Namespace == "" {
		return nil, util.NewInvalidInputError("job namespace is empty. Please specify a valid value.")
	}

	job, err := s.resourceManager.GetJob(ctx, request.Name, request.Namespace)
	if err != nil {
		return nil, util.Wrap(err, "Get cluster failed.")
	}

	return model.FromCrdToApiJob(job), nil
}

// Finds all Jobs in a given namespace.
func (s *RayJobServer) ListRayJobs(ctx context.Context, request *api.ListRayJobsRequest) (*api.ListRayJobsResponse, error) {
	if request.Namespace == "" {
		return nil, util.NewInvalidInputError("job namespace is empty. Please specify a valid value.")
	}

	jobs, err := s.resourceManager.ListJobs(ctx, request.Namespace)
	if err != nil {
		return nil, util.Wrap(err, "List jobs failed.")
	}

	return &api.ListRayJobsResponse{
		Jobs: model.FromCrdToApiJobs(jobs),
	}, nil
}

// Finds all Jobs in all namespaces.
func (s *RayJobServer) ListAllRayJobs(ctx context.Context, request *api.ListAllRayJobsRequest) (*api.ListAllRayJobsResponse, error) {
	jobs, err := s.resourceManager.ListAllJobs(ctx)
	if err != nil {
		return nil, util.Wrap(err, "List jobs failed.")
	}

	return &api.ListAllRayJobsResponse{
		Jobs: model.FromCrdToApiJobs(jobs),
	}, nil
}

// Deletes an Job
func (s *RayJobServer) DeleteRayJob(ctx context.Context, request *api.DeleteRayJobRequest) (*emptypb.Empty, error) {
	if request.Name == "" {
		return nil, util.NewInvalidInputError("job name is empty. Please specify a valid value.")
	}

	if request.Namespace == "" {
		return nil, util.NewInvalidInputError("job namespace is empty. Please specify a valid value.")
	}

	if err := s.resourceManager.DeleteJob(ctx, request.Name, request.Namespace); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}
