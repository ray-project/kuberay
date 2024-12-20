package e2e

import (
	"context"
	"net/http"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	petnames "github.com/dustinkirkland/golang-petname"
	kuberayHTTP "github.com/ray-project/kuberay/apiserver/pkg/http"
	api "github.com/ray-project/kuberay/proto/go_client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1 "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/typed/ray/v1"
)

// GenericEnd2EndTest struct allows for reuse in setting up and running tests
type GenericEnd2EndTest[I proto.Message] struct {
	Name          string
	Input         I
	ExpectedError error
}

// End2EndTestingContext provides a common set of values and methods that
// can be used in executing the tests
type End2EndTestingContext struct {
	ctx                    context.Context
	apiServerHttpClient    *http.Client
	kuberayAPIServerClient *kuberayHTTP.KuberayAPIServerClient
	rayClient              rayv1.RayV1Interface
	k8client               *kubernetes.Clientset
	apiServerBaseURL       string
	rayImage               string
	rayVersion             string
	namespaceName          string
	computeTemplateName    string
	clusterName            string
	configMapName          string
	currentName            string
}

// contextOption is a functional option that allows for building out an instance
// of *End2EndTestingContext
type contextOption func(t *testing.T, tCtx *End2EndTestingContext) error

// NewEnd2EndTestingContext constructs a *End2EndTestingContext
func NewEnd2EndTestingContext(t *testing.T) (*End2EndTestingContext, error) {
	petnames.NonDeterministicMode()
	// ordering is important as there dependencies between field values
	return newEnd2EndTestingContext(t,
		withRayImage(),
		withRayVersion(),
		withBaseURL(),
		withHttpClient(),
		withContext(),
		withK8sClient(),
		withRayClient(),
		withNamespace(),
	)
}

func newEnd2EndTestingContext(t *testing.T, options ...contextOption) (*End2EndTestingContext, error) {
	testingContext := &End2EndTestingContext{
		namespaceName:       petnames.Generate(2, "-"),
		computeTemplateName: petnames.Name(),
		clusterName:         petnames.Name(),
		configMapName:       petnames.Generate(2, "-"),
		currentName:         petnames.Name(),
	}
	for _, o := range options {
		err := o(t, testingContext)
		if err != nil {
			return nil, err
		}
	}
	return testingContext, nil
}

func withHttpClient() contextOption {
	return func(_ *testing.T, testingContext *End2EndTestingContext) error {
		testingContext.apiServerHttpClient = &http.Client{Timeout: time.Duration(10) * time.Second}
		testingContext.kuberayAPIServerClient = kuberayHTTP.NewKuberayAPIServerClient(testingContext.apiServerBaseURL, testingContext.apiServerHttpClient)
		return nil
	}
}

func withContext() contextOption {
	return func(_ *testing.T, testingContext *End2EndTestingContext) error {
		testingContext.ctx = context.Background()
		return nil
	}
}

func withBaseURL() contextOption {
	return func(_ *testing.T, testingContext *End2EndTestingContext) error {
		baseURL := os.Getenv("E2E_API_SERVER_URL")
		if strings.TrimSpace(baseURL) == "" {
			baseURL = "http://localhost:31888"
		}
		testingContext.apiServerBaseURL = baseURL
		return nil
	}
}

func withRayImage() contextOption {
	return func(_ *testing.T, testingContext *End2EndTestingContext) error {
		rayImage := os.Getenv("E2E_API_SERVER_RAY_IMAGE")
		if strings.TrimSpace(rayImage) == "" {
			rayImage = RayImage + "-py310"
		}
		// detect if we are running on arm64 machine, most likely apple silicon
		// the os name is not checked as it also possible that it might be linux
		// also check if the image does not have the `-aarch64` suffix
		if runtime.GOARCH == "arm64" && !strings.HasSuffix(rayImage, "-aarch64") {
			rayImage = rayImage + "-aarch64"
		}
		testingContext.rayImage = rayImage
		return nil
	}
}

func withRayVersion() contextOption {
	return func(_ *testing.T, testingContext *End2EndTestingContext) error {
		rayVersion := os.Getenv("E2E_API_SERVER_RAY_VERSION")
		if strings.TrimSpace(rayVersion) == "" {
			rayVersion = RayVersion
		}
		testingContext.rayVersion = rayVersion
		return nil
	}
}

