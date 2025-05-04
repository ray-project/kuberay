package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	rpcStatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	// "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
)

const (
	GET    = "GET"
	POST   = "POST"
	DELETE = "DELETE"
)

type KuberayAPIServerClient struct {
	KubeClient  kubernetes.Interface
	RestConfig  *rest.Config
	marshaler   *protojson.MarshalOptions
	unmarshaler *protojson.UnmarshalOptions
	baseURL     string
}

type KuberayAPIServerClientError struct {
	HTTPStatusCode int
}

func (krce *KuberayAPIServerClientError) Error() string {
	return fmt.Sprintf("kuberay api server request failed with HTTP status (%d: %s)", krce.HTTPStatusCode, http.StatusText(krce.HTTPStatusCode))
}

func IsNotFoundError(err error) bool {
	if err != nil {
		apiServerError := &KuberayAPIServerClientError{}
		if errors.As(err, &apiServerError); apiServerError.HTTPStatusCode == http.StatusNotFound {
			return true
		}
	}
	return false
}

func NewKuberayAPIServerClient(baseURL string) (*KuberayAPIServerClient, error) {
	kubeconfigPath := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return &KuberayAPIServerClient{
		baseURL:    baseURL,
		KubeClient: clientset,
		RestConfig: config,
		marshaler: &protojson.MarshalOptions{
			Multiline:       true,
			Indent:          "    ",
			AllowPartial:    false,
			UseProtoNames:   true,
			UseEnumNumbers:  false,
			EmitUnpopulated: false,
			Resolver:        nil,
		},
		unmarshaler: &protojson.UnmarshalOptions{
			AllowPartial:   false,
			DiscardUnknown: false,
			Resolver:       nil,
		},
	}, nil
}

func (krc *KuberayAPIServerClient) findPod(namespace string) (*corev1.Pod, error) {
	// Find the KubeRay API server pod

	selector := labels.Set(map[string]string{
		util.KubernetesComponentLabelKey: util.ComponentName,
	}).AsSelector().String()

	podList, err := krc.KubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pods found with label %s=%s", util.KubernetesComponentLabelKey, util.ComponentName)
	}
	// pick the first pod selected
	targetPod := podList.Items[0]
	return &targetPod, nil
}

func (krc *KuberayAPIServerClient) ExecCommandWithCurlInPod(pod *corev1.Pod, url string, method string, jsonBody string) ([]byte, *rpcStatus.Status, error) {
	var (
		execOut bytes.Buffer
		execErr bytes.Buffer
	)

	command := []string{"curl", "-s", "-L", "-w", "HTTP_STATUS:%{http_code}", "-H", "Accept: application/json", "-X", method}

	if jsonBody != "" {
		command = append(command, "-H", "Content-Type: application/json", "-d", jsonBody)
	}

	command = append(command, url)

	// get curl container
	var containerName string
	for _, container := range pod.Spec.Containers {
		if container.Name == util.CurlContainerName {
			containerName = container.Name
			break
		}
	}
	if containerName == "" {
		return nil, nil, fmt.Errorf("could not find container %s in pod", util.CurlContainerName)
	}

	req := krc.KubeClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: containerName,
		Command:   command,
		Stdout:    true,
		Stderr:    true,
	}, runtime.NewParameterCodec(scheme.Scheme))

	exec, err := remotecommand.NewSPDYExecutor(krc.RestConfig, "POST", req.URL())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize executor: %w", err)
	}

	err = exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
		Stdout: &execOut,
		Stderr: &execErr,
		Tty:    false,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("command execution failed: %w", err)
	}

	if execErr.Len() > 0 {
		return nil, nil, fmt.Errorf("stderr: %s", execErr.String())
	}

	// extract status code
	parts := strings.Split(execOut.String(), "HTTP_STATUS:")
	if len(parts) != 2 {
		return nil, nil, fmt.Errorf("unexpected curl output format")
	}
	statusCodeStr := strings.TrimSpace(parts[1])
	statusCode, err := strconv.Atoi(statusCodeStr)
	if err != nil {
		return nil, nil, fmt.Errorf("Cannot convert status code string to int: %s", statusCodeStr)
	}

	bodyBytes := []byte(parts[0])

	if statusCode != http.StatusOK {
		status, err := krc.extractStatus(bodyBytes)
		if err != nil {
			return nil, nil, err
		}
		return nil, status, &KuberayAPIServerClientError{
			HTTPStatusCode: statusCode,
		}
	}
	return bodyBytes, nil, nil
}

