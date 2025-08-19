package collector

import (
	"encoding/json"
	"flag"
	"os"
	"time"

	"github.com/ray-project/kuberay/historyserver/backend"
	"github.com/ray-project/kuberay/historyserver/backend/collector/runtime"
	"github.com/ray-project/kuberay/historyserver/backend/types"
)

const runtimeClassConfigPath = "/var/collector-config/data"

func main() {
	role := ""
	runtimeClassName := ""
	rayClusterName := ""
	rayClusterId := ""
	logBatching := 1000
	pushInterval := time.Minute

	flag.StringVar(&role, "role", "Worker", "")
	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "")
	flag.StringVar(&rayClusterName, "ray-cluster-name", "", "")
	flag.StringVar(&rayClusterId, "ray-cluster-id", "", "")
	flag.IntVar(&logBatching, "log-batching", 1000, "")
	flag.DurationVar(&pushInterval, "push-interval", time.Minute, "")

	flag.Parse()

	data, err := os.ReadFile(runtimeClassConfigPath)
	if err != nil {
		panic("Failed to read runtime class config " + err.Error())
	}
	jsonData := make(map[string]interface{})
	err = json.Unmarshal(data, &jsonData)
	if err != nil {
		panic("Failed to parse runtime class config: " + err.Error())
	}

	registry := backend.GetRegistry()
	factory, ok := registry[runtimeClassName]
	if !ok {
		panic("Not supported runtime class name: " + runtimeClassName + " for role: " + role + ".")
	}

	globalConfig := types.RayCollectorConfig{
		Role:           role,
		RayClusterName: rayClusterName,
		RayClusterID:   rayClusterId,
		PushInterval:   pushInterval,
		LogBatching:    logBatching,
	}

	writter, err := factory(&globalConfig, jsonData)
	if err != nil {
		panic("Failed to create writter for runtime class name: " + runtimeClassName + " for role: " + role + ".")
	}
	collector := runtime.NewCollector(&globalConfig, writter)

	stop := make(chan int)
	collector.Start(stop)

	<-stop
}
