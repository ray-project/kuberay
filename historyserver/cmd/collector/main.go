package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/ray-project/kuberay/historyserver/pkg/collector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/eventcollector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/runtime"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

const runtimeClassConfigPath = "/var/collector-config/data"

func main() {
	role := ""
	runtimeClassName := ""
	rayClusterName := ""
	rayClusterNamespace := ""
	rayRootDir := ""
	logBatching := 1000
	eventsPort := 8080
	pushInterval := time.Minute
	runtimeClassConfigPath := "/var/collector-config/data"
	ownerKind := ""
	ownerName := ""

	// Event collector disk-first storage flags.
	eventDataDir := "/tmp/ray/event-data"
	eventRotationInterval := 5 * time.Minute
	eventMaxFileSizeMB := 100
	eventMaxDiskMB := 200
	eventCompressionEnabled := false

	flag.StringVar(&role, "role", "Worker", "")
	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "")
	flag.StringVar(&rayClusterName, "ray-cluster-name", "", "")
	flag.StringVar(&rayClusterNamespace, "ray-cluster-namespace", "default", "")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "")
	flag.IntVar(&logBatching, "log-batching", 1000, "")
	flag.IntVar(&eventsPort, "events-port", 8080, "")
	flag.StringVar(&runtimeClassConfigPath, "runtime-class-config-path", "", "") //"/var/collector-config/data"
	flag.DurationVar(&pushInterval, "push-interval", time.Minute, "")
	flag.StringVar(&ownerKind, "owner-kind", "", "")
	flag.StringVar(&ownerName, "owner-name", "", "")

	flag.StringVar(&eventDataDir, "event-data-dir", eventDataDir, "Root directory for JSONL event files")
	flag.DurationVar(&eventRotationInterval, "event-rotation-interval", eventRotationInterval, "Time threshold to rotate active JSONL file")
	flag.IntVar(&eventMaxFileSizeMB, "event-max-file-size-mb", eventMaxFileSizeMB, "Size threshold (MB) to rotate active JSONL file")
	flag.IntVar(&eventMaxDiskMB, "event-max-disk-mb", eventMaxDiskMB, "Max total disk usage (MB) before 503 backpressure")
	flag.BoolVar(&eventCompressionEnabled, "event-compression-enabled", eventCompressionEnabled, "Enable gzip compression when uploading rotated JSONL files to remote storage (false uploads plain JSONL)")

	flag.Parse()

	if err := validateFlags(&rayClusterName, &rayClusterNamespace, &ownerKind, &ownerName); err != nil {
		logrus.Fatalf("Failed to validate flags: %v", err)
	}

	// Override event collector settings from environment variables if present.
	if v := os.Getenv("RAY_COLLECTOR_EVENT_DATA_DIR"); v != "" {
		eventDataDir = v
	}
	if v := os.Getenv("RAY_COLLECTOR_EVENT_ROTATION_INTERVAL"); v != "" {
		if parsed, err := time.ParseDuration(v); err == nil && parsed > 0 {
			eventRotationInterval = parsed
		} else {
			logrus.Warnf("Invalid RAY_COLLECTOR_EVENT_ROTATION_INTERVAL=%s, using default %s", v, eventRotationInterval)
		}
	}
	if v := os.Getenv("RAY_COLLECTOR_EVENT_MAX_FILE_SIZE_MB"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			eventMaxFileSizeMB = parsed
		} else {
			logrus.Warnf("Invalid RAY_COLLECTOR_EVENT_MAX_FILE_SIZE_MB=%s, using default %d", v, eventMaxFileSizeMB)
		}
	}
	if v := os.Getenv("RAY_COLLECTOR_EVENT_MAX_DISK_MB"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			eventMaxDiskMB = parsed
		} else {
			logrus.Warnf("Invalid RAY_COLLECTOR_EVENT_MAX_DISK_MB=%s, using default %d", v, eventMaxDiskMB)
		}
	}
	if v := os.Getenv("RAY_COLLECTOR_EVENT_COMPRESSION_ENABLED"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			eventCompressionEnabled = parsed
		} else {
			logrus.Warnf("Invalid RAY_COLLECTOR_EVENT_COMPRESSION_ENABLED=%s, using default %v", v, eventCompressionEnabled)
		}
	}

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
			panic("Failed to parse RAY_COLLECTOR_POLL_INTERVAL: " + parseErr.Error())
		}
		if parsed <= 0 {
			panic("RAY_COLLECTOR_POLL_INTERVAL must be positive, got: " + intervalStr)
		}
		endpointPollInterval = parsed
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
		RootDir:             rayRootDir,
		SessionDir:          sessionDir,
		RayNodeName:         rayNodeId,
		Role:                role,
		RayClusterName:      rayClusterName,
		RayClusterNamespace: rayClusterNamespace,
		PushInterval:        pushInterval,
		LogBatching:         logBatching,
		DashboardAddress:    os.Getenv("RAY_DASHBOARD_ADDRESS"),
		OwnerKind:           ownerKind,
		OwnerName:           ownerName,

		AdditionalEndpoints:  additionalEndpoints,
		EndpointPollInterval: endpointPollInterval,

		EventDataDir:            eventDataDir,
		EventRotationInterval:   eventRotationInterval,
		EventMaxFileSizeMB:      eventMaxFileSizeMB,
		EventMaxDiskMB:          eventMaxDiskMB,
		EventCompressionEnabled: eventCompressionEnabled,
	}
	logrus.Info("Using collector config: ", globalConfig)

	writer, err := factory(&globalConfig, jsonData)
	if err != nil {
		panic(fmt.Sprintf("Failed to create writer for runtime class name: %s for role: %s, err: %+v", runtimeClassName, role, err))
	}

	var wg sync.WaitGroup

	sigChan := make(chan os.Signal, 1)
	stop := make(chan struct{}, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	wg.Add(1)
	// Create and initialize EventCollector
	go func() {
		defer wg.Done()
		eventCollector := eventcollector.NewEventCollector(writer, rayRootDir, sessionDir, rayNodeId, rayClusterName, rayClusterNamespace, sessionName, eventcollector.Options{
			DataDir:            eventDataDir,
			RotationInterval:   eventRotationInterval,
			MaxFileSizeBytes:   int64(eventMaxFileSizeMB) * 1024 * 1024,
			MaxDiskBytes:       int64(eventMaxDiskMB) * 1024 * 1024,
			CompressionEnabled: eventCompressionEnabled,
		})
		eventCollector.Run(stop, eventsPort)
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

func validateFlags(rayClusterName, rayClusterNamespace, ownerKind, ownerName *string) error {
	*rayClusterName = strings.TrimSpace(*rayClusterName)
	*rayClusterNamespace = strings.TrimSpace(*rayClusterNamespace)

	if errs := validation.IsDNS1123Subdomain(*rayClusterName); len(errs) > 0 {
		return fmt.Errorf("invalid ray-cluster-name %q: %s", *rayClusterName, strings.Join(errs, ", "))
	}
	if errs := validation.IsDNS1123Subdomain(*rayClusterNamespace); len(errs) > 0 {
		return fmt.Errorf("invalid ray-cluster-namespace %q: %s", *rayClusterNamespace, strings.Join(errs, ", "))
	}

	*ownerKind = strings.ToLower(strings.TrimSpace(*ownerKind))
	*ownerName = strings.TrimSpace(*ownerName)
	if (*ownerName != "" && *ownerKind == "") || (*ownerName == "" && *ownerKind != "") {
		return fmt.Errorf("both --owner-name and --owner-kind must be provided together, or both omitted")
	}
	if *ownerKind != "" && *ownerKind != utils.RayJobKind && *ownerKind != utils.RayServiceKind {
		return fmt.Errorf("unsupported owner-kind: %q. Supported kinds are %q or %q", *ownerKind, utils.RayJobKind, utils.RayServiceKind)
	}
	if *ownerName != "" {
		if errs := validation.IsDNS1123Subdomain(*ownerName); len(errs) > 0 {
			return fmt.Errorf("invalid owner-name %q: %s", *ownerName, strings.Join(errs, ", "))
		}
	}
	return nil
}