func withK8sClient() contextOption {
	return func(t *testing.T, testingContext *End2EndTestingContext) error {
		cfg, err := config.GetConfig()
		require.NoError(t, err, "No error expected when getting k8s client configuration")
		clientSet, err := kubernetes.NewForConfig(cfg)
		require.NoError(t, err, "No error expected when creating k8s client")
		testingContext.k8client = clientSet
		return nil
	}
}

func withNamespace() contextOption {
	return func(t *testing.T, tCtx *End2EndTestingContext) error {
		require.NotNil(t, tCtx.k8client, "A k8s client must be created prior to creating a namespace")
		require.NotNil(t, tCtx.ctx, "A context must exist prior to creating a namespace")
		require.NotEmpty(t, tCtx.namespaceName, "Namespace name must be set prior to creating a namespace")
		nsName := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: tCtx.namespaceName,
			},
		}

		_, err := tCtx.k8client.CoreV1().Namespaces().Create(tCtx.ctx, nsName, metav1.CreateOptions{})
		require.NoErrorf(t, err, "Expected to create a namespace '%s", nsName.ObjectMeta.Name)

		// register an automatic deletion of the namespace at test's end
		t.Cleanup(func() {
			err := tCtx.k8client.CoreV1().Namespaces().Delete(tCtx.ctx, tCtx.namespaceName, metav1.DeleteOptions{})
			assert.NoErrorf(t, err, "No error expected when deleting namespace '%s'", tCtx.namespaceName)
		})
		return nil
	}
}

func withRayClient() contextOption {
	return func(t *testing.T, tCtx *End2EndTestingContext) error {
		cfg, err := config.GetConfig()
		require.NoError(t, err, "No error expected when getting k8s client configuration")
		tCtx.rayClient, err = rayv1.NewForConfig(cfg)
		require.NoError(t, err, "No error expected when getting a ray")
		return nil
	}
}

func (e2etc *End2EndTestingContext) GetRayServiceByName(serviceName string) (*rayv1api.RayService, error) {
	return e2etc.rayClient.RayServices(e2etc.namespaceName).Get(e2etc.ctx, serviceName, metav1.GetOptions{})
}

func (e2etc *End2EndTestingContext) GetRayClusterByName(clusterName string) (*rayv1api.RayCluster, error) {
	return e2etc.rayClient.RayClusters(e2etc.namespaceName).Get(e2etc.ctx, clusterName, metav1.GetOptions{})
}

func (e2etc *End2EndTestingContext) GetBatchV1JobByName(jobName string) (*batchv1.Job, error) {
	return e2etc.k8client.BatchV1().Jobs(e2etc.namespaceName).Get(e2etc.ctx, jobName, metav1.GetOptions{})
}

func (e2etc *End2EndTestingContext) GetRayClusterName() string {
	return e2etc.clusterName
}

func (e2etc *End2EndTestingContext) GetRayJobByName(rayJobName string) (*rayv1api.RayJob, error) {
	return e2etc.rayClient.RayJobs(e2etc.namespaceName).Get(e2etc.ctx, rayJobName, metav1.GetOptions{})
}

func (e2etc *End2EndTestingContext) GetConfigMapName() string {
	return e2etc.configMapName
}

func (e2etc *End2EndTestingContext) GetNamespaceName() string {
	return e2etc.namespaceName
}

func (e2etc *End2EndTestingContext) GetComputeTemplateName() string {
	return e2etc.computeTemplateName
}

func (e2etc *End2EndTestingContext) GetRayImage() string {
	return e2etc.rayImage
}

func (e2etc *End2EndTestingContext) GetRayVersion() string {
	return e2etc.rayVersion
}

func (e2etc *End2EndTestingContext) GetRayApiServerClient() *kuberayHTTP.KuberayAPIServerClient {
	return e2etc.kuberayAPIServerClient
}

func (e2etc *End2EndTestingContext) GetNextName() string {
	e2etc.currentName = petnames.Name()
	return e2etc.currentName
}

func (e2etc *End2EndTestingContext) GetCurrentName() string {
	return e2etc.currentName
}

