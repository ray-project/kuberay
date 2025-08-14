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
	DefaultEnableMTLS           = false
	DefaultCertGeneratorImage   = "registry.redhat.io/ubi9@sha256:770cf07083e1c85ae69c25181a205b7cdef63c11b794c89b3b487d4670b4c328"
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

	if cfg.EnableMTLS == nil {
		cfg.EnableMTLS = ptr.To(DefaultEnableMTLS)
	}

	if cfg.CertGeneratorImage == "" {
		cfg.CertGeneratorImage = DefaultCertGeneratorImage
	}
}
