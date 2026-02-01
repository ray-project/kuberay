package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/collector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/runtime"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
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
	supportUnsupportedData := false
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

	// Read SUPPORT_RAY_EVENT_UNSUPPORTED_DATA environment variable if collector runs in head node
	if envValue := os.Getenv("SUPPORT_RAY_EVENT_UNSUPPORTED_DATA"); role == "Head" && envValue != "" {
		if parsed, err := strconv.ParseBool(envValue); err == nil {
			supportUnsupportedData = parsed
		} else {
			logrus.Warnf("Invalid value for SUPPORT_RAY_EVENT_UNSUPPORTED_DATA: %s, using default: %v", envValue, supportUnsupportedData)
		}
	}

	dashboardAddress := os.Getenv("RAY_DASHBOARD_ADDRESS")
	if dashboardAddress == "" {
		panic(fmt.Errorf("missing RAY_DASHBOARD_ADDRESS in environment variables"))
	}

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
		RootDir:                      rayRootDir,
		SessionDir:                   sessionDir,
		RayNodeName:                  rayNodeId,
		Role:                         role,
		RayClusterName:               rayClusterName,
		RayClusterID:                 rayClusterId,
		PushInterval:                 pushInterval,
		LogBatching:                  logBatching,
		DashboardAddress:             dashboardAddress,
		SupportRayEventUnSupportData: supportUnsupportedData,
	}
	logrus.Info("Using collector config: ", globalConfig)

	writer, err := factory(&globalConfig, jsonData)
	if err != nil {
		panic("Failed to create writer for runtime class name: " + runtimeClassName + " for role: " + role + ".")
	}

	// Create and initialize EventServer
	eventServer := eventserver.NewEventServer(writer, rayRootDir, sessionDir, rayNodeId, rayClusterName, rayClusterId, sessionName)
	eventServer.InitServer(eventsPort)

	collector := runtime.NewCollector(&globalConfig, writer)
	_ = collector.Start(context.TODO().Done())

	eventStop := eventServer.WaitForStop()
	logStop := collector.WaitForStop()
	<-eventStop
	logrus.Info("Event server shutdown")
	<-logStop
	logrus.Info("Log server shutdown")
	logrus.Info("All servers shutdown")
}
