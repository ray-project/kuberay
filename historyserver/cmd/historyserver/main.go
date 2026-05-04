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

	"github.com/ray-project/kuberay/historyserver/pkg/collector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
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
		snapshotCacheSize      int
	)
	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "Storage backend: s3 / gcs / azureblob / aliyunoss / localtest")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "Root dir inside the bucket")
	flag.StringVar(&kubeconfigs, "kubeconfigs", "", "Kubeconfig path; empty = in-cluster")
	flag.StringVar(&dashboardDir, "dashboard-dir", "/dashboard", "Path to Ray Dashboard static assets")
	flag.StringVar(&runtimeClassConfigPath, "runtime-class-config-path", "", "Path to backend config JSON")
	flag.BoolVar(&useKubernetesProxy, "use-kubernetes-proxy", false, "Use local kubeconfig instead of in-cluster config")
	flag.IntVar(&snapshotCacheSize, "snapshot-cache-size", historyserver.DefaultCacheSize, "LRU capacity for cached SessionSnapshots")
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

	// ===== Server context =====
	serverCtx, serverCancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT, syscall.SIGTERM,
	)
	defer serverCancel()

	// ===== SnapshotLoader =====
	loader, err := historyserver.NewSnapshotLoader(snapshotCacheSize)
	if err != nil {
		logrus.Fatalf("snapshot loader: %v", err)
	}

	// ===== Pipeline & Supervisor =====
	pipeline := historyserver.NewPipeline(reader, cliMgr.Client())
	supervisor := historyserver.NewSupervisor(pipeline, loader, serverCtx)

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
	handler, err := historyserver.NewServerHandler(&globalConfig, dashboardDir, reader, cliMgr, supervisor, loader, useKubernetesProxy)
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
