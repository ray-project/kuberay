package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/collector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/historyserver"
)

const (
	// Aligned with ray-operator defaults (see ray-operator/apis/config/v1alpha1/defaults.go).
	defaultKubeAPIQPS   = float64(100)
	defaultKubeAPIBurst = 200
)

func main() {
	runtimeClassName := ""
	rayRootDir := ""
	kubeconfigs := ""
	runtimeClassConfigPath := ""
	dashboardDir := ""
	useKubernetesProxy := false
	qps := defaultKubeAPIQPS
	burst := defaultKubeAPIBurst
	sessionProcessTimeout := historyserver.DefaultSessionProcessTimeout
	sessionCacheSize := historyserver.DefaultSessionCacheSize
	sessionCacheTTL := historyserver.DefaultSessionCacheTTL
	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "Storage backend: s3 / gcs / azureblob / aliyunoss / localtest")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "Root dir inside the bucket")
	flag.StringVar(&kubeconfigs, "kubeconfigs", "", "Kubeconfig path; empty = in-cluster")
	flag.StringVar(&dashboardDir, "dashboard-dir", "/dashboard", "Path to Ray Dashboard static assets")
	flag.StringVar(&runtimeClassConfigPath, "runtime-class-config-path", "", "Path to backend config JSON")
	flag.BoolVar(&useKubernetesProxy, "use-kubernetes-proxy", false, "Use local kubeconfig instead of in-cluster config")
	flag.Float64Var(&qps, "kube-api-qps", defaultKubeAPIQPS,
		"The QPS value for the client communicating with the Kubernetes API server.")
	flag.IntVar(&burst, "kube-api-burst", defaultKubeAPIBurst,
		"The maximum burst for throttling requests from this client to the Kubernetes API server.")
	flag.DurationVar(&sessionProcessTimeout, "session-process-timeout", historyserver.DefaultSessionProcessTimeout, "Timeout duration for processing and loading a single Ray cluster session.")
	flag.IntVar(&sessionCacheSize, "session-cache-size", historyserver.DefaultSessionCacheSize, "Max number of dead-session snapshots held in the LRU cache.")
	flag.DurationVar(&sessionCacheTTL, "session-cache-ttl", historyserver.DefaultSessionCacheTTL, "How long a dead-session snapshot stays cached after last access. 0 disables TTL.")
	flag.Parse()

	if runtimeClassName == "" {
		logrus.Fatal("--runtime-class-name is required")
	}
	if sessionCacheSize <= 0 {
		logrus.Fatalf("--session-cache-size must be > 0, got %d", sessionCacheSize)
	}
	if sessionCacheTTL < 0 {
		logrus.Fatalf("--session-cache-ttl must be >= 0, got %s", sessionCacheTTL)
	}

	cliMgr, err := historyserver.NewClientManager(historyserver.ClientManagerConfig{
		Kubeconfigs:        kubeconfigs,
		UseKubernetesProxy: useKubernetesProxy,
		QPS:                float32(qps),
		Burst:              burst,
	})
	if err != nil {
		logrus.Fatalf("Failed to create client manager: %v", err)
	}

	jsonData := make(map[string]interface{})
	if runtimeClassConfigPath != "" {
		data, err := os.ReadFile(runtimeClassConfigPath)
		if err != nil {
			logrus.Fatalf("Failed to read runtime-class-config-path from %s: %v", runtimeClassConfigPath, err)
		}
		if err := json.Unmarshal(data, &jsonData); err != nil {
			logrus.Fatalf("Failed to parse runtime-class-config-path from %s: %v", runtimeClassConfigPath, err)
		}
	}

	registry := collector.GetReaderRegistry()
	factory, ok := registry[runtimeClassName]
	if !ok {
		logrus.Fatalf("Unsupported runtime-class-name for reader: %s", runtimeClassName)
	}

	globalConfig := types.RayHistoryServerConfig{
		RootDir: rayRootDir,
	}

	reader, err := factory(&globalConfig, jsonData)
	if err != nil {
		logrus.Fatalf("Failed to create reader for runtime class name %s: %v", runtimeClassName, err)
	}

	serverCtx, serverCancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT, syscall.SIGTERM,
	)
	defer serverCancel()

	processor := historyserver.NewSessionProcessor(reader, cliMgr.Client())
	sessionLoader := historyserver.NewSessionLoader(processor, serverCtx, sessionProcessTimeout, sessionCacheSize, sessionCacheTTL)

	// ServerHandler.Run consumes a stop chan; bridge serverCtx into it.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	go func() {
		<-serverCtx.Done()
		logrus.Info("Received shutdown signal, initiating graceful shutdown...")
		close(stop)
	}()

	handler, err := historyserver.NewServerHandler(&globalConfig, dashboardDir, reader, cliMgr, sessionLoader, useKubernetesProxy)
	if err != nil {
		logrus.Fatalf("Failed to create server handler: %v", err)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		handler.Run(stop)
		logrus.Info("HTTP server shutdown complete")
	}()

	wg.Wait()
	logrus.Info("Graceful shutdown complete")
}
