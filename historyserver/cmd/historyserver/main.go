package main

import (
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/ray-project/kuberay/historyserver/pkg/collector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/historyserver"
)

func main() {
	runtimeClassName := ""
	rayRootDir := ""
	kubeconfigs := ""
	runtimeClassConfigPath := "/var/collector-config/data"
	dashboardDir := ""
	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "")
	flag.StringVar(&kubeconfigs, "kubeconfigs", "", "")
	flag.StringVar(&dashboardDir, "dashboard-dir", "/dashboard", "")
	flag.StringVar(&runtimeClassConfigPath, "runtime-class-config-path", "", "") //"/var/collector-config/data"
	flag.Parse()

	cliMgr := historyserver.NewClientManager(kubeconfigs)

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

	handler := historyserver.NewServerHandler(&globalConfig, dashboardDir, reader, cliMgr)

	sigChan := make(chan os.Signal, 1)
	stop := make(chan struct{}, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	go handler.Run(stop)
	<-sigChan
	stop <- struct{}{}
}
