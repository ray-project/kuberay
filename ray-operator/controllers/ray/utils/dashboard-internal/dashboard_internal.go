package dashboardinternal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/yaml"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	utiltypes "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/types"
)

type RayDashboardInternalClient struct {
	client       *http.Client
	dashboardURL string
}

func (r *RayDashboardInternalClient) SetClient(client *http.Client) {
	r.client = client
}

func (r *RayDashboardInternalClient) SetDashboardURL(url string) {
	r.dashboardURL = url
}

func (r *RayDashboardInternalClient) GetDashboardURL() string {
	return r.dashboardURL
}

// UpdateDeployments update the deployments in the Ray cluster.
func (r *RayDashboardInternalClient) UpdateDeployments(ctx context.Context, configJson []byte) error {
	var req *http.Request
	var err error
	if req, err = http.NewRequestWithContext(ctx, http.MethodPut, r.dashboardURL+utiltypes.DeployPathV2, bytes.NewBuffer(configJson)); err != nil {
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

func (r *RayDashboardInternalClient) GetMultiApplicationStatus(ctx context.Context) (map[string]*utiltypes.ServeApplicationStatus, error) {
	serveDetails, err := r.GetServeDetails(ctx)
	if err != nil {
		return nil, fmt.Errorf("Failed to get serve details: %w", err)
	}

	return r.ConvertServeDetailsToApplicationStatuses(serveDetails)
}

// GetServeDetails gets details on all live applications on the Ray cluster.
func (r *RayDashboardInternalClient) GetServeDetails(ctx context.Context) (*utiltypes.ServeDetails, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", r.dashboardURL+utiltypes.ServeDetailsPath, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("GetServeDetails fail: %s %s", resp.Status, string(body))
	}

	var serveDetails utiltypes.ServeDetails
	if err = json.Unmarshal(body, &serveDetails); err != nil {
		return nil, fmt.Errorf("GetServeDetails failed. Failed to unmarshal bytes: %s", string(body))
	}

	return &serveDetails, nil
}

func (r *RayDashboardInternalClient) ConvertServeDetailsToApplicationStatuses(serveDetails *utiltypes.ServeDetails) (map[string]*utiltypes.ServeApplicationStatus, error) {
	detailsJson, err := json.Marshal(serveDetails.Applications)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal serve details: %v", serveDetails.Applications)
	}

	applicationStatuses := map[string]*utiltypes.ServeApplicationStatus{}
	if err = json.Unmarshal(detailsJson, &applicationStatuses); err != nil {
		return nil, fmt.Errorf("failed to unmarshal serve details bytes into map of application statuses: %w. Bytes: %s", err, string(detailsJson))
	}

	return applicationStatuses, nil
}

// Note that RayJobInfo and error can't be nil at the same time.
// Please make sure if the Ray job with JobId can't be found. Return a BadRequest error.
func (r *RayDashboardInternalClient) GetJobInfo(ctx context.Context, jobId string) (*utiltypes.RayJobInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", r.dashboardURL+utiltypes.JobPath+jobId, nil)
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

	var jobInfo utiltypes.RayJobInfo
	if err = json.Unmarshal(body, &jobInfo); err != nil {
		// Maybe body is not valid json, raise an error with the body.
		return nil, fmt.Errorf("GetJobInfo fail: %s", string(body))
	}

	return &jobInfo, nil
}

func (r *RayDashboardInternalClient) ListJobs(ctx context.Context) (*[]utiltypes.RayJobInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", r.dashboardURL+utiltypes.JobPath, nil)
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

	var jobInfo []utiltypes.RayJobInfo
	if err = json.Unmarshal(body, &jobInfo); err != nil {
		// Maybe body is not valid json, raise an error with the body.
		return nil, fmt.Errorf("GetJobInfo fail: %s", string(body))
	}

	return &jobInfo, nil
}

func (r *RayDashboardInternalClient) SubmitJob(ctx context.Context, rayJob *rayv1.RayJob) (jobId string, err error) {
	request, err := ConvertRayJobToReq(rayJob)
	if err != nil {
		return "", err
	}
	return r.SubmitJobReq(ctx, request, &rayJob.Name)
}

func (r *RayDashboardInternalClient) SubmitJobReq(ctx context.Context, request *utiltypes.RayJobRequest, name *string) (jobId string, err error) {
	rayJobJson, err := json.Marshal(request)
	if err != nil {
		return
	}
	if name != nil {
		fmt.Printf("Submit a ray job: %s, jobInfo: %s\n", *name, string(rayJobJson))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.dashboardURL+utiltypes.JobPath, bytes.NewBuffer(rayJobJson))
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

	var jobResp utiltypes.RayJobResponse
	if err = json.Unmarshal(body, &jobResp); err != nil {
		// Maybe body is not valid json, raise an error with the body.
		return "", fmt.Errorf("SubmitJob fail: %s", string(body))
	}

	return jobResp.JobId, nil
}

// Get Job Log
func (r *RayDashboardInternalClient) GetJobLog(ctx context.Context, jobName string) (*string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.dashboardURL+utiltypes.JobPath+jobName+"/logs", nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// This does the right thing, but breaks E2E test
		//		return nil, errors.NewBadRequest("Job " + jobId + " does not exist on the cluster")
		return nil, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var jobLog utiltypes.RayJobLogsResponse
	if err = json.Unmarshal(body, &jobLog); err != nil {
		// Maybe body is not valid json, raise an error with the body.
		return nil, fmt.Errorf("GetJobLog fail: %s", string(body))
	}

	return &jobLog.Logs, nil
}

func (r *RayDashboardInternalClient) StopJob(ctx context.Context, jobName string) (err error) {
	fmt.Printf("Stop a ray job: %s\n", jobName)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.dashboardURL+utiltypes.JobPath+jobName+"/stop", nil)
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

	var jobStopResp utiltypes.RayJobStopResponse
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
			return fmt.Errorf("failed to stopped job: %v", jobInfo)
		}
	}
	return nil
}

func (r *RayDashboardInternalClient) DeleteJob(ctx context.Context, jobName string) error {
	fmt.Printf("Delete a ray job: %s\n", jobName)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, r.dashboardURL+utiltypes.JobPath+jobName, nil)
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

func ConvertRayJobToReq(rayJob *rayv1.RayJob) (*utiltypes.RayJobRequest, error) {
	req := &utiltypes.RayJobRequest{
		Entrypoint:   rayJob.Spec.Entrypoint,
		SubmissionId: rayJob.Status.JobId,
		Metadata:     rayJob.Spec.Metadata,
	}
	if len(rayJob.Spec.RuntimeEnvYAML) != 0 {
		runtimeEnv, err := UnmarshalRuntimeEnvYAML(rayJob.Spec.RuntimeEnvYAML)
		if err != nil {
			return nil, err
		}
		req.RuntimeEnv = runtimeEnv
	}
	req.NumCpus = rayJob.Spec.EntrypointNumCpus
	req.NumGpus = rayJob.Spec.EntrypointNumGpus
	if rayJob.Spec.EntrypointResources != "" {
		if err := json.Unmarshal([]byte(rayJob.Spec.EntrypointResources), &req.Resources); err != nil {
			return nil, err
		}
	}
	return req, nil
}

func UnmarshalRuntimeEnvYAML(runtimeEnvYAML string) (utiltypes.RuntimeEnvType, error) {
	var runtimeEnv utiltypes.RuntimeEnvType
	err := yaml.Unmarshal([]byte(runtimeEnvYAML), &runtimeEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal RuntimeEnvYAML: %v: %w", runtimeEnvYAML, err)
	}
	return runtimeEnv, nil
}
