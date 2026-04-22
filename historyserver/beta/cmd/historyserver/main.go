// Package main is the entrypoint for the History Server v2 beta HTTP daemon.
// The historyserver answers Ray Dashboard-shaped API calls by loading immutable
// per-session snapshots from object storage (dead clusters) or reverse-proxying
// to the Ray Dashboard on the head pod (live clusters).
//
// See implementation_plan.md §Phase 4.5 for the constructor-tree spec and §8
// for the full endpoint table.
//
// Constructor tree:
//  1. flags  -> parsed
//  2. backend config JSON -> reader (via ReaderRegistry)
//  3. ClientManager -> used by getClusters to enumerate live RayClusters
//  4. SnapshotLoader -> LRU over storage reader
//  5. Server -> wired to (loader, reader, cm, dashboardDir, useKubeProxy)
//  6. productionProxyResolver -> a fresh controller-runtime client.Client for
//     the live reverse proxy (ClientManager's clients field is private, so
//     we build a second client here for the proxy wiring).
//  7. signal handling -> close stop on SIGINT/SIGTERM, srv.Run(stop).
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

	"github.com/ray-project/kuberay/historyserver/beta/pkg/server"
	"github.com/ray-project/kuberay/historyserver/pkg/collector"
	collectortypes "github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/historyserver"
)

// rayDashboardPort mirrors v1 getClusterSvcInfo (router.go:1820). The Ray
// Dashboard listens on 8265 regardless of deployment; this is a Ray constant.
const rayDashboardPort = 8265

// httpClientTimeout bounds a single proxied dashboard round-trip. Matches the
// order of v1 behavior (no explicit timeout) but we add 60s as a safety net
// so a misbehaving upstream cannot wedge a handler forever.
const httpClientTimeout = 60 * time.Second

func main() {
	// ===== Flags =====
	// Mirror v1 pkg/cmd/historyserver/main.go so operators see a familiar
	// surface; add only snapshot-cache-size which is v2-specific.
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
	// v2 historyserver only reads from storage; writing is the processor's
	// job. Reader factory signature is in pkg/collector/registry.go.
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
	// getClusters uses ClientManager.ListRayClusters() to enumerate live
	// RayClusters. That's all the exported API we need from ClientManager —
	// the proxy resolver below uses a separate client because ClientManager's
	// `clients` field is unexported.
	cm, err := historyserver.NewClientManager(kubeconfigs, useKubernetesProxy)
	if err != nil {
		logrus.Fatalf("client manager: %v", err)
	}

	// ===== SnapshotLoader =====
	loader, err := server.NewSnapshotLoader(reader, cacheSize)
	if err != nil {
		logrus.Fatalf("snapshot loader: %v", err)
	}

	// ===== Server =====
	srv := server.NewServer(loader, reader, cm, dashboardDir, useKubernetesProxy)

	// ===== ProxyResolver wiring =====
	// Build an independent controller-runtime client + capture rest.Config.Host
	// so the production ProxyResolver can answer ResolveHead() + APIServerHost().
	// We duplicate the client-building logic (instead of reusing ClientManager)
	// because ClientManager's clients/configs fields are unexported.
	proxyClient, apiHost, err := buildProxyPrimitives(kubeconfigs, useKubernetesProxy)
	if err != nil {
		logrus.Fatalf("build proxy primitives: %v", err)
	}
	srv.SetProxyResolver(&productionProxyResolver{
		k8sClient:     proxyClient,
		apiServerHost: apiHost,
	})

	// ===== HTTP client for the reverse proxy =====
	// PoC uses a plain http.Client. For useKubernetesProxy=true we would
	// ideally wrap with a kube-aware RoundTripper (cert/bearer-token auth) so
	// that buildProxyTargetURL's "/api/v1/namespaces/.../services/.../proxy"
	// path authenticates against kube-apiserver. That wiring is deferred —
	// see v1 NewServerHandler for the reference implementation.
	httpClient := &http.Client{Timeout: httpClientTimeout}
	srv.SetHTTPClient(httpClient)

	// ===== Signals + run =====
	// Mirror v1 main.go: SIGINT/SIGTERM -> close stop -> srv.Run returns.
	sigCh := make(chan os.Signal, 1)
	stop := make(chan struct{})
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logrus.Info("shutdown signal received")
		close(stop)
	}()

	logrus.Infof("historyserver starting: runtime=%s cache-size=%d use-kube-proxy=%v",
		runtimeClassName, cacheSize, useKubernetesProxy)
	srv.Run(stop)
	logrus.Info("historyserver exited")
}

// buildProxyPrimitives builds a controller-runtime client.Client and returns
// the rest.Config.Host used for the ProxyResolver production adapter.
// Mirrors v1 ClientManager's config-building (clientmanager.go) but exposes
// the pieces (client + host) that the proxy resolver needs.
//
// Note: we intentionally duplicate this instead of calling ClientManager
// methods because ClientManager.clients / .configs are unexported and W10's
// spec forbids modifying v1.
func buildProxyPrimitives(kubeconfigs string, useKubeProxy bool) (client.Client, string, error) {
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
		return nil, "", err
	}
	cfg.QPS = 50
	cfg.Burst = 100

	scheme := runtime.NewScheme()
	utilruntime.Must(rayv1.AddToScheme(scheme))
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, "", err
	}
	return c, cfg.Host, nil
}

// productionProxyResolver implements server.ProxyResolver by querying K8s for
// the RayCluster CR and returning its head-service info. ResolveHead's body
// is a port of v1 getClusterSvcInfo (router.go:1806) adapted from
// `[]client.Client` to a single client, and returning the v2 server.ServiceInfo
// shape instead of v1 historyserver.ServiceInfo.
type productionProxyResolver struct {
	k8sClient     client.Client
	apiServerHost string
}

// ResolveHead looks up the RayCluster by (namespace, name), derives the head
// service name from Status.Head.ServiceName (set by the ray-operator), and
// returns a ServiceInfo with the Ray Dashboard port (8265).
//
// Error taxonomy (same as v1):
//   - Get returns an error (NotFound or otherwise) -> "RayCluster not found"
//   - Status.Head.ServiceName is empty -> "RayCluster head service not ready"
//
// The error messages are kept verbatim from v1 so UX of the live-proxy path
// matches exactly.
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

// APIServerHost returns the kube-apiserver base URL for useKubeProxy=true
// targetURL construction. Empty means "fall back to in-cluster DNS" — the
// server already handles that path in buildProxyTargetURL.
func (p *productionProxyResolver) APIServerHost() string {
	return p.apiServerHost
}
