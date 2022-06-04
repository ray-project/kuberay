package utils

import (
	"bytes"
	"io/ioutil"
	"net/http"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"k8s.io/apimachinery/pkg/util/json"
)

var (
	DEPLOY_PATH = "/api/serve/deployments/"
	STATUS_PATH = "/api/serve/deployments/status"
)

type ServingClusterDeployments struct {
	Deployments []rayv1alpha1.ServeConfigSpec `json:"deployments,omitempty"`
}

type RayDashboardClient struct {
	client       http.Client
	dashboardURL string
}

func (r *RayDashboardClient) InitClient(url string) {
	r.client = http.Client{}
	r.dashboardURL = "http://" + url
}

func (r *RayDashboardClient) GetDeployments() (string, error) {
	req, err := http.NewRequest("GET", r.dashboardURL+DEPLOY_PATH, nil)
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

func (r *RayDashboardClient) UpdateDeployments(specs []rayv1alpha1.ServeConfigSpec) error {

	servingClusterDeployments := ServingClusterDeployments{
		Deployments: specs,
	}

	deploymentJson, err := json.Marshal(servingClusterDeployments)

	if err != nil {
		return err
	}

	req, err := http.NewRequest("PUT", r.dashboardURL+DEPLOY_PATH, bytes.NewBuffer(deploymentJson))
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

func (r *RayDashboardClient) GetDeploymentsStatus() (*rayv1alpha1.ServeStatuses, error) {
	req, err := http.NewRequest("GET", r.dashboardURL+STATUS_PATH, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(req)

	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)

	var serveStatuses rayv1alpha1.ServeStatuses
	if err = json.Unmarshal(body, &serveStatuses); err != nil {
		return nil, err
	}

	return &serveStatuses, nil
}
