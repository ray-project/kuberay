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
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/sirupsen/logrus"
)

func main() {
	runtimeClassName := ""
	rayRootDir := ""
	runtimeClassConfigPath := ""
	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "")
	flag.StringVar(&runtimeClassConfigPath, "runtime-class-config-path", "", "")
	flag.Parse()

	if runtimeClassName == "" {
		logrus.Fatal("runtime-class-name is required")
	}

	jsonData := make(map[string]interface{})
	if runtimeClassConfigPath != "" {
		data, err := os.ReadFile(runtimeClassConfigPath)
		if err != nil {
			logrus.Fatalf("Failed to read runtime class config: %v", err)
		}
		err = json.Unmarshal(data, &jsonData)
		if err != nil {
			logrus.Fatalf("Failed to parse runtime class config: %v", err)
		}
	}

	readerRegistry := collector.GetReaderRegistry()
	readerFactory, ok := readerRegistry[runtimeClassName]
	if !ok {
		logrus.Fatalf("Not supported runtime class name: %s", runtimeClassName)
	}

	globalConfig := types.RayHistoryServerConfig{
		RootDir: rayRootDir,
	}

	reader, err := readerFactory(&globalConfig, jsonData)
	if err != nil {
		logrus.Fatalf("Failed to create reader: %v", err)
	}

	writerRegistry := collector.GetWriterRegistry()
	writerFactory, ok := writerRegistry[runtimeClassName]
	var writer storage.StorageWriter
	if ok {
		collectorConfig := types.RayCollectorConfig{
			RootDir: rayRootDir,
		}
		writer, err = writerFactory(&collectorConfig, jsonData)
		if err != nil {
			logrus.Errorf("Failed to create writer: %v", err)
		}
	} else {
		logrus.Warnf("No writer found for runtime class name: %s", runtimeClassName)
	}

	// Create EventHandler with storage reader and writer
	// NOTE: NewEventHandler signature needs to be updated to accept writer.
	eventHandler := eventserver.NewEventHandler(reader, writer)

	var wg sync.WaitGroup
	sigChan := make(chan os.Signal, 1)
	stop := make(chan struct{}, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	wg.Add(1)
	go func() {
		defer wg.Done()
		logrus.Info("Starting EventProcessor in background...")
		if err := eventHandler.Run(stop, 2); err != nil {
			logrus.Errorf("EventProcessor stopped with error: %v", err)
		}
		logrus.Info("EventProcessor shutdown complete")
	}()

	<-sigChan
	logrus.Info("Received shutdown signal, initiating graceful shutdown...")
	close(stop)
	wg.Wait()
	logrus.Info("Graceful shutdown complete")
}
