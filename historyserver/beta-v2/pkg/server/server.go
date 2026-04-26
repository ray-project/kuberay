// Package server — HTTP lifecycle and top-level handlers for the v2 beta
// History Server.
//
// This file owns three concerns:
//  1. Run(): bring up the go-restful container, listen, and shutdown gracefully.
//  2. redirectRequest(): live-session reverse proxy to the Ray Dashboard
//     on the head pod.
//  3. getClusters / getTimezone: top-level handlers that don't fit the
//     snapshot-only pattern of handlers.go.
//
// Design notes:
//   - HTTP port 8080 (matches v1).
//   - Shutdown bounded by 10s graceful drain.
//   - Reverse proxy uses plain http.Client (not httputil.ReverseProxy) to
//     match v1 byte-for-byte; ReverseProxy would change Hop-by-hop / XFF
//     handling for no v1-parity gain.
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/emicklei/go-restful/v3"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

const (
	// defaultHTTPPort is the listener port for the v2 History Server. Matches
	// v1 (pkg/historyserver/server.go:74) so existing Dashboard clients and
	// k8s Service specs keep working without changes.
	defaultHTTPPort = 8080

	// gracefulShutdownTimeout bounds how long Run() waits for in-flight
	// requests to complete during shutdown. 10s is generous enough for a slow
	// proxied dashboard response while still keeping pod termination snappy.
	gracefulShutdownTimeout = 10 * time.Second

	// httpReadTimeout / httpWriteTimeout mirror v1 server.go. WriteTimeout
	// must be >= the proxy httpClient timeout (30s) so proxied responses have
	// time to drain before the listener cuts the client off.
	httpReadTimeout  = 5 * time.Second
	httpWriteTimeout = 35 * time.Second
	httpIdleTimeout  = 60 * time.Second

	// rayDashboardPort is the Ray Dashboard port on the head pod. Fixed by
	// Ray itself; see v1 getClusterSvcInfo in router.go:1820.
	rayDashboardPort = 8265
)

// ServiceInfo describes a RayCluster head service for proxy routing. It
// intentionally mirrors v1 historyserver.ServiceInfo but is declared in beta
// so server.go does not need to reach into v1 package private state.
type ServiceInfo struct {
	ServiceName string
	Namespace   string
	Port        int
}

// ProxyResolver answers "which head-service handles this RayCluster?" for
// redirectRequest's live-proxy path. Production wires it to a
// controller-runtime client lookup; tests leave it nil so redirectRequest
// returns a deterministic 501.
//
// Indirected because v1 ClientManager hides its clients behind private
// fields; concrete wiring lives in main via SetProxyResolver.
type ProxyResolver interface {
	// ResolveHead returns head-service info for (namespace, name), or an
	// error if the cluster is missing or its head service is not ready.
	ResolveHead(ctx context.Context, namespace, name string) (ServiceInfo, error)

	// APIServerHost returns the base URL for useKubeProxy=true mode.
	// Empty string = "no kube-apiserver proxy; use in-cluster DNS".
	APIServerHost() string
}

// SetProxyResolver wires the ProxyResolver. Separated from NewServer so tests
// can construct a Server via newServerWithLoader without needing a resolver.
func (s *Server) SetProxyResolver(r ProxyResolver) {
	s.proxyResolver = r
}

// Run starts the HTTP server and blocks until stop is closed. On stop, it
// performs a graceful shutdown bounded by gracefulShutdownTimeout.
//
// It uses a scoped *restful.Container (vs v1's DefaultContainer) so tests
// can mount a fresh router without global state bleed.
func (s *Server) Run(stop <-chan struct{}) {
	container := restful.NewContainer()
	s.RegisterRouter(container)

	addr := fmt.Sprintf(":%d", defaultHTTPPort)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      container,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: httpWriteTimeout,
		IdleTimeout:  httpIdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		logrus.Infof("Starting HTTP server on %s", addr)
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-stop:
		logrus.Info("HTTP server stop signal received, shutting down gracefully")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			logrus.Errorf("graceful shutdown failed: %v", err)
		}
	case err := <-errCh:
		logrus.Errorf("HTTP server error: %v", err)
	}
}

