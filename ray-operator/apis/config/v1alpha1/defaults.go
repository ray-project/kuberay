package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	DefaultMetricsAddr          = ":8080"
	DefaultProbeAddr            = ":8082"
	DefaultEnableLeaderElection = true
	DefaultReconcileConcurrency = 1
	DefaultQPS                  = float64(100)
	DefaultBurst                = 200
)

func addDefaultingFuncs(scheme *runtime.Scheme) error {
	scheme.AddTypeDefaultingFunc(&Configuration{}, func(obj any) {
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
		cfg.EnableLeaderElection = new(DefaultEnableLeaderElection)
	}

	if cfg.ReconcileConcurrency == 0 {
		cfg.ReconcileConcurrency = DefaultReconcileConcurrency
	}

	if cfg.QPS == nil {
		cfg.QPS = new(DefaultQPS)
	}

	if cfg.Burst == nil {
		cfg.Burst = new(DefaultBurst)
	}
}
