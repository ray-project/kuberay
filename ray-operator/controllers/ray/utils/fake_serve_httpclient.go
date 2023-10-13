package utils

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/go-logr/logr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

type FakeRayDashboardClient struct {
	BaseDashboardClient
	singleAppStatus  ServeApplicationStatus
	multiAppStatuses map[string]*ServeApplicationStatus
	serveDetails     ServeDetails

	GetJobInfoMock atomic.Pointer[func(context.Context, string) (*RayJobInfo, error)]
}

var _ RayDashboardClientInterface = (*FakeRayDashboardClient)(nil)

func (r *FakeRayDashboardClient) InitClient(url string) {
	r.client = http.Client{}
	r.dashboardURL = "http://" + url
}

func (r *FakeRayDashboardClient) GetDeployments(_ context.Context) (string, error) {
	panic("Fake GetDeployments not implemented")
}

func (r *FakeRayDashboardClient) UpdateDeployments(_ context.Context, configJson []byte, serveConfigType RayServeConfigType) error {
	fmt.Print("UpdateDeployments fake succeeds.")
	return nil
}

func (r *FakeRayDashboardClient) GetSingleApplicationStatus(_ context.Context) (*ServeApplicationStatus, error) {
	return &r.singleAppStatus, nil
}

func (r *FakeRayDashboardClient) GetMultiApplicationStatus(_ context.Context) (map[string]*ServeApplicationStatus, error) {
	return r.multiAppStatuses, nil
}

func (r *FakeRayDashboardClient) GetServeDetails(_ context.Context) (*ServeDetails, error) {
	return &r.serveDetails, nil
}

func (r *FakeRayDashboardClient) SetSingleApplicationStatus(status ServeApplicationStatus) {
	r.singleAppStatus = status
}

func (r *FakeRayDashboardClient) SetMultiApplicationStatuses(statuses map[string]*ServeApplicationStatus) {
	r.multiAppStatuses = statuses
}

func (r *FakeRayDashboardClient) GetJobInfo(ctx context.Context, jobId string) (*RayJobInfo, error) {
	if mock := r.GetJobInfoMock.Load(); mock != nil {
		return (*mock)(ctx, jobId)
	}
	return nil, nil
}

func (r *FakeRayDashboardClient) SubmitJob(_ context.Context, rayJob *rayv1.RayJob, log *logr.Logger) (jobId string, err error) {
	return "", nil
}

func (r *FakeRayDashboardClient) StopJob(_ context.Context, jobName string, log *logr.Logger) (err error) {
	return nil
}
