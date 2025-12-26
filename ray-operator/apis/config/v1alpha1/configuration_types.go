package v1alpha1

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/dashboardclient"
)

//+kubebuilder:object:root=true

// Configuration is the Schema for Ray operator config.
type Configuration struct {
	// Burst allows temporary exceeding of QPS limit for handling request spikes.
	// Default: 200
	Burst *int `json:"burst,omitempty"`

	// QPS controls the maximum requests per second to the Kubernetes API server.
	// Default: 100.0
	QPS *float64 `json:"qps,omitempty"`

	// EnableLeaderElection enables leader election. Enabling this will ensure
	// there is only one active instance of the operator.
	EnableLeaderElection *bool `json:"enableLeaderElection,omitempty"`

	metav1.TypeMeta `json:",inline"`

	// LogStdoutEncoder is the encoder to use when logging to stdout. Valid values are "json" and "console".
	// Defaults to `json` if empty.
	LogStdoutEncoder string `json:"logStdoutEncoder,omitempty"`

	// ProbeAddr is the address the probe endpoint binds to.
	ProbeAddr string `json:"probeAddr,omitempty"`

	// LogFile is a path to a local file for synchronizing logs.
	LogFile string `json:"logFile,omitempty"`

	// LogFileEncoder is the encoder to use when logging to a file. Valid values are "json" and "console".
	// Defaults to `json` if empty.
	LogFileEncoder string `json:"logFileEncoder,omitempty"`

	// LeaderElectionNamespace is the namespace where the leader election
	// resources live. Defaults to the pod namesapce if not set.
	LeaderElectionNamespace string `json:"leaderElectionNamespace,omitempty"`

	// BatchScheduler enables the batch scheduler integration with a specific scheduler
	// based on the given name, currently, supported values are volcano, yunikorn, kai-scheduler.
	BatchScheduler string `json:"batchScheduler,omitempty"`

	// MetricsAddr is the address the metrics endpoint binds to.
	MetricsAddr string `json:"metricsAddr,omitempty"`

	// WatchNamespace specifies a list of namespaces to watch for custom resources, separated by commas.
	// If empty, all namespaces will be watched.
	WatchNamespace string `json:"watchNamespace,omitempty"`

	// WorkerSidecarContainers includes specification for a sidecar container
	// to inject into every Worker pod.
	WorkerSidecarContainers []corev1.Container `json:"workerSidecarContainers,omitempty"`

	// HeadSidecarContainers includes specification for a sidecar container
	// to inject into every Head pod.
	HeadSidecarContainers []corev1.Container `json:"headSidecarContainers,omitempty"`

	// DefaultContainerEnvs specifies default environment variables to inject into all Ray containers
	DefaultContainerEnvs []corev1.EnvVar `json:"defaultContainerEnvs,omitempty"`

	// ReconcileConcurrency is the max concurrency for each reconciler.
	ReconcileConcurrency int `json:"reconcileConcurrency,omitempty"`

	// EnableBatchScheduler enables the batch scheduler. Currently this is supported
	// by Volcano to support gang scheduling.
	EnableBatchScheduler bool `json:"enableBatchScheduler,omitempty"`

	// UseKubernetesProxy indicates that the services/proxy and pods/proxy subresource should be used
	// when connecting to the Ray Head node. This is useful when network policies disallow
	// ingress traffic to the Ray cluster from other pods or Kuberay is running in a network without
	// connectivity to Pods.
	UseKubernetesProxy bool `json:"useKubernetesProxy,omitempty"`

	// DeleteRayJobAfterJobFinishes deletes the RayJob CR itself if shutdownAfterJobFinishes is set to true.
	DeleteRayJobAfterJobFinishes bool `json:"deleteRayJobAfterJobFinishes,omitempty"`

	// EnableMetrics indicates whether KubeRay operator should emit control plane metrics.
	EnableMetrics bool `json:"enableMetrics,omitempty"`
}

func (config Configuration) GetDashboardClient(ctx context.Context, mgr manager.Manager) func(rayCluster *rayv1.RayCluster, url string) (dashboardclient.RayDashboardClientInterface, error) {
	return utils.GetRayDashboardClientFunc(ctx, mgr, config.UseKubernetesProxy)
}

func (config Configuration) GetHttpProxyClient(mgr manager.Manager) func(hostIp, podNamespace, podName string, port int) utils.RayHttpProxyClientInterface {
	return utils.GetRayHttpProxyClientFunc(mgr, config.UseKubernetesProxy)
}