// ExecRequest executes arbitrary command inside the pod
func (krc *KuberayAPIServerClient) ExecRequest(url string, method string, jsonBody string) ([]byte, *rpcStatus.Status, error) {
	pod, err := krc.findPod(manager.DefaultNamespace)
	if err != nil {
		return nil, nil, fmt.Errorf("could not find pod: %w", err)
	}

	bodyBytes, status, err := krc.ExecCommandWithCurlInPod(pod, url, method, jsonBody)

	return bodyBytes, status, err
}

// CreateComputeTemplate creates a new compute template.
func (krc *KuberayAPIServerClient) CreateComputeTemplate(request *api.CreateComputeTemplateRequest) (*api.ComputeTemplate, *rpcStatus.Status, error) {
	createURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/compute_templates"

	bytez, err := krc.marshaler.Marshal(request.ComputeTemplate)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal api.ComputeTemplate to JSON: %w", err)
	}

	// Prepare the JSON body as a string
	jsonBody := string(bytez)

	// Execute the curl command inside the pod using kubectl exec
	bodyBytes, status, err := krc.ExecRequest(createURL, POST, jsonBody)
	if err != nil {
		return nil, status, err
	}

	computeTemplate := &api.ComputeTemplate{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, computeTemplate); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}

	return computeTemplate, nil, nil
}

// DeleteComputeTemplate deletes a compute template.
func (krc *KuberayAPIServerClient) DeleteComputeTemplate(request *api.DeleteComputeTemplateRequest) (*rpcStatus.Status, error) {
	deleteURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/compute_templates/" + request.Name
	return krc.doDelete(deleteURL)
}

// Finds a specific compute template by its name and namespace.
func (krc *KuberayAPIServerClient) GetComputeTemplate(request *api.GetComputeTemplateRequest) (*api.ComputeTemplate, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/compute_templates/" + request.Name

	bodyBytes, status, err := krc.ExecRequest(getURL, GET, "")
	if err != nil {
		return nil, status, err
	}
	computeTemplate := &api.ComputeTemplate{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, computeTemplate); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return computeTemplate, nil, nil
}

// GetAllComputeTemplates finds all compute templates in all namespaces.
func (krc *KuberayAPIServerClient) GetAllComputeTemplates() (*api.ListAllComputeTemplatesResponse, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/compute_templates"

	bodyBytes, status, err := krc.ExecRequest(getURL, GET, "")
	if err != nil {
		return nil, status, err
	}

	response := &api.ListAllComputeTemplatesResponse{}

	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}

	return response, nil, nil
}

// GetAllComputeTemplatesInNamespace Finds all compute templates in a given namespace.
func (krc *KuberayAPIServerClient) GetAllComputeTemplatesInNamespace(request *api.ListComputeTemplatesRequest) (*api.ListComputeTemplatesResponse, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/compute_templates"

	bodyBytes, status, err := krc.ExecRequest(getURL, GET, "")
	if err != nil {
		return nil, status, err
	}
	response := &api.ListComputeTemplatesResponse{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}

	return response, nil, nil
}

