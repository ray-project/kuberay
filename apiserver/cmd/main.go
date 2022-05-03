package main

import (
	"context"
	"flag"
	"math"
	"net"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"k8s.io/klog/v2"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/ray-project/kuberay/apiserver/pkg/interceptor"
	"github.com/ray-project/kuberay/apiserver/pkg/manager"
	"github.com/ray-project/kuberay/apiserver/pkg/server"
	api "github.com/ray-project/kuberay/proto/go_client"
)

var (
	rpcPortFlag        = flag.String("rpcPortFlag", ":8887", "RPC Port")
	httpPortFlag       = flag.String("httpPortFlag", ":8888", "Http Proxy Port")
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

	s := grpc.NewServer(
		grpc.StreamInterceptor(grpc_prometheus.StreamServerInterceptor),
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(grpc_prometheus.UnaryServerInterceptor, interceptor.ApiServerInterceptor)),
		grpc.MaxRecvMsgSize(math.MaxInt32))
	api.RegisterClusterServiceServer(s, server.NewClusterServer(resourceManager, &server.ClusterServerOptions{CollectMetrics: *collectMetricsFlag}))
	api.RegisterComputeTemplateServiceServer(s, server.NewComputeTemplateServer(resourceManager, &server.ComputeTemplateServerOptions{CollectMetrics: *collectMetricsFlag}))

	// Register reflection service on gRPC server.
	reflection.Register(s)
	// Make sure all of the Prometheus metrics are initialized.
	grpc_prometheus.Register(s)
	// This is to enable `grpc_server_handling_seconds`, otherwise we won't have latency metrics.
	// see https://github.com/grpc-ecosystem/go-grpc-prometheus/blob/master/README.md#histograms for details.
	grpc_prometheus.EnableHandlingTimeHistogram()
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
	registerHttpHandlerFromEndpoint(api.RegisterComputeTemplateServiceHandlerFromEndpoint, "ComputeTemplateService", ctx, runtimeMux)

	// Create a top level mux to include both Http gRPC servers and other endpoints like metrics
	topMux := http.NewServeMux()
	// Seems /apis (matches /apis/v1alpha1/clusters) works fine
	topMux.Handle("/", runtimeMux)
	topMux.Handle("/metrics", promhttp.Handler())

	if err := http.ListenAndServe(*httpPortFlag, topMux); err != nil {
		klog.Fatal(err)
	}

	klog.Info("Http Proxy started")
}

func registerHttpHandlerFromEndpoint(handler RegisterHttpHandlerFromEndpoint, serviceName string, ctx context.Context, mux *runtime.ServeMux) {
	endpoint := "localhost" + *rpcPortFlag
	opts := []grpc.DialOption{grpc.WithInsecure(), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(math.MaxInt32))}

	if err := handler(ctx, mux, endpoint, opts); err != nil {
		klog.Fatalf("Failed to register %v handler: %v", serviceName, err)
	}
}
