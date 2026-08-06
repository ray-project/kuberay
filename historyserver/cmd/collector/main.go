package main

import (
	"encoding/json"
	"errors"
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

func main() {
	role := ""
	runtimeClassName := ""
	rayClusterName := ""
	rayClusterNamespace := ""
	rayRootDir := ""
	logBatching := 1000
	eventsPort := 8080
	pushInterval := time.Minute
	ownerKind := ""
	ownerName := ""
	enableEventCollector := true
	enableLogCollector := true
	runtimeClassConfigPath := ""

	// Event collector disk-first storage flags.
	eventDataDir := "/tmp/ray/event-data"
	eventRotationInterval := 5 * time.Minute
	eventMaxFileSizeMB := 100
	eventMaxDiskMB := 200
	eventCompressionEnabled := false

	flag.BoolVar(&enableEventCollector, "enable-event-collector", true, "Enable event collector")
	flag.BoolVar(&enableLogCollector, "enable-log-collector", true, "Enable log collector")
	flag.StringVar(&role, "role", "Worker", "Role of the collector node: Head or Worker")
	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "")
	flag.StringVar(&rayClusterName, "ray-cluster-name", "", "")
	flag.StringVar(&rayClusterNamespace, "ray-cluster-namespace", "default", "")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "")
	flag.IntVar(&logBatching, "log-batching", 1000, "")
	flag.IntVar(&eventsPort, "events-port", 8080, "")
	flag.StringVar(&runtimeClassConfigPath, "runtime-class-config-path", "", "")
	flag.DurationVar(&pushInterval, "push-interval", time.Minute, "")
	flag.StringVar(&ownerKind, "owner-kind", "", "")
	flag.StringVar(&ownerName, "owner-name", "", "")

	flag.StringVar(&eventDataDir, "event-data-dir", eventDataDir, "Root directory for JSONL event files")
	flag.DurationVar(&eventRotationInterval, "event-rotation-interval", eventRotationInterval, "Time threshold to rotate active JSONL file")
	flag.IntVar(&eventMaxFileSizeMB, "event-max-file-size-mb", eventMaxFileSizeMB, "Size threshold (MB) to rotate active JSONL file")
	flag.IntVar(&eventMaxDiskMB, "event-max-disk-mb", eventMaxDiskMB, "Max total disk usage (MB) before 503 backpressure")
	flag.BoolVar(&eventCompressionEnabled, "event-compression-enabled", eventCompressionEnabled, "Enable gzip compression when uploading rotated JSONL files to remote storage (false uploads plain JSONL)")

	flag.Parse()

	if val := os.Getenv("RAY_CLUSTER_NAME"); val != "" {
		rayClusterName = val
	}
	if val := os.Getenv("RAY_CLUSTER_NAMESPACE"); val != "" {
		rayClusterNamespace = val
	}
	if val := os.Getenv("RAY_ROLE"); val != "" {
		role = val
	}
	if val := os.Getenv("OWNER_KIND"); val != "" {
		ownerKind = val
	}
	if val := os.Getenv("OWNER_NAME"); val != "" {
		ownerName = val
	}
	if val := os.Getenv("RAY_ROOT_DIR"); val != "" {
		rayRootDir = val
	}
	if val := os.Getenv("EVENTS_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			eventsPort = port
		}
	}
	if val := os.Getenv("LOG_BATCHING"); val != "" {
		if batch, err := strconv.Atoi(val); err == nil {
			logBatching = batch
		}
	}
	if val := os.Getenv("PUSH_INTERVAL"); val != "" {
		if interval, err := time.ParseDuration(val); err == nil {
			pushInterval = interval
		}
	}
	if val := os.Getenv("ENABLE_EVENT_COLLECTOR"); val != "" {
		if enabled, err := strconv.ParseBool(val); err == nil {
			enableEventCollector = enabled
		}
	}
	if val := os.Getenv("ENABLE_LOG_COLLECTOR"); val != "" {
		if enabled, err := strconv.ParseBool(val); err == nil {
			enableLogCollector = enabled
		}
	}
	if val := os.Getenv("RUNTIME_CLASS_CONFIG_PATH"); val != "" {
		runtimeClassConfigPath = val
	}

	role = strings.TrimSpace(role)
	if strings.EqualFold(role, "head") {
		role = "Head"
	} else if strings.EqualFold(role, "worker") {
		role = "Worker"
	} else {
		logrus.Fatalf("Invalid role: %s, must be Head or Worker", role)
	}

	if err := validateFlags(&rayClusterName, &rayClusterNamespace, &ownerKind, &ownerName, enableEventCollector, enableLogCollector); err != nil {
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

	// RAY_COLLECTOR_ADDITIONAL_ENDPOINTS is optional: the head collector always
	// polls its built-in endpoints, and anything listed here is polled on top.
	var additionalEndpoints []string
	if epStr := os.Getenv("RAY_COLLECTOR_ADDITIONAL_ENDPOINTS"); epStr != "" {
		for _, ep := range strings.Split(epStr, ",") {
			ep = strings.TrimSpace(ep)
			if ep != "" {
				additionalEndpoints = append(additionalEndpoints, ep)
			}
		}
	}

	// Fall back instead of exiting: crash-looping this sidecar would take the head pod
	// out of its Service endpoints.
	endpointPollInterval := 30 * time.Second
	if v := os.Getenv("RAY_COLLECTOR_POLL_INTERVAL"); v != "" {
		parsed, err := time.ParseDuration(v)
		if err == nil && parsed <= 0 {
			err = errors.New("must be positive")
		}
		if err != nil {
			logrus.Warnf("Invalid RAY_COLLECTOR_POLL_INTERVAL=%s (%v), using default %s", v, err, endpointPollInterval)
		} else {
			endpointPollInterval = parsed
		}
	}

	jsonData := make(map[string]interface{})
	if runtimeClassConfigPath != "" {
		data, err := os.ReadFile(runtimeClassConfigPath)
		if err != nil {
			logrus.Fatalf("Failed to read runtime class config from %s: %v", runtimeClassConfigPath, err)
		}
		if err := json.Unmarshal(data, &jsonData); err != nil {
			logrus.Fatalf("Failed to parse runtime class config from %s: %v", runtimeClassConfigPath, err)
		}
	}

	if val := os.Getenv("STORAGE_BACKEND"); val != "" {
		runtimeClassName = val
	} else if val := os.Getenv("RUNTIME_CLASS_NAME"); val != "" {
		runtimeClassName = val
	}
	runtimeClassName = strings.ToLower(runtimeClassName)

	registry := collector.GetWriterRegistry()
	factory, ok := registry[runtimeClassName]
	if !ok {
		logrus.Fatalf("Not supported runtime class name: %s for role: %s.", runtimeClassName, role)
	}

	rayNodeId, err := utils.GetNodeRayIDWithFQIP()
	if err != nil {
		logrus.Fatalf("Failed to get ray node id via HTTP endpoint: %v", err)
	}

	rayNodeId, err = utils.ConvertBase64ToHex(rayNodeId)
	if err != nil {
		logrus.Fatalf("Failed to normalize ray node id to hex: %v", err)
	}

	activeSessionDir, err := utils.GetSessionDir()
	if err != nil {
		logrus.Fatalf("Failed to get active session dir after discovering node id: %v", err)
	}

	if enableLogCollector {
		if err := utils.MoveLeftoverSessionLogs(activeSessionDir, rayNodeId); err != nil {
			logrus.Warnf("Failed to relocate leftover session logs at startup: %v", err)
		}
	}

	sessionName := path.Base(activeSessionDir)

	// The head collector shares a pod with the dashboard; override for non-default ports.
	dashboardAddress := "http://localhost:8265"
	if v := os.Getenv("RAY_DASHBOARD_ADDRESS"); v != "" {
		dashboardAddress = v
	}

	globalConfig := types.RayCollectorConfig{
		RootDir:             rayRootDir,
		SessionDir:          activeSessionDir,
		RayNodeName:         rayNodeId,
		Role:                role,
		RayClusterName:      rayClusterName,
		RayClusterNamespace: rayClusterNamespace,
		PushInterval:        pushInterval,
		LogBatching:         logBatching,
		DashboardAddress:    dashboardAddress,
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
		logrus.Fatalf("Failed to create writer for runtime class name: %s for role: %s, err: %v", runtimeClassName, role, err)
	}

	var wg sync.WaitGroup

	sigChan := make(chan os.Signal, 1)
	stop := make(chan struct{}, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if enableEventCollector {
		wg.Add(1)
		// Create and initialize EventCollector
		go func() {
			defer wg.Done()
			eventCollector := eventcollector.NewEventCollector(writer, rayRootDir, activeSessionDir, rayNodeId, rayClusterName, rayClusterNamespace, sessionName, ownerKind, ownerName, eventcollector.Options{
				DataDir:            eventDataDir,
				RotationInterval:   eventRotationInterval,
				MaxFileSizeBytes:   int64(eventMaxFileSizeMB) * 1024 * 1024,
				MaxDiskBytes:       int64(eventMaxDiskMB) * 1024 * 1024,
				CompressionEnabled: eventCompressionEnabled,
			})
			eventCollector.Run(stop, eventsPort)
			logrus.Info("Event collector shutdown")
		}()
	}

	if enableLogCollector {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logCollector := runtime.NewCollector(&globalConfig, writer)
			logCollector.Run(stop)
			logrus.Info("Log collector shutdown")
		}()
	}

	<-sigChan
	logrus.Info("Received shutdown signal, initiating graceful shutdown...")

	// Stop both the event collector and the log collector
	close(stop)

	// Wait for both goroutines to complete
	wg.Wait()
	logrus.Info("Graceful shutdown complete")
}

func validateFlags(rayClusterName, rayClusterNamespace, ownerKind, ownerName *string, enableEventCollector, enableLogCollector bool) error {
	if !enableEventCollector && !enableLogCollector {
		return fmt.Errorf("at least one of --enable-event-collector or --enable-log-collector must be enabled")
	}
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
