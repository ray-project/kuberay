package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ray-project/kuberay/ray-operator/controllers/raycluster"

	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/raycluster/v1alpha1"
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
	utilruntime.Must(rayiov1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var version bool
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var reconcileConcurrency int
	var watchNamespace string
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
		"Watch custom resources in the namespace, ignore other namespaces. If empty, all namespaces will be watched.")
	flag.BoolVar(&raycluster.PrioritizeWorkersToDelete, "prioritize-workers-to-delete", false,
		"Temporary feature flag - to be deleted after testing")
	opts := zap.Options{
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

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info("the operator", "version:", os.Getenv("OPERATOR_VERSION"))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "ray-operator-leader",
		Namespace:              watchNamespace,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = raycluster.NewReconciler(mgr).SetupWithManager(mgr, reconcileConcurrency); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "RayCluster")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
