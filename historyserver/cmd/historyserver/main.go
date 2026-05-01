// Package main is the entrypoint for the History Server HTTP daemon.
// It exposes Ray Dashboard-shaped API endpoints over HTTP and runs the
// EventHandler as a background goroutine that processes raw event files
// into in-memory state served by the API surface.
package main

import (
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

	// ===== ClientManager =====
	cliMgr, err := historyserver.NewClientManager(kubeconfigs, useKubernetesProxy)
	if err != nil {
		logrus.Errorf("Failed to create client manager: %v", err)
		os.Exit(1)
	}

	// ===== Backend config =====
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

	// ===== Reader factory =====
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

	// ===== EventHandler =====
	eventHandler := eventserver.NewEventHandler(reader)

	// ===== Shutdown signaling =====
	var wg sync.WaitGroup
	sigChan := make(chan os.Signal, 1)
	stop := make(chan struct{}, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// ===== Start EventHandler in background =====
	wg.Add(1)
	go func() {
		defer wg.Done()
		logrus.Info("Starting EventHandler in background...")
		if err := eventHandler.Run(stop, 2); err != nil {
			logrus.Errorf("EventHandler stopped with error: %v", err)
		}
		logrus.Info("EventHandler shutdown complete")
	}()

	// ===== ServerHandler =====
	handler, err := historyserver.NewServerHandler(&globalConfig, dashboardDir, reader, cliMgr, eventHandler, useKubernetesProxy)
	if err != nil {
		logrus.Errorf("Failed to create server handler: %v", err)
		os.Exit(1)
	}

	// ===== Run HTTP server =====
	wg.Add(1)
	go func() {
		defer wg.Done()
		handler.Run(stop)
		logrus.Info("HTTP server shutdown complete")
	}()

	// ===== Wait for shutdown signal =====
	<-sigChan
	logrus.Info("Received shutdown signal, initiating graceful shutdown...")

	close(stop)
	wg.Wait()
	logrus.Info("Graceful shutdown complete")
}
