package apiserversdk

import (
	"context"
	"net"
	"net/http"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	rayclient "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/typed/ray/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var (
	ln        net.Listener
	cfg       *rest.Config
	rayClient *rayclient.RayV1Client
	k8sClient *k8sclient.CoreV1Client
	testEnv   *envtest.Environment
)

func TestProxy(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Proxy Suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {
	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "ray-operator", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	mux, err := NewMux(MuxConfig{KubernetesConfig: cfg})
	Expect(err).ToNot(HaveOccurred())
	Expect(mux).ToNot(BeNil())

	ln, err = net.Listen("tcp", "127.0.0.1:0")
	Expect(err).ToNot(HaveOccurred())
	Expect(ln).ToNot(BeNil())
	go http.Serve(ln, mux)

	proxyCfg := &rest.Config{Host: "http://" + ln.Addr().String()}
	rayClient = rayclient.NewForConfigOrDie(proxyCfg)
	k8sClient = k8sclient.NewForConfigOrDie(proxyCfg)
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	_ = testEnv.Stop()
	_ = ln.Close()
})

var _ = Describe("RayCluster", Ordered, func() {
	It("Create RayCluster", func() {
		_, err := rayClient.RayClusters("default").Create(context.Background(), &rayv1.RayCluster{
			ObjectMeta: v1.ObjectMeta{Name: "proxy-test"},
			Spec: rayv1.RayClusterSpec{
				HeadGroupSpec: rayv1.HeadGroupSpec{
					RayStartParams: make(map[string]string),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test",
									Image: "test",
								},
							},
						},
					},
				},
			},
		}, v1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
	})
	It("Get RayCluster", func() {
		cluster, err := rayClient.RayClusters("default").Get(context.Background(), "proxy-test", v1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(cluster.Name).To(Equal("proxy-test"))
	})
	It("List RayCluster", func() {
		clusters, err := rayClient.RayClusters("default").List(context.Background(), v1.ListOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(clusters.Items).To(HaveLen(1))
		Expect(clusters.Items[0].Name).To(Equal("proxy-test"))
	})
	It("Delete RayCluster", func() {
		err := rayClient.RayClusters("default").Delete(context.Background(), "proxy-test", v1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
	})
})

// TODO: add tests for RayJobs
// TODO: add tests for RayServices

var _ = Describe("events", Ordered, func() {
	It("List events", func() {
		events, err := k8sClient.Events("default").List(context.Background(), v1.ListOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(events.Items).To(HaveLen(0))
	})
})
