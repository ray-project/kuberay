package e2e

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	. "github.com/ray-project/kuberay/historyserver/test/support"
	. "github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestAzureHistoryServer(t *testing.T) {
	azureClient := EnsureAzureBlobClient(t)

	tests := []struct {
		name     string
		testFunc func(Test, *WithT, *corev1.Namespace, *azblob.Client)
	}{
		{
			name:     "Live cluster: historyserver endpoints should be accessible",
			testFunc: testAzureLiveClusters,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			test := With(t)
			g := NewWithT(t)
			namespace := test.NewTestNamespace()

			tt.testFunc(test, g, namespace, azureClient)
		})
	}
}

func testAzureLiveClusters(test Test, g *WithT, namespace *corev1.Namespace, azureClient *azblob.Client) {
	rayCluster := PrepareAzureHistoryServerTestEnv(test, g, namespace, azureClient)
	ApplyRayJobAndWaitForCompletion(test, g, namespace, rayCluster)
	ApplyAzureHistoryServer(test, g, namespace)
	historyServerURL := GetHistoryServerURL(test, g, namespace)

	clusterInfo := getClusterFromList(test, g, historyServerURL, rayCluster.Name, namespace.Name)
	g.Expect(clusterInfo.SessionName).To(Equal(LiveSessionName), "Live cluster should have sessionName='live'")

	client := CreateHTTPClientWithCookieJar(g)
	setClusterContext(test, g, client, historyServerURL, namespace.Name, rayCluster.Name, clusterInfo.SessionName)
	verifyHistoryServerEndpoints(test, g, client, historyServerURL)

	DeleteAzureBlobContainer(test, g, azureClient)
	LogWithTimestamp(test.T(), "Azure live clusters E2E test completed successfully")
}
