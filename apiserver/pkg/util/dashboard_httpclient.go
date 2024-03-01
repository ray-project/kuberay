package util

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/go-logr/logr"
	fmtErrors "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/apimachinery/pkg/util/json"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	operatorutils "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

var (
	// Multi-application URL paths
	DeployPathV2 = "/api/serve/applications/"
	// Job URL paths
	JobPath = "/api/jobs/"
)

type RayDashboardClientInterface interface {
	InitClient(url string)
	// Serve support
	DeployApplication(context.Context, string) error
	UpdateDeployments(ctx context.Context, configJson []byte) error
	// V2/multi-app Rest API
	GetServeDetails(ctx context.Context) (*ServeDetails, error)
	GetMultiApplicationStatus(context.Context) (map[string]*ServeApplicationStatus, error)
	DeleteServeApplications(context.Context) error
	// Job support
	GetJobInfo(ctx context.Context, jobId string) (*RayJobInfo, error)
	ListJobs(ctx context.Context) (*[]RayJobInfo, error)
	SubmitJob(ctx context.Context, rayJob *rayv1.RayJob, log *logr.Logger) (string, error)
	SubmitJobReq(ctx context.Context, request *RayJobRequest, name *string, log *logr.Logger) (string, error)
	GetJobLog(ctx context.Context, jobName string, log *logr.Logger) (*string, error)
	StopJob(ctx context.Context, jobName string, log *logr.Logger) error
	DeleteJob(ctx context.Context, jobName string, log *logr.Logger) error
}

type BaseDashboardClient struct {
	client       http.Client
	dashboardURL string
}

func GetRayDashboardClient() RayDashboardClientInterface {
	return &RayDashboardClient{}
}

type RayDashboardClient struct {
	BaseDashboardClient
}

// FetchHeadServiceURL fetches the URL that consists of the FQDN for the RayCluster's head service
// and the port with the given port name (defaultPortName).
func FetchHeadServiceURL(ctx context.Context, log *logr.Logger, cli client.Client, rayCluster *rayv1.RayCluster, defaultPortName string) (string, error) {
	headSvc := &corev1.Service{}
	headSvcName, err := operatorutils.GenerateHeadServiceName(operatorutils.RayClusterCRD, rayCluster.Spec, rayCluster.Name)
	if err != nil {
		log.Error(err, "Failed to generate head service name", "RayCluster name", rayCluster.Name, "RayCluster spec", rayCluster.Spec)
		return "", err
	}

	if err = cli.Get(ctx, client.ObjectKey{Name: headSvcName, Namespace: rayCluster.Namespace}, headSvc); err != nil {
		if errors.IsNotFound(err) {
			log.Error(err, "Head service is not found", "head service name", headSvcName, "namespace", rayCluster.Namespace)
		}
		return "", err
	}

	log.Info("FetchHeadServiceURL", "head service name", headSvc.Name, "namespace", headSvc.Namespace)
	servicePorts := headSvc.Spec.Ports
	port := int32(-1)

	for _, servicePort := range servicePorts {
		if servicePort.Name == defaultPortName {
			port = servicePort.Port
			break
		}
	}

	if port == int32(-1) {
		return "", fmtErrors.Errorf("%s port is not found", defaultPortName)
	}

	domainName := operatorutils.GetClusterDomainName()
	headServiceURL := fmt.Sprintf("%s.%s.svc.%s:%v",
		headSvc.Name,
		headSvc.Namespace,
		domainName,
		port)
	log.Info("FetchHeadServiceURL", "head service URL", headServiceURL, "port", defaultPortName)
	return headServiceURL, nil
}

func (r *RayDashboardClient) InitClient(url string) {
	r.client = http.Client{
		Timeout: 120 * time.Second,
	}
	r.dashboardURL = "http://" + url
}

// Deploy
func (r *RayDashboardClient) DeployApplication(ctx context.Context, yamls string) error {
	// Convert input to bytes
	jreq, err := yaml.ToJSON([]byte(yamls))
	if err != nil {
		return err
	}
	return r.UpdateDeployments(ctx, jreq)
}

// Shuts down Serve and all applications running on the Ray cluster.
func (r *RayDashboardClient) DeleteServeApplications(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, r.dashboardURL+DeployPathV2, nil)
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

