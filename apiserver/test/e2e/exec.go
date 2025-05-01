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

func (krc *KuberayAPIServerClient) ExecCommandWithCurlInPod(pod *corev1.Pod, url string, jsonBody string) (string, error) {
	var (
		execOut bytes.Buffer
		execErr bytes.Buffer
	)

	command := []string{"curl", "-s", "-H", "Accept: application/json"}

	if jsonBody != "" {
		command = append(command, "-X", "POST", "-H", "Content-Type: application/json", "-d", jsonBody)
	}

	command = append(command, url)

	containerName := pod.Spec.Containers[0].Name

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
		return "", fmt.Errorf("failed to initialize executor: %w", err)
	}

	err = exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
		Stdout: &execOut,
		Stderr: &execErr,
		Tty:    false,
	})
	if err != nil {
		return "", fmt.Errorf("command execution failed: %w", err)
	}

	if execErr.Len() > 0 {
		return "", fmt.Errorf("stderr: %s", execErr.String())
	}

	return execOut.String(), nil
}

// ExecRequest executes arbitrary command inside the pod
func (krc *KuberayAPIServerClient) ExecRequest(url string, jsonBody string) (string, error) {
	pod, err := krc.findPod(manager.DefaultNamespace)
	if err != nil {
		return "", fmt.Errorf("could not find pod: %w", err)
	}

	execOut, err := krc.ExecCommandWithCurlInPod(pod, url, jsonBody)

	return execOut, err
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
	response, err := krc.ExecRequest(createURL, jsonBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	computeTemplate := &api.ComputeTemplate{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), computeTemplate); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal response: %+w", err)
	}

	return computeTemplate, nil, nil
}

// DeleteComputeTemplate deletes a compute template.
func (krc *KuberayAPIServerClient) DeleteComputeTemplate(request *api.DeleteComputeTemplateRequest) (string, error) {
	deleteURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/compute_templates/" + request.Name
	response, err := krc.doDelete(deleteURL)
	return response, err
}

// Finds a specific compute template by its name and namespace.
func (krc *KuberayAPIServerClient) GetComputeTemplate(request *api.GetComputeTemplateRequest) (*api.ComputeTemplate, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/compute_templates/" + request.Name

	// Execute the curl command inside the pod using kubectl exec
	response, err := krc.ExecRequest(getURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	// Now, parse the response into ComputeTemplate
	computeTemplate := &api.ComputeTemplate{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), computeTemplate); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal response: %+w", err)
	}

	return computeTemplate, nil, nil
}

// GetAllComputeTemplates finds all compute templates in all namespaces.
func (krc *KuberayAPIServerClient) GetAllComputeTemplates() (*api.ListAllComputeTemplatesResponse, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/compute_templates"

	response, err := krc.ExecRequest(getURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	allComputeTemplates := &api.ListAllComputeTemplatesResponse{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), allComputeTemplates); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}

	return allComputeTemplates, nil, nil
}

// GetAllComputeTemplatesInNamespace Finds all compute templates in a given namespace.
func (krc *KuberayAPIServerClient) GetAllComputeTemplatesInNamespace(request *api.ListComputeTemplatesRequest) (*api.ListComputeTemplatesResponse, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/compute_templates"

	response, err := krc.ExecRequest(getURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	computeTemplates := &api.ListComputeTemplatesResponse{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), computeTemplates); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}

	return computeTemplates, nil, nil
}

// CreateCluster creates a new cluster.
func (krc *KuberayAPIServerClient) CreateCluster(request *api.CreateClusterRequest) (*api.Cluster, *rpcStatus.Status, error) {
	createURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/clusters"

	bytez, err := krc.marshaler.Marshal(request.Cluster)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal api.Cluster to JSON: %w", err)
	}

	// Prepare the JSON body as a string
	jsonBody := string(bytez)

	// Execute the curl command inside the pod using kubectl exec
	response, err := krc.ExecRequest(createURL, jsonBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	cluster := &api.Cluster{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), cluster); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}

	return cluster, nil, nil
}

// DeleteCluster deletes a cluster
func (krc *KuberayAPIServerClient) DeleteCluster(request *api.DeleteClusterRequest) (string, error) {
	deleteURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/clusters/" + request.Name
	response, err := krc.doDelete(deleteURL)
	return response, err
}

// GetCluster finds a specific Cluster by ID.
func (krc *KuberayAPIServerClient) GetCluster(request *api.GetClusterRequest) (*api.Cluster, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/clusters/" + request.Name

	response, err := krc.ExecRequest(getURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	cluster := &api.Cluster{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), cluster); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return cluster, nil, nil
}

