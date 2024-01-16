package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	// ReconcileConcurrency is the max concurrency for each reconciler.
	ReconcileConcurrency int `json:"reconcileConcurrency,omitempty"`

	// WatchNamespace specifies a list of namespaces to watch for custom resources, separated by commas.
	// If empty, all namespaces will be watched.
	WatchNamespace string `json:"watchNamespace,omitempty"`

	// ForcedClusterUpgrade enables force upgrading clusters.
	ForcedClusterUpgrade bool `json:"forcedClusterUpgrade,omitempty"`

	// LogFile is a path to a local file for synchronizing logs.
	LogFile string `json:"logFile,omitempty"`

	// EnableBatchScheduler enables the batch scheduler. Currently this is supported
	// by Volcano to support gang scheduling.
	EnableBatchScheduler bool `json:"enableBatchScheduler,omitempty"`

	// HeadSidecarContainers includes specification for a sidecar container
	// to inject into every Head pod.
	HeadSidecarContainers []corev1.Container `json:"headSidecarContainers,omitempty"`

	// WorkerSidecarContainers includes specification for a sidecar container
	// to inject into every Worker pod.
	WorkerSidecarContainers []corev1.Container `json:"workerSidecarContainers,omitempty"`
}
