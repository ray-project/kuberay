// Package main is the entrypoint for the History Server HTTP daemon.
// It runs as a single binary that serves Ray Dashboard-shaped API calls
// and drives the per-session snapshot pipeline on demand via a Supervisor
// when /enter_cluster hits a dead session.
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
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"

	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/processor"
	"github.com/ray-project/kuberay/historyserver/beta-v2/pkg/server"
	"github.com/ray-project/kuberay/historyserver/pkg/collector"
	collectortypes "github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/historyserver"
)

// rayDashboardPort is fixed by Ray.
const rayDashboardPort = 8265

// httpClientTimeout bounds a single proxied dashboard round-trip — a safety
// net so a misbehaving upstream cannot wedge a handler forever.
const httpClientTimeout = 60 * time.Second

func main() {
	// ===== Flags =====
	var (
		runtimeClassName       string
		rayRootDir             string
		kubeconfigs            string
		dashboardDir           string
		runtimeClassConfigPath string
		useKubernetesProxy     bool
		cacheSize              int
	)
	flag.StringVar(&runtimeClassName, "runtime-class-name", "", "Storage backend: s3 / gcs / azureblob / aliyunoss / localtest")
	flag.StringVar(&rayRootDir, "ray-root-dir", "", "Root dir inside the bucket")
	flag.StringVar(&kubeconfigs, "kubeconfigs", "", "Kubeconfig path; empty = in-cluster")
	flag.StringVar(&dashboardDir, "dashboard-dir", "/dashboard", "Path to Ray Dashboard static assets")
	flag.StringVar(&runtimeClassConfigPath, "runtime-class-config-path", "", "Path to backend config JSON")
	flag.BoolVar(&useKubernetesProxy, "use-kubernetes-proxy", false, "Use local kubeconfig instead of in-cluster config")
	flag.IntVar(&cacheSize, "snapshot-cache-size", server.DefaultCacheSize, "LRU capacity for cached SessionSnapshots")
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

	// ===== Reader from registry =====
	// Reader is used by snapshot handlers (file-backed endpoints) and by the
	// Pipeline (event-file reads during parse). The collector writes raw
	// events; the Pipeline only reads them.
	readerFactory, ok := collector.GetReaderRegistry()[runtimeClassName]
	if !ok {
		logrus.Fatalf("unsupported runtime-class-name for reader: %s", runtimeClassName)
	}
	hsConfig := &collectortypes.RayHistoryServerConfig{RootDir: rayRootDir}
	reader, err := readerFactory(hsConfig, jsonData)
	if err != nil {
		logrus.Fatalf("create reader: %v", err)
	}

	// ===== ClientManager (for getClusters) =====
	cm, err := historyserver.NewClientManager(kubeconfigs, useKubernetesProxy)
	if err != nil {
		logrus.Fatalf("client manager: %v", err)
	}

	// ===== SnapshotLoader =====
	loader, err := server.NewSnapshotLoader(cacheSize)
	if err != nil {
		logrus.Fatalf("snapshot loader: %v", err)
	}

	// ===== Server context =====
	serverCtx, serverCancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT, syscall.SIGTERM,
	)
	defer serverCancel()

	// ===== K8s client (shared) =====
	// One controller-runtime client.Client + *rest.Config is shared by
	// both Pipeline.isDead and the production ProxyResolver.
	k8sClient, k8sCfg, err := buildK8sClient(kubeconfigs, useKubernetesProxy)
	if err != nil {
		logrus.Fatalf("build k8s client: %v", err)
	}

	// ===== Pipeline & Supervisor =====
	pipeline := processor.NewPipeline(reader, k8sClient)
	supervisor := server.NewSupervisor(pipeline, loader, serverCtx)

	// ===== Server =====
	srv := server.NewServer(loader, supervisor, reader, cm, dashboardDir, useKubernetesProxy)

	// ===== ProxyResolver wiring =====
	// Reuses k8sClient + cfg.Host built above. APIServerHost is empty when
	// using in-cluster config — buildProxyTargetURL handles that fallback.
	srv.SetProxyResolver(&productionProxyResolver{
		k8sClient:     k8sClient,
		apiServerHost: k8sCfg.Host,
	})

	// ===== HTTP client for the reverse proxy =====
	// Plain http.Client; useKubernetesProxy=true ideally needs a kube-aware RoundTripper.
	httpClient := &http.Client{Timeout: httpClientTimeout}
	srv.SetHTTPClient(httpClient)

	// ===== Signals + run =====
	stop := make(chan struct{})
	go func() {
		<-serverCtx.Done()
		logrus.Info("History server received shutdown signal, initiating graceful shutdown...")
		close(stop)
	}()

	logrus.Infof("historyserver (beta-v2, lazy) starting: runtime=%s cache-size=%d use-kube-proxy=%v",
		runtimeClassName, cacheSize, useKubernetesProxy)
	srv.Run(stop)
	logrus.Info("historyserver exited")
}

// buildK8sClient constructs a controller-runtime client.Client with the
// rayv1 scheme registered, returning the underlying *rest.Config so callers
// that need cfg.Host (e.g. ProxyResolver) can reuse it.
func buildK8sClient(kubeconfigs string, useKubeProxy bool) (client.Client, *rest.Config, error) {
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
		return nil, nil, err
	}
	cfg.QPS = 50
	cfg.Burst = 100

	scheme := runtime.NewScheme()
	utilruntime.Must(rayv1.AddToScheme(scheme))
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, nil, err
	}
	return c, cfg, nil
}

// productionProxyResolver implements server.ProxyResolver by querying K8s
// for the RayCluster CR and returning its head-service info.
type productionProxyResolver struct {
	k8sClient     client.Client
	apiServerHost string
}

// ResolveHead looks up the RayCluster by (namespace, name), derives the
// head service name from Status.Head.ServiceName, and returns a ServiceInfo
// with the Ray Dashboard port.
func (p *productionProxyResolver) ResolveHead(ctx context.Context, namespace, name string) (server.ServiceInfo, error) {
	if p.k8sClient == nil {
		return server.ServiceInfo{}, errors.New("No available kubernetes config found")
	}
	rc := rayv1.RayCluster{}
	if err := p.k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &rc); err != nil {
		return server.ServiceInfo{}, errors.New("RayCluster not found")
	}
	svcName := rc.Status.Head.ServiceName
	if svcName == "" {
		return server.ServiceInfo{}, errors.New("RayCluster head service not ready")
	}
	return server.ServiceInfo{
		ServiceName: svcName,
		Namespace:   namespace,
		Port:        rayDashboardPort,
	}, nil
}

// APIServerHost returns the kube-apiserver base URL for useKubeProxy mode.
// Empty means "fall back to in-cluster DNS".
func (p *productionProxyResolver) APIServerHost() string {
	return p.apiServerHost
}
