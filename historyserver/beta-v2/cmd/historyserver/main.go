// Package main is the entrypoint for the History Server v2 beta-v2 HTTP
// daemon. beta-v2 collapses the beta "historyserver + eventprocessor" pair
// into a single binary: the HS serves Ray Dashboard-shaped API calls AND
// drives the per-session snapshot pipeline on demand via a Supervisor when
// /enter_cluster hits a dead session. See historyserver/beta_poc.md §3 for
// the motivation and §1 for the three-layer idempotency guarantee that
// makes single-binary safe.
//
// Constructor tree (lazy mode):
//  1. flags -> parsed
//  2. backend config JSON -> reader + writer (ReaderRegistry / WriterRegistry)
//  3. ClientManager -> used by getClusters to enumerate live RayClusters
//  4. SnapshotLoader -> LRU over storage reader
//  5. k8s client.Client -> used by Pipeline.isDead (same shape beta's
//     eventprocessor built; see beta/cmd/eventprocessor/main.go:167)
//  6. Pipeline -> (reader, writer, k8sClient, rayRootDir)
//  7. Supervisor -> (pipeline, loader)
//  8. Server -> (loader, supervisor, reader, cm, dashboardDir, useKubeProxy)
//  9. productionProxyResolver -> a second controller-runtime client.Client
//     for the live reverse proxy (ClientManager's clients field is private,
//     so we build a separate client here and a separate one for Pipeline —
//     cheap; both are read-only).
//  10. signal handling -> close stop on SIGINT/SIGTERM, srv.Run(stop).
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

// rayDashboardPort mirrors v1 getClusterSvcInfo (router.go:1820). The Ray
// Dashboard listens on 8265 regardless of deployment; this is a Ray constant.
const rayDashboardPort = 8265

// httpClientTimeout bounds a single proxied dashboard round-trip. Matches
// the order of v1 behavior (no explicit timeout) but we add 60s as a
// safety net so a misbehaving upstream cannot wedge a handler forever.
const httpClientTimeout = 60 * time.Second

func main() {
	// ===== Flags =====
	// Mirror beta's historyserver flags (beta/cmd/historyserver/main.go) and
	// absorb the eventprocessor-only flag beta-v2 still needs: rayRootDir is
	// now used by the Pipeline's writeSnapshot (it was the eventprocessor's
	// sole job in beta).
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

	// ===== Reader + Writer from registries =====
	// Reader: used by snapshot handlers AND by the lazy-mode Pipeline
	// (Layer-2 S3 GET inside snapshotExists).
	// Writer: used by Pipeline.writeSnapshot for the skip-if-exists PUT.
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
	// Writes are global (not tied to a specific pod/session) so we populate
	// only RootDir — same pattern beta's eventprocessor used.
	collectorCfg := &collectortypes.RayCollectorConfig{RootDir: rayRootDir}
	writer, err := writerFactory(collectorCfg, jsonData)
	if err != nil {
		logrus.Fatalf("create writer: %v", err)
	}

	// ===== ClientManager (for getClusters) =====
	cm, err := historyserver.NewClientManager(kubeconfigs, useKubernetesProxy)
	if err != nil {
		logrus.Fatalf("client manager: %v", err)
	}

	// ===== SnapshotLoader =====
	loader, err := server.NewSnapshotLoader(reader, cacheSize)
	if err != nil {
		logrus.Fatalf("snapshot loader: %v", err)
	}

	// ===== Pipeline & Supervisor =====
	// Pipeline's K8s client is separate from the proxy-resolver client
	// below. Both are lightweight read-only clients; building two (vs.
	// threading one through ClientManager) is cheaper than forking v1 to
	// expose its private fields. Same rationale beta uses for its
	// dedicated eventprocessor client.
	pipelineK8s, err := buildK8sClient(kubeconfigs, useKubernetesProxy)
	if err != nil {
		logrus.Fatalf("build pipeline k8s client: %v", err)
	}
	// rayRootDir is passed so writeSnapshot prepends it when generating S3
	// keys, matching what the reader's GetContent auto-prepends on read.
	pipeline := processor.NewPipeline(reader, writer, pipelineK8s, rayRootDir)
	supervisor := server.NewSupervisor(pipeline, loader)

	// ===== Server =====
	srv := server.NewServer(loader, supervisor, reader, cm, dashboardDir, useKubernetesProxy)

	// ===== ProxyResolver wiring =====
	// Build an independent controller-runtime client + capture rest.Config.Host
	// so the production ProxyResolver can answer ResolveHead() +
	// APIServerHost().
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
	// ideally wrap with a kube-aware RoundTripper so buildProxyTargetURL's
	// "/api/v1/namespaces/.../services/.../proxy" path authenticates against
	// kube-apiserver. That wiring is deferred — see v1 NewServerHandler for
	// the reference implementation.
	httpClient := &http.Client{Timeout: httpClientTimeout}
	srv.SetHTTPClient(httpClient)

	// ===== Signals + run =====
	sigCh := make(chan os.Signal, 1)
	stop := make(chan struct{})
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		logrus.Info("shutdown signal received")
		close(stop)
	}()

	logrus.Infof("historyserver (beta-v2, lazy) starting: runtime=%s cache-size=%d use-kube-proxy=%v",
		runtimeClassName, cacheSize, useKubernetesProxy)
	srv.Run(stop)
	logrus.Info("historyserver exited")
}

// buildK8sClient constructs a controller-runtime client.Client with the
// rayv1 scheme registered — used by Pipeline.isDead. Pattern lifted
// verbatim from beta/cmd/eventprocessor/main.go:167.
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

// buildProxyPrimitives builds a controller-runtime client.Client and
// returns the rest.Config.Host used for the ProxyResolver production
// adapter. Mirrors v1 ClientManager's config-building
// (clientmanager.go) but exposes the pieces (client + host) that the
// proxy resolver needs.
//
// Note: we intentionally duplicate this instead of calling ClientManager
// methods because ClientManager.clients / .configs are unexported and
// beta-v2's spec forbids modifying v1.
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

// productionProxyResolver implements server.ProxyResolver by querying K8s
// for the RayCluster CR and returning its head-service info. Body is a
// port of v1 getClusterSvcInfo (router.go:1806) adapted from
// []client.Client to a single client, and returning the v2 server.ServiceInfo
// shape instead of v1 historyserver.ServiceInfo.
type productionProxyResolver struct {
	k8sClient     client.Client
	apiServerHost string
}

// ResolveHead looks up the RayCluster by (namespace, name), derives the
// head service name from Status.Head.ServiceName (set by the ray-operator),
// and returns a ServiceInfo with the Ray Dashboard port (8265).
//
// Error taxonomy matches v1 verbatim so UX of the live-proxy path stays
// identical between v1, beta, and beta-v2.
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
