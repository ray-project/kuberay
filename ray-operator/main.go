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
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	k8szap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/batchscheduler"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	// +kubebuilder:scaffold:imports
)

var (
	_version_   = "0.2"
	_buildTime_ = ""
	_commitId_  = ""
	scheme      = runtime.NewScheme()
	setupLog    = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(rayv1.AddToScheme(scheme))
	utilruntime.Must(routev1.Install(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
	batchscheduler.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var version bool
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var reconcileConcurrency int
	var watchNamespace string
	var logFile string
	flag.BoolVar(&version, "version", false, "Show the version information.")
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8082", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", true,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.IntVar(&reconcileConcurrency, "reconcile-concurrency", 1, "max concurrency for reconciling")
	flag.StringVar(
		&watchNamespace,
		"watch-namespace",
		"",
		"Specify a list of namespaces to watch for custom resources, separated by commas. If left empty, all namespaces will be watched.")
	flag.BoolVar(&ray.ForcedClusterUpgrade, "forced-cluster-upgrade", false,
		"Forced cluster upgrade flag")
	flag.StringVar(&logFile, "log-file-path", "",
		"Synchronize logs to local file")
	flag.BoolVar(&ray.EnableBatchScheduler, "enable-batch-scheduler", false,
		"Enable batch scheduler. Currently is volcano, which supports gang scheduler policy.")

	opts := k8szap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	if version {
		fmt.Printf("Version:\t%s\n", _version_)
		fmt.Printf("Commit ID:\t%s\n", _commitId_)
		fmt.Printf("Build time:\t%s\n", _buildTime_)
		os.Exit(0)
	}

	if logFile != "" {
		fileWriter := &lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    500, // megabytes
			MaxBackups: 10,  // files
			MaxAge:     30,  // days
		}

		pe := zap.NewProductionEncoderConfig()
		pe.EncodeTime = zapcore.ISO8601TimeEncoder
		consoleEncoder := zapcore.NewConsoleEncoder(pe)

		k8sLogger := k8szap.NewRaw(k8szap.UseFlagOptions(&opts))
		zapOpts := append(opts.ZapOpts, zap.AddCallerSkip(1))
		combineLogger := zap.New(zapcore.NewTee(
			k8sLogger.Core(),
			zapcore.NewCore(consoleEncoder, zapcore.AddSync(fileWriter), zap.InfoLevel),
		)).WithOptions(zapOpts...)
		combineLoggerR := zapr.NewLogger(combineLogger)

		ctrl.SetLogger(combineLoggerR)
	} else {
		ctrl.SetLogger(k8szap.New(k8szap.UseFlagOptions(&opts)))
	}

	setupLog.Info("the operator", "version:", os.Getenv("OPERATOR_VERSION"))
	if ray.ForcedClusterUpgrade {
		setupLog.Info("Feature flag forced-cluster-upgrade is enabled.")
	}
	if ray.EnableBatchScheduler {
		setupLog.Info("Feature flag enable-batch-scheduler is enabled.")
	}

	// Manager options
	options := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "ray-operator-leader",
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

	if watchNamespaces := strings.Split(watchNamespace, ","); len(watchNamespaces) == 1 { // It is not possible for len(watchNamespaces) == 0 to be true. The length of `strings.Split("", ",")` is still 1.
		if watchNamespaces[0] == "" {
			setupLog.Info("Flag watchNamespace is not set. Watch custom resources in all namespaces.")
		} else {
			setupLog.Info(fmt.Sprintf("Only watch custom resources in the namespace: %s", watchNamespaces[0]))
			options.Cache.DefaultNamespaces[watchNamespaces[0]] = cache.Config{}
		}
	} else {
		setupLog.Info(fmt.Sprintf("Only watch custom resources in multiple namespaces: %v", watchNamespaces))
		for _, namespace := range watchNamespaces {
			options.Cache.DefaultNamespaces[namespace] = cache.Config{}
		}
	}

	setupLog.Info("Setup manager")
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	exitOnError(err, "unable to start manager")

	exitOnError(ray.NewReconciler(mgr).SetupWithManager(mgr, reconcileConcurrency),
		"unable to create controller", "controller", "RayCluster")
	exitOnError(ray.NewRayServiceReconciler(mgr).SetupWithManager(mgr),
		"unable to create controller", "controller", "RayService")
	exitOnError(ray.NewRayJobReconciler(mgr).SetupWithManager(mgr),
		"unable to create controller", "controller", "RayJob")

	// +kubebuilder:scaffold:builder

	exitOnError(mgr.AddHealthzCheck("healthz", healthz.Ping), "unable to set up health check")
	exitOnError(mgr.AddReadyzCheck("readyz", healthz.Ping), "unable to set up ready check")

	setupLog.Info("starting manager")
	exitOnError(mgr.Start(ctrl.SetupSignalHandler()), "problem running manager")
}

func cacheSelectors() (map[client.Object]cache.ByObject, error) {
	label, err := labels.NewRequirement(common.KubernetesCreatedByLabelKey, selection.Equals, []string{common.ComponentName})
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