// redirectRequest reverse-proxies a live-session request to the Ray Dashboard
// on the head pod. Mirrors v1 redirectRequest in pkg/historyserver/router.go
// behavior-for-behavior.
//
// Resolution flow:
//  1. Validate cookies (cluster_name, cluster_namespace). Session must be
//     "live" — callers enforce this upstream.
//  2. Look up the RayCluster head service via proxyResolver (always resolve,
//     never trust cookie-provided ServiceName — SSRF defense).
//  3. Build targetURL: through kube-apiserver proxy when useKubeProxy=true,
//     otherwise via in-cluster service DNS.
//  4. Copy method/body/headers, execute the round-trip, copy status/headers/
//     body back to the client.
//
// Returns 501 when proxy plumbing is unavailable (proxyResolver or httpClient
// nil) so unit tests built via newServerWithLoader observe a predictable
// status. W6's tests depend on this.
func (s *Server) redirectRequest(req *restful.Request, resp *restful.Response) {
	if s.proxyResolver == nil || s.httpClient == nil {
		resp.WriteErrorString(http.StatusNotImplemented,
			"live proxy not available: proxyResolver or httpClient is nil")
		return
	}

	clusterName, err := req.Request.Cookie(cookieClusterNameKey)
	if err != nil {
		writeMissingCookies(resp)
		return
	}
	clusterNamespace, err := req.Request.Cookie(cookieClusterNamespaceKey)
	if err != nil {
		writeMissingCookies(resp)
		return
	}

	svcInfo, err := s.proxyResolver.ResolveHead(req.Request.Context(),
		clusterNamespace.Value, clusterName.Value)
	if err != nil {
		logrus.Errorf("redirectRequest: ResolveHead %s/%s failed: %v",
			clusterNamespace.Value, clusterName.Value, err)
		resp.WriteErrorString(http.StatusBadRequest, err.Error())
		return
	}

	targetURL := buildProxyTargetURL(req.Request.URL.String(), svcInfo,
		s.useKubeProxy, s.proxyResolver.APIServerHost())

	proxyReq, err := http.NewRequest(req.Request.Method, targetURL, req.Request.Body)
	if err != nil {
		logrus.Errorf("redirectRequest: NewRequest failed: %v", err)
		resp.WriteError(http.StatusInternalServerError, err)
		return
	}

	// Copy headers — skip Host (the destination will set its own).
	for key, values := range req.Request.Header {
		if strings.EqualFold(key, "host") {
			continue
		}
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	remoteResp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		logrus.Errorf("redirectRequest: upstream Do failed for %s: %v", targetURL, err)
		resp.WriteError(http.StatusBadGateway, err)
		return
	}
	defer remoteResp.Body.Close()

	// Copy response headers verbatim, then status, then body — in that order,
	// because net/http locks the header map as soon as WriteHeader is called.
	for key, values := range remoteResp.Header {
		for _, value := range values {
			resp.Header().Add(key, value)
		}
	}
	resp.WriteHeader(remoteResp.StatusCode)
	if _, err := io.Copy(resp, remoteResp.Body); err != nil {
		logrus.Errorf("redirectRequest: body copy failed: %v", err)
	}
}

// buildProxyTargetURL constructs the URL redirectRequest proxies to. Pulled
// out as a pure function so it is easy to unit-test without spinning up an
// HTTP client. Two modes mirror v1:
//
//   - useKubeProxy && apiServerHost != ""  →
//     "{apiServerHost}/api/v1/namespaces/{ns}/services/{svc}:dashboard/proxy{origURL}"
//   - else  →
//     "http://{svc}:{port}{origURL}"  (in-cluster DNS)
func buildProxyTargetURL(origURL string, svc ServiceInfo, useKubeProxy bool, apiServerHost string) string {
	if useKubeProxy && apiServerHost != "" {
		return fmt.Sprintf("%s/api/v1/namespaces/%s/services/%s:dashboard/proxy%s",
			apiServerHost, svc.Namespace, svc.ServiceName, origURL)
	}
	return fmt.Sprintf("http://%s:%d%s", svc.ServiceName, svc.Port, origURL)
}

