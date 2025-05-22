package apiserversdk

import (
	"context"
	"os"
	"runtime"
	"strings"
	"testing"

	petnames "github.com/dustinkirkland/golang-petname"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayv1 "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/typed/ray/v1"
)

// GenericEnd2EndTest struct allows for reuse in setting up and running tests
type GenericEnd2EndTest[I proto.Message] struct {
	Input         I
	ExpectedError error
	Name          string
}

// End2EndTestingContext provides a common set of values and methods that
// can be used in executing the tests
type End2EndTestingContext struct {
	ctx              context.Context
	rayHttpClient    rayv1.RayV1Interface
	k8sHttpClient    *kubernetes.Clientset
	k8client         *kubernetes.Clientset
	apiServerBaseURL string
	rayImage         string
	namespaceName    string
	clusterName      string
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
		withBaseURL(),
		withRayHttpClient(),
		withK8sHttpClient(),
		withK8sClient(),
		withContext(),
		withNamespace(),
	)
}

func newEnd2EndTestingContext(t *testing.T, options ...contextOption) (*End2EndTestingContext, error) {
	testingContext := &End2EndTestingContext{
		namespaceName: petnames.Generate(2, "-"),
		clusterName:   petnames.Name(),
	}
	for _, o := range options {
		err := o(t, testingContext)
		if err != nil {
			return nil, err
		}
	}
	return testingContext, nil
}

func withRayHttpClient() contextOption {
	return func(_ *testing.T, testingContext *End2EndTestingContext) error {
		var err error
		testingContext.rayHttpClient, err = rayv1.NewForConfig(&rest.Config{Host: testingContext.apiServerBaseURL})
		if err != nil {
			return err
		}
		return nil
	}
}

func withK8sHttpClient() contextOption {
	return func(_ *testing.T, testingContext *End2EndTestingContext) error {
		var err error
		testingContext.k8sHttpClient, err = kubernetes.NewForConfig(&rest.Config{Host: testingContext.apiServerBaseURL})
		if err != nil {
			return err
		}
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
			require.NoErrorf(t, err, "No error expected when deleting namespace '%s'", tCtx.namespaceName)
		})
		return nil
	}
}

func (e2etc *End2EndTestingContext) GetCtx() context.Context {
	return e2etc.ctx
}

func (e2etc *End2EndTestingContext) GetK8sHttpClient() *kubernetes.Clientset {
	return e2etc.k8sHttpClient
}

func (e2etc *End2EndTestingContext) GetRayHttpClient() rayv1.RayV1Interface {
	return e2etc.rayHttpClient
}

func (e2etc *End2EndTestingContext) GetRayClusterByName(clusterName string) (*rayv1api.RayCluster, error) {
	return e2etc.rayHttpClient.RayClusters(e2etc.namespaceName).Get(e2etc.ctx, clusterName, metav1.GetOptions{})
}

func (e2etc *End2EndTestingContext) GetRayClusterName() string {
	return e2etc.clusterName
}

func (e2etc *End2EndTestingContext) GetNamespaceName() string {
	return e2etc.namespaceName
}

func (e2etc *End2EndTestingContext) GetRayImage() string {
	return e2etc.rayImage
}
