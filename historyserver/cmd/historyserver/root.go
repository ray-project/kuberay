package main

import (
	"encoding/json"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/ray-project/kuberay/historyserver/pkg/collector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/historyserver"
)

// HistoryServerOptions holds the configuration options for the history server command.
type HistoryServerOptions struct {
	RuntimeClassName       string
	RayRootDir             string
	Kubeconfigs            string
	RuntimeClassConfigPath string
	DashboardDir           string
	UseKubernetesProxy     bool
}

// NewHistoryServerCommand creates a new history server command.
func NewHistoryServerCommand() *cobra.Command {
	opts := &HistoryServerOptions{}

	cmd := &cobra.Command{
		Use:   "historyserver",
		Short: "Ray history server",
		Long:  "History server provides a web interface to view Ray job history and logs.",
		Run: func(cmd *cobra.Command, args []string) {
			runHistoryServer(opts)
		},
	}

	cmd.Flags().StringVar(&opts.RuntimeClassName, "runtime-class-name", "", "Runtime class name for storage backend")
	cmd.Flags().StringVar(&opts.RayRootDir, "ray-root-dir", "", "Ray root directory")
	cmd.Flags().StringVar(&opts.Kubeconfigs, "kubeconfigs", "", "Kubeconfig paths for multiple clusters")
	cmd.Flags().StringVar(&opts.DashboardDir, "dashboard-dir", "/dashboard", "Dashboard directory path")
	cmd.Flags().StringVar(&opts.RuntimeClassConfigPath, "runtime-class-config-path", "", "Path to runtime class config file")
	cmd.Flags().BoolVar(&opts.UseKubernetesProxy, "use-kubernetes-proxy", false, "Use Kubernetes proxy for API server access")

	_ = cmd.MarkFlagRequired("runtime-class-name")

	return cmd
}

func runHistoryServer(opts *HistoryServerOptions) {
	cliMgr, err := historyserver.NewClientManager(opts.Kubeconfigs, opts.UseKubernetesProxy)
	if err != nil {
		logrus.Errorf("Failed to create client manager: %v", err)
		os.Exit(1)
	}

	jsonData := make(map[string]interface{})
	if opts.RuntimeClassConfigPath != "" {
		data, err := os.ReadFile(opts.RuntimeClassConfigPath)
		if err != nil {
			logrus.Fatalf("Failed to read runtime class config: %v", err)
		}
		err = json.Unmarshal(data, &jsonData)
		if err != nil {
			logrus.Fatalf("Failed to parse runtime class config: %v", err)
		}
	}

	registry := collector.GetReaderRegistry()
	factory, ok := registry[opts.RuntimeClassName]
	if !ok {
		logrus.Fatalf("Not supported runtime class name: %s", opts.RuntimeClassName)
	}

	globalConfig := types.RayHistoryServerConfig{
		RootDir: opts.RayRootDir,
	}

	reader, err := factory(&globalConfig, jsonData)
	if err != nil {
		logrus.Fatalf("Failed to create reader for runtime class name: %s", opts.RuntimeClassName)
	}

	// Create EventHandler with storage reader
	eventHandler := eventserver.NewEventHandler(reader)

	// WaitGroup to track goroutine completion
	var wg sync.WaitGroup

	sigChan := make(chan os.Signal, 1)
	stop := make(chan struct{}, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start EventHandler in background goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		logrus.Info("Starting EventHandler in background...")
		if err := eventHandler.Run(stop, 2); err != nil {
			logrus.Errorf("EventHandler stopped with error: %v", err)
		}
		logrus.Info("EventHandler shutdown complete")
	}()

	handler, err := historyserver.NewServerHandler(&globalConfig, opts.DashboardDir, reader, cliMgr, eventHandler, opts.UseKubernetesProxy)
	if err != nil {
		logrus.Errorf("Failed to create server handler: %v", err)
		os.Exit(1)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		handler.Run(stop)
		logrus.Info("HTTP server shutdown complete")
	}()

	<-sigChan
	logrus.Info("Received shutdown signal, initiating graceful shutdown...")

	// Stop both the server and the event handler
	close(stop)

	// Wait for both goroutines to complete
	wg.Wait()
	logrus.Info("Graceful shutdown complete")
}
