package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/collector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/runtime"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	"github.com/sirupsen/logrus"
)

const runtimeClassConfigPath = "/var/collector-config/data"

func main() {
	role := ""
	runtimeClassName := ""
	rayClusterName := ""
	rayClusterId := ""
	rayRootDir := ""
	logBatching := 1000
	eventsPort := 8080
	pushInterval := time.Minute
	runtimeClassConfigPath := "/var/collector-config/data"

	flag.StringVar(&role, "role", "Worker", "")
	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "")
	flag.StringVar(&rayClusterName, "ray-cluster-name", "", "")
	flag.StringVar(&rayClusterId, "ray-cluster-id", "default", "")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "")
	flag.IntVar(&logBatching, "log-batching", 1000, "")
	flag.IntVar(&eventsPort, "events-port", 8080, "")
	flag.StringVar(&runtimeClassConfigPath, "runtime-class-config-path", "", "") //"/var/collector-config/data"
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

	registry := collector.GetWriterRegistry()
	factory, ok := registry[runtimeClassName]
	if !ok {
		panic("Not supported runtime class name: " + runtimeClassName + " for role: " + role + ".")
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

	writter, err := factory(&globalConfig, jsonData)
	if err != nil {
		panic("Failed to create writter for runtime class name: " + runtimeClassName + " for role: " + role + ".")
	}

	// 创建并初始化EventServer
	eventServer := eventserver.NewEventServer(writter, rayRootDir, sessionDir, rayNodeId, rayClusterName, rayClusterId, sessionName)
	eventServer.InitServer(eventsPort)

	collector := runtime.NewCollector(&globalConfig, writter)
	_ = collector.Start(context.TODO().Done())

	eventStop := eventServer.WaitForStop()
	logStop := collector.WaitForStop()
	<-eventStop
	logrus.Info("Event server shutdown")
	<-logStop
	logrus.Info("Log server shutdown")
	logrus.Info("All servers shutdown")
}
