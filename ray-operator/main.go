package main

import (
	"context"
	"encoding/json"
	"flag"
	"net"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/ray-project/kuberay/ray-operator/controllers"
	rpc "github.com/ray-project/kuberay/ray-operator/rpc"

	"github.com/google/uuid"
	clientset "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection" // For debugging
	types "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/api/raycluster/v1alpha1"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(rayiov1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

type server struct {
	rpc.UnimplementedKuberayNodeProviderServer
	ClientSet *clientset.Clientset
}

type patchArrayOfStrings struct {
	Op    string   `json:"op"`
	Path  string   `json:"path"`
	Value []string `json:"value"`
}

func (s *server) NonTerminatedNodes(ctx context.Context, in *rpc.NonTerminatedNodesRequest) (*rpc.NonTerminatedNodesResponse, error) {
	setupLog.Info("the rpc server", "received:", in.GetClusterName())
	return &rpc.NonTerminatedNodesResponse{}, nil
}

func (s *server) CreateNode(ctx context.Context, in *rpc.CreateNodeRequest) (*rpc.CreateNodeResponse, error) {
	// TODO: Support creation of multiple nodes
	setupLog.Info("the rpc server", "received count:", in.GetCount())
	setupLog.Info("the rpc server", "received tags:", in.GetTags())
	id := uuid.New()
	// Patch the RayCluster
	payload := []patchArrayOfStrings{{
		Op:    "add",
		Path:  "/spec/desiredWorkers",
		Value: []string{id.String()},
	}}
	// TODO: error handling
	payloadBytes, _ := json.Marshal(payload)
	// TODO: Make the patching work
	result, err := s.ClientSet.RayV1alpha1().RayClusters("default").Patch(ctx, "test", types.JSONPatchType, payloadBytes)
	return &rpc.CreateNodeResponse{
		NodeToMeta: map[string]*rpc.NodeMeta{
			id.String(): &rpc.NodeMeta{},
		},
	}, nil
}

func startRpcServer(config *rest.Config) {
	setupLog.Info("starting rpc server")
	lis, err := net.Listen("tcp", ":5000")
	if err != nil {
		setupLog.Error(err, "failed to listen")
	}
	server := &server{
		ClientSet: rayv1alpha1.NewForConfigOrDie(config),
	}
	s := grpc.NewServer()
	rpc.RegisterKuberayNodeProviderServer(s, server)
	reflection.Register(s)
	if err := s.Serve(lis); err != nil {
		setupLog.Error(err, "failed to serve")
	}
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var reconcileConcurrency int
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.IntVar(&reconcileConcurrency, "reconcile-concurrency", 1, "max concurrency for reconciling")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info("the operator", "version:", os.Getenv("OPERATOR_VERSION"))

	config := ctrl.GetConfigOrDie()

	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "ray-operator-leader",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = controllers.NewReconciler(mgr).SetupWithManager(mgr, reconcileConcurrency); err != nil {
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

	go startRpcServer(config)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