// CreateCluster creates a new cluster.
func (krc *KuberayAPIServerClient) CreateCluster(request *api.CreateClusterRequest) (*api.Cluster, *rpcStatus.Status, error) {
	createURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/clusters"

	bytez, err := krc.marshaler.Marshal(request.Cluster)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal api.Cluster to JSON: %w", err)
	}

	jsonBody := string(bytez)

	bodyBytes, status, err := krc.ExecRequest(createURL, POST, jsonBody)
	if err != nil {
		return nil, status, err
	}

	cluster := &api.Cluster{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, cluster); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}

	return cluster, nil, nil
}

// DeleteCluster deletes a cluster.
func (krc *KuberayAPIServerClient) DeleteCluster(request *api.DeleteClusterRequest) (*rpcStatus.Status, error) {
	deleteURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/clusters/" + request.Name
	return krc.doDelete(deleteURL)
}

// GetCluster finds a specific cluster by its name and namespace.
func (krc *KuberayAPIServerClient) GetCluster(request *api.GetClusterRequest) (*api.Cluster, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/clusters/" + request.Name

	bodyBytes, status, err := krc.ExecRequest(getURL, GET, "")
	if err != nil {
		return nil, status, err
	}

	cluster := &api.Cluster{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, cluster); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}

	return cluster, nil, nil
}

// ListClusters finds all clusters in a given namespace.
func (krc *KuberayAPIServerClient) ListClusters(request *api.ListClustersRequest) (*api.ListClustersResponse, *rpcStatus.Status, error) {
	u, err := url.Parse(krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/clusters")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	q.Set("limit", strconv.FormatInt(request.Limit, 10))
	q.Set("continue", request.Continue)
	u.RawQuery = q.Encode()

	bodyBytes, status, err := krc.ExecRequest(u.String(), GET, "")
	if err != nil {
		return nil, status, err
	}

	response := &api.ListClustersResponse{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}

	return response, nil, nil
}

// ListAllClusters finds all clusters in all namespaces.
func (krc *KuberayAPIServerClient) ListAllClusters(request *api.ListAllClustersRequest) (*api.ListAllClustersResponse, *rpcStatus.Status, error) {
	baseURL := krc.baseURL + "/apis/v1/clusters"

	values := url.Values{}
	values.Set("limit", strconv.FormatInt(request.Limit, 10))
	if request.Continue != "" {
		values.Set("continue", request.Continue)
	}
	getURL := baseURL + "?" + values.Encode()

	bodyBytes, status, err := krc.ExecRequest(getURL, GET, "")
	if err != nil {
		return nil, status, err
	}

	response := &api.ListAllClustersResponse{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}

	return response, nil, nil
}

// CreateRayJob creates a new job.
func (krc *KuberayAPIServerClient) CreateRayJob(request *api.CreateRayJobRequest) (*api.RayJob, *rpcStatus.Status, error) {
	createURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobs"

	bytez, err := krc.marshaler.Marshal(request.Job)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal api.Cluster to JSON: %w", err)
	}

	// Prepare the JSON body as a string
	jsonBody := string(bytez)

	// Execute the curl command inside the pod using kubectl exec
	bodyBytes, status, err := krc.ExecRequest(createURL, POST, jsonBody)
	if err != nil {
		return nil, status, err
	}

	rayJob := &api.RayJob{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, rayJob); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return rayJob, nil, nil
}

// GetRayJob finds a specific job by its name and namespace.
func (krc *KuberayAPIServerClient) GetRayJob(request *api.GetRayJobRequest) (*api.RayJob, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobs/" + request.Name

	bodyBytes, status, err := krc.ExecRequest(getURL, GET, "")
	if err != nil {
		return nil, status, err
	}

	rayJob := &api.RayJob{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, rayJob); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return rayJob, nil, nil
}

// Finds all job in a given namespace.
func (krc *KuberayAPIServerClient) ListRayJobs(request *api.ListRayJobsRequest) (*api.ListRayJobsResponse, *rpcStatus.Status, error) {
	baseURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobs"

	values := url.Values{}
	values.Set("limit", strconv.FormatInt(request.Limit, 10))
	if request.Continue != "" {
		values.Set("continue", request.Continue)
	}
	getURL := baseURL + "?" + values.Encode()

	bodyBytes, status, err := krc.ExecRequest(getURL, GET, "")
	if err != nil {
		return nil, status, err
	}

	rayJobResponse := &api.ListRayJobsResponse{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, rayJobResponse); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return rayJobResponse, nil, nil
}

