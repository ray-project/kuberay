package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path"
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
	runtimeClassConfigPath := "/var/collector-config/data"

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
	role = strings.TrimSpace(role)
	// Check incase users manually set role env var
	if strings.EqualFold(role, "head") {
		role = "Head"
	} else if strings.EqualFold(role, "worker") {
		role = "Worker"
	} else {
		panic("Invalid role: " + role + ", must be Head or Worker")
	}

	if err := validateFlags(&rayClusterName, &rayClusterNamespace, &ownerKind, &ownerName); err != nil {
		logrus.Fatalf("Failed to validate flags: %v", err)
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

	jsonData := make(map[string]interface{})
	if runtimeClassConfigPath != "" {
		data, err := os.ReadFile(runtimeClassConfigPath)
		if err != nil {
			panic(fmt.Sprintf("Failed to read runtime class config from %s: %v", runtimeClassConfigPath, err))
		}
		if err := json.Unmarshal(data, &jsonData); err != nil {
			panic(fmt.Sprintf("Failed to parse runtime class config from %s: %v", runtimeClassConfigPath, err))
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
		panic("Not supported runtime class name: " + runtimeClassName + " for role: " + role + ".")
	}

	rayNodeId, err := utils.GetNodeRayIDWithFQIP()
	if err != nil {
		panic("Failed to get ray node id via HTTP endpoint: " + err.Error())
	}

	rayNodeId, err = utils.ConvertBase64ToHex(rayNodeId)
	if err != nil {
		panic("Failed to normalize ray node id to hex: " + err.Error())
	}

	activeSessionDir, err := utils.GetSessionDir()
	if err != nil {
		panic("Failed to get active session dir after discovering node id: " + err.Error())
	}

	if err := utils.MoveLeftoverSessionLogs(activeSessionDir, rayNodeId); err != nil {
		logrus.Warnf("Failed to relocate leftover session logs at startup: %v", err)
	}

	sessionName := path.Base(activeSessionDir)

	globalConfig := types.RayCollectorConfig{
		RootDir:             rayRootDir,
		SessionDir:          activeSessionDir,
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
		eventCollector := eventcollector.NewEventCollector(writer, rayRootDir, activeSessionDir, rayNodeId, rayClusterName, rayClusterNamespace, sessionName)
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
