// Package main is the entrypoint for the History Server HTTP daemon.
// It exposes Ray Dashboard-shaped API endpoints over HTTP and drives
// per-session event processing on demand via a Supervisor when
// /enter_cluster hits a dead session.
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

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	"github.com/ray-project/kuberay/historyserver/pkg/collector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/historyserver"
)

func main() {
	runtimeClassName := ""
	rayRootDir := ""
	kubeconfigs := ""
	runtimeClassConfigPath := ""
	dashboardDir := ""
	useKubernetesProxy := false
	sessionProcessTimeout := historyserver.DefaultSessionProcessTimeout
	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "Storage backend: s3 / gcs / azureblob / aliyunoss / localtest")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "Root dir inside the bucket")
	flag.StringVar(&kubeconfigs, "kubeconfigs", "", "Kubeconfig path; empty = in-cluster")
	flag.StringVar(&dashboardDir, "dashboard-dir", "/dashboard", "Path to Ray Dashboard static assets")
	flag.StringVar(&runtimeClassConfigPath, "runtime-class-config-path", "", "Path to backend config JSON")
	flag.BoolVar(&useKubernetesProxy, "use-kubernetes-proxy", false, "Use local kubeconfig instead of in-cluster config")
	flag.DurationVar(&sessionProcessTimeout, "session-process-timeout", historyserver.DefaultSessionProcessTimeout, "Timeout duration for processing and loading a single Ray cluster session.")
	flag.Parse()

	if runtimeClassName == "" {
		logrus.Fatal("--runtime-class-name is required")
	}

	cliMgr, err := historyserver.NewClientManager(kubeconfigs, useKubernetesProxy)
	if err != nil {
		logrus.Fatalf("Failed to create client manager: %v", err)
	}

	// ===== Backend config =====
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

	// ===== Reader factory =====
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

	eventHandler := eventserver.NewEventHandler(reader)

	serverCtx, serverCancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT, syscall.SIGTERM,
	)
	defer serverCancel()

	processor := historyserver.NewSessionProcessor(eventHandler, cliMgr.Client())
	sessionLoader := historyserver.NewSessionLoader(processor, serverCtx, sessionProcessTimeout)

	// ServerHandler.Run consumes a stop chan; bridge serverCtx into it.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	go func() {
		<-serverCtx.Done()
		logrus.Info("Received shutdown signal, initiating graceful shutdown...")
		close(stop)
	}()

	handler, err := historyserver.NewServerHandler(&globalConfig, dashboardDir, reader, cliMgr, eventHandler, sessionLoader, useKubernetesProxy)
	if err != nil {
		logrus.Fatalf("Failed to create server handler: %v", err)
	}

	// ===== Run HTTP server =====
	wg.Add(1)
	go func() {
		defer wg.Done()
		handler.Run(stop)
		logrus.Info("HTTP server shutdown complete")
	}()

	wg.Wait()
	logrus.Info("Graceful shutdown complete")
}

// buildK8sClient constructs a controller-runtime client.Client with the
// rayv1 scheme registered. Used by Pipeline.isDead.
func buildK8sClient(kubeconfigs string, useKubeProxy bool) (client.Client, error) {
	var cfg *rest.Config
	var err error

	switch {
	case kubeconfigs != "":
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigs)
	case useKubeProxy:
		loading := clientcmd.NewDefaultClientConfigLoadingRules()
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loading, &clientcmd.ConfigOverrides{}).ClientConfig()
	default:
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}
	cfg.QPS = 50
	cfg.Burst = 100

	scheme := runtime.NewScheme()
	utilruntime.Must(rayv1.AddToScheme(scheme))
	return client.New(cfg, client.Options{Scheme: scheme})
}