func (e2etc *End2EndTestingContext) CreateComputeTemplate(t *testing.T) {
	computeTemplateRequest := &api.CreateComputeTemplateRequest{
		ComputeTemplate: &api.ComputeTemplate{
			Name:      e2etc.computeTemplateName,
			Namespace: e2etc.namespaceName,
			Cpu:       2,
			Memory:    4,
		},
		Namespace: e2etc.namespaceName,
	}

	_, status, err := e2etc.kuberayAPIServerClient.CreateComputeTemplate(computeTemplateRequest)
	if !assert.NoErrorf(t, err, "No error expected while creating a compute template (%s, %s)", e2etc.namespaceName, e2etc.computeTemplateName) {
		t.Fatalf("Received status of %v when attempting to create compute template", status)
	}
}

func (e2etc *End2EndTestingContext) DeleteComputeTemplate(t *testing.T) {
	deleteComputeTemplateRequest := &api.DeleteClusterRequest{
		Name:      e2etc.computeTemplateName,
		Namespace: e2etc.namespaceName,
	}
	status, err := e2etc.kuberayAPIServerClient.DeleteComputeTemplate((*api.DeleteComputeTemplateRequest)(deleteComputeTemplateRequest))
	if !assert.NoErrorf(t, err, "No error expected while deleting a compute template (%s, %s)", e2etc.computeTemplateName, e2etc.namespaceName) {
		t.Fatalf("Received status of %v when attempting to create compute template", status)
	}
}

func (e2etc *End2EndTestingContext) CreateRayClusterWithConfigMaps(t *testing.T, configMapValues map[string]string) (*api.Cluster, string) {
	configMapName := e2etc.CreateConfigMap(t, configMapValues)
	t.Cleanup(func() {
		e2etc.DeleteConfigMap(t, configMapName)
	})
	items := make(map[string]string)
	for k := range configMapValues {
		items[k] = k
	}
	actualCluster, status, err := e2etc.kuberayAPIServerClient.CreateCluster(&api.CreateClusterRequest{
		Cluster: &api.Cluster{
			Name:        e2etc.clusterName,
			Namespace:   e2etc.namespaceName,
			User:        "3cpo",
			Environment: api.Cluster_DEV,
			ClusterSpec: &api.ClusterSpec{
				HeadGroupSpec: &api.HeadGroupSpec{
					ComputeTemplate: e2etc.computeTemplateName,
					Image:           e2etc.rayImage,
					ServiceType:     "NodePort",
					EnableIngress:   false,
					RayStartParams: map[string]string{
						"dashboard-host":      "0.0.0.0",
						"metrics-export-port": "8080",
					},
					Volumes: []*api.Volume{
						{
							MountPath:  "/home/ray/samples",
							VolumeType: api.Volume_CONFIGMAP,
							Name:       "code-sample",
							Source:     e2etc.configMapName,
							Items:      items,
						},
					},
				},
				WorkerGroupSpec: []*api.WorkerGroupSpec{
					{
						GroupName:       "small-wg",
						ComputeTemplate: e2etc.computeTemplateName,
						Image:           e2etc.rayImage,
						Replicas:        1,
						MinReplicas:     1,
						MaxReplicas:     5,
						RayStartParams: map[string]string{
							"dashboard-host":      "0.0.0.0",
							"metrics-export-port": "8080",
						},
						Volumes: []*api.Volume{
							{
								MountPath:  "/home/ray/samples",
								VolumeType: api.Volume_CONFIGMAP,
								Name:       "code-sample",
								Source:     e2etc.configMapName,
								Items:      items,
							},
						},
					},
				},
			},
		},
		Namespace: e2etc.namespaceName,
	})
	if !assert.NoErrorf(t, err, "No error expected while creating cluster (%s/%s)", e2etc.namespaceName, e2etc.clusterName) {
		t.Fatalf("Received status of %v when attempting to create a cluster", status)
	}
	// wait for the cluster to be in a running state for 3 minutes
	// if is not in that state, return an error
	err = wait.PollUntilContextTimeout(e2etc.ctx, 500*time.Millisecond, 3*time.Minute, false, func(_ context.Context) (done bool, err error) {
		rayCluster, err00 := e2etc.GetRayClusterByName(actualCluster.Name)
		if err00 != nil {
			return true, err00
		}
		t.Logf("Found cluster state of '%s' for ray cluster '%s'", rayCluster.Status.State, e2etc.GetRayClusterName())
		return rayCluster.Status.State == rayv1api.Ready, nil
	})
	require.NoErrorf(t, err, "No error expected when getting ray cluster: '%s', err %v", e2etc.GetRayClusterName(), err)
	return actualCluster, configMapName
}

