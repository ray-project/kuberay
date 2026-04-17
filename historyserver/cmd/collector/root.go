package main

import (
	"encoding/json"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/ray-project/kuberay/historyserver/pkg/collector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/eventcollector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/runtime"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

// CollectorOptions holds the configuration options for the collector command.
type CollectorOptions struct {
	Role                   string
	RuntimeClassName       string
	RayClusterName         string
	RayClusterNamespace    string
	RayRootDir             string
	LogBatching            int
	EventsPort             int
	PushInterval           time.Duration
	RuntimeClassConfigPath string
}

// NewCollectorCommand creates a new collector command.
func NewCollectorCommand() *cobra.Command {
	opts := &CollectorOptions{}

	cmd := &cobra.Command{
		Use:   "collector",
		Short: "Ray log and event collector",
		Long:  "Collector collects Ray logs and events from Ray clusters and pushes them to storage.",
		Run: func(cmd *cobra.Command, args []string) {
			runCollector(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Role, "role", "Worker", "Role of the Ray node (Head or Worker)")
	cmd.Flags().StringVar(&opts.RuntimeClassName, "runtime-class-name", "", "Runtime class name for storage backend")
	cmd.Flags().StringVar(&opts.RayClusterName, "ray-cluster-name", "", "Name of the Ray cluster")
	cmd.Flags().StringVar(&opts.RayClusterNamespace, "ray-cluster-namespace", "default", "Namespace of the Ray cluster")
	cmd.Flags().StringVar(&opts.RayRootDir, "ray-root-dir", "", "Ray root directory")
	cmd.Flags().IntVar(&opts.LogBatching, "log-batching", 1000, "Number of logs to batch before pushing")
	cmd.Flags().IntVar(&opts.EventsPort, "events-port", 8080, "Port for events server")
	cmd.Flags().StringVar(&opts.RuntimeClassConfigPath, "runtime-class-config-path", "", "Path to runtime class config file")
	cmd.Flags().DurationVar(&opts.PushInterval, "push-interval", time.Minute, "Interval for pushing logs to storage")

	_ = cmd.MarkFlagRequired("runtime-class-name")
	_ = cmd.MarkFlagRequired("ray-cluster-name")
	_ = cmd.MarkFlagRequired("ray-root-dir")

	return cmd
}

func runCollector(opts *CollectorOptions) {
	var additionalEndpoints []string
	if epStr := os.Getenv("RAY_COLLECTOR_ADDITIONAL_ENDPOINTS"); epStr != "" {
		for _, ep := range strings.Split(epStr, ",") {
			ep = strings.TrimSpace(ep)
			if ep != "" {
				additionalEndpoints = append(additionalEndpoints, ep)
			}
		}
	}

	endpointPollInterval := 30 * time.Second
	if intervalStr := os.Getenv("RAY_COLLECTOR_POLL_INTERVAL"); intervalStr != "" {
		parsed, parseErr := time.ParseDuration(intervalStr)
		if parseErr != nil {
			logrus.Fatalf("Failed to parse RAY_COLLECTOR_POLL_INTERVAL: %v", parseErr)
		}
		if parsed <= 0 {
			logrus.Fatalf("RAY_COLLECTOR_POLL_INTERVAL must be positive, got: %s", intervalStr)
		}
		endpointPollInterval = parsed
	}

	sessionDir, err := utils.GetSessionDir()
	if err != nil {
		logrus.Fatalf("Failed to get session dir: %v", err)
	}

	rayNodeId, err := utils.GetRayNodeID()
	if err != nil {
		logrus.Fatalf("Failed to get ray node id: %v", err)
	}

	sessionName := path.Base(sessionDir)

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

	registry := collector.GetWriterRegistry()
	factory, ok := registry[opts.RuntimeClassName]
	if !ok {
		logrus.Fatalf("Not supported runtime class name: %s for role: %s", opts.RuntimeClassName, opts.Role)
	}

	globalConfig := types.RayCollectorConfig{
		RootDir:             opts.RayRootDir,
		SessionDir:          sessionDir,
		RayNodeName:         rayNodeId,
		Role:                opts.Role,
		RayClusterName:      opts.RayClusterName,
		RayClusterNamespace: opts.RayClusterNamespace,
		PushInterval:        opts.PushInterval,
		LogBatching:         opts.LogBatching,
		DashboardAddress:    os.Getenv("RAY_DASHBOARD_ADDRESS"),

		AdditionalEndpoints:  additionalEndpoints,
		EndpointPollInterval: endpointPollInterval,
	}
	logrus.Info("Using collector config: ", globalConfig)

	writer, err := factory(&globalConfig, jsonData)
	if err != nil {
		logrus.Fatalf("Failed to create writer for runtime class name: %s for role: %s", opts.RuntimeClassName, opts.Role)
	}

	var wg sync.WaitGroup

	sigChan := make(chan os.Signal, 1)
	stop := make(chan struct{}, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	wg.Add(1)
	// Create and initialize EventCollector
	go func() {
		defer wg.Done()
		eventCollector := eventcollector.NewEventCollector(writer, opts.RayRootDir, sessionDir, rayNodeId, opts.RayClusterName, opts.RayClusterNamespace, sessionName)
		eventCollector.Run(stop, opts.EventsPort)
		logrus.Info("Event collector shutdown")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		logCollector := runtime.NewCollector(&globalConfig, writer)
		logCollector.Run(stop)
		logrus.Info("Log collector shutdown")
	}()

	<-sigChan
	logrus.Info("Received shutdown signal, initiating graceful shutdown...")

	// Stop both the event collector and the log collector
	close(stop)

	// Wait for both goroutines to complete
	wg.Wait()
	logrus.Info("Graceful shutdown complete")
}