// ListAllRayJobs Finds all job in all namespaces.
func (krc *KuberayAPIServerClient) ListAllRayJobs(request *api.ListAllRayJobsRequest) (*api.ListAllRayJobsResponse, *rpcStatus.Status, error) {
	baseURL := krc.baseURL + "/apis/v1/jobs"

	values := url.Values{}
	values.Set("limit", strconv.FormatInt(request.Limit, 10))
	if request.Continue != "" {
		values.Set("continue", request.Continue)
	}
	getURL := baseURL + "?" + values.Encode()

	bodyBytes, status, err := krc.ExecRequest(getURL, GET, "")
	if err != nil {
		return nil, status, err
	}

	allRayJobResponse := &api.ListAllRayJobsResponse{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, allRayJobResponse); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return allRayJobResponse, nil, nil
}

// Deletes a job by its name and namespace.
func (krc *KuberayAPIServerClient) DeleteRayJob(request *api.DeleteRayJobRequest) (*rpcStatus.Status, error) {
	deleteURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobs/" + request.Name
	return krc.doDelete(deleteURL)
}

// CreateRayService create a new ray serve.
func (krc *KuberayAPIServerClient) CreateRayService(request *api.CreateRayServiceRequest) (*api.RayService, *rpcStatus.Status, error) {
	createURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/services"
	bytez, err := krc.marshaler.Marshal(request.Service)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal api.Cluster to JSON: %w", err)
	}

	jsonBody := string(bytez)

	bodyBytes, status, err := krc.ExecRequest(createURL, POST, jsonBody)
	if err != nil {
		return nil, status, err
	}

	rayService := &api.RayService{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, rayService); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return rayService, nil, nil
}

// UpdateRayService updates a ray serve service.
func (krc *KuberayAPIServerClient) UpdateRayService(request *api.UpdateRayServiceRequest) (*api.RayService, *rpcStatus.Status, error) {
	updateURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/services/" + request.Name
	bytez, err := krc.marshaler.Marshal(request.Service)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal api.Cluster to JSON: %w", err)
	}

	jsonBody := string(bytez)

	bodyBytes, status, err := krc.ExecRequest(updateURL, POST, jsonBody)
	if err != nil {
		return nil, status, err
	}

	rayService := &api.RayService{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, rayService); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return rayService, nil, nil
}

// Find a specific ray serve by name and namespace.
func (krc *KuberayAPIServerClient) GetRayService(request *api.GetRayServiceRequest) (*api.RayService, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/services/" + request.Name
	bodyBytes, status, err := krc.ExecRequest(getURL, GET, "")
	if err != nil {
		return nil, status, err
	}
	rayService := &api.RayService{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, rayService); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return rayService, nil, nil
}

// Finds all ray services in a given namespace.
func (krc *KuberayAPIServerClient) ListRayServices(request *api.ListRayServicesRequest) (*api.ListRayServicesResponse, *rpcStatus.Status, error) {
	u, err := url.Parse(krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/services")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	q.Set("pageSize", strconv.FormatInt(int64(request.PageSize), 10))
	q.Set("pageToken", request.PageToken)
	u.RawQuery = q.Encode()

	bodyBytes, status, err := krc.ExecRequest(u.String(), GET, "")
	if err != nil {
		return nil, status, err
	}
	rayServiceResponses := &api.ListRayServicesResponse{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, rayServiceResponses); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}

	return rayServiceResponses, nil, nil
}