// --- getClusters: GET /clusters ----------------------------------------------

// getClusters returns the union of live + dead cluster sessions. Mirrors v1
// listClusters in pkg/historyserver/reader.go:45.
//
// Order: live first (most recently-relevant to the user), then dead sessions
// sorted by create-time descending. Dead sessions are truncated to maxClusters
// (v1 uses 100; we use the same default).
//
// Partial-failure posture: if live listing fails we log + continue with the
// dead list. Returning *something* beats returning an error — the frontend's
// cluster picker degrades gracefully.
func (s *Server) getClusters(req *restful.Request, resp *restful.Response) {
	const maxClusters = 100

	liveInfos := make([]utils.ClusterInfo, 0)
	if s.clientManager != nil {
		liveClusters, err := s.clientManager.ListRayClusters(req.Request.Context())
		if err != nil {
			logrus.Errorf("getClusters: ListRayClusters failed: %v", err)
		}
		for _, lc := range liveClusters {
			liveInfos = append(liveInfos, utils.ClusterInfo{
				Name:            lc.Name,
				Namespace:       lc.Namespace,
				CreateTime:      lc.CreationTimestamp.String(),
				CreateTimeStamp: lc.CreationTimestamp.Unix(),
				SessionName:     liveSessionSentinel,
			})
		}
	}

	var dead []utils.ClusterInfo
	if s.reader != nil {
		dead = s.reader.List()
		sort.Sort(utils.ClusterInfoList(dead))
		if maxClusters > 0 && len(dead) > maxClusters {
			dead = dead[:maxClusters]
		}
	}

	all := append(liveInfos, dead...)
	if err := resp.WriteAsJson(all); err != nil {
		logrus.Errorf("getClusters: WriteAsJson failed: %v", err)
	}
}

// --- getTimezone: GET /timezone ----------------------------------------------

// getTimezone returns the timezone offset for the session. Mirrors v1
// pkg/historyserver/timezone.go.
//
// Unlike the snapshot-backed endpoints, v1 stores timezone.json as a polled
// endpoint file under {clusterNameID}/{session}/fetched_endpoints/. We read
// it directly rather than through the snapshot loader — the snapshot pipeline
// doesn't own this metadata and there's no benefit to duplicating it.
func (s *Server) getTimezone(req *restful.Request, resp *restful.Response) {
	clusterNameID, sessionName, ok := extractCookies(req)
	if !ok {
		writeMissingCookies(resp)
		return
	}
	if sessionName == liveSessionSentinel {
		s.redirectRequest(req, resp)
		return
	}

	if s.reader == nil {
		// v1 returns a valid-but-empty payload when timezone is unavailable;
		// keep the same behavior so frontend JSON parsing never fails.
		writeTimezoneEmpty(resp)
		return
	}

	storageKey := utils.EndpointPathToStorageKey("/timezone")
	endpointPath := path.Join(sessionName, utils.RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME, storageKey)
	reader := s.reader.GetContent(clusterNameID, endpointPath)
	if reader == nil {
		writeTimezoneEmpty(resp)
		return
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		logrus.Errorf("getTimezone: read failed: %v", err)
		resp.WriteErrorString(http.StatusInternalServerError, "Failed to read timezone metadata")
		return
	}

	resp.Header().Set("Content-Type", "application/json")
	if _, err := resp.Write(data); err != nil {
		logrus.Errorf("getTimezone: write failed: %v", err)
	}
}

// writeTimezoneEmpty writes the fallback body v1 uses when no timezone file
// exists. Frontend expects this shape; deviating would cause a TypeError.
func writeTimezoneEmpty(resp *restful.Response) {
	resp.Header().Set("Content-Type", "application/json")
	if _, err := resp.Write([]byte(`{"offset":"","value":""}`)); err != nil {
		logrus.Errorf("writeTimezoneEmpty: write failed: %v", err)
	}
}
