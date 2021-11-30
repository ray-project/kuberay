package main

import (
	"context"
	"flag"
	"net"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/ray-project/kuberay/ray-operator/controllers"
	rpc "github.com/ray-project/kuberay/ray-operator/rpc"

	"github.com/ray-project/kuberay/ray-operator/controllers/common"
	clientset "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection" // For debugging
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	Client    client.Client
	Scheme    *runtime.Scheme
}

type patchArrayOfStrings struct {
	Op    string   `json:"op"`
	Path  string   `json:"path"`
	Value []string `json:"value"`
}

// Translate tags from Kuberay format to Ray autoscaler format
func translateTags(labels map[string]string) map[string]string {
	tags := map[string]string{}
	for k, v := range labels {
		tags[k] = v
	}
	tags["ray-node-status"] = "up-to-date"
	if tags["ray.io/node-type"] == "head" {
		tags["ray-node-type"] = "head"
		tags["ray-user-node-type"] = "ray.head.default"
	} else {
		tags["ray-node-type"] = "worker"
		tags["ray-user-node-type"] = "ray.worker.default"
	}
	return tags
}

func listPods(ctx context.Context, s *server, clusterName string, namespace string) (corev1.PodList, error) {
	pods := corev1.PodList{}
	filterLabels := client.MatchingLabels{common.RayClusterLabelKey: clusterName}
	err := s.Client.List(ctx, &pods, client.InNamespace(namespace), filterLabels)
	return pods, err
}

func (s *server) NonTerminatedNodes(ctx context.Context, in *rpc.NonTerminatedNodesRequest) (*rpc.NonTerminatedNodesResponse, error) {
	// TODO: Cluster name and namespace
	pods, err := listPods(ctx, s, "raycluster-complete", "default")
	if err != nil {
		setupLog.Error(err, "error listing pods")
	}
	nodeIds := []string{}
	nodeIps := []string{}
	nodeTags := []*rpc.NodeTags{}
	for index := range pods.Items {
		nodeIds = append(nodeIds, pods.Items[index].ObjectMeta.Name)
		nodeIps = append(nodeIps, pods.Items[index].Status.PodIP)
		tags := translateTags(pods.Items[index].ObjectMeta.Labels)
		nodeTags = append(nodeTags, &rpc.NodeTags{NodeTags: tags})
	}
	result := &rpc.NonTerminatedNodesResponse{
		NodeIds:      nodeIds,
		NodeIps:      nodeIps,
		NodeTagsList: nodeTags,
	}
	return result, nil
}

func (s *server) CreateNode(ctx context.Context, in *rpc.CreateNodeRequest) (*rpc.CreateNodeResponse, error) {
	// TODO: Support creation of multiple nodes
	setupLog.Info("the rpc server", "received count:", in.GetCount())
	setupLog.Info("the rpc server", "received tags:", in.GetTags())

	instance, err := s.ClientSet.RayV1alpha1().RayClusters("default").Get(ctx, "raycluster-complete", v1.GetOptions{})
	if err != nil {
		setupLog.Error(err, "error getting cluster resource")
	}
	// TODO: Support multiple WorkerGroupSpecs
	worker := instance.Spec.WorkerGroupSpecs[0]
	pod := common.BuildWorkerPod(*instance, worker, s.Scheme)

	err = s.Client.Create(ctx, &pod)
	if err != nil {
		setupLog.Error(err, "failed to create pod")
	}

	return &rpc.CreateNodeResponse{
		NodeToMeta: map[string]*rpc.NodeMeta{
			// TODO: Add labels
			pod.Name: &rpc.NodeMeta{},
		},
	}, nil
}

func (s *server) TerminateNodes(ctx context.Context, in *rpc.TerminateNodesRequest) (*rpc.TerminateNodesResponse, error) {
	deletedNodesMeta := map[string]*rpc.NodeMeta{}
	for index := range in.NodeIds {
		pod := corev1.Pod{}
		pod.Name = in.NodeIds[index]
		err := s.Client.Delete(ctx, &pod)
		if err != nil {
			setupLog.Error(err, "failed to delete pod")
		} else {
			deletedNodesMeta[in.NodeIds[index]] = &rpc.NodeMeta{
				NodeMeta: map[string]string{"terminated": "ok"},
			}
		}
	}
	return &rpc.TerminateNodesResponse{
		NodeToMeta: deletedNodesMeta,
	}, nil
}

func (s *server) InternalIp(ctx context.Context, in *rpc.InternalIpRequest) (*rpc.InternalIpResponse, error) {
	// TODO: Cluster name and namespace
	// TODO: Only query the node in in.NodeId
	pods, err := listPods(ctx, s, "raycluster-complete", "default")
	if err != nil {
		setupLog.Error(err, "error listing pods")
	}
	nodeIp := "unassigned"
	for index := range pods.Items {
		if pods.Items[index].ObjectMeta.Name == in.NodeId {
			nodeIp = pods.Items[index].Status.PodIP
		}
	}
	return &rpc.InternalIpResponse{
		NodeIp: nodeIp,
	}, nil
}

func (s *server) NodeTags(ctx context.Context, in *rpc.NodeTagsRequest) (*rpc.NodeTagsResponse, error) {
	// TODO: Cluster name and namespace
	// TODO: Only query the node in in.NodeId
	pods, err := listPods(ctx, s, "raycluster-complete", "default")
	if err != nil {
		setupLog.Error(err, "error listing pods")
	}
	nodeTags := map[string]string{}
	for index := range pods.Items {
		if pods.Items[index].ObjectMeta.Name == in.NodeId {
			nodeTags = translateTags(pods.Items[index].ObjectMeta.Labels)
		}
	}
	return &rpc.NodeTagsResponse{
		NodeTags: nodeTags,
	}, nil
}

func startRpcServer(config *rest.Config, scheme *runtime.Scheme) {
	setupLog.Info("starting rpc server")
	lis, err := net.Listen("tcp", ":5000")
	if err != nil {
		setupLog.Error(err, "failed to listen")
	}
	client, err := client.New(config, client.Options{})
	server := &server{
		ClientSet: clientset.NewForConfigOrDie(config),
		Client:    client,
		Scheme:    scheme,
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

	go startRpcServer(config, scheme)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
