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
	EnableLeaderElection         *bool `json:"enableLeaderElection,omitempty"`
	EnableMTLS                   *bool `json:"enableMTLS,omitempty"`
	metav1.TypeMeta              `json:",inline"`
	LogFile                      string             `json:"logFile,omitempty"`
	LeaderElectionNamespace      string             `json:"leaderElectionNamespace,omitempty"`
	WatchNamespace               string             `json:"watchNamespace,omitempty"`
	ProbeAddr                    string             `json:"probeAddr,omitempty"`
	LogFileEncoder               string             `json:"logFileEncoder,omitempty"`
	LogStdoutEncoder             string             `json:"logStdoutEncoder,omitempty"`
	BatchScheduler               string             `json:"batchScheduler,omitempty"`
	MetricsAddr                  string             `json:"metricsAddr,omitempty"`
	MTLSSecretNamespace          string             `json:"mtlsSecretNamespace,omitempty"`
	CertGeneratorImage           string             `json:"certGeneratorImage,omitempty"`
	HeadSidecarContainers        []corev1.Container `json:"headSidecarContainers,omitempty"`
	WorkerSidecarContainers      []corev1.Container `json:"workerSidecarContainers,omitempty"`
	ReconcileConcurrency         int                `json:"reconcileConcurrency,omitempty"`
	UseKubernetesProxy           bool               `json:"useKubernetesProxy,omitempty"`
	DeleteRayJobAfterJobFinishes bool               `json:"deleteRayJobAfterJobFinishes,omitempty"`
	EnableMetrics                bool               `json:"enableMetrics,omitempty"`
	EnableBatchScheduler         bool               `json:"enableBatchScheduler,omitempty"`
}

func (config Configuration) GetDashboardClient(mgr manager.Manager) func() utils.RayDashboardClientInterface {
	return utils.GetRayDashboardClientFunc(mgr, config.UseKubernetesProxy)
}

func (config Configuration) GetHttpProxyClient(mgr manager.Manager) func() utils.RayHttpProxyClientInterface {
	return utils.GetRayHttpProxyClientFunc(mgr, config.UseKubernetesProxy)
}
