package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/collector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/runtime"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

func main() {
	var role string
	var storageProvider string
	var rayClusterName string
	var rayClusterId string
	var rayRootDir string
	var logBatching int
	var eventsPort int
	var pushInterval time.Duration
	var storageProviderConfigPath string

	flag.StringVar(&role, "role", "Worker", "")
	flag.StringVar(&storageProvider, "storage-provider", "", "")
	flag.StringVar(&rayClusterName, "ray-cluster-name", "", "")
	flag.StringVar(&rayClusterId, "ray-cluster-id", "default", "")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "")
	flag.IntVar(&logBatching, "log-batching", 1000, "")
	flag.IntVar(&eventsPort, "events-port", 8080, "")
	flag.StringVar(&storageProviderConfigPath, "storage-provider-config-path", "", "") //"/var/collector-config/data"
	flag.DurationVar(&pushInterval, "push-interval", time.Minute, "")

	flag.Parse()

	sessionDir, err := utils.GetSessionDir()
	if err != nil {
		panic("Failed to get session dir: " + err.Error())
	}

	rayNodeId, err := utils.GetRayNodeID()
	if err != nil {
		panic("Failed to get ray node id: " + err.Error())
	}

	sessionName := path.Base(sessionDir)

	jsonData := make(map[string]interface{})
	if storageProviderConfigPath != "" {
		data, err := os.ReadFile(storageProviderConfigPath)
		if err != nil {
			panic("Failed to read storage provider config " + err.Error())
		}
		err = json.Unmarshal(data, &jsonData)
		if err != nil {
			panic("Failed to parse storage provider config: " + err.Error())
		}
	}

	registry := collector.GetWriterRegistry()
	factory, ok := registry[storageProvider]
	if !ok {
		panic("Not supported storage provider: " + storageProvider + " for role: " + role + ".")
	}

	globalConfig := types.RayCollectorConfig{
		RootDir:        rayRootDir,
		SessionDir:     sessionDir,
		RayNodeName:    rayNodeId,
		Role:           role,
		RayClusterName: rayClusterName,
		RayClusterID:   rayClusterId,
		PushInterval:   pushInterval,
		LogBatching:    logBatching,
	}
	logrus.Info("Using collector config: ", globalConfig)

	writer, err := factory(&globalConfig, jsonData)
	if err != nil {
		panic("Failed to create writer for storage provider: " + storageProvider + " for role: " + role + ".")
	}

	// Create and initialize EventServer
	eventServer := eventserver.NewEventServer(writer, rayRootDir, sessionDir, rayNodeId, rayClusterName, rayClusterId, sessionName)
	eventServer.InitServer(eventsPort)

	logCollector := runtime.NewCollector(&globalConfig, writer)
	_ = logCollector.Start(context.TODO().Done())

	eventStop := eventServer.WaitForStop()
	logStop := logCollector.WaitForStop()
	<-eventStop
	logrus.Info("Event server shutdown")
	<-logStop
	logrus.Info("Log server shutdown")
	logrus.Info("All servers shutdown")
}
