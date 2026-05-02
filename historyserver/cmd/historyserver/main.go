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
	// ===== Flags =====
	var (
		runtimeClassName       string
		rayRootDir             string
		kubeconfigs            string
		dashboardDir           string
		runtimeClassConfigPath string
		useKubernetesProxy     bool
	)
	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "Storage backend: s3 / gcs / azureblob / aliyunoss / localtest")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "Root dir inside the bucket")
	flag.StringVar(&kubeconfigs, "kubeconfigs", "", "Kubeconfig path; empty = in-cluster")
	flag.StringVar(&dashboardDir, "dashboard-dir", "/dashboard", "Path to Ray Dashboard static assets")
	flag.StringVar(&runtimeClassConfigPath, "runtime-class-config-path", "", "Path to backend config JSON")
	flag.BoolVar(&useKubernetesProxy, "use-kubernetes-proxy", false, "Use local kubeconfig instead of in-cluster config")
	flag.Parse()

	if runtimeClassName == "" {
		logrus.Fatal("--runtime-class-name is required")
	}

	// ===== ClientManager =====
	cliMgr, err := historyserver.NewClientManager(kubeconfigs, useKubernetesProxy)
	if err != nil {
		logrus.Fatalf("client manager: %v", err)
	}

	// ===== Backend config =====
	jsonData := make(map[string]interface{})
	if runtimeClassConfigPath != "" {
		data, err := os.ReadFile(runtimeClassConfigPath)
		if err != nil {
			logrus.Fatalf("read runtime-class-config: %v", err)
		}
		if err := json.Unmarshal(data, &jsonData); err != nil {
			logrus.Fatalf("parse runtime-class-config: %v", err)
		}
	}

	// ===== Reader factory =====
	registry := collector.GetReaderRegistry()
	factory, ok := registry[runtimeClassName]
	if !ok {
		logrus.Fatalf("unsupported runtime-class-name for reader: %s", runtimeClassName)
	}

	globalConfig := types.RayHistoryServerConfig{
		RootDir: rayRootDir,
	}

	reader, err := factory(&globalConfig, jsonData)
	if err != nil {
		logrus.Fatalf("create reader: %v", err)
	}

	// ===== EventHandler =====
	eventHandler := eventserver.NewEventHandler(reader)

	// ===== Server context =====
	serverCtx, serverCancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT, syscall.SIGTERM,
	)
	defer serverCancel()

	// ===== K8s client (for Pipeline.isDead) =====
	k8sClient, err := buildK8sClient(kubeconfigs, useKubernetesProxy)
	if err != nil {
		logrus.Fatalf("build k8s client: %v", err)
	}

	// ===== Pipeline & Supervisor =====
	pipeline := historyserver.NewPipeline(eventHandler, k8sClient)
	supervisor := historyserver.NewSupervisor(pipeline, serverCtx)

	// ===== Shutdown signaling =====
	// Bridge serverCtx into the legacy stop channel that ServerHandler.Run
	// consumes; the existing chan-based API is preserved.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	go func() {
		<-serverCtx.Done()
		logrus.Info("Received shutdown signal, initiating graceful shutdown...")
		close(stop)
	}()

	// ===== ServerHandler =====
	handler, err := historyserver.NewServerHandler(&globalConfig, dashboardDir, reader, cliMgr, eventHandler, supervisor, useKubernetesProxy)
	if err != nil {
		logrus.Fatalf("create server handler: %v", err)
	}

	// ===== Run HTTP server =====
	wg.Add(1)
	go func() {
		defer wg.Done()
		handler.Run(stop)
		logrus.Info("HTTP server shutdown complete")
	}()

	// ===== Wait for graceful shutdown =====
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
