// Package server implements the HTTP layer of the History Server: lifecycle,
// routing, and request handlers for both dead-session snapshots and
// live-session reverse proxies.
//
// Dead-session snapshots are served from an LRU cache backed by object
// storage, with on-demand snapshot building coalesced through a Supervisor.
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
	defaultHTTPPort         = 8080
	gracefulShutdownTimeout = 10 * time.Second

	// httpWriteTimeout must be ≥ the proxy httpClient timeout so proxied
	// responses have time to drain.
	httpReadTimeout  = 5 * time.Second
	httpWriteTimeout = 35 * time.Second
	httpIdleTimeout  = 60 * time.Second

	// rayDashboardPort is fixed by Ray.
	rayDashboardPort = 8265
)

// ServiceInfo describes a RayCluster head service for proxy routing.
type ServiceInfo struct {
	ServiceName string
	Namespace   string
	Port        int
}

// ProxyResolver answers "which head-service handles this RayCluster?" for
// redirectRequest's live-proxy path.
type ProxyResolver interface {
	// ResolveHead returns head-service info for (namespace, name), or an
	// error if the cluster is missing or its head service is not ready.
	ResolveHead(ctx context.Context, namespace, name string) (ServiceInfo, error)

	// APIServerHost returns the base URL for useKubeProxy mode; an empty
	// string means "use in-cluster DNS".
	APIServerHost() string
}

// SetProxyResolver wires the ProxyResolver.
func (s *Server) SetProxyResolver(r ProxyResolver) {
	s.proxyResolver = r
}

// Run starts the HTTP server and blocks until stop is closed. On stop, it
// performs a graceful shutdown bounded by gracefulShutdownTimeout.
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
// on the head pod.
//
// Resolution flow:
//  1. Validate cookies (cluster_name, cluster_namespace).
//  2. Resolve the RayCluster head service via proxyResolver (always resolve;
//     never trust the cookie ServiceName — SSRF defense).
//  3. Build targetURL: through kube-apiserver proxy when useKubeProxy=true,
//     otherwise via in-cluster service DNS.
//  4. Copy method/body/headers, execute the round-trip, copy response back.
//
// Returns 501 when proxy plumbing is unavailable.
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

// buildProxyTargetURL constructs the URL redirectRequest proxies to.
//
//   - useKubeProxy && apiServerHost != "":
//     "{apiServerHost}/api/v1/namespaces/{ns}/services/{svc}:dashboard/proxy{origURL}"
//   - else:
//     "http://{svc}:{port}{origURL}"  (in-cluster DNS)
func buildProxyTargetURL(origURL string, svc ServiceInfo, useKubeProxy bool, apiServerHost string) string {
	if useKubeProxy && apiServerHost != "" {
		return fmt.Sprintf("%s/api/v1/namespaces/%s/services/%s:dashboard/proxy%s",
			apiServerHost, svc.Namespace, svc.ServiceName, origURL)
	}
	return fmt.Sprintf("http://%s:%d%s", svc.ServiceName, svc.Port, origURL)
}

// --- getClusters: GET /clusters ----------------------------------------------

// getClusters returns the union of live + dead cluster sessions.
//
// Order: live first, then dead sorted by create-time descending. Dead
// sessions are truncated to maxClusters (default 100).
//
// Partial-failure: if live listing fails we log and continue with the dead
// list, so the frontend cluster picker degrades gracefully.
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

// getTimezone returns the timezone offset for the session. Read directly
// from the fetched_endpoints metadata, not the snapshot pipeline.
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
		// Frontend expects a valid JSON payload even when timezone is unavailable.
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

// writeTimezoneEmpty writes the fallback body when no timezone file exists.
// Frontend expects this exact shape; deviating would cause a TypeError.
func writeTimezoneEmpty(resp *restful.Response) {
	resp.Header().Set("Content-Type", "application/json")
	if _, err := resp.Write([]byte(`{"offset":"","value":""}`)); err != nil {
		logrus.Errorf("writeTimezoneEmpty: write failed: %v", err)
	}
}
