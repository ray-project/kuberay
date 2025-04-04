package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

//+kubebuilder:object:root=true

// Configuration is the Schema for Ray operator config.
type Configuration struct {
	metav1.TypeMeta `json:",inline"`

	// MetricsAddr is the address the metrics endpoint binds to.
	MetricsAddr string `json:"metricsAddr,omitempty"`

	// ProbeAddr is the address the probe endpoint binds to.
	ProbeAddr string `json:"probeAddr,omitempty"`

	// EnableLeaderElection enables leader election. Enabling this will ensure
	// there is only one active instance of the operator.
	EnableLeaderElection *bool `json:"enableLeaderElection,omitempty"`

	// LeaderElectionNamespace is the namespace where the leader election
	// resources live. Defaults to the pod namesapce if not set.
	LeaderElectionNamespace string `json:"leaderElectionNamespace,omitempty"`

	// WatchNamespace specifies a list of namespaces to watch for custom resources, separated by commas.
	// If empty, all namespaces will be watched.
	WatchNamespace string `json:"watchNamespace,omitempty"`

	// LogFile is a path to a local file for synchronizing logs.
	LogFile string `json:"logFile,omitempty"`

	// LogFileEncoder is the encoder to use when logging to a file. Valid values are "json" and "console".
	// Defaults to `json` if empty.
	LogFileEncoder string `json:"logFileEncoder,omitempty"`

	// LogFileEncoder is the encoder to use when logging to a file. Valid values are "json" and "console".
	// Defaults to `json` if empty.
	LogStdoutEncoder string `json:"logStdoutEncoder,omitempty"`

	// BatchScheduler enables the batch scheduler integration with a specific scheduler
	// based on the given name, currently, supported values are volcano and yunikorn.
	BatchScheduler string `json:"batchScheduler,omitempty"`

	// HeadSidecarContainers includes specification for a sidecar container
	// to inject into every Head pod.
	HeadSidecarContainers []corev1.Container `json:"headSidecarContainers,omitempty"`

	// WorkerSidecarContainers includes specification for a sidecar container
	// to inject into every Worker pod.
	WorkerSidecarContainers []corev1.Container `json:"workerSidecarContainers,omitempty"`

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

func (config Configuration) GetDashboardClient(mgr manager.Manager) func() utils.RayDashboardClientInterface {
	return utils.GetRayDashboardClientFunc(mgr, config.UseKubernetesProxy)
}

func (config Configuration) GetHttpProxyClient(mgr manager.Manager) func() utils.RayHttpProxyClientInterface {
	return utils.GetRayHttpProxyClientFunc(mgr, config.UseKubernetesProxy)
}
