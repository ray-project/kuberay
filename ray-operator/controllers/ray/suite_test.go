/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ray

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/dashboardclient"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	mgr       ctrl.Manager

	fakeRayDashboardClient *utils.FakeRayDashboardClient
	fakeRayHttpProxyClient *utils.FakeRayHttpProxyClient
)

type TestClientProvider struct{}

func (testProvider TestClientProvider) GetDashboardClient(_ context.Context, _ manager.Manager) func(rayCluster *rayv1.RayCluster, url string) (dashboardclient.RayDashboardClientInterface, error) {
	return func(_ *rayv1.RayCluster, _ string) (dashboardclient.RayDashboardClientInterface, error) {
		return fakeRayDashboardClient, nil
	}
}

func (testProvider TestClientProvider) GetHttpProxyClient(_ manager.Manager) func(hostIp, podNamespace, podName string, port int) utils.RayHttpProxyClientInterface {
	return func(_, _, _ string, _ int) utils.RayHttpProxyClientInterface {
		return fakeRayHttpProxyClient
	}
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = rayv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())

	os.Setenv(utils.RAYCLUSTER_DEFAULT_REQUEUE_SECONDS_ENV, "10")

	// 建立 cache selectors
	rayNodeLabel, err := labels.NewRequirement(utils.RayNodeLabelKey, selection.Equals, []string{"yes"})
	Expect(err).NotTo(HaveOccurred())
	podSelector := labels.NewSelector().Add(*rayNodeLabel)

	createdByLabel, err := labels.NewRequirement(utils.KubernetesCreatedByLabelKey, selection.Equals, []string{utils.ComponentName})
	Expect(err).NotTo(HaveOccurred())
	jobSelector := labels.NewSelector().Add(*createdByLabel)

	mgr, err = ctrl.NewManager(cfg, ctrl.Options{ // ← 注意這裡是 = 不是 :=
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&batchv1.Job{}: {Label: jobSelector},
				&corev1.Pod{}:  {Label: podSelector},
			},
		},
	})
	Expect(err).NotTo(HaveOccurred(), "failed to create manager")

	fakeRayDashboardClient = prepareFakeRayDashboardClient()
	fakeRayHttpProxyClient = &utils.FakeRayHttpProxyClient{}

	options := RayClusterReconcilerOptions{
		HeadSidecarContainers: []corev1.Container{
			{
				Name:  "fluentbit",
				Image: "fluent/fluent-bit:1.9.6",
			},
		},
	}
	err = NewReconciler(mgr, options).SetupWithManager(mgr, 1)
	Expect(err).NotTo(HaveOccurred(), "failed to setup RayCluster controller")

	testClientProvider := TestClientProvider{}
	err = NewRayServiceReconciler(ctx, mgr, testClientProvider).SetupWithManager(mgr, 1)
	Expect(err).NotTo(HaveOccurred(), "failed to setup RayService controller")

	rayJobOptions := RayJobReconcilerOptions{}
	err = NewRayJobReconciler(ctx, mgr, rayJobOptions, testClientProvider).SetupWithManager(mgr, 1)
	Expect(err).NotTo(HaveOccurred(), "failed to setup RayJob controller")

	go func() {
		err = mgr.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")

	// NOTE(simon): the error is ignored because it gets raised in macOS due
	// to a harmless timeout error.
	_ = testEnv.Stop()
})