func (e2etc *End2EndTestingContext) DeleteRayCluster(t *testing.T, clusterName string) {
	_, err := e2etc.kuberayAPIServerClient.DeleteCluster(&api.DeleteClusterRequest{
		Name:      clusterName,
		Namespace: e2etc.namespaceName,
	})
	require.NoErrorf(t, err, "No error expected when deleting ray cluster: '%s', err %v", clusterName, err)

	// wait for the cluster to be deleted for 3 minutes
	// if is not in that state, return an error
	err = wait.PollUntilContextTimeout(e2etc.ctx, 500*time.Millisecond, 3*time.Minute, false, func(_ context.Context) (done bool, err error) {
		rayCluster, err00 := e2etc.GetRayClusterByName(clusterName)
		if err00 != nil && k8sApiErrors.IsNotFound(err00) {
			return true, nil
		}
		t.Logf("Found cluster state of '%s' for ray cluster '%s'", rayCluster.Status.State, clusterName)
		return false, nil
	})
	require.NoErrorf(t, err, "No error expected when waiting for ray cluster: '%s' to be deleted, err %v", clusterName, err)
}

func (e2etc *End2EndTestingContext) DeleteRayService(t *testing.T, serviceName string) {
	_, err := e2etc.kuberayAPIServerClient.DeleteRayService(&api.DeleteRayServiceRequest{
		Name:      serviceName,
		Namespace: e2etc.namespaceName,
	})

	require.NoErrorf(t, err, "No error expected when deleting ray service: '%s', err %v", serviceName, err)

	// wait for the cluster to be deleted for 3 minutes
	// if is not in that state, return an error
	err = wait.PollUntilContextTimeout(e2etc.ctx, 500*time.Millisecond, 3*time.Minute, false, func(_ context.Context) (done bool, err error) {
		rayService, err00 := e2etc.GetRayServiceByName(serviceName)
		if err00 != nil && k8sApiErrors.IsNotFound(err00) {
			return true, nil
		}
		t.Logf("Found service state of '%s' for ray cluster '%s'", rayService.Status.ServiceStatus, serviceName)
		return false, nil
	})
	require.NoErrorf(t, err, "No error expected when waiting to delete ray service: '%s', err %v", serviceName, err)
}

func (e2etc *End2EndTestingContext) DeleteRayJobByName(t *testing.T, rayJobName string) {
	_, err := e2etc.kuberayAPIServerClient.DeleteRayJob(&api.DeleteRayJobRequest{
		Name:      rayJobName,
		Namespace: e2etc.namespaceName,
	})

	require.NoErrorf(t, err, "No error expected when deleting ray job: '%s', err %v", rayJobName, err)

	// wait for the cluster to be deleted for 3 minutes
	// if is not in that state, return an error
	err = wait.PollUntilContextTimeout(e2etc.ctx, 500*time.Millisecond, 3*time.Minute, false, func(_ context.Context) (done bool, err error) {
		rayJob, err00 := e2etc.GetRayJobByName(rayJobName)
		if err00 != nil && k8sApiErrors.IsNotFound(err00) {
			return true, nil
		}
		t.Logf("Found job state of '%s' for ray cluster '%s'", rayJob.Status.JobStatus, rayJobName)
		return false, nil
	})
	require.NoErrorf(t, err, "No error expected when waiting to delete ray job: '%s', err %v", rayJobName, err)
}

func (e2etc *End2EndTestingContext) CreateConfigMap(t *testing.T, values map[string]string) string {
	cm := &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: e2etc.configMapName, Namespace: e2etc.namespaceName},
		Immutable:  new(bool),
		Data:       values,
	}
	_, err := e2etc.k8client.CoreV1().ConfigMaps(e2etc.namespaceName).Create(e2etc.ctx, cm, metav1.CreateOptions{})
	require.NoErrorf(t, err, "No error expected when creating config map '%s' in namespace '%s'", e2etc.configMapName, e2etc.namespaceName)
	return e2etc.configMapName
}

func (e2etc *End2EndTestingContext) DeleteConfigMap(t *testing.T, configMapName string) {
	err := e2etc.k8client.CoreV1().ConfigMaps(e2etc.namespaceName).Delete(e2etc.ctx, configMapName, metav1.DeleteOptions{})
	if err != nil {
		assert.Truef(t, k8sApiErrors.IsNotFound(err), "Only IsNotFoundException allowed, received %v", err)
	}
}