// Finds all ray services in a given namespace.
func (krc *KuberayAPIServerClient) ListAllRayServices() (*api.ListAllRayServicesResponse, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/services"
	bodyBytes, status, err := krc.ExecRequest(getURL, GET, "")
	if err != nil {
		return nil, status, err
	}

	allRayServiceResponses := &api.ListAllRayServicesResponse{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, allRayServiceResponses); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return allRayServiceResponses, nil, nil
}

// DeleteRayService deletes a ray service by its name and namespace
func (krc *KuberayAPIServerClient) DeleteRayService(request *api.DeleteRayServiceRequest) (*rpcStatus.Status, error) {
	deleteURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/services/" + request.Name
	return krc.doDelete(deleteURL)
}

// SubmitRayJob creates a new job on a given cluster.
func (krc *KuberayAPIServerClient) SubmitRayJob(request *api.SubmitRayJobRequest) (*api.SubmitRayJobReply, *rpcStatus.Status, error) {
	createURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername
	bytez, err := krc.marshaler.Marshal(request.Jobsubmission)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal api.Cluster to JSON: %w", err)
	}
	jsonBody := string(bytez)

	bodyBytes, status, err := krc.ExecRequest(createURL, POST, jsonBody)
	if err != nil {
		return nil, status, err
	}

	submission := &api.SubmitRayJobReply{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, submission); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return submission, nil, nil
}

// GetRayJobDetails. Get details about specific job on a given cluster.
func (krc *KuberayAPIServerClient) GetRayJobDetails(request *api.GetJobDetailsRequest) (*api.JobSubmissionInfo, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername + "/" + request.Submissionid
	bodyBytes, status, err := krc.ExecRequest(getURL, GET, "")
	if err != nil {
		return nil, status, err
	}

	jobSubmission := &api.JobSubmissionInfo{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, jobSubmission); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return jobSubmission, nil, nil
}

// GetRayJobLog. Get log for a specific job on a given cluster.
func (krc *KuberayAPIServerClient) GetRayJobLog(request *api.GetJobLogRequest) (*api.GetJobLogReply, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername + "/log/" + request.Submissionid
	bodyBytes, status, err := krc.ExecRequest(getURL, GET, "")
	if err != nil {
		return nil, status, err
	}
	jobLogReply := &api.GetJobLogReply{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, jobLogReply); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return jobLogReply, nil, nil
}

// ListRayJobsCluster. List Ray jobs on a given cluster.
func (krc *KuberayAPIServerClient) ListRayJobsCluster(request *api.ListJobDetailsRequest) (*api.ListJobSubmissionInfo, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername

	bodyBytes, status, err := krc.ExecRequest(getURL, GET, "")
	if err != nil {
		return nil, status, err
	}

	jobSubmissionInfo := &api.ListJobSubmissionInfo{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, jobSubmissionInfo); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return jobSubmissionInfo, nil, nil
}

// StopRayJob stops job on a given cluster.
func (krc *KuberayAPIServerClient) StopRayJob(request *api.StopRayJobSubmissionRequest) (*rpcStatus.Status, error) {
	createURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername + "/" + request.Submissionid

	_, status, err := krc.ExecRequest(createURL, POST, "")
	if err != nil {
		return status, err
	}
	return nil, nil
}

// DeleteRayService deletes a ray service by its name and namespace
func (krc *KuberayAPIServerClient) DeleteRayJobCluster(request *api.DeleteRayJobSubmissionRequest) (*rpcStatus.Status, error) {
	deleteURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername + "/" + request.Submissionid
	return krc.doDelete(deleteURL)
}

func (krc *KuberayAPIServerClient) doDelete(deleteURL string) (*rpcStatus.Status, error) {
	_, status, err := krc.ExecRequest(deleteURL, DELETE, "")
	return status, err
}

func (krc *KuberayAPIServerClient) extractStatus(bodyBytes []byte) (*rpcStatus.Status, error) {
	status := &rpcStatus.Status{}
	err := krc.unmarshaler.Unmarshal(bodyBytes, status)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal status object: %w", err)
	}
	return status, nil
}
