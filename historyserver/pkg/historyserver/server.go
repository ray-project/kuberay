package historyserver

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/eventserver"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
)

type ServerHandler struct {
	maxClusters  int
	rootDir      string
	dashboardDir string

	reader        storage.StorageReader
	clientManager *ClientManager
	eventHandler  *eventserver.EventHandler
	httpClient    *http.Client

	k8sRestConfig *rest.Config
	k8sHTTPClient *http.Client
	useK8sProxy   bool
}

func NewServerHandler(c *types.RayHistoryServerConfig, dashboardDir string, reader storage.StorageReader, clientManager *ClientManager, eventHandler *eventserver.EventHandler) *ServerHandler {
	handler := &ServerHandler{
		reader:        reader,
		clientManager: clientManager,
		eventHandler:  eventHandler,

		rootDir:      c.RootDir,
		dashboardDir: dashboardDir,
		// TODO: make this configurable
		maxClusters: 100,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	if len(clientManager.configs) > 0 {
		handler.k8sRestConfig = clientManager.configs[0]
		transportConfig, err := handler.k8sRestConfig.TransportConfig()
		if err == nil {
			rt, err := transport.New(transportConfig)
			if err == nil {
				handler.k8sHTTPClient = &http.Client{
					Timeout:   30 * time.Second,
					Transport: rt,
				}
				handler.useK8sProxy = true
				logrus.Infof("K8s proxy support enabled for accessing live clusters")
			} else {
				logrus.Warnf("Failed to create Kubernetes transport, will use direct connection: %v", err)
			}
		} else {
			logrus.Warnf("Failed to get transport config, will use direct connection: %v", err)
		}
	}
	return handler
}

func (s *ServerHandler) Run(stop chan struct{}) error {
	s.RegisterRouter()
	port := ":8080"
	server := &http.Server{
		Addr:         port,             // Listen address
		ReadTimeout:  5 * time.Second,  // Read timeout
		WriteTimeout: 35 * time.Second, // Write response timeout (must be >= httpClient.Timeout for proxy requests)
		IdleTimeout:  60 * time.Second, // Idle timeout
	}
	go func() {
		logrus.Infof("Starting server on %s", port)
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			logrus.Fatalf("Error starting server: %v", err)
		}
		logrus.Infof("Server stopped gracefully")
	}()

	<-stop
	logrus.Warnf("Receive stop single, so stop ray history server")
	// Create a context with 1 second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Shutdown the server
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Ray HistoryServer forced to shutdown: %v", err)
	}
	return nil
}