// UpdateDeployments update the deployments in the Ray cluster.
func (r *RayDashboardClient) UpdateDeployments(ctx context.Context, configJson []byte) error {
	var req *http.Request
	var err error
	if req, err = http.NewRequestWithContext(ctx, http.MethodPut, r.dashboardURL+DeployPathV2, bytes.NewBuffer(configJson)); err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("UpdateDeployments fail: %s %s", resp.Status, string(body))
	}

	return nil
}

func (r *RayDashboardClient) GetMultiApplicationStatus(ctx context.Context) (map[string]*ServeApplicationStatus, error) {
	serveDetails, err := r.GetServeDetails(ctx)
	if err != nil {
		return nil, fmt.Errorf("Failed to get serve details: %v", err)
	}

	return r.ConvertServeDetailsToApplicationStatuses(serveDetails)
}

// GetServeDetails gets details on all live applications on the Ray cluster.
func (r *RayDashboardClient) GetServeDetails(ctx context.Context) (*ServeDetails, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", r.dashboardURL+DeployPathV2, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("GetServeDetails fail: %s %s", resp.Status, string(body))
	}

	var serveDetails ServeDetails
	if err = json.Unmarshal(body, &serveDetails); err != nil {
		return nil, fmt.Errorf("GetServeDetails failed. Failed to unmarshal bytes: %s", string(body))
	}

	return &serveDetails, nil
}

func (r *RayDashboardClient) ConvertServeDetailsToApplicationStatuses(serveDetails *ServeDetails) (map[string]*ServeApplicationStatus, error) {
	detailsJson, err := json.Marshal(serveDetails.Applications)
	if err != nil {
		return nil, fmt.Errorf("Failed to marshal serve details: %v.", serveDetails.Applications)
	}

	applicationStatuses := map[string]*ServeApplicationStatus{}
	if err = json.Unmarshal(detailsJson, &applicationStatuses); err != nil {
		return nil, fmt.Errorf("Failed to unmarshal serve details bytes into map of application statuses: %v. Bytes: %s", err, string(detailsJson))
	}

	return applicationStatuses, nil
}

// RayJobInfo is the response of "ray job status" api.
// Reference to https://docs.ray.io/en/latest/cluster/jobs-package-ref.html#jobinfo.
type RayJobInfo struct {
	JobStatus    rayv1.JobStatus        `json:"status,omitempty"`
	Entrypoint   string                 `json:"entrypoint,omitempty"`
	JobId        string                 `json:"job_id,omitempty"`
	SubmissionId string                 `json:"submission_id,omitempty"`
	Message      string                 `json:"message,omitempty"`
	ErrorType    *string                `json:"error_type,omitempty"`
	StartTime    uint64                 `json:"start_time,omitempty"`
	EndTime      uint64                 `json:"end_time,omitempty"`
	Metadata     map[string]string      `json:"metadata,omitempty"`
	RuntimeEnv   map[string]interface{} `json:"runtime_env,omitempty"`
}

// RayJobRequest is the request body to submit.
// Reference to https://docs.ray.io/en/latest/cluster/jobs-package-ref.html#jobsubmissionclient.
type RayJobRequest struct {
	Entrypoint   string                 `json:"entrypoint"`
	JobId        string                 `json:"job_id,omitempty"`
	SubmissionId string                 `json:"submission_id,omitempty"`
	RuntimeEnv   map[string]interface{} `json:"runtime_env,omitempty"`
	Metadata     map[string]string      `json:"metadata,omitempty"`
	NumCpus      float32                `json:"entrypoint_num_cpus,omitempty"`
	NumGpus      float32                `json:"entrypoint_num_gpus,omitempty"`
	Resources    map[string]string      `json:"entrypoint_resources,omitempty"`
}

type RayJobResponse struct {
	JobId string `json:"job_id"`
}

type RayJobStopResponse struct {
	Stopped bool `json:"stopped"`
}

type RayJobLogsResponse struct {
	Logs string `json:"logs,omitempty"`
}

// Note that RayJobInfo and error can't be nil at the same time.
func (r *RayDashboardClient) GetJobInfo(ctx context.Context, jobId string) (*RayJobInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", r.dashboardURL+JobPath+jobId, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.NewBadRequest("Job " + jobId + " does not exist on the cluster")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var jobInfo RayJobInfo
	if err = json.Unmarshal(body, &jobInfo); err != nil {
		// Maybe body is not valid json, raise an error with the body.
		return nil, fmt.Errorf("GetJobInfo fail: %s", string(body))
	}

	return &jobInfo, nil
}

