package utils

import (
	"bytes"
	"io/ioutil"
	"net/http"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"k8s.io/apimachinery/pkg/util/json"
)

var (
	DeployPath = "/api/serve/deployments/"
	StatusPath = "/api/serve/deployments/status"
)

// ServeConfigSpec defines the desired state of RayService, used by Ray Dashboard.
type ServeConfigSpec struct {
	Name                      string             `json:"name"`
	ImportPath                string             `json:"import_path"`
	InitArgs                  []string           `json:"init_args,omitempty"`
	InitKwargs                map[string]string  `json:"init_kwargs,omitempty"`
	NumReplicas               *int32             `json:"num_replicas,omitempty"`
	RoutePrefix               string             `json:"route_prefix,omitempty"`
	MaxConcurrentQueries      *int32             `json:"max_concurrent_queries,omitempty"`
	UserConfig                map[string]string  `json:"user_config,omitempty"`
	AutoscalingConfig         map[string]string  `json:"autoscaling_config,omitempty"`
	GracefulShutdownWaitLoopS *int32             `json:"graceful_shutdown_wait_loop_s,omitempty"`
	GracefulShutdownTimeoutS  *int32             `json:"graceful_shutdown_timeout_s,omitempty"`
	HealthCheckPeriodS        *int32             `json:"health_check_period_s,omitempty"`
	HealthCheckTimeoutS       *int32             `json:"health_check_timeout_s,omitempty"`
	RayActorOptions           RayActorOptionSpec `json:"ray_actor_options,omitempty"`
}

// RayActorOptionSpec defines the desired state of RayActor, used by Ray Dashboard.
type RayActorOptionSpec struct {
	RuntimeEnv        map[string][]string `json:"runtime_env,omitempty"`
	NumCpus           *float64            `json:"num_cpus,omitempty"`
	NumGpus           *float64            `json:"num_gpus,omitempty"`
	Memory            *int32              `json:"memory,omitempty"`
	ObjectStoreMemory *int32              `json:"object_store_memory,omitempty"`
	Resources         map[string]string   `json:"resources,omitempty"`
	AcceleratorType   string              `json:"accelerator_type,omitempty"`
}

// ServeDeploymentStatuses defines the current states of all Serve Deployments.
type ServeDeploymentStatuses struct {
	ApplicationStatus  rayv1alpha1.AppStatus               `json:"app_status,omitempty"`
	DeploymentStatuses []rayv1alpha1.ServeDeploymentStatus `json:"deployment_statuses,omitempty"`
}

type ServingClusterDeployments struct {
	Deployments []ServeConfigSpec `json:"deployments,omitempty"`
}

type RayDashboardClientInterface interface {
	InitClient(url string)
	GetDeployments() (string, error)
	UpdateDeployments(specs []rayv1alpha1.ServeConfigSpec) error
	GetDeploymentsStatus() (*ServeDeploymentStatuses, error)
	convertServeConfig(specs []rayv1alpha1.ServeConfigSpec) []ServeConfigSpec
}

// GetRayDashboardClientFunc Used for unit tests.
var GetRayDashboardClientFunc = GetRayDashboardClient

func GetRayDashboardClient() RayDashboardClientInterface {
	return &RayDashboardClient{}
}

type RayDashboardClient struct {
	client       http.Client
	dashboardURL string
}

func (r *RayDashboardClient) InitClient(url string) {
	r.client = http.Client{}
	r.dashboardURL = "http://" + url
}

func (r *RayDashboardClient) GetDeployments() (string, error) {
	req, err := http.NewRequest("GET", r.dashboardURL+DeployPath, nil)
	if err != nil {
		return "", err
	}

	resp, err := r.client.Do(req)

	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)

	return string(body), nil
}

func (r *RayDashboardClient) UpdateDeployments(specs []rayv1alpha1.ServeConfigSpec) error {
	servingClusterDeployments := ServingClusterDeployments{
		Deployments: r.convertServeConfig(specs),
	}

	deploymentJson, err := json.Marshal(servingClusterDeployments)

	if err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", r.dashboardURL+DeployPath, bytes.NewBuffer(deploymentJson))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)

	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (r *RayDashboardClient) GetDeploymentsStatus() (*ServeDeploymentStatuses, error) {
	req, err := http.NewRequest("GET", r.dashboardURL+StatusPath, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req)

	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)

	var serveStatuses ServeDeploymentStatuses
	if err = json.Unmarshal(body, &serveStatuses); err != nil {
		return nil, err
	}

	return &serveStatuses, nil
}

func (r *RayDashboardClient) convertServeConfig(specs []rayv1alpha1.ServeConfigSpec) []ServeConfigSpec {
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
