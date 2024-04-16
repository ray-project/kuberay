package utils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
)

type RayHttpProxyClientInterface interface {
	InitClient()
	CheckProxyActorHealth(ctx context.Context) error
	SetHostIp(hostIp string, port int)
}

func GetRayHttpProxyClient() RayHttpProxyClientInterface {
	return &RayHttpProxyClient{}
}

type RayHttpProxyClient struct {
	client       http.Client
	httpProxyURL string
}

func (r *RayHttpProxyClient) InitClient() {
	r.client = http.Client{
		Timeout: 2 * time.Second,
	}
}

func (r *RayHttpProxyClient) SetHostIp(hostIp string, port int) {
	r.httpProxyURL = fmt.Sprintf("http://%s:%d/", hostIp, port)
}

// CheckProxyActorHealth checks the health status of the Ray Serve proxy actor.
func (r *RayHttpProxyClient) CheckProxyActorHealth(ctx context.Context) error {
	logger := ctrl.LoggerFrom(ctx)
	resp, err := r.client.Get(r.httpProxyURL + RayServeProxyHealthPath)
	if err != nil {
		logger.Error(err, "CheckProxyActorHealth fails.")
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		err := fmt.Errorf("CheckProxyActorHealth fails: Status code is not 200")
		logger.Error(err, "CheckProxyActorHealth fails.", "status code", resp.StatusCode, "status", resp.Status, "body", string(body))
		return err
	}

	return nil
}
