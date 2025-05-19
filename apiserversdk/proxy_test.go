package apiserversdk

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	rayclient "github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/typed/ray/v1"
)

var (
	ln                    net.Listener
	cfg                   *rest.Config
	rayClient             *rayclient.RayV1Client
	k8sClient             *k8sclient.CoreV1Client
	k8sClientWithoutProxy *k8sclient.CoreV1Client
	testEnv               *envtest.Environment
	lastReq               atomic.Pointer[http.Request]
)

func TestProxy(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Proxy Suite")
}

var _ = BeforeSuite(func(_ SpecContext) {
	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "ray-operator", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	mux, err := NewMux(MuxConfig{
		KubernetesConfig: cfg,
		Middleware: func(handler http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				lastReq.Store(r.Clone(r.Context()))
				handler.ServeHTTP(w, r)
			})
		},
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(mux).ToNot(BeNil())

	ln, err = net.Listen("tcp", "127.0.0.1:0")
	Expect(err).ToNot(HaveOccurred())
	Expect(ln).ToNot(BeNil())
	go func() {
		svc := &http.Server{Handler: mux, ReadHeaderTimeout: time.Minute}
		err := svc.Serve(ln)
		Expect(err).To(MatchError(net.ErrClosed))
	}()

	proxyCfg := &rest.Config{Host: "http://" + ln.Addr().String()}
	rayClient = rayclient.NewForConfigOrDie(proxyCfg)
	k8sClient = k8sclient.NewForConfigOrDie(proxyCfg)
	k8sClientWithoutProxy = k8sclient.NewForConfigOrDie(cfg)
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	_ = testEnv.Stop()
	_ = ln.Close()
})

