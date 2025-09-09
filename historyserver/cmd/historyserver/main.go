package main

import (
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/ray-project/kuberay/historyserver/backend"
	"github.com/ray-project/kuberay/historyserver/backend/historyserver"
	"github.com/ray-project/kuberay/historyserver/backend/types"
)

const runtimeClassConfigPath = "/var/collector-config/data"

func main() {
	runtimeClassName := ""
	rayRootDir := ""
	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "")

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

	registry := backend.GetReaderRegistry()
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

	handler := historyserver.NewServerHandler(&globalConfig, reader)

	sigChan := make(chan os.Signal, 1)
	stop := make(chan struct{}, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	go handler.Run(stop)
	<-sigChan
	stop <- struct{}{}
}