// ListCluster finds all clusters in a given namespace.
func (krc *KuberayAPIServerClient) ListClusters(request *api.ListClustersRequest) (*api.ListClustersResponse, *rpcStatus.Status, error) {
	baseURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/clusters"

	values := url.Values{}
	values.Set("limit", strconv.FormatInt(request.Limit, 10))
	if request.Continue != "" {
		values.Set("continue", request.Continue)
	}
	getURL := baseURL + "?" + values.Encode()

	response, err := krc.ExecRequest(getURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}
	clustersResponses := &api.ListClustersResponse{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), clustersResponses); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return clustersResponses, nil, nil
}

// ListAllClusters finds all Clusters in all namespaces.
func (krc *KuberayAPIServerClient) ListAllClusters(request *api.ListAllClustersRequest) (*api.ListAllClustersResponse, *rpcStatus.Status, error) {
	baseURL := krc.baseURL + "/apis/v1/clusters"

	values := url.Values{}
	values.Set("limit", strconv.FormatInt(request.Limit, 10))
	if request.Continue != "" {
		values.Set("continue", request.Continue)
	}
	getURL := baseURL + "?" + values.Encode()

	response, err := krc.ExecRequest(getURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}
	allClustersResponses := &api.ListAllClustersResponse{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), allClustersResponses); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return allClustersResponses, nil, nil
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
	response, err := krc.ExecRequest(createURL, jsonBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	rayJob := &api.RayJob{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), rayJob); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return rayJob, nil, nil
}

// GetRayJob finds a specific job by its name and namespace.
func (krc *KuberayAPIServerClient) GetRayJob(request *api.GetRayJobRequest) (*api.RayJob, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobs/" + request.Name

	// Execute the curl command inside the pod using kubectl exec
	response, err := krc.ExecRequest(getURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	rayJob := &api.RayJob{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), rayJob); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
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

	response, err := krc.ExecRequest(getURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	rayJobResponse := &api.ListRayJobsResponse{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), rayJobResponse); err != nil {
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

	response, err := krc.ExecRequest(getURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	allRayJobResponse := &api.ListAllRayJobsResponse{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), allRayJobResponse); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return allRayJobResponse, nil, nil
}

// Deletes a job by its name and namespace.
func (krc *KuberayAPIServerClient) DeleteRayJob(request *api.DeleteRayJobRequest) (string, error) {
	deleteURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobs/" + request.Name
	response, err := krc.doDelete(deleteURL)
	return response, err
}

// CreateRayService create a new ray serve.
func (krc *KuberayAPIServerClient) CreateRayService(request *api.CreateRayServiceRequest) (*api.RayService, *rpcStatus.Status, error) {
	createURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/services"
	bytez, err := krc.marshaler.Marshal(request.Service)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal api.Cluster to JSON: %w", err)
	}

	jsonBody := string(bytez)

	response, err := krc.ExecRequest(createURL, jsonBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	rayService := &api.RayService{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), rayService); err != nil {
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

	response, err := krc.ExecRequest(updateURL, jsonBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	rayService := &api.RayService{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), rayService); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return rayService, nil, nil
}

// Find a specific ray serve by name and namespace.
func (krc *KuberayAPIServerClient) GetRayService(request *api.GetRayServiceRequest) (*api.RayService, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/services/" + request.Name
	response, err := krc.ExecRequest(getURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}
	rayService := &api.RayService{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), rayService); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return rayService, nil, nil
}

// Finds all ray services in a given namespace.
func (krc *KuberayAPIServerClient) ListRayServices(request *api.ListRayServicesRequest) (*api.ListRayServicesResponse, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/services"
	response, err := krc.ExecRequest(getURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}
	rayServiceResponses := &api.ListRayServicesResponse{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), rayServiceResponses); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}

	return rayServiceResponses, nil, nil
}

// Finds all ray services in a given namespace.
func (krc *KuberayAPIServerClient) ListAllRayServices() (*api.ListAllRayServicesResponse, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/services"
	response, err := krc.ExecRequest(getURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	allRayServiceResponses := &api.ListAllRayServicesResponse{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), allRayServiceResponses); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return allRayServiceResponses, nil, nil
}

// DeleteRayService deletes a ray service by its name and namespace
func (krc *KuberayAPIServerClient) DeleteRayService(request *api.DeleteRayServiceRequest) (string, error) {
	deleteURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/services/" + request.Name
	response, err := krc.doDelete(deleteURL)
	return response, err
}