var _ = Describe("RayCluster", Ordered, func() {
	It("Create RayCluster", func() {
		_, err := rayClient.RayClusters("default").Create(context.Background(), &rayv1.RayCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "proxy-test"},
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
		}, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(lastReq.Load().Method).To(Equal(http.MethodPost))
		Expect(lastReq.Load().RequestURI).To(Equal("/apis/ray.io/v1/namespaces/default/rayclusters"))
	})
	It("Get RayCluster", func() {
		cluster, err := rayClient.RayClusters("default").Get(context.Background(), "proxy-test", metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(cluster.Name).To(Equal("proxy-test"))
		Expect(lastReq.Load().Method).To(Equal(http.MethodGet))
		Expect(lastReq.Load().RequestURI).To(Equal("/apis/ray.io/v1/namespaces/default/rayclusters/proxy-test"))
	})
	It("List RayCluster", func() {
		clusters, err := rayClient.RayClusters("default").List(context.Background(), metav1.ListOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(clusters.Items).To(HaveLen(1))
		Expect(clusters.Items[0].Name).To(Equal("proxy-test"))
		Expect(lastReq.Load().Method).To(Equal(http.MethodGet))
		Expect(lastReq.Load().RequestURI).To(Equal("/apis/ray.io/v1/namespaces/default/rayclusters"))
	})
	It("Delete RayCluster", func() {
		err := rayClient.RayClusters("default").Delete(context.Background(), "proxy-test", metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(lastReq.Load().Method).To(Equal(http.MethodDelete))
		Expect(lastReq.Load().RequestURI).To(Equal("/apis/ray.io/v1/namespaces/default/rayclusters/proxy-test"))
	})
})

// TODO: add tests for RayJobs
// TODO: add tests for RayServices

var _ = Describe("events", Ordered, func() {
	It("List events", func() {
		events, err := k8sClient.Events("default").List(context.Background(), metav1.ListOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(events.Items).To(BeEmpty())
		Expect(lastReq.Load().Method).To(Equal(http.MethodGet))
		Expect(lastReq.Load().RequestURI).To(Equal("/api/v1/namespaces/default/events"))
	})
	It("Only GET method is allowed for events endpoint", func() {
		event := &corev1.Event{}
		_, err := k8sClient.Events("default").Create(context.Background(), event, metav1.CreateOptions{})
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("the server does not allow this method on the requested resource")))
	})
	It("Only querying KubeRay CR events", func() {
		testEvent := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-event-",
				Namespace:    "default",
			},
			InvolvedObject: corev1.ObjectReference{
				Kind:       "RayCluster",
				Namespace:  "default",
				Name:       "test-event",
				APIVersion: "ray.io/v1",
			},
			Type:    "Normal",
			Reason:  "Testing",
			Message: "This is a test event",
			Source: corev1.EventSource{
				Component: "test-component",
			},
		}
		testEvent2 := testEvent.DeepCopy()
		testEvent2.InvolvedObject.APIVersion = ""
		_, err := k8sClientWithoutProxy.Events("default").Create(context.Background(), testEvent, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		_, err = k8sClientWithoutProxy.Events("default").Create(context.Background(), testEvent2, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		events, err := k8sClient.Events("default").List(context.Background(), metav1.ListOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(events.Items).To(HaveLen(1))
		Expect(events.Items[0].ObjectMeta.GenerateName).To(Equal(testEvent.ObjectMeta.GenerateName))
		Expect(events.Items[0].InvolvedObject.APIVersion).To(Equal("ray.io/v1"))
		// Test the user selector won't override "involvedObject.apiVersion=ray.io/v1"
		fieldSelectorString := "involvedObject.apiVersion="
		events, err = k8sClient.Events("default").List(context.Background(), metav1.ListOptions{FieldSelector: fieldSelectorString})
		Expect(err).ToNot(HaveOccurred())
		Expect(events.Items).To(BeEmpty())
	})
})

var _ = Describe("not match", Ordered, func() {
	It("List Pods", func() {
		_, err := k8sClient.Pods("default").List(context.Background(), metav1.ListOptions{})
		Expect(err).To(MatchError(ContainSubstring("the server could not find the requested resource")))
	})
})

var _ = Describe("kuberay service", Ordered, func() {
	svcName := "head-svc"

	AfterEach(func() {
		_ = k8sClientWithoutProxy.Services("default").Delete(context.Background(), svcName, metav1.DeleteOptions{})
	})

	Context("when a service has the KubeRay label", func() {
		BeforeEach(func() {
			_, err := k8sClientWithoutProxy.Services("default").Create(context.Background(), &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: svcName,
					Labels: map[string]string{
						utils.KubernetesApplicationNameLabelKey: utils.ApplicationName,
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Name: "http", Port: 80, TargetPort: intstr.FromInt(80)},
					},
				},
			}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
		})

		// Since envtest doesn't create a full K8s cluster but only the control plane, we cannot actually hit the pod.
		// So we just check the request and skip checking the error which is always a 404.
		It("should allow proxying to KubeRay-labeled service", func() {
			r := k8sClient.Services("default").ProxyGet("http", svcName, "80", "foo/bar", nil)
			_, _ = r.DoRaw(context.Background()) // Expect 404 due to envtest limitation
			Expect(lastReq.Load().Method).To(Equal(http.MethodGet))
			Expect(lastReq.Load().RequestURI).To(Equal("/api/v1/namespaces/default/services/http:head-svc:80/proxy/foo/bar"))
		})
		It("should allow proxying to KubeRay-labeled service without trailing slash", func() {
			r := k8sClient.Services("default").ProxyGet("http", svcName, "80", "", nil)
			_, _ = r.DoRaw(context.Background()) // Expect 404 due to envtest limitation
			Expect(lastReq.Load().Method).To(Equal(http.MethodGet))
			Expect(lastReq.Load().RequestURI).To(Equal("/api/v1/namespaces/default/services/http:head-svc:80/proxy"))

			// We register both "/proxy" and "/proxy/" to prevent implicit redirects.
			// This test make sure trailing slash issue is handled correctly.
			// Without explicitly handling "/proxy", a request to it will be redirected to "/proxy/".
			// Also, a POST request to "/proxy" will be changed from POST to GET, and drops the body.
			restClient := k8sClient.RESTClient()
			_, _ = restClient.Post().
				Namespace("default").
				Resource("services").
				Name("http:head-svc:80").
				SubResource("proxy").
				DoRaw(context.Background())

			Expect(lastReq.Load().Method).To(Equal(http.MethodPost))
			Expect(lastReq.Load().RequestURI).To(Equal("/api/v1/namespaces/default/services/http:head-svc:80/proxy"))
		})
	})

	Context("when a service lacks the KubeRay label", func() {
		BeforeEach(func() {
			_, err := k8sClientWithoutProxy.Services("default").Create(context.Background(), &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: svcName,
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Name: "http", Port: 80, TargetPort: intstr.FromInt(80)},
					},
				},
			}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
		})

		It("should reject proxying to service without KubeRay label", func() {
			r := k8sClient.Services("default").ProxyGet("http", svcName, "80", "foo/bar", nil)
			_, err := r.DoRaw(context.Background())
			Expect(err).To(HaveOccurred())
			var statusErr *apierrors.StatusError
			ok := errors.As(err, &statusErr)
			Expect(ok).To(BeTrue())
			Expect(statusErr.Status().Details.Causes[0].Message).To(Equal("kuberay service not found"))
		})
	})
})
