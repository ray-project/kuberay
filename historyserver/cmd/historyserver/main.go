package historyserver

import (
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/ray-project/kuberay/historyserver/backend"
	"github.com/ray-project/kuberay/historyserver/backend/historyserver"
	"github.com/ray-project/kuberay/historyserver/backend/types"
	"github.com/ray-project/kuberay/historyserver/utils"
)

const runtimeClassConfigPath = "/var/collector-config/data"

func main() {
	role := ""
	runtimeClassName := ""
	rayClusterName := ""
	rayClusterId := ""
	rayRootDir := ""

	flag.StringVar(&role, "role", "Worker", "")
	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "")
	flag.StringVar(&rayClusterName, "ray-cluster-name", "", "")
	flag.StringVar(&rayClusterId, "ray-cluster-id", "", "")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "")

	flag.Parse()

	sessionDir, err := utils.GetSessionDir()
	if err != nil {
		panic("Failed to get session dir: " + err.Error())
	}

	rayNodeId, err := utils.GetRayNodeID()
	if err != nil {
		panic("Failed to get ray node id: " + err.Error())
	}

	data, err := os.ReadFile(runtimeClassConfigPath)
	if err != nil {
		panic("Failed to read runtime class config " + err.Error())
	}
	jsonData := make(map[string]interface{})
	err = json.Unmarshal(data, &jsonData)
	if err != nil {
		panic("Failed to parse runtime class config: " + err.Error())
	}

	registry := backend.GetReaderRegistry()
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
	}

	reader, err := factory(&globalConfig, jsonData)
	if err != nil {
		panic("Failed to create reader for runtime class name: " + runtimeClassName + " for role: " + role + ".")
	}

	handler := historyserver.NewServerHandler(reader)

	sigChan := make(chan os.Signal, 1)
	stop := make(chan struct{}, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	handler.Run(stop)
}
