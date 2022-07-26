package utils

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	fmtErrors "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/util/yaml"

	"k8s.io/apimachinery/pkg/util/json"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
)

// TODO: currently the following constants are also declared in ray-operator/controllers/ray/common
// We cannot import them to avoid cycles
const (
	DefaultDashboardName                = "dashboard"
	DefaultDashboardAgentListenPortName = "dashboard-agent"
)

var (
	DeployPath = "/api/serve/deployments/"
	StatusPath = "/api/serve/deployments/status"
	JobPath    = "/api/jobs/"
)

// ServeConfigSpec defines the desired state of RayService, used by Ray Dashboard.
type ServeConfigSpec struct {
	Name                      string                 `json:"name"`
	NumReplicas               *int32                 `json:"num_replicas,omitempty"`
	RoutePrefix               string                 `json:"route_prefix,omitempty"`
	MaxConcurrentQueries      *int32                 `json:"max_concurrent_queries,omitempty"`
	UserConfig                map[string]interface{} `json:"user_config,omitempty"`
	AutoscalingConfig         map[string]interface{} `json:"autoscaling_config,omitempty"`
	GracefulShutdownWaitLoopS *int32                 `json:"graceful_shutdown_wait_loop_s,omitempty"`
	GracefulShutdownTimeoutS  *int32                 `json:"graceful_shutdown_timeout_s,omitempty"`
	HealthCheckPeriodS        *int32                 `json:"health_check_period_s,omitempty"`
	HealthCheckTimeoutS       *int32                 `json:"health_check_timeout_s,omitempty"`
	RayActorOptions           RayActorOptionSpec     `json:"ray_actor_options,omitempty"`
}

// RayActorOptionSpec defines the desired state of RayActor, used by Ray Dashboard.
type RayActorOptionSpec struct {
	RuntimeEnv        map[string]interface{} `json:"runtime_env,omitempty"`
	NumCpus           *float64               `json:"num_cpus,omitempty"`
	NumGpus           *float64               `json:"num_gpus,omitempty"`
	Memory            *int32                 `json:"memory,omitempty"`
	ObjectStoreMemory *int32                 `json:"object_store_memory,omitempty"`
	Resources         map[string]interface{} `json:"resources,omitempty"`
	AcceleratorType   string                 `json:"accelerator_type,omitempty"`
}

// ServeDeploymentStatuses defines the current states of all Serve Deployments.
type ServeDeploymentStatuses struct {
	ApplicationStatus  rayv1alpha1.AppStatus               `json:"app_status,omitempty"`
	DeploymentStatuses []rayv1alpha1.ServeDeploymentStatus `json:"deployment_statuses,omitempty"`
}

// ServingClusterDeployments defines the request sent to the dashboard api server.
type ServingClusterDeployments struct {
	ImportPath  string                 `json:"import_path"`
	RuntimeEnv  map[string]interface{} `json:"runtime_env,omitempty"`
	Deployments []ServeConfigSpec      `json:"deployments,omitempty"`
}

