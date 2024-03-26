package utils

import (
	"fmt"
	"io"
	"net/http"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
)

type RayHttpProxyClientInterface interface {
	InitClient()
	WithKubernetesPodProxy(podNamespace, podName string, port int)
	CheckHealth() error
	SetHostIp(hostIp string, port int)
}

func GetRayHttpProxyClientFunc(mgr ctrl.Manager) func() RayHttpProxyClientInterface {
	return func() RayHttpProxyClientInterface {
		return &RayHttpProxyClient{
			mgr: mgr,
		}
	}
}

type RayHttpProxyClient struct {
	client       *http.Client
	httpProxyURL string

	mgr ctrl.Manager
}

func (r *RayHttpProxyClient) InitClient() {
	r.client = &http.Client{
		Timeout: 20 * time.Millisecond,
	}
}

func (r *RayHttpProxyClient) WithKubernetesPodProxy(podNamespace, podName string, port int) {
	r.client = r.mgr.GetHTTPClient()
	r.httpProxyURL = fmt.Sprintf("%s/api/v1/namespaces/%s/pods/%s:%d/proxy/", r.mgr.GetConfig().Host, podNamespace, podName, port)
}

func (r *RayHttpProxyClient) SetHostIp(hostIp string, port int) {
	r.httpProxyURL = fmt.Sprintf("http://%s:%d/", hostIp, port)
}

func (r *RayHttpProxyClient) CheckHealth() error {
	req, err := http.NewRequest("GET", r.httpProxyURL+RayServeProxyHealthPath, nil)
	if err != nil {
		return err
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("RayHttpProxyClient CheckHealth fail: %s %s", resp.Status, string(body))
	}

	return nil
}