func (r *RayDashboardClient) ListJobs(ctx context.Context) (*[]RayJobInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", r.dashboardURL+JobPath, nil)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var jobInfo []RayJobInfo
	if err = json.Unmarshal(body, &jobInfo); err != nil {
		// Maybe body is not valid json, raise an error with the body.
		return nil, fmt.Errorf("GetJobInfo fail: %s", string(body))
	}

	return &jobInfo, nil
}

func (r *RayDashboardClient) SubmitJob(ctx context.Context, rayJob *rayv1.RayJob, log *logr.Logger) (jobId string, err error) {
	request, err := ConvertRayJobToReq(rayJob)
	if err != nil {
		return "", err
	}
	return r.SubmitJobReq(ctx, request, &rayJob.Name, log)
}

func (r *RayDashboardClient) SubmitJobReq(ctx context.Context, request *RayJobRequest, name *string, log *logr.Logger) (jobId string, err error) {
	rayJobJson, err := json.Marshal(request)
	if err != nil {
		return
	}
	if name != nil {
		log.Info("Submit a ray job", "rayJob", name, "jobInfo", string(rayJobJson))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.dashboardURL+JobPath, bytes.NewBuffer(rayJobJson))
	if err != nil {
		return
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var jobResp RayJobResponse
	if err = json.Unmarshal(body, &jobResp); err != nil {
		// Maybe body is not valid json, raise an error with the body.
		return "", fmt.Errorf("SubmitJob fail: %s", string(body))
	}

	return jobResp.JobId, nil
}

// Get Job Log
func (r *RayDashboardClient) GetJobLog(ctx context.Context, jobName string, log *logr.Logger) (*string, error) {
	log.Info("Get ray job log", "rayJob", jobName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.dashboardURL+JobPath+jobName+"/logs", nil)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var jobLog RayJobLogsResponse
	if err = json.Unmarshal(body, &jobLog); err != nil {
		// Maybe body is not valid json, raise an error with the body.
		return nil, fmt.Errorf("GetJobLog fail: %s", string(body))
	}

	return &jobLog.Logs, nil
}

func (r *RayDashboardClient) StopJob(ctx context.Context, jobName string, log *logr.Logger) (err error) {
	log.Info("Stop a ray job", "rayJob", jobName)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.dashboardURL+JobPath+jobName+"/stop", nil)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var jobStopResp RayJobStopResponse
	if err = json.Unmarshal(body, &jobStopResp); err != nil {
		return err
	}

	if !jobStopResp.Stopped {
		jobInfo, err := r.GetJobInfo(ctx, jobName)
		if err != nil {
			return err
		}
		// StopJob only returns an error when JobStatus is not in terminal states (STOPPED / SUCCEEDED / FAILED)
		if !rayv1.IsJobTerminal(jobInfo.JobStatus) {
			return fmt.Errorf("Failed to stopped job: %v", jobInfo)
		}
	}
	return nil
}

func (r *RayDashboardClient) DeleteJob(ctx context.Context, jobName string, log *logr.Logger) error {
	log.Info("Delete a ray job", "rayJob", jobName)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, r.dashboardURL+JobPath+jobName, nil)
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

func ConvertRayJobToReq(rayJob *rayv1.RayJob) (*RayJobRequest, error) {
	req := &RayJobRequest{
		Entrypoint: rayJob.Spec.Entrypoint,
		Metadata:   rayJob.Spec.Metadata,
		JobId:      rayJob.Status.JobId,
	}
	if len(rayJob.Spec.RuntimeEnvYAML) == 0 {
		return req, nil
	}
	var runtimeEnv map[string]interface{}
	err := yaml.Unmarshal([]byte(rayJob.Spec.RuntimeEnvYAML), &runtimeEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal runtimeEnv: %v: %v", rayJob.Spec.RuntimeEnvYAML, err)
	}
	req.RuntimeEnv = runtimeEnv
	return req, nil
<<<<<<< HEAD
}
=======
}
>>>>>>> 46642c4 (moved dashboard httpclient and supporting code to the API server)