type RayDashboardClientInterface interface {
	InitClient(url string)
	GetDeployments() (string, error)
	UpdateDeployments(specs rayv1alpha1.ServeDeploymentGraphSpec) error
	GetDeploymentsStatus() (*ServeDeploymentStatuses, error)
	ConvertServeConfig(specs []rayv1alpha1.ServeConfigSpec) []ServeConfigSpec
	GetJobInfo(jobId string) (*RayJobInfo, error)
	SubmitJob(rayJob *rayv1alpha1.RayJob, log *logr.Logger) (jobId string, err error)
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

func FetchDashboardAgentURL(ctx context.Context, log *logr.Logger, cli client.Client, rayCluster *rayv1alpha1.RayCluster) (string, error) {
	dashboardAgentService := &corev1.Service{}
	dashboardAgentServiceName := CheckName(GenerateDashboardServiceName(rayCluster.Name))
	if err := cli.Get(ctx, client.ObjectKey{Name: dashboardAgentServiceName, Namespace: rayCluster.Namespace}, dashboardAgentService); err != nil {
		return "", err
	}

	log.V(1).Info("fetchDashboardAgentURL ", "dashboard agent service found", dashboardAgentService.Name)
	// TODO: compare diff and reconcile the object. For example. ServiceType might be changed or port might be modified
	servicePorts := dashboardAgentService.Spec.Ports

	dashboardPort := int32(-1)

	for _, servicePort := range servicePorts {
		if servicePort.Name == DefaultDashboardAgentListenPortName {
			dashboardPort = servicePort.Port
			break
		}
	}

	if dashboardPort == int32(-1) {
		return "", fmtErrors.Errorf("dashboard port not found")
	}

	dashboardAgentURL := fmt.Sprintf("%s.%s.svc.cluster.local:%v",
		dashboardAgentService.Name,
		dashboardAgentService.Namespace,
		dashboardPort)
	log.V(1).Info("fetchDashboardAgentURL ", "dashboardURL", dashboardAgentURL)
	return dashboardAgentURL, nil
}

func FetchDashboardURL(ctx context.Context, log *logr.Logger, cli client.Client, rayCluster *rayv1alpha1.RayCluster) (string, error) {
	headSvc := &corev1.Service{}
	headSvcName := CheckName(GenerateServiceName(rayCluster.Name))
	if err := cli.Get(ctx, client.ObjectKey{Name: headSvcName, Namespace: rayCluster.Namespace}, headSvc); err != nil {
		return "", err
	}

	log.V(3).Info("fetchDashboardURL ", "dashboard service found", headSvc.Name)
	servicePorts := headSvc.Spec.Ports
	dashboardPort := int32(-1)

	for _, servicePort := range servicePorts {
		if servicePort.Name == DefaultDashboardName {
			dashboardPort = servicePort.Port
			break
		}
	}

	if dashboardPort == int32(-1) {
		return "", fmtErrors.Errorf("dashboard port not found")
	}

	dashboardURL := fmt.Sprintf("%s.%s.svc.cluster.local:%v",
		headSvc.Name,
		headSvc.Namespace,
		dashboardPort)
	log.V(1).Info("fetchDashboardURL ", "dashboardURL", dashboardURL)
	return dashboardURL, nil
}

func (r *RayDashboardClient) InitClient(url string) {
	r.client = http.Client{
		Timeout: 2 * time.Second,
	}
	r.dashboardURL = "http://" + url
}

// GetDeployments get the current deployments in the Ray cluster.
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

// UpdateDeployments update the deployments in the Ray cluster.
func (r *RayDashboardClient) UpdateDeployments(specs rayv1alpha1.ServeDeploymentGraphSpec) error {
	runtimeEnv := make(map[string]interface{})
	_ = yaml.Unmarshal([]byte(specs.RuntimeEnv), &runtimeEnv)

	servingClusterDeployments := ServingClusterDeployments{
		ImportPath:  specs.ImportPath,
		RuntimeEnv:  runtimeEnv,
		Deployments: r.ConvertServeConfig(specs.ServeConfigSpecs),
	}

	deploymentJson, err := json.Marshal(servingClusterDeployments)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPut, r.dashboardURL+DeployPath, bytes.NewBuffer(deploymentJson))
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

// GetDeploymentsStatus get the current deployment statuses in the Ray cluster.
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

func (r *RayDashboardClient) ConvertServeConfig(specs []rayv1alpha1.ServeConfigSpec) []ServeConfigSpec {
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

// RayJobInfo is the response of "ray job status" api.
// Reference to https://docs.ray.io/en/master/cluster/jobs-package-ref.html#jobinfo.
type RayJobInfo struct {
	JobStatus  rayv1alpha1.JobStatus `json:"status,omitempty"`
	Entrypoint string                `json:"entrypoint,omitempty"`
	Message    string                `json:"message,omitempty"`
	ErrorType  *string               `json:"error_type,omitempty"`
	StartTime  int64                 `json:"start_time,omitempty"`
	EndTime    int64                 `json:"end_time,omitempty"`
	Metadata   map[string]string     `json:"metadata,omitempty"`
}

// RayJobRequest is the request body to submit.
// Reference to https://docs.ray.io/en/master/cluster/jobs-package-ref.html#jobsubmissionclient.
type RayJobRequest struct {
	Entrypoint string                 `json:"entrypoint"`
	JobId      string                 `json:"job_id,omitempty"`
	RuntimeEnv map[string]interface{} `json:"runtime_env,omitempty"`
	Metadata   map[string]string      `json:"metadata,omitempty"`
}

type RayJobResponse struct {
	JobId string `json:"job_id"`
}

func (r *RayDashboardClient) GetJobInfo(jobId string) (*RayJobInfo, error) {
	req, err := http.NewRequest("GET", r.dashboardURL+JobPath+jobId, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var jobInfo RayJobInfo
	if err = json.Unmarshal(body, &jobInfo); err != nil {
		return nil, err
	}

	return &jobInfo, nil
}

func (r *RayDashboardClient) SubmitJob(rayJob *rayv1alpha1.RayJob, log *logr.Logger) (jobId string, err error) {
	request, err := ConvertRayJobToReq(rayJob)
	if err != nil {
		return "", err
	}
	rayJobJson, err := json.Marshal(request)
	if err != nil {
		return
	}
	log.Info("Submit a ray job", "rayJob", rayJob.Name, "jobInfo", string(rayJobJson))

	req, err := http.NewRequest(http.MethodPost, r.dashboardURL+JobPath, bytes.NewBuffer(rayJobJson))
	if err != nil {
		return
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)

	var jobResp RayJobResponse
	if err = json.Unmarshal(body, &jobResp); err != nil {
		return
	}

	return jobResp.JobId, nil
}

func ConvertRayJobToReq(rayJob *rayv1alpha1.RayJob) (*RayJobRequest, error) {
	req := &RayJobRequest{
		Entrypoint: rayJob.Spec.Entrypoint,
		Metadata:   rayJob.Spec.Metadata,
		JobId:      rayJob.Status.JobId,
	}
	if len(rayJob.Spec.RuntimeEnv) == 0 {
		return req, nil
	}
	decodeBytes, err := base64.StdEncoding.DecodeString(rayJob.Spec.RuntimeEnv)
	if err != nil {
		return nil, fmt.Errorf("Failed to decode runtimeEnv: %v: %v", rayJob.Spec.RuntimeEnv, err)
	}
	var runtimeEnv map[string]interface{}
	err = json.Unmarshal(decodeBytes, &runtimeEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal runtimeEnv: %v: %v", decodeBytes, err)
	}
	req.RuntimeEnv = runtimeEnv
	return req, nil
}
