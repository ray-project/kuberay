package utils

import (
	"context"
	"fmt"
	"net/http"
	"time"

	fmtErrors "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	dashboardinternal "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/dashboard-internal"
	utiltypes "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/types"
)

type RayDashboardClient struct {
	mgr                        ctrl.Manager
	RayDashboardInternalClient utiltypes.RayDashboardInternalClientInterface
	useKubernetesProxy         bool
}

func GetRayDashboardClientFunc(mgr ctrl.Manager, useKubernetesProxy bool) func() utiltypes.RayDashboardClientInterface {
	return func() utiltypes.RayDashboardClientInterface {
		return &RayDashboardClient{
			mgr:                        mgr,
			RayDashboardInternalClient: &dashboardinternal.RayDashboardInternalClient{},
			useKubernetesProxy:         useKubernetesProxy,
		}
	}
}

// FetchHeadServiceURL fetches the URL that consists of the FQDN for the RayCluster's head service
// and the port with the given port name (defaultPortName).
func FetchHeadServiceURL(ctx context.Context, cli client.Client, rayCluster *rayv1.RayCluster, defaultPortName string) (string, error) {
	log := ctrl.LoggerFrom(ctx)
	headSvc := &corev1.Service{}
	headSvcName, err := GenerateHeadServiceName(RayClusterCRD, rayCluster.Spec, rayCluster.Name)
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

	domainName := GetClusterDomainName()
	headServiceURL := fmt.Sprintf("%s.%s.svc.%s:%v",
		headSvc.Name,
		headSvc.Namespace,
		domainName,
		port)
	log.Info("FetchHeadServiceURL", "head service URL", headServiceURL)
	return headServiceURL, nil
}

func (r *RayDashboardClient) InitClient(ctx context.Context, url string, rayCluster *rayv1.RayCluster) error {
	log := ctrl.LoggerFrom(ctx)

	if r.useKubernetesProxy {
		var err error
		headSvcName := rayCluster.Status.Head.ServiceName
		if headSvcName == "" {
			log.Info("RayCluster is missing .status.head.serviceName, calling GenerateHeadServiceName instead...", "RayCluster name", rayCluster.Name, "namespace", rayCluster.Namespace)
			headSvcName, err = GenerateHeadServiceName(RayClusterCRD, rayCluster.Spec, rayCluster.Name)
			if err != nil {
				return err
			}
		}

		r.RayDashboardInternalClient.SetClient(r.mgr.GetHTTPClient())
		r.RayDashboardInternalClient.SetDashboardURL(fmt.Sprintf("%s/api/v1/namespaces/%s/services/%s:dashboard/proxy", r.mgr.GetConfig().Host, rayCluster.Namespace, headSvcName))
		return nil
	}

	r.RayDashboardInternalClient.SetClient(&http.Client{
		Timeout: 2 * time.Second,
	})

	r.RayDashboardInternalClient.SetDashboardURL("http://" + url)
	return nil
}

// Implement all RayDashboardInternalClientInterface methods by delegating
func (r *RayDashboardClient) UpdateDeployments(ctx context.Context, configJson []byte) error {
	return r.RayDashboardInternalClient.UpdateDeployments(ctx, configJson)
}

func (r *RayDashboardClient) GetServeDetails(ctx context.Context) (*utiltypes.ServeDetails, error) {
	return r.RayDashboardInternalClient.GetServeDetails(ctx)
}

func (r *RayDashboardClient) GetMultiApplicationStatus(ctx context.Context) (map[string]*utiltypes.ServeApplicationStatus, error) {
	return r.RayDashboardInternalClient.GetMultiApplicationStatus(ctx)
}

func (r *RayDashboardClient) GetJobInfo(ctx context.Context, jobId string) (*utiltypes.RayJobInfo, error) {
	return r.RayDashboardInternalClient.GetJobInfo(ctx, jobId)
}

func (r *RayDashboardClient) ListJobs(ctx context.Context) (*[]utiltypes.RayJobInfo, error) {
	return r.RayDashboardInternalClient.ListJobs(ctx)
}

func (r *RayDashboardClient) SubmitJob(ctx context.Context, rayJob *rayv1.RayJob) (string, error) {
	return r.RayDashboardInternalClient.SubmitJob(ctx, rayJob)
}

func (r *RayDashboardClient) SubmitJobReq(ctx context.Context, request *utiltypes.RayJobRequest, name *string) (string, error) {
	return r.RayDashboardInternalClient.SubmitJobReq(ctx, request, name)
}

func (r *RayDashboardClient) GetJobLog(ctx context.Context, jobName string) (*string, error) {
	return r.RayDashboardInternalClient.GetJobLog(ctx, jobName)
}

func (r *RayDashboardClient) StopJob(ctx context.Context, jobName string) error {
	return r.RayDashboardInternalClient.StopJob(ctx, jobName)
}

func (r *RayDashboardClient) DeleteJob(ctx context.Context, jobName string) error {
	return r.RayDashboardInternalClient.DeleteJob(ctx, jobName)
}

func (r *RayDashboardClient) SetClient(client *http.Client) {
	r.RayDashboardInternalClient.SetClient(client)
}

func (r *RayDashboardClient) GetDashboardURL() string {
	return r.RayDashboardInternalClient.GetDashboardURL()
}

func (r *RayDashboardClient) SetDashboardURL(url string) {
	r.RayDashboardInternalClient.SetDashboardURL(url)
}
