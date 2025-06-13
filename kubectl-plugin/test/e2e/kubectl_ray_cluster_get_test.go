package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Calling ray plugin `get` command", func() {
	var namespace string
	var ctx context.Context
	var testClient Client

	BeforeEach(func() {
		var err error
		testClient, err = newTestClient()
		Expect(err).NotTo(HaveOccurred())

		namespace = createTestNamespace(testClient)
		ctx = context.Background()

		deployTestRayCluster(namespace)
		DeferCleanup(func() {
			deleteTestNamespace(namespace, testClient)
			namespace = ""
		})
	})

	It("succeed in listing ray cluster information", func() {
		rayClusters, err := testClient.Ray().RayV1().RayClusters(namespace).List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		Expect(rayClusters.Items).To(HaveLen(1))

		Expect(rayClusters.Items[0].Namespace).To(Equal(namespace))
		Expect(rayClusters.Items[0].Name).To(Equal("raycluster-kuberay"))
	})

	It("fail on getting non-existing ray cluster", func() {
		_, err := testClient.Ray().RayV1().RayClusters(namespace).Get(ctx, "fakeclustername", metav1.GetOptions{})
		Expect(err).To(HaveOccurred())
	})
})
