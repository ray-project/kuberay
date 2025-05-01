package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	rpcStatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	// "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	klog "k8s.io/klog/v2"

	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
)

type KuberayAPIServerClient struct {
	httpClient  *http.Client
	marshaler   *protojson.MarshalOptions
	unmarshaler *protojson.UnmarshalOptions
	// TODO(hjiang): here we use function to allow customized http request handling logic in unit test, worth revisiting if there're better ways;
	// for example, (1) wrap an interface to process request; (2) inject round-trip logic into http client.
	// See https://github.com/ray-project/kuberay/pull/3334/files#r2041183495 for details.
	//
	// Store http request handling function for unit test purpose.
	executeHttpRequest func(httpRequest *http.Request, URL string) ([]byte, *rpcStatus.Status, error)
	baseURL            string
}

type KuberayAPIServerExecClient struct {
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

func NewKuberayAPIServerExecClient(baseURL string) (*KuberayAPIServerExecClient, error) {
	kubeconfigPath := filepath.Join(os.Getenv("HOME"), ".kube", "config")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return &KuberayAPIServerExecClient{
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

func NewKuberayAPIServerClient(baseURL string, httpClient *http.Client) *KuberayAPIServerClient {
	client := &KuberayAPIServerClient{
		httpClient: httpClient,
		baseURL:    baseURL,
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
	}
	client.executeHttpRequest = client.executeRequest
	return client
}

func (krc *KuberayAPIServerExecClient) findPod(namespace string) (*corev1.Pod, error) {
	// Find the KubeRay API server pod

	// selector := labels.Set(map[string]string{
	// 	util.KubernetesComponentLabelKey: util.ComponentName,
	// }).AsSelector().String()

	// podListAll, err := krc.KubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	// if err != nil {
	// 	return nil, err
	// }

	podList, err := krc.KubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: "", // selector,
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

func (krc *KuberayAPIServerExecClient) ExecCommandWithCurlInPod(pod *corev1.Pod, url string, jsonBody string) (string, error) {
	var (
		execOut bytes.Buffer
		execErr bytes.Buffer
	)

	command := []string{
		"curl", "-s", "-X", "POST", url,
		"-H", "Content-Type: application/json",
		"-H", "Accept: application/json",
		"-d", jsonBody,
	}

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
func (krc *KuberayAPIServerExecClient) ExecRequest(url string, jsonBody string) (string, error) {
	pod, err := krc.findPod(manager.DefaultNamespace)
	if err != nil {
		return "", fmt.Errorf("could not find pod: %w", err)
	}

	execOut, err := krc.ExecCommandWithCurlInPod(pod, url, jsonBody)

	return execOut, err
}

// CreateComputeTemplate creates a new compute template.
func (krc *KuberayAPIServerExecClient) CreateComputeTemplate(request *api.CreateComputeTemplateRequest) (*api.ComputeTemplate, *rpcStatus.Status, error) {
	createURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/compute_templates"
	// createURL := "http://localhost:8888/apis/v1/namespaces/" + request.Namespace + "/compute_templates"

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

	// Now, parse the response into ComputeTemplate
	computeTemplate := &api.ComputeTemplate{}
	if err := krc.unmarshaler.Unmarshal([]byte(response), computeTemplate); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal response: %+w", err)
	}

	return computeTemplate, nil, nil
}

// CreateComputeTemplate creates a new compute template.
func (krc *KuberayAPIServerClient) CreateComputeTemplate(request *api.CreateComputeTemplateRequest) (*api.ComputeTemplate, *rpcStatus.Status, error) {
	createURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/compute_templates"

	bytez, err := krc.marshaler.Marshal(request.ComputeTemplate)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal api.ComputeTemplate to JSON: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(context.TODO(), "POST", createURL, bytes.NewReader(bytez))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", createURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")
	httpRequest.Header.Add("Content-Type", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, createURL)
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
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "GET", getURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", getURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, getURL)
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
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "GET", getURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", getURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, getURL)
	if err != nil {
		return nil, status, err
	}
	response := &api.ListAllComputeTemplatesResponse{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return response, nil, nil
}

// GetAllComputeTemplatesInNamespace Finds all compute templates in a given namespace.
func (krc *KuberayAPIServerClient) GetAllComputeTemplatesInNamespace(request *api.ListComputeTemplatesRequest) (*api.ListComputeTemplatesResponse, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/compute_templates"
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "GET", getURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", getURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, getURL)
	if err != nil {
		return nil, status, err
	}
	response := &api.ListComputeTemplatesResponse{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
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

	httpRequest, err := http.NewRequestWithContext(context.TODO(), "POST", createURL, bytes.NewReader(bytez))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", createURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")
	httpRequest.Header.Add("Content-Type", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, createURL)
	if err != nil {
		return nil, status, err
	}
	cluster := &api.Cluster{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, cluster); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return cluster, nil, nil
}

// DeleteCluster deletes a cluster
func (krc *KuberayAPIServerClient) DeleteCluster(request *api.DeleteClusterRequest) (*rpcStatus.Status, error) {
	deleteURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/clusters/" + request.Name
	return krc.doDelete(deleteURL)
}

// GetCluster finds a specific Cluster by ID.
func (krc *KuberayAPIServerClient) GetCluster(request *api.GetClusterRequest) (*api.Cluster, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/clusters/" + request.Name
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "GET", getURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", getURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, getURL)
	if err != nil {
		return nil, status, err
	}
	cluster := &api.Cluster{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, cluster); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return cluster, nil, nil
}

// ListCluster finds all clusters in a given namespace.
func (krc *KuberayAPIServerClient) ListClusters(request *api.ListClustersRequest) (*api.ListClustersResponse, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/clusters"
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "GET", getURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", getURL, err)
	}

	q := httpRequest.URL.Query()
	q.Set("limit", strconv.FormatInt(request.Limit, 10))
	q.Set("continue", request.Continue)
	httpRequest.URL.RawQuery = q.Encode()

	httpRequest.Header.Add("Accept", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, getURL)
	if err != nil {
		return nil, status, err
	}
	response := &api.ListClustersResponse{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return response, nil, nil
}

// ListAllClusters finds all Clusters in all namespaces.
func (krc *KuberayAPIServerClient) ListAllClusters(request *api.ListAllClustersRequest) (*api.ListAllClustersResponse, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/clusters"
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "GET", getURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", getURL, err)
	}

	q := httpRequest.URL.Query()
	q.Set("limit", strconv.FormatInt(request.Limit, 10))
	q.Set("continue", request.Continue)
	httpRequest.URL.RawQuery = q.Encode()

	httpRequest.Header.Add("Accept", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, getURL)
	if err != nil {
		return nil, status, err
	}
	response := &api.ListAllClustersResponse{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
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

	httpRequest, err := http.NewRequestWithContext(context.TODO(), "POST", createURL, bytes.NewReader(bytez))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", createURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")
	httpRequest.Header.Add("Content-Type", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, createURL)
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
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "GET", getURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", getURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, getURL)
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
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobs"
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "GET", getURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", getURL, err)
	}

	q := httpRequest.URL.Query()
	q.Set("limit", strconv.FormatInt(request.Limit, 10))
	q.Set("continue", request.Continue)
	httpRequest.URL.RawQuery = q.Encode()

	httpRequest.Header.Add("Accept", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, getURL)
	if err != nil {
		return nil, status, err
	}
	response := &api.ListRayJobsResponse{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return response, nil, nil
}

// ListAllRayJobs Finds all job in all namespaces.
func (krc *KuberayAPIServerClient) ListAllRayJobs(request *api.ListAllRayJobsRequest) (*api.ListAllRayJobsResponse, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/jobs"
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "GET", getURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", getURL, err)
	}

	q := httpRequest.URL.Query()
	q.Set("limit", strconv.FormatInt(request.Limit, 10))
	q.Set("continue", request.Continue)
	httpRequest.URL.RawQuery = q.Encode()
	httpRequest.Header.Add("Accept", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, getURL)
	if err != nil {
		return nil, status, err
	}
	response := &api.ListAllRayJobsResponse{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return response, nil, nil
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

	httpRequest, err := http.NewRequestWithContext(context.TODO(), "POST", createURL, bytes.NewReader(bytez))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", createURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")
	httpRequest.Header.Add("Content-Type", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, createURL)
	if err != nil {
		return nil, status, err
	}
	rayService := &api.RayService{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, rayService); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
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

	httpRequest, err := http.NewRequestWithContext(context.TODO(), "PUT", updateURL, bytes.NewReader(bytez))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", updateURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")
	httpRequest.Header.Add("Content-Type", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, updateURL)
	if err != nil {
		return nil, status, err
	}
	rayService := &api.RayService{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, rayService); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return rayService, nil, nil
}

// Find a specific ray serve by name and namespace.
func (krc *KuberayAPIServerClient) GetRayService(request *api.GetRayServiceRequest) (*api.RayService, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/services/" + request.Name
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "GET", getURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", getURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, getURL)
	if err != nil {
		return nil, status, err
	}
	response := &api.RayService{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return response, nil, nil
}

// Finds all ray services in a given namespace.
func (krc *KuberayAPIServerClient) ListRayServices(request *api.ListRayServicesRequest) (*api.ListRayServicesResponse, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/services"
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "GET", getURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", getURL, err)
	}

	q := httpRequest.URL.Query()
	q.Set("pageSize", strconv.FormatInt(int64(request.PageSize), 10))
	q.Set("pageToken", request.PageToken)
	httpRequest.URL.RawQuery = q.Encode()
	httpRequest.Header.Add("Accept", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, getURL)
	if err != nil {
		return nil, status, err
	}
	response := &api.ListRayServicesResponse{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}

	return response, nil, nil
}

// Finds all ray services in a given namespace.
func (krc *KuberayAPIServerClient) ListAllRayServices() (*api.ListAllRayServicesResponse, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/services"
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "GET", getURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", getURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, getURL)
	if err != nil {
		return nil, status, err
	}
	response := &api.ListAllRayServicesResponse{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return response, nil, nil
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

	httpRequest, err := http.NewRequestWithContext(context.TODO(), "POST", createURL, bytes.NewReader(bytez))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", createURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")
	httpRequest.Header.Add("Content-Type", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, createURL)
	if err != nil {
		return nil, status, err
	}
	submission := &api.SubmitRayJobReply{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, submission); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return submission, nil, nil
}

// GetRayJobDetails. Get details about specific job on a given cluster.
func (krc *KuberayAPIServerClient) GetRayJobDetails(request *api.GetJobDetailsRequest) (*api.JobSubmissionInfo, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername + "/" + request.Submissionid
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "GET", getURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", getURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, getURL)
	if err != nil {
		return nil, status, err
	}
	response := &api.JobSubmissionInfo{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return response, nil, nil
}

// GetRayJobLog. Get log for a specific job on a given cluster.
func (krc *KuberayAPIServerClient) GetRayJobLog(request *api.GetJobLogRequest) (*api.GetJobLogReply, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername + "/log/" + request.Submissionid
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "GET", getURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", getURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, getURL)
	if err != nil {
		return nil, status, err
	}
	response := &api.GetJobLogReply{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return response, nil, nil
}

// ListRayJobsCluster. List Ray jobs on a given cluster.
func (krc *KuberayAPIServerClient) ListRayJobsCluster(request *api.ListJobDetailsRequest) (*api.ListJobSubmissionInfo, *rpcStatus.Status, error) {
	getURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "GET", getURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http request for url '%s': %w", getURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")

	bodyBytes, status, err := krc.executeHttpRequest(httpRequest, getURL)
	if err != nil {
		return nil, status, err
	}
	response := &api.ListJobSubmissionInfo{}
	if err := krc.unmarshaler.Unmarshal(bodyBytes, response); err != nil {
		return nil, status, fmt.Errorf("failed to unmarshal: %+w", err)
	}
	return response, nil, nil
}

// StopRayJob stops job on a given cluster.
func (krc *KuberayAPIServerClient) StopRayJob(request *api.StopRayJobSubmissionRequest) (*rpcStatus.Status, error) {
	createURL := krc.baseURL + "/apis/v1/namespaces/" + request.Namespace + "/jobsubmissions/" + request.Clustername + "/" + request.Submissionid

	httpRequest, err := http.NewRequestWithContext(context.TODO(), "POST", createURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create http request for url '%s': %w", createURL, err)
	}

	httpRequest.Header.Add("Accept", "application/json")
	httpRequest.Header.Add("Content-Type", "application/json")

	_, status, err := krc.executeHttpRequest(httpRequest, createURL)
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
	httpRequest, err := http.NewRequestWithContext(context.TODO(), "DELETE", deleteURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create http request for url '%s': %w", deleteURL, err)
	}
	httpRequest.Header.Add("Accept", "application/json")
	_, status, err := krc.executeHttpRequest(httpRequest, deleteURL)
	return status, err
}

func (krc *KuberayAPIServerClient) executeRequest(httpRequest *http.Request, URL string) ([]byte, *rpcStatus.Status, error) {
	response, err := krc.httpClient.Do(httpRequest)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute http request for url '%s': %w", URL, err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			klog.Errorf("Failed to close http response body because %+v", closeErr)
		}
	}()
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response body bytes: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		status, err := krc.extractStatus(bodyBytes)
		if err != nil {
			return nil, nil, err
		}
		return nil, status, &KuberayAPIServerClientError{
			HTTPStatusCode: response.StatusCode,
		}
	}
	return bodyBytes, nil, nil
}

func (krc *KuberayAPIServerClient) extractStatus(bodyBytes []byte) (*rpcStatus.Status, error) {
	status := &rpcStatus.Status{}
	err := krc.unmarshaler.Unmarshal(bodyBytes, status)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal status object: %w", err)
	}
	return status, nil
}
