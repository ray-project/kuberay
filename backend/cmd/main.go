package main

import (
	"context"
	"flag"
	"math"
	"net"
	"net/http"

	"github.com/ray-project/kuberay/backend/pkg/interceptor"
	"github.com/ray-project/kuberay/backend/pkg/manager"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/ray-project/kuberay/backend/pkg/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"k8s.io/klog/v2"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	api "github.com/ray-project/kuberay/api/go_client"
)

var (
	rpcPortFlag        = flag.String("rpcPortFlag", ":8887", "RPC Port")
	httpPortFlag       = flag.String("httpPortFlag", ":8888", "Http Proxy Port")
	configPath         = flag.String("config", "", "Path to JSON file containing config")
	collectMetricsFlag = flag.Bool("collectMetricsFlag", true, "Whether to collect Prometheus metrics in API server.")
)

func main() {
	flag.Parse()

	clientManager := manager.NewClientManager()
	resourceManager := manager.NewResourceManager(&clientManager)

	go startRpcServer(resourceManager)
	startHttpProxy()

}

type RegisterHttpHandlerFromEndpoint func(ctx context.Context, mux *runtime.ServeMux, endpoint string, opts []grpc.DialOption) error

func startRpcServer(resourceManager *manager.ResourceManager) {
	klog.Info("Starting gRPC server")

	listener, err := net.Listen("tcp", *rpcPortFlag)
	if err != nil {
		klog.Fatalf("Failed to start GPRC server: %v", err)
	}

	s := grpc.NewServer(grpc.UnaryInterceptor(interceptor.ApiServerInterceptor), grpc.MaxRecvMsgSize(math.MaxInt32))
	api.RegisterClusterServiceServer(s, server.NewClusterServer(resourceManager, &server.ClusterServerOptions{CollectMetrics: *collectMetricsFlag}))
	api.RegisterClusterRuntimeServiceServer(s, server.NewClusterRuntimeServer(resourceManager, &server.ClusterRuntimeServerOptions{CollectMetrics: *collectMetricsFlag}))
	api.RegisterComputeRuntimeServiceServer(s, server.NewComputeRuntimeServer(resourceManager, &server.ComputeRuntimeServerOptions{CollectMetrics: *collectMetricsFlag}))

	// Register reflection service on gRPC server.
	reflection.Register(s)
	if err := s.Serve(listener); err != nil {
		klog.Fatalf("Failed to serve gRPC listener: %v", err)
	}

	klog.Info("gRPC server started")
}

func startHttpProxy() {
	klog.Info("Starting Http Proxy")

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create gRPC HTTP MUX and register services.
	runtimeMux := runtime.NewServeMux(runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{OrigName: true, EmitDefaults: true}))
	registerHttpHandlerFromEndpoint(api.RegisterClusterServiceHandlerFromEndpoint, "ClusterService", ctx, runtimeMux)
	registerHttpHandlerFromEndpoint(api.RegisterClusterRuntimeServiceHandlerFromEndpoint, "ClusterRuntimeService", ctx, runtimeMux)
	registerHttpHandlerFromEndpoint(api.RegisterComputeRuntimeServiceHandlerFromEndpoint, "ComputeRuntimeService", ctx, runtimeMux)

	// Create a top level mux to include both Http gRPC servers and other endpoints like metrics
	topMux := http.NewServeMux()
	// Seems /apis (matches /apis/v1alpha1/clusters) works fine
	topMux.Handle("/", runtimeMux)
	topMux.Handle("/metrics", promhttp.Handler())

	http.ListenAndServe(*httpPortFlag, topMux)
	klog.Info("Http Proxy started")
}

func registerHttpHandlerFromEndpoint(handler RegisterHttpHandlerFromEndpoint, serviceName string, ctx context.Context, mux *runtime.ServeMux) {
	endpoint := "localhost" + *rpcPortFlag
	opts := []grpc.DialOption{grpc.WithInsecure(), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(math.MaxInt32))}

	if err := handler(ctx, mux, endpoint, opts); err != nil {
		klog.Fatalf("Failed to register %v handler: %v", serviceName, err)
	}
}
