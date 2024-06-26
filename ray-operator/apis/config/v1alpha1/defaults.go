package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

const (
	DefaultMetricsAddr          = ":8080"
	DefaultProbeAddr            = ":8082"
	DefaultEnableLeaderElection = true
	DefaultReconcileConcurrency = 1
)

func addDefaultingFuncs(scheme *runtime.Scheme) error {
	scheme.AddTypeDefaultingFunc(&Configuration{}, func(obj interface{}) {
		SetDefaults_Configuration(obj.(*Configuration))
	})
	return nil
}

// SetDefaults_Configuration sets default values for ComponentConfig.
func SetDefaults_Configuration(cfg *Configuration) {
	if cfg.MetricsAddr == "" {
		cfg.MetricsAddr = DefaultMetricsAddr
	}

	if cfg.ProbeAddr == "" {
		cfg.ProbeAddr = DefaultProbeAddr
	}

	if cfg.EnableLeaderElection == nil {
		cfg.EnableLeaderElection = ptr.To(DefaultEnableLeaderElection)
	}

	if cfg.ReconcileConcurrency == 0 {
		cfg.ReconcileConcurrency = DefaultReconcileConcurrency
	}
}