// SubmitRayJob creates a new job on a given cluster.
func (krc *KuberayAPIServerClient) SubmitRayJob(request *api.SubmitRayJobRequest) (*api.SubmitRayJobReply, *rpcStatus.Status, error) {
	createURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername
	bytez, err := krc.marshaler.Marshal(request.Jobsubmission)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal api.Cluster to JSON: %w", err)
	}
	jsonBody := string(bytez)

	// Execute the curl command inside the pod using kubectl exec
	response, err := krc.ExecRequest(createURL, jsonBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	submission := &api.SubmitRayJobReply{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), submission); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return submission, nil, nil
}

// GetRayJobDetails. Get details about specific job on a given cluster.
func (krc *KuberayAPIServerClient) GetRayJobDetails(request *api.GetJobDetailsRequest) (*api.JobSubmissionInfo, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername + "/" + request.Submissionid
	response, err := krc.ExecRequest(getURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	jobSubmission := &api.JobSubmissionInfo{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), jobSubmission); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return jobSubmission, nil, nil
}

// GetRayJobLog. Get log for a specific job on a given cluster.
func (krc *KuberayAPIServerClient) GetRayJobLog(request *api.GetJobLogRequest) (*api.GetJobLogReply, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername + "/log/" + request.Submissionid
	response, err := krc.ExecRequest(getURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}
	jobLogReply := &api.GetJobLogReply{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), jobLogReply); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return jobLogReply, nil, nil
}

// ListRayJobsCluster. List Ray jobs on a given cluster.
func (krc *KuberayAPIServerClient) ListRayJobsCluster(request *api.ListJobDetailsRequest) (*api.ListJobSubmissionInfo, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername

	response, err := krc.ExecRequest(getURL, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	jobSubmissionInfo := &api.ListJobSubmissionInfo{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), jobSubmissionInfo); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return jobSubmissionInfo, nil, nil
}

// StopRayJob stops job on a given cluster.
func (krc *KuberayAPIServerClient) StopRayJob(request *api.StopRayJobSubmissionRequest) (string, error) {
	createURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername + "/" + request.Submissionid

	response, err := krc.ExecRequest(createURL, "")
	if err != nil {
		return "", fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}
	return response, nil
}

// DeleteRayService deletes a ray service by its name and namespace
func (krc *KuberayAPIServerClient) DeleteRayJobCluster(request *api.DeleteRayJobSubmissionRequest) (string, error) {
	deleteURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername + "/" + request.Submissionid
	response, err := krc.doDelete(deleteURL)
	return response, err
}

func (krc *KuberayAPIServerClient) doDelete(deleteURL string) (string, error) {
	// Execute the curl command inside the pod using kubectl exec
	response, err := krc.ExecRequest(deleteURL, "")
	if err != nil {
		return "", fmt.Errorf("failed to execute curl command inside pod: %w", err)
	}

	return response, err
}

//
// func (krc *KuberayAPIServerClient) executeRequest(httpRequest *http.Request, URL string) ([]byte, *rpcStatus.Status, error) {
// 	response, err := krc.httpClient.Do(httpRequest)
// 	if err != nil {
// 		return nil, nil, fmt.Errorf("failed to execute http request for url '%s': %w", URL, err)
// 	}
// 	defer func() {
// 		if closeErr := response.Body.Close(); closeErr != nil {
// 			klog.Errorf("Failed to close http response body because %+v", closeErr)
// 		}
// 	}()
// 	bodyBytes, err := io.ReadAll(response.Body)
// 	if err != nil {
// 		return nil, nil, fmt.Errorf("failed to read response body bytes: %w", err)
// 	}
// 	if response.StatusCode != http.StatusOK {
// 		status, err := krc.extractStatus(bodyBytes)
// 		if err != nil {
// 			return nil, nil, err
// 		}
// 		return nil, status, &KuberayAPIServerClientError{
// 			HTTPStatusCode: response.StatusCode,
// 		}
// 	}
// 	return bodyBytes, nil, nil
// }

// func (krc *KuberayAPIServerClient) extractStatus(bodyBytes []byte) (*rpcStatus.Status, error) {
// 	status := &rpcStatus.Status{}
// 	err := krc.unmarshaler.Unmarshal(bodyBytes, status)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to unmarshal status object: %w", err)
// 	}
// 	return status, nil
// }
