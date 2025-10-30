package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path"
	"time"

	"github.com/ray-project/kuberay/historyserver/backend"
	"github.com/ray-project/kuberay/historyserver/backend/collector/runtime"
	"github.com/ray-project/kuberay/historyserver/backend/server"
	"github.com/ray-project/kuberay/historyserver/backend/types"
	"github.com/ray-project/kuberay/historyserver/utils"
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

	flag.StringVar(&role, "role", "Worker", "")
	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "")
	flag.StringVar(&rayClusterName, "ray-cluster-name", "", "")
	flag.StringVar(&rayClusterId, "ray-cluster-id", "default", "")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "")
	flag.IntVar(&logBatching, "log-batching", 1000, "")
	flag.IntVar(&eventsPort, "events-port", 8080, "")
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

	data, err := os.ReadFile(runtimeClassConfigPath)
	if err != nil {
		panic("Failed to read runtime class config " + err.Error())
	}
	jsonData := make(map[string]interface{})
	err = json.Unmarshal(data, &jsonData)
	if err != nil {
		panic("Failed to parse runtime class config: " + err.Error())
	}

	registry := backend.GetWriterRegistry()
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
	eventServer := server.NewEventServer(writter, rayRootDir, sessionDir, rayNodeId, rayClusterName, rayClusterId, sessionName)
	eventServer.InitServer(eventsPort)

	collector := runtime.NewCollector(&globalConfig, writter)
	_ = collector.Start(context.TODO().Done())

	eventStop := eventServer.WaitForStop()
	logStop := collector.WaitForStop()
	<-eventStop
	<-logStop
	logrus.Info("All servers shutdown")
}
