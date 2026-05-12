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

	cliMgr, err := historyserver.NewClientManager(kubeconfigs, useKubernetesProxy)
	if err != nil {
		logrus.Fatalf("Failed to create client manager: %v", err)
	}

	jsonData := make(map[string]interface{})
	if runtimeClassConfigPath != "" {
		data, err := os.ReadFile(runtimeClassConfigPath)
		if err != nil {
			logrus.Fatalf("Failed to read runtime-class-config from %s: %v", runtimeClassConfigPath, err)
		}
		if err := json.Unmarshal(data, &jsonData); err != nil {
			logrus.Fatalf("Failed to parse runtime-class-config from %s: %v", runtimeClassConfigPath, err)
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

	eventHandler := eventserver.NewEventHandler(reader)

	serverCtx, serverCancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT, syscall.SIGTERM,
	)
	defer serverCancel()

	resolver, err := historyserver.NewHTTPLiveSessionResolver(cliMgr, useKubernetesProxy)
	if err != nil {
		logrus.Fatalf("Failed to create live session resolver: %v", err)
	}
	processor := historyserver.NewSessionProcessor(eventHandler, cliMgr.Client(), resolver)
	sessionLoader := historyserver.NewSessionLoader(processor, serverCtx)

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

	wg.Add(1)
	go func() {
		defer wg.Done()
		handler.Run(stop)
		logrus.Info("HTTP server shutdown complete")
	}()

	wg.Wait()
	logrus.Info("Graceful shutdown complete")
}
