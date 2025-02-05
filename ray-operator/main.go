package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/go-logr/zapr"
	routev1 "github.com/openshift/api/route/v1"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	k8szap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	configapi "github.com/ray-project/kuberay/ray-operator/apis/config/v1alpha1"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
	webhooks "github.com/ray-project/kuberay/ray-operator/pkg/webhooks/v1"
	// +kubebuilder:scaffold:imports
)

var (
	scheme    = runtime.NewScheme()
	setupLog  = ctrl.Log.WithName("setup")
	userAgent = fmt.Sprintf("kuberay-operator/%s", utils.KUBERAY_VERSION)
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(rayv1.AddToScheme(scheme))
	utilruntime.Must(routev1.Install(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
	utilruntime.Must(configapi.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var leaderElectionNamespace string
	var probeAddr string
	var reconcileConcurrency int
	var watchNamespace string
	var forcedClusterUpgrade bool
	var logFile string
	var logFileEncoder string
	var logStdoutEncoder string
	var useKubernetesProxy bool
	var configFile string
	var featureGates string
	var enableBatchScheduler bool
	var batchScheduler string

	// TODO: remove flag-based config once Configuration API graduates to v1.
	flag.StringVar(&metricsAddr, "metrics-addr", configapi.DefaultMetricsAddr, "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", configapi.DefaultProbeAddr, "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", configapi.DefaultEnableLeaderElection,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&leaderElectionNamespace, "leader-election-namespace", "",
		"Namespace where the leader election resource lives. Defaults to the pod namespace if not set.")
	flag.IntVar(&reconcileConcurrency, "reconcile-concurrency", configapi.DefaultReconcileConcurrency, "max concurrency for reconciling")
	flag.StringVar(
		&watchNamespace,
		"watch-namespace",
		"",
		"Specify a list of namespaces to watch for custom resources, separated by commas. If left empty, all namespaces will be watched.")
	flag.BoolVar(&forcedClusterUpgrade, "forced-cluster-upgrade", false,
		"(Deprecated) Forced cluster upgrade flag")
	flag.StringVar(&logFile, "log-file-path", "",
		"Synchronize logs to local file")
	flag.StringVar(&logFileEncoder, "log-file-encoder", "json",
		"Encoder to use for log file. Valid values are 'json' and 'console'. Defaults to 'json'")
	flag.StringVar(&logStdoutEncoder, "log-stdout-encoder", "json",
		"Encoder to use for logging stdout. Valid values are 'json' and 'console'. Defaults to 'json'")
	flag.BoolVar(&enableBatchScheduler, "enable-batch-scheduler", false,
		"(Deprecated) Enable batch scheduler. Currently is volcano, which supports gang scheduler policy. Please use --batch-scheduler instead.")
	flag.StringVar(&batchScheduler, "batch-scheduler", "",
		"Batch scheduler name, supported values are volcano and yunikorn.")
	flag.StringVar(&configFile, "config", "", "Path to structured config file. Flags are ignored if config file is set.")
	flag.BoolVar(&useKubernetesProxy, "use-kubernetes-proxy", false,
		"Use Kubernetes proxy subresource when connecting to the Ray Head node.")
	flag.StringVar(&featureGates, "feature-gates", "", "A set of key=value pairs that describe feature gates. E.g. FeatureOne=true,FeatureTwo=false,...")

	opts := k8szap.Options{
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	var config configapi.Configuration
	if configFile != "" {
		var err error
		configData, err := os.ReadFile(configFile)
		exitOnError(err, "failed to read config file")

		config, err = decodeConfig(configData, scheme)
		exitOnError(err, "failed to decode config file")
	} else {
		config.MetricsAddr = metricsAddr
		config.ProbeAddr = probeAddr
		config.EnableLeaderElection = &enableLeaderElection
		config.LeaderElectionNamespace = leaderElectionNamespace
		config.ReconcileConcurrency = reconcileConcurrency
		config.WatchNamespace = watchNamespace
		config.LogFile = logFile
		config.LogFileEncoder = logFileEncoder
		config.LogStdoutEncoder = logStdoutEncoder
		config.EnableBatchScheduler = enableBatchScheduler
		config.BatchScheduler = batchScheduler
		config.UseKubernetesProxy = useKubernetesProxy
		config.DeleteRayJobAfterJobFinishes = os.Getenv(utils.DELETE_RAYJOB_CR_AFTER_JOB_FINISHES) == "true"
	}

	stdoutEncoder, err := newLogEncoder(logStdoutEncoder)
	exitOnError(err, "failed to create log encoder for stdout")
	opts.Encoder = stdoutEncoder

	if config.LogFile != "" {
		fileWriter := &lumberjack.Logger{
			Filename:   config.LogFile,
			MaxSize:    500, // megabytes
			MaxBackups: 10,  // files
			MaxAge:     30,  // days
		}

		fileEncoder, err := newLogEncoder(logFileEncoder)
		exitOnError(err, "failed to create log encoder for file")

		k8sLogger := k8szap.NewRaw(k8szap.UseFlagOptions(&opts))
		zapOpts := append(opts.ZapOpts, zap.AddCallerSkip(1))
		combineLogger := zap.New(zapcore.NewTee(
			k8sLogger.Core(),
			zapcore.NewCore(fileEncoder, zapcore.AddSync(fileWriter), zap.InfoLevel),
		)).WithOptions(zapOpts...)
		combineLoggerR := zapr.NewLogger(combineLogger)

		ctrl.SetLogger(combineLoggerR)

		// By default, the log from kubernetes/client-go is not json format.
		// This will apply the logger to kubernetes/client-go and change it to json format.
		klog.SetLogger(combineLoggerR)
	} else {
		k8sLogger := k8szap.New(k8szap.UseFlagOptions(&opts))
		ctrl.SetLogger(k8sLogger)

		// By default, the log from kubernetes/client-go is not json format.
		// This will apply the logger to kubernetes/client-go and change it to json format.
		klog.SetLogger(k8sLogger)
	}

	if forcedClusterUpgrade {
		setupLog.Info("Deprecated feature flag forced-cluster-upgrade is enabled, which has no effect.")
	}

	// validate the batch scheduler configs,
	// exit with error if the configs is invalid.
	if err := configapi.ValidateBatchSchedulerConfig(setupLog, config); err != nil {
		exitOnError(err, "batch scheduler configs validation failed")
	}

	if err := utilfeature.DefaultMutableFeatureGate.Set(featureGates); err != nil {
		exitOnError(err, "Unable to set flag gates for known features")
	}
	features.LogFeatureGates(setupLog)

	// Manager options
	options := ctrl.Options{
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{},
		},
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: config.MetricsAddr,
		},
		HealthProbeBindAddress:  config.ProbeAddr,
		LeaderElection:          *config.EnableLeaderElection,
		LeaderElectionID:        "ray-operator-leader",
		LeaderElectionNamespace: config.LeaderElectionNamespace,
	}

	// Manager Cache
	// Set the informers label selectors to narrow the scope of the resources being watched and cached.
	// This improves the scalability of the system, both for KubeRay itself by reducing the size of the
	// informers cache, and for the API server / etcd, by reducing the number of watch events.
	// For example, KubeRay is only interested in the batch Jobs it creates when reconciling RayJobs,
	// so the controller sets the app.kubernetes.io/created-by=kuberay-operator label on any Job it creates,
	// and that label is provided to the manager cache as a selector for Job resources.
	selectorsByObject, err := cacheSelectors()
	exitOnError(err, "unable to create cache selectors")
	options.Cache.ByObject = selectorsByObject

	if watchNamespaces := strings.Split(config.WatchNamespace, ","); len(watchNamespaces) == 1 { // It is not possible for len(watchNamespaces) == 0 to be true. The length of `strings.Split("", ",")` is still 1.
		if watchNamespaces[0] == "" {
			setupLog.Info("Flag watchNamespace is not set. Watch custom resources in all namespaces.")
		} else {
			setupLog.Info("Only watch custom resources in the namespace.", "namespace", watchNamespaces[0])
			options.Cache.DefaultNamespaces[watchNamespaces[0]] = cache.Config{}
		}
	} else {
		setupLog.Info("Only watch custom resources in multiple namespaces.", "namespaces", watchNamespaces)
		for _, namespace := range watchNamespaces {
			options.Cache.DefaultNamespaces[namespace] = cache.Config{}
		}
	}

	setupLog.Info("Setup manager")
	restConfig := ctrl.GetConfigOrDie()
	restConfig.UserAgent = userAgent
	mgr, err := ctrl.NewManager(restConfig, options)
	exitOnError(err, "unable to start manager")

	rayClusterOptions := ray.RayClusterReconcilerOptions{
		HeadSidecarContainers:   config.HeadSidecarContainers,
		WorkerSidecarContainers: config.WorkerSidecarContainers,
	}
	ctx := ctrl.SetupSignalHandler()
	exitOnError(ray.NewReconciler(ctx, mgr, rayClusterOptions, config).SetupWithManager(mgr, config.ReconcileConcurrency),
		"unable to create controller", "controller", "RayCluster")
	exitOnError(ray.NewRayServiceReconciler(ctx, mgr, config).SetupWithManager(mgr, config.ReconcileConcurrency),
		"unable to create controller", "controller", "RayService")
	exitOnError(ray.NewRayJobReconciler(ctx, mgr, config).SetupWithManager(mgr, config.ReconcileConcurrency),
		"unable to create controller", "controller", "RayJob")

	if os.Getenv("ENABLE_WEBHOOKS") == "true" {
		exitOnError(webhooks.SetupRayClusterWebhookWithManager(mgr),
			"unable to create webhook", "webhook", "RayCluster")
	}
	// +kubebuilder:scaffold:builder

	exitOnError(mgr.AddHealthzCheck("healthz", healthz.Ping), "unable to set up health check")
	exitOnError(mgr.AddReadyzCheck("readyz", healthz.Ping), "unable to set up ready check")

	setupLog.Info("starting manager")
	exitOnError(mgr.Start(ctx), "problem running manager")
}

func cacheSelectors() (map[client.Object]cache.ByObject, error) {
	label, err := labels.NewRequirement(utils.KubernetesCreatedByLabelKey, selection.Equals, []string{utils.ComponentName})
	if err != nil {
		return nil, err
	}
	selector := labels.NewSelector().Add(*label)

	return map[client.Object]cache.ByObject{
		&batchv1.Job{}: {Label: selector},
	}, nil
}

func exitOnError(err error, msg string, keysAndValues ...interface{}) {
	if err != nil {
		setupLog.Error(err, msg, keysAndValues...)
		os.Exit(1)
	}
}

// decodeConfig decodes raw config data and returns the Configuration type.
func decodeConfig(configData []byte, scheme *runtime.Scheme) (configapi.Configuration, error) {
	cfg := configapi.Configuration{}
	codecs := serializer.NewCodecFactory(scheme)

	if err := runtime.DecodeInto(codecs.UniversalDecoder(), configData, &cfg); err != nil {
		return cfg, err
	}

	scheme.Default(&cfg)
	return cfg, nil
}

// newLogEncoder returns a zapcore.Encoder based on the encoder type ('json' or 'console')
func newLogEncoder(encoderType string) (zapcore.Encoder, error) {
	pe := zap.NewProductionEncoderConfig()
	pe.EncodeTime = zapcore.ISO8601TimeEncoder

	if encoderType == "json" || encoderType == "" {
		return zapcore.NewJSONEncoder(pe), nil
	}
	if encoderType == "console" {
		return zapcore.NewConsoleEncoder(pe), nil
	}

	return nil, fmt.Errorf("invalid encoder %q (must be 'json' or 'console')", encoderType)
}
