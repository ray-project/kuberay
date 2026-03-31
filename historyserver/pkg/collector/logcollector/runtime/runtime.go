package runtime

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/kube"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/runtime/logcollector"
	"github.com/ray-project/kuberay/historyserver/pkg/collector/types"
	"github.com/ray-project/kuberay/historyserver/pkg/storage"
	"github.com/ray-project/kuberay/historyserver/pkg/utils"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

type logCollectorOwnerResolver struct {
	kubeClient *kube.KubeClient
}

func NewLogCollectorOwnerResolver(c *kube.KubeClient) OwnerResolver {
	return &logCollectorOwnerResolver{kubeClient: c}
}

func (r *logCollectorOwnerResolver) Resolve(ctx context.Context, namespace, rayClusterName string) (string, string, error) {
	rc := &rayv1.RayCluster{}
	key := client.ObjectKey{Namespace: namespace, Name: rayClusterName}
	if err := r.kubeClient.Client.Get(ctx, key, rc); err != nil {
		return "", "", fmt.Errorf("get RayCluster %s: %w", key, err)
	}
	ref := metav1.GetControllerOf(rc)
	if ref == nil {
		return "", "", nil
	}
	return ref.Kind, ref.Name, nil
}

func NewCollector(config *types.RayCollectorConfig, writer storage.StorageWriter) RayLogCollector {
	handler := logcollector.RayLogHandler{
		IsHead:   config.Role == "Head",
		LogFiles: make(chan string),

		RootDir:    config.RootDir,
		SessionDir: config.SessionDir,

		RayClusterName:      config.RayClusterName,
		RayClusterNamespace: config.RayClusterNamespace,
		RayNodeName:         config.RayNodeName,

		LogBatching:          config.LogBatching,
		PushInterval:         config.PushInterval,
		DashboardAddress:     config.DashboardAddress,
		AdditionalEndpoints:  config.AdditionalEndpoints,
		EndpointPollInterval: config.EndpointPollInterval,

		HttpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,              // Max idle connections
				MaxIdleConnsPerHost: 20,               // Max idle connections per host
				IdleConnTimeout:     90 * time.Second, // Idle connection timeout
			},
		},
		Writer:       writer,
		ShutdownChan: make(chan struct{}),
	}

	if handler.IsHead {
		handler.OwnerKind = config.OwnerKind
		handler.OwnerName = config.OwnerName
		if handler.OwnerKind != "" {
			logrus.Infof("The associated owner resource is: %s/%s", handler.OwnerKind, handler.OwnerName)
		}
	}

	logDir := strings.TrimSpace(filepath.Join(config.SessionDir, utils.RAY_SESSIONDIR_LOGDIR_NAME))
	handler.LogDir = logDir
	clusterRootDir := fmt.Sprintf("%s/", path.Clean(path.Join(handler.RootDir, utils.AppendRayClusterNameNamespace(handler.RayClusterName, handler.RayClusterNamespace))))
	handler.ClusterDir = clusterRootDir

	return &handler
}
