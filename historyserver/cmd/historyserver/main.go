package main

import (
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/ray-project/kuberay/historyserver/pkg/collector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/historyserver"
	"github.com/sirupsen/logrus"
)

func main() {
	runtimeClassName := ""
	rayRootDir := ""
	kubeconfigs := ""
	runtimeClassConfigPath := "/var/collector-config/data"
	dashboardDir := ""
	useKubernetesProxy := false
	useAuthTokenMode := false

	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "")
	flag.StringVar(&kubeconfigs, "kubeconfigs", "", "")
	flag.StringVar(&dashboardDir, "dashboard-dir", "/dashboard", "")
	flag.StringVar(&runtimeClassConfigPath, "runtime-class-config-path", "", "") //"/var/collector-config/data"
	flag.BoolVar(&useKubernetesProxy, "use-kubernetes-proxy", false, "")
	flag.BoolVar(&useAuthTokenMode, "use-auth-token-mode", false, "Enable Ray dashboard token authentication mode (requires x-ray-authorization header)")
	flag.Parse()

	cliMgr, err := historyserver.NewClientManager(kubeconfigs, useKubernetesProxy)
	if err != nil {
		logrus.Errorf("Failed to create client manager: %v", err)
		os.Exit(1)
	}

	jsonData := make(map[string]interface{})
	if runtimeClassConfigPath != "" {
		data, err := os.ReadFile(runtimeClassConfigPath)
		if err != nil {
			panic("Failed to read runtime class config " + err.Error())
		}
		err = json.Unmarshal(data, &jsonData)
		if err != nil {
			panic("Failed to parse runtime class config: " + err.Error())
		}
	}

	registry := collector.GetReaderRegistry()
	factory, ok := registry[runtimeClassName]
	if !ok {
		panic("Not supported runtime class name: " + runtimeClassName + ".")
	}

	globalConfig := types.RayHistoryServerConfig{
		RootDir: rayRootDir,
	}

	reader, err := factory(&globalConfig, jsonData)
	if err != nil {
		panic("Failed to create reader for runtime class name: " + runtimeClassName + ".")
	}

	// Create EventHandler with storage reader
	eventHandler := eventserver.NewEventHandler(reader)

	// WaitGroup to track goroutine completion
	var wg sync.WaitGroup

	sigChan := make(chan os.Signal, 1)
	stop := make(chan struct{}, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start EventHandler in background goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		logrus.Info("Starting EventHandler in background...")
		if err := eventHandler.Run(stop, 2); err != nil {
			logrus.Errorf("EventHandler stopped with error: %v", err)
		}
		logrus.Info("EventHandler shutdown complete")
	}()

	handler, err := historyserver.NewServerHandler(
		&globalConfig,
		dashboardDir,
		reader, cliMgr,
		eventHandler,
		useKubernetesProxy,
		useAuthTokenMode,
	)
	if err != nil {
		logrus.Errorf("Failed to create server handler: %v", err)
		os.Exit(1)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		handler.Run(stop)
		logrus.Info("HTTP server shutdown complete")
	}()

	<-sigChan
	logrus.Info("Received shutdown signal, initiating graceful shutdown...")

	// Stop both the server and the event handler
	close(stop)

	// Wait for both goroutines to complete
	wg.Wait()
	logrus.Info("Graceful shutdown complete")
}
