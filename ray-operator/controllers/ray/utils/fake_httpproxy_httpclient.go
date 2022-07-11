package utils

import (
	"fmt"
	"net/http"
	"time"
)

func GetFakeRayHttpProxyClient() RayHttpProxyClientInterface {
	return &FakeRayHttpProxyClient{}
}

type FakeRayHttpProxyClient struct {
	client       http.Client
	httpProxyURL string
}

func (r *FakeRayHttpProxyClient) InitClient() {
	r.client = http.Client{
		Timeout: 20 * time.Millisecond,
	}
}

func (r *FakeRayHttpProxyClient) SetHostIp(hostIp string) {
	r.httpProxyURL = fmt.Sprint("http://", hostIp, ":", DefaultHttpProxyPort)
}

func (r *FakeRayHttpProxyClient) CheckHealth() error {
	// Always return successful.
	return nil
}
