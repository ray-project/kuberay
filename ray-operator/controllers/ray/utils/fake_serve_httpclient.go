package utils

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/yaml"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
)

type FakeRayDashboardClient struct {
	client        http.Client
	dashboardURL  string
	serveStatuses ServeDeploymentStatuses
}

var _ RayDashboardClientInterface = (*FakeRayDashboardClient)(nil)

func (r *FakeRayDashboardClient) InitClient(url string) {
	r.client = http.Client{}
	r.dashboardURL = "http://" + url
}

func (r *FakeRayDashboardClient) GetDeployments(_ context.Context) (string, error) {
	panic("Fake GetDeployments not implemented")
}

func (r *FakeRayDashboardClient) UpdateDeployments(_ context.Context, specs rayv1alpha1.ServeDeploymentGraphSpec) error {
	fmt.Print("UpdateDeployments fake succeeds.")
	return nil
}

func (r *FakeRayDashboardClient) GetDeploymentsStatus(_ context.Context) (*ServeDeploymentStatuses, error) {
	return &r.serveStatuses, nil
}

func (r *FakeRayDashboardClient) ConvertServeConfig(specs []rayv1alpha1.ServeConfigSpec) []ServeConfigSpec {
	serveConfigToSend := make([]ServeConfigSpec, len(specs))

	for i, config := range specs {
		userConfig := make(map[string]interface{})
		_ = yaml.Unmarshal([]byte(config.UserConfig), &userConfig)

		autoscalingConfig := make(map[string]interface{})
		_ = yaml.Unmarshal([]byte(config.AutoscalingConfig), &autoscalingConfig)

		runtimeEnv := make(map[string]interface{})
		_ = yaml.Unmarshal([]byte(config.RayActorOptions.RuntimeEnv), &runtimeEnv)

		resources := make(map[string]interface{})
		_ = yaml.Unmarshal([]byte(config.RayActorOptions.Resources), &resources)

		serveConfigToSend[i] = ServeConfigSpec{
			Name:                      config.Name,
			NumReplicas:               config.NumReplicas,
			RoutePrefix:               config.RoutePrefix,
			MaxConcurrentQueries:      config.MaxConcurrentQueries,
			UserConfig:                userConfig,
			AutoscalingConfig:         autoscalingConfig,
			GracefulShutdownWaitLoopS: config.GracefulShutdownWaitLoopS,
			GracefulShutdownTimeoutS:  config.GracefulShutdownTimeoutS,
			HealthCheckPeriodS:        config.HealthCheckPeriodS,
			HealthCheckTimeoutS:       config.GracefulShutdownTimeoutS,
			RayActorOptions: RayActorOptionSpec{
				RuntimeEnv:        runtimeEnv,
				NumCpus:           config.RayActorOptions.NumCpus,
				NumGpus:           config.RayActorOptions.NumGpus,
				Memory:            config.RayActorOptions.Memory,
				ObjectStoreMemory: config.RayActorOptions.ObjectStoreMemory,
				Resources:         resources,
				AcceleratorType:   config.RayActorOptions.AcceleratorType,
			},
		}
	}

	return serveConfigToSend
}

func (r *FakeRayDashboardClient) SetServeStatus(status ServeDeploymentStatuses) {
	r.serveStatuses = status
}

func (r *FakeRayDashboardClient) GetJobInfo(_ context.Context, jobId string) (*RayJobInfo, error) {
	return nil, nil
}

func (r *FakeRayDashboardClient) SubmitJob(_ context.Context, rayJob *rayv1alpha1.RayJob, log *logr.Logger) (jobId string, err error) {
	return "", nil
}

func (r *FakeRayDashboardClient) StopJob(_ context.Context, jobName string, log *logr.Logger) (err error) {
	return nil
}
