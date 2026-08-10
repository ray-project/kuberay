package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/collector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/historyserver"
)

func main() {
	storageBackend := ""
	rayRootDir := ""
	kubeconfigs := ""
	storageBackendConfigPath := ""
	dashboardDir := ""
	useKubernetesProxy := false
	useAuthTokenMode := false
	qps := historyserver.DefaultKubeAPIQPS
	burst := historyserver.DefaultKubeAPIBurst
	sessionProcessTimeout := historyserver.DefaultSessionProcessTimeout
	sessionCacheSize := historyserver.DefaultSessionCacheSize
	sessionCacheMaxBytes := historyserver.DefaultSessionCacheMaxBytes
	sessionCacheTTL := historyserver.DefaultSessionCacheTTL
	flag.StringVar(&storageBackend, "storage-backend", "", "Storage backend: s3 / gcs / azureblob / aliyunoss / localtest")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "Root dir inside the bucket")
	flag.StringVar(&kubeconfigs, "kubeconfigs", "", "Kubeconfig path; empty = in-cluster")
	flag.StringVar(&dashboardDir, "dashboard-dir", "/dashboard", "Path to Ray Dashboard static assets")
	flag.StringVar(&storageBackendConfigPath, "storage-backend-config-path", "", "Path to backend config JSON")
	flag.BoolVar(&useKubernetesProxy, "use-kubernetes-proxy", false, "Use local kubeconfig instead of in-cluster config")
	flag.BoolVar(&useAuthTokenMode, "use-auth-token-mode", false, "Enable Ray dashboard token authentication mode (requires x-ray-authorization header)")
	flag.Float64Var(&qps, "kube-api-qps", historyserver.DefaultKubeAPIQPS, "The QPS value for the client communicating with the Kubernetes API server.")
	flag.IntVar(&burst, "kube-api-burst", historyserver.DefaultKubeAPIBurst, "The maximum burst for throttling requests from this client to the Kubernetes API server.")
	flag.DurationVar(&sessionProcessTimeout, "session-process-timeout", historyserver.DefaultSessionProcessTimeout, "Timeout duration for processing and loading a single Ray cluster session.")
	flag.IntVar(&sessionCacheSize, "session-cache-size", historyserver.DefaultSessionCacheSize, "Max number of dead-session snapshots held in the LRU cache.")
	flag.IntVar(&sessionCacheMaxBytes, "session-cache-max-bytes", historyserver.DefaultSessionCacheMaxBytes, "Soft cap on cached snapshot bytes; tune under pod memory. 0 disables the byte bound.")
	flag.DurationVar(&sessionCacheTTL, "session-cache-ttl", historyserver.DefaultSessionCacheTTL, "How long a dead-session snapshot stays cached after last access. 0 disables TTL.")
	flag.Parse()

	if val := os.Getenv("STORAGE_BACKEND"); val != "" {
		storageBackend = val
	}
	if storageBackend == "" {
		logrus.Fatal("--storage-backend or STORAGE_BACKEND environment variable is required")
	}
	storageBackend = strings.ToLower(storageBackend)

	if val := os.Getenv("RAY_ROOT_DIR"); val != "" {
		rayRootDir = val
	}

	if qps <= 0 {
		logrus.Fatalf("--kube-api-qps must be > 0, got %v", qps)
	}
	if burst <= 0 {
		logrus.Fatalf("--kube-api-burst must be > 0, got %d", burst)
	}
	if sessionCacheSize <= 0 {
		logrus.Fatalf("--session-cache-size must be > 0, got %d", sessionCacheSize)
	}
	if sessionCacheMaxBytes < 0 {
		logrus.Fatalf("--session-cache-max-bytes must be >= 0, got %d", sessionCacheMaxBytes)
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
	if storageBackendConfigPath != "" {
		data, err := os.ReadFile(storageBackendConfigPath)
		if err != nil {
			logrus.Fatalf("Failed to read storage-backend-config-path from %s: %v", storageBackendConfigPath, err)
		}
		if err := json.Unmarshal(data, &jsonData); err != nil {
			logrus.Fatalf("Failed to parse storage-backend-config-path from %s: %v", storageBackendConfigPath, err)
		}
	}

	registry := collector.GetReaderRegistry()
	factory, ok := registry[storageBackend]
	if !ok {
		logrus.Fatalf("Unsupported storage-backend for reader: %s", storageBackend)
	}

	globalConfig := types.RayHistoryServerConfig{
		RootDir: rayRootDir,
	}

	reader, err := factory(&globalConfig, jsonData)
	if err != nil {
		logrus.Fatalf("Failed to create reader for storage backend %s: %v", storageBackend, err)
	}

	serverCtx, serverCancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT, syscall.SIGTERM,
	)
	defer serverCancel()

	processor := historyserver.NewSessionProcessor(reader, cliMgr.Client())
	sessionLoader := historyserver.NewSessionLoader(processor, serverCtx, sessionProcessTimeout, sessionCacheSize, sessionCacheMaxBytes, sessionCacheTTL)

	// ServerHandler.Run consumes a stop chan; bridge serverCtx into it.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	go func() {
		<-serverCtx.Done()
		logrus.Info("Received shutdown signal, initiating graceful shutdown...")
		close(stop)
	}()

	handler, err := historyserver.NewServerHandler(&globalConfig, dashboardDir, reader, cliMgr, sessionLoader, useKubernetesProxy, useAuthTokenMode)
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
