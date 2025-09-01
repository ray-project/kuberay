package apiserversdk

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	rpcStatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	"github.com/ray-project/kuberay/apiserver/pkg/util"
)

type ProxyRoundTripper struct {
	Transport http.RoundTripper
}

func newProxyRoundTripper(cfg *rest.Config) (*ProxyRoundTripper, error) {
	transport, err := rest.TransportFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create http RoundTripper: %w", err)
	}

	return &ProxyRoundTripper{
		Transport: transport,
	}, nil
}

// RoundTrp send the request through the Kubernetes service proxy subresource
func (rt *ProxyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	newReq := req.Clone(req.Context())

	newReq.URL.Path = path.Join(
		"/api/v1/namespaces/ray-system/services/kuberay-apiserver:8888/proxy",
		req.URL.Path,
	)

	return rt.Transport.RoundTrip(newReq)
}

// RemoteExecuteClient allows executing HTTP requests against a service running inside a Kubernetes pod
// by using `kubectl exec`-style command execution, without requiring a NodePort for external access.
type RemoteExecuteClient struct {
	KubeClient  kubernetes.Interface        // Kubernetes client interface for API operations
	RestConfig  *rest.Config                // Kubernetes REST config for executing remote commands
	unmarshaler *protojson.UnmarshalOptions // Protobuf JSON unmarshaler for decoding API error responses
}

func NewRemoteExecuteClient() (*RemoteExecuteClient, error) {
	config, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return &RemoteExecuteClient{
		KubeClient: clientset,
		RestConfig: config,
		unmarshaler: &protojson.UnmarshalOptions{
			AllowPartial:   false,
			DiscardUnknown: false,
			Resolver:       nil,
		},
	}, nil
}

// ExecuteRequest executes an HTTP request by forwarding it to a Kubernetes pod using `kubectl exec`. It extracts the
// request body, locates the target pod, and invokes a curl command inside the pod to perform the request
func (rec *RemoteExecuteClient) executeRequest(httpRequest *http.Request, _ string) ([]byte, *rpcStatus.Status, error) {
	method := httpRequest.Method
	var body string

	if httpRequest.Body != nil {
		bodyBytes, err := io.ReadAll(httpRequest.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read request body: %w", err)
		}
		body = string(bodyBytes)
	}

	pod, err := rec.findPod(manager.DefaultNamespace)
	if err != nil {
		return nil, nil, fmt.Errorf("could not find pod: %w", err)
	}

	// call curl execution inside pod
	bodyBytes, err := rec.execCommandWithCurlInPod(pod, httpRequest.URL.String(), method, body)
	if err != nil {
		return nil, nil, err
	}

	return bodyBytes, nil, nil
}

// findPod locates the KubeRay API server pod by using a label selector. It assumes a single API server pod is running
// in the given namespace
func (rec *RemoteExecuteClient) findPod(namespace string) (*corev1.Pod, error) {
	selector := labels.Set(map[string]string{
		util.KubernetesComponentLabelKey: util.ComponentName,
	}).AsSelector().String()

	podList, err := rec.KubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pods found with label %s=%s", util.KubernetesComponentLabelKey, util.ComponentName)
	}
	targetPod := podList.Items[0]
	return &targetPod, nil
}

// execCommandWithCurlInPod executes a curl command inside the specified pod's container by `kubectl exec`
func (rec *RemoteExecuteClient) execCommandWithCurlInPod(pod *corev1.Pod, url string, method string, jsonBody string) ([]byte, error) {
	var (
		execOut bytes.Buffer
		execErr bytes.Buffer
	)

	// The http status code will be added in the end of the response body.
	// E.g. {foo: boo, ...}HTTP_STATUS:200
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
		return nil, fmt.Errorf("could not find container %s in pod", util.CurlContainerName)
	}

	req := rec.KubeClient.CoreV1().RESTClient().Post().
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

	// TODO: Consider migrating to WebSocketExecutor for streaming APIs in the future.
	// SPDYExecutor is currently used, but SPDY is being deprecated in Kubernetes.
	// See: https://kubernetes.io/blog/2024/08/20/websockets-transition/
	// API Reference: https://pkg.go.dev/k8s.io/client-go/tools/remotecommand#NewWebSocketExecutor
	exec, err := remotecommand.NewSPDYExecutor(rec.RestConfig, "POST", req.URL())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize executor: %w", err)
	}

	err = exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
		Stdout: &execOut,
		Stderr: &execErr,
		Tty:    false,
	})
	if err != nil {
		return nil, fmt.Errorf("command execution failed: %w", err)
	}

	if execErr.Len() > 0 {
		return nil, fmt.Errorf("failed to POST to %s: stderr=%q, stdout=%q", url, execErr.String(), execOut.String())
	}

	// Split the http status code (in the end of the response) out from the response body
	// Expected output: [{"foo": "boo", ... } 200]
	parts := strings.Split(execOut.String(), "HTTP_STATUS:")
	if len(parts) != 2 {
		return nil, fmt.Errorf("unexpected curl output format")
	}
	statusCodeStr := strings.TrimSpace(parts[1])
	statusCode, err := strconv.Atoi(statusCodeStr)
	if err != nil {
		return nil, fmt.Errorf("Cannot convert status code string to int: %s", statusCodeStr)
	}

	bodyBytes := []byte(parts[0])

	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", statusCode, string(bodyBytes))
	}
	return bodyBytes, nil
}
