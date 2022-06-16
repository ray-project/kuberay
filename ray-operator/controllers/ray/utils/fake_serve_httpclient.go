package utils

import (
	"fmt"
	"net/http"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
)

type FakeRayDashboardClient struct {
	client        http.Client
	dashboardURL  string
	serveStatuses rayv1alpha1.ServeDeploymentStatuses
}

func (r *FakeRayDashboardClient) InitClient(url string) {
	r.client = http.Client{}
	r.dashboardURL = "http://" + url
}

func (r *FakeRayDashboardClient) GetDeployments() (string, error) {
	panic("Fake GetDeployments not implemented")
}

func (r *FakeRayDashboardClient) UpdateDeployments(specs []rayv1alpha1.ServeConfigSpec) error {
	fmt.Print("UpdateDeployments fake succeeds.")
	return nil
}

func (r *FakeRayDashboardClient) GetDeploymentsStatus() (*rayv1alpha1.ServeDeploymentStatuses, error) {
	return &r.serveStatuses, nil
}

func (r *FakeRayDashboardClient) convertServeConfig(specs []rayv1alpha1.ServeConfigSpec) []ServeConfigSpec {
	serveConfigToSend := make([]ServeConfigSpec, len(specs))

	for i, config := range specs {
		serveConfigToSend[i] = ServeConfigSpec{
			Name:                      config.Name,
			ImportPath:                config.ImportPath,
			InitArgs:                  config.InitArgs,
			InitKwargs:                config.InitKwargs,
			NumReplicas:               config.NumReplicas,
			RoutePrefix:               config.RoutePrefix,
			MaxConcurrentQueries:      config.MaxConcurrentQueries,
			UserConfig:                config.UserConfig,
			AutoscalingConfig:         config.AutoscalingConfig,
			GracefulShutdownWaitLoopS: config.GracefulShutdownWaitLoopS,
			GracefulShutdownTimeoutS:  config.GracefulShutdownTimeoutS,
			HealthCheckPeriodS:        config.HealthCheckPeriodS,
			HealthCheckTimeoutS:       config.GracefulShutdownTimeoutS,
			RayActorOptions: RayActorOptionSpec{
				RuntimeEnv:        config.RayActorOptions.RuntimeEnv,
				NumCpus:           config.RayActorOptions.NumCpus,
				NumGpus:           config.RayActorOptions.NumGpus,
				Memory:            config.RayActorOptions.Memory,
				ObjectStoreMemory: config.RayActorOptions.ObjectStoreMemory,
				Resources:         config.RayActorOptions.Resources,
				AcceleratorType:   config.RayActorOptions.AcceleratorType,
			},
		}
	}

	return serveConfigToSend
}

func (r *FakeRayDashboardClient) SetServeStatus(status rayv1alpha1.ServeDeploymentStatuses) {
	r.serveStatuses = status
}
