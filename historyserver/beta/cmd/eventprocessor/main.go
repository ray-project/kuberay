// Package main is the entrypoint for the History Server v2 beta eventprocessor
// daemon. The eventprocessor periodically scans all Ray sessions in object
// storage, detects dead clusters via the Kubernetes API, and writes immutable
// per-session snapshots back to storage.
//
// See implementation_plan.md §Phase 3.3 for the full constructor-tree spec and
// §9 decision #11 for the default scan interval (10 minutes).
//
// This binary exposes a sidecar Prometheus /metrics listener (default :9090)
// because, unlike the historyserver, it has no main HTTP server. Operators
// scrape this endpoint to alert on processor_last_tick_timestamp_seconds
// (plan §7 Open Risk #1) and processor_orphan_sessions (plan §7 Open Risk #8).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	"github.com/ray-project/kuberay/historyserver/beta/pkg/metrics"
	"github.com/ray-project/kuberay/historyserver/beta/pkg/processor"
	"github.com/ray-project/kuberay/historyserver/pkg/collector"
	collectortypes "github.com/ray-project/kuberay/historyserver/pkg/collector/types"
)

// metricsShutdownTimeout bounds how long we wait for in-flight /metrics
// scrapes to finish during shutdown. Prometheus scrapes are tiny and
// idempotent, so 5s is more than sufficient.
const metricsShutdownTimeout = 5 * time.Second

func main() {
	// ===== Flags =====
	// Mirror v1 collector/historyserver flag style (pkg/cmd/collector/main.go,
	// pkg/cmd/historyserver/main.go) so operators see a familiar surface.
	var (
		runtimeClassName       string
		rayRootDir             string
		runtimeClassConfigPath string
		kubeconfigs            string
		useKubernetesProxy     bool
		processInterval        time.Duration
		metricsAddr            string
	)
	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "Storage backend: s3 / gcs / azureblob / aliyunoss / localtest")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "Root dir inside the bucket")
	flag.StringVar(&runtimeClassConfigPath, "runtime-class-config-path", "/var/collector-config/data", "Path to backend config JSON")
	flag.StringVar(&kubeconfigs, "kubeconfigs", "", "Kubeconfig path; empty = in-cluster")
	flag.BoolVar(&useKubernetesProxy, "use-kubernetes-proxy", false, "Use local kubeconfig instead of in-cluster config")
	flag.DurationVar(&processInterval, "process-interval", processor.DefaultInterval, "Scan interval (default 10m)")
	flag.StringVar(&metricsAddr, "metrics-addr", ":9090", "Prometheus /metrics listener address")
	flag.Parse()

	if runtimeClassName == "" {
		logrus.Fatal("--runtime-class-name is required")
	}

	// ===== Load backend config =====
	jsonData := make(map[string]interface{})
	if runtimeClassConfigPath != "" {
		data, err := os.ReadFile(runtimeClassConfigPath)
		if err != nil {
			logrus.Fatalf("read runtime-class-config: %v", err)
		}
		if err := json.Unmarshal(data, &jsonData); err != nil {
			logrus.Fatalf("parse runtime-class-config: %v", err)
		}
	}

	// ===== Reader + Writer from registries =====
	// Reader factory signature: func(*RayHistoryServerConfig, map[string]interface{}) (StorageReader, error)
	// Writer factory signature: func(*RayCollectorConfig,      map[string]interface{}) (StorageWriter, error)
	// See pkg/collector/registry.go for the authoritative types.
	readerFactory, ok := collector.GetReaderRegistry()[runtimeClassName]
	if !ok {
		logrus.Fatalf("unsupported runtime-class-name for reader: %s", runtimeClassName)
	}
	writerFactory, ok := collector.GetWriterRegistry()[runtimeClassName]
	if !ok {
		logrus.Fatalf("unsupported runtime-class-name for writer: %s", runtimeClassName)
	}

	hsConfig := &collectortypes.RayHistoryServerConfig{RootDir: rayRootDir}
	reader, err := readerFactory(hsConfig, jsonData)
	if err != nil {
		logrus.Fatalf("create reader: %v", err)
	}

	// Processor writes are global — not tied to a specific pod/session — so
	// we supply only the fields the backend constructors actually read at
	// construction time (RootDir). Per-session fields (SessionDir,
	// RayClusterName, etc.) are unused on the write path for snapshots.
	collectorCfg := &collectortypes.RayCollectorConfig{RootDir: rayRootDir}
	writer, err := writerFactory(collectorCfg, jsonData)
	if err != nil {
		logrus.Fatalf("create writer: %v", err)
	}

	// ===== K8s client =====
	// Pipeline.isDead does a single Get on RayCluster by (namespace, name).
	// We build rest.Config inline rather than reusing v1 ClientManager
	// because its client list is unexported and we only need one cluster.
	k8sClient, err := buildK8sClient(kubeconfigs, useKubernetesProxy)
	if err != nil {
		logrus.Fatalf("build k8s client: %v", err)
	}

	// ===== Wire processor =====
	// rayRootDir is passed so writeSnapshot prepends it when generating S3 keys,
	// matching what the reader's GetContent auto-prepends on read.
	pipeline := processor.NewPipeline(reader, writer, k8sClient, rayRootDir)
	proc := processor.NewProcessor(pipeline, reader, processInterval)

	// ===== Signals + run =====
	sigCh := make(chan os.Signal, 1)
	stop := make(chan struct{})
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logrus.Info("shutdown signal received")
		close(stop)
	}()

	// ===== Metrics sidecar =====
	// Dedicated http.Server with its own mux so the /metrics endpoint cannot
	// ever collide with arbitrary future routes. No other handlers on this
	// listener — scope is Prometheus scrapes only.
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	metricsSrv := &http.Server{Addr: metricsAddr, Handler: mux}
	go func() {
		logrus.Infof("metrics server starting on %s", metricsAddr)
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.Errorf("metrics server: %v", err)
		}
	}()
	// Piggy-back on stop: when main orchestrates shutdown, drain /metrics too.
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), metricsShutdownTimeout)
		defer cancel()
		if err := metricsSrv.Shutdown(ctx); err != nil {
			logrus.Errorf("metrics server shutdown: %v", err)
		}
	}()

	logrus.Infof("eventprocessor starting: runtime=%s interval=%s", runtimeClassName, processInterval)
	proc.Run(stop)
	logrus.Info("eventprocessor exited")
}

// buildK8sClient constructs a controller-runtime client.Client with the rayv1
// scheme registered, using either an explicit kubeconfig path, a local
// kubeconfig (proxy mode), or in-cluster config. Pattern lifted from
// pkg/historyserver/clientmanager.go but simplified for a single client.
func buildK8sClient(kubeconfigs string, useKubeProxy bool) (client.Client, error) {
	var cfg *rest.Config
	var err error

	switch {
	case kubeconfigs != "":
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigs)
	case useKubeProxy:
		loading := clientcmd.NewDefaultClientConfigLoadingRules()
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loading, &clientcmd.ConfigOverrides{}).ClientConfig()
	default:
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}
	cfg.QPS = 50
	cfg.Burst = 100

	scheme := runtime.NewScheme()
	utilruntime.Must(rayv1.AddToScheme(scheme))

	return client.New(cfg, client.Options{Scheme: scheme})
}
