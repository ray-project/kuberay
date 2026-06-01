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
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/features"
)

var (
	testNetworkPolicyController  *NetworkPolicyController
	testRayClusterBasic          *rayv1.RayCluster
	testRayClusterDenyAllIngress *rayv1.RayCluster
	testRayClusterDenyAllEgress  *rayv1.RayCluster
	testRayClusterWithRayJob     *rayv1.RayCluster
)

func setupNetworkPolicyTest(t *testing.T) {
	t.Helper()
	features.SetFeatureGateDuringTest(t, features.RayClusterNetworkIsolation, true)
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	testScheme := runtime.NewScheme()
	_ = discoveryv1.AddToScheme(testScheme)
	testNetworkPolicyController = &NetworkPolicyController{
		Scheme:            testScheme,
		OperatorNamespace: "kuberay-system",
		Client: clientFake.NewClientBuilder().
			WithScheme(testScheme).
			Build(),
	}

	// Basic cluster — DenyAll mode (default).
	testRayClusterBasic = &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: rayv1.RayClusterSpec{
			NetworkIsolation: &rayv1.NetworkIsolationConfig{
				Mode: ptr.To(rayv1.NetworkIsolationDenyAll),
			},
			HeadGroupSpec: rayv1.HeadGroupSpec{
				RayStartParams: map[string]string{},
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "ray-head", Image: "rayproject/ray:latest"},
						},
					},
				},
			},
		},
	}

	// Cluster configured with DenyAllIngress mode.
	testRayClusterDenyAllIngress = &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-ingress",
			Namespace: "default",
		},
		Spec: rayv1.RayClusterSpec{
			NetworkIsolation: &rayv1.NetworkIsolationConfig{
				Mode: ptr.To(rayv1.NetworkIsolationDenyAllIngress),
			},
			HeadGroupSpec: rayv1.HeadGroupSpec{
				RayStartParams: map[string]string{},
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "ray-head", Image: "rayproject/ray:latest"},
						},
					},
				},
			},
		},
	}

	// Cluster configured with DenyAllEgress mode.
	testRayClusterDenyAllEgress = &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-egress",
			Namespace: "default",
		},
		Spec: rayv1.RayClusterSpec{
			NetworkIsolation: &rayv1.NetworkIsolationConfig{
				Mode: ptr.To(rayv1.NetworkIsolationDenyAllEgress),
			},
			HeadGroupSpec: rayv1.HeadGroupSpec{
				RayStartParams: map[string]string{},
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "ray-head", Image: "rayproject/ray:latest"},
						},
					},
				},
			},
		},
	}

	// Cluster owned by a RayJob.
	testRayClusterWithRayJob = &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-rayjob",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "ray.io/v1",
					Kind:       "RayJob",
					Name:       "test-job",
					UID:        "12345",
					Controller: ptr.To(true),
				},
			},
		},
		Spec: rayv1.RayClusterSpec{
			NetworkIsolation: &rayv1.NetworkIsolationConfig{
				Mode: ptr.To(rayv1.NetworkIsolationDenyAll),
			},
			HeadGroupSpec: rayv1.HeadGroupSpec{
				RayStartParams: map[string]string{},
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{Name: "ray-head", Image: "rayproject/ray:latest"},
						},
					},
				},
			},
		},
	}
}

// TestBuildHeadNetworkPolicy_DenyAll verifies the head NetworkPolicy in default DenyAll mode.
func TestBuildHeadNetworkPolicy_DenyAll(t *testing.T) {
	setupNetworkPolicyTest(t)

	policy := testNetworkPolicyController.buildHeadNetworkPolicy(context.Background(), testRayClusterBasic, rayv1.NetworkIsolationDenyAll)

	assert.Equal(t, "test-cluster-head", policy.Name)
	assert.Equal(t, "default", policy.Namespace)

	// Labels must identify the cluster and the operator.
	expectedLabels := map[string]string{
		utils.RayClusterLabelKey:                testRayClusterBasic.Name,
		utils.KubernetesApplicationNameLabelKey: utils.ApplicationName,
		utils.KubernetesCreatedByLabelKey:       utils.ComponentName,
	}
	assert.Equal(t, expectedLabels, policy.Labels)

	// PodSelector must target head pods of this cluster.
	assert.Equal(t, map[string]string{
		utils.RayClusterLabelKey:  "test-cluster",
		utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
	}, policy.Spec.PodSelector.MatchLabels)

	// DenyAll restricts both directions.
	assert.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	assert.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)

	// 2 base ingress rules: intra-cluster and operator access.
	assert.Len(t, policy.Spec.Ingress, 2)

	// 2 base egress rules: intra-cluster and DNS.
	assert.Len(t, policy.Spec.Egress, 2)
}

// TestBuildHeadNetworkPolicy_DenyAllIngress verifies that DenyAllIngress omits the Egress policy type.
func TestBuildHeadNetworkPolicy_DenyAllIngress(t *testing.T) {
	setupNetworkPolicyTest(t)

	policy := testNetworkPolicyController.buildHeadNetworkPolicy(context.Background(), testRayClusterDenyAllIngress, rayv1.NetworkIsolationDenyAllIngress)

	assert.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	assert.NotContains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
	assert.Len(t, policy.Spec.Ingress, 2)
	assert.Empty(t, policy.Spec.Egress)
}

// TestBuildHeadNetworkPolicy_DenyAllEgress verifies that DenyAllEgress only restricts egress.
func TestBuildHeadNetworkPolicy_DenyAllEgress(t *testing.T) {
	setupNetworkPolicyTest(t)

	policy := testNetworkPolicyController.buildHeadNetworkPolicy(context.Background(), testRayClusterDenyAllEgress, rayv1.NetworkIsolationDenyAllEgress)

	assert.NotContains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	assert.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
	assert.Empty(t, policy.Spec.Ingress)
	assert.Len(t, policy.Spec.Egress, 2)
}

// TestBuildWorkerNetworkPolicy_DenyAll verifies the worker NetworkPolicy in DenyAll mode.
func TestBuildWorkerNetworkPolicy_DenyAll(t *testing.T) {
	setupNetworkPolicyTest(t)

	policy := testNetworkPolicyController.buildWorkerNetworkPolicy(testRayClusterBasic, rayv1.NetworkIsolationDenyAll)

	assert.Equal(t, "test-cluster-workers", policy.Name)
	assert.Equal(t, "default", policy.Namespace)

	// Labels must identify the cluster and the operator.
	expectedLabels := map[string]string{
		utils.RayClusterLabelKey:                testRayClusterBasic.Name,
		utils.KubernetesApplicationNameLabelKey: utils.ApplicationName,
		utils.KubernetesCreatedByLabelKey:       utils.ComponentName,
	}
	assert.Equal(t, expectedLabels, policy.Labels)

	// PodSelector must target worker pods of this cluster.
	assert.Equal(t, map[string]string{
		utils.RayClusterLabelKey:  "test-cluster",
		utils.RayNodeTypeLabelKey: string(rayv1.WorkerNode),
	}, policy.Spec.PodSelector.MatchLabels)

	assert.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	assert.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)

	// Workers only allow intra-cluster ingress (no Ports = all ports).
	require.Len(t, policy.Spec.Ingress, 1)
	require.Len(t, policy.Spec.Ingress[0].From, 1)
	assert.Equal(t, map[string]string{utils.RayClusterLabelKey: "test-cluster"},
		policy.Spec.Ingress[0].From[0].PodSelector.MatchLabels)
	assert.Empty(t, policy.Spec.Ingress[0].Ports)

	// 2 base egress rules: intra-cluster and DNS.
	assert.Len(t, policy.Spec.Egress, 2)
}

// TestBuildWorkerNetworkPolicy_DenyAllIngress verifies no egress is added for DenyAllIngress mode.
func TestBuildWorkerNetworkPolicy_DenyAllIngress(t *testing.T) {
	setupNetworkPolicyTest(t)

	policy := testNetworkPolicyController.buildWorkerNetworkPolicy(testRayClusterDenyAllIngress, rayv1.NetworkIsolationDenyAllIngress)

	assert.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	assert.NotContains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
	assert.Len(t, policy.Spec.Ingress, 1)
	assert.Empty(t, policy.Spec.Egress)
}

// TestBuildWorkerNetworkPolicy_CustomIngressRules verifies that custom IngressRules are appended to the worker policy.
func TestBuildWorkerNetworkPolicy_CustomIngressRules(t *testing.T) {
	setupNetworkPolicyTest(t)

	customPort := intstr.FromInt32(9999)
	tcpProto := corev1.ProtocolTCP
	cluster := testRayClusterBasic.DeepCopy()
	cluster.Spec.NetworkIsolation.IngressRules = []networkingv1.NetworkPolicyIngressRule{
		{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &tcpProto, Port: &customPort},
			},
		},
	}

	policy := testNetworkPolicyController.buildWorkerNetworkPolicy(cluster, rayv1.NetworkIsolationDenyAll)

	// 1 base intra-cluster + 1 custom = 2.
	require.Len(t, policy.Spec.Ingress, 2)
	require.Len(t, policy.Spec.Ingress[1].Ports, 1)
	assert.Equal(t, &customPort, policy.Spec.Ingress[1].Ports[0].Port)
}

// TestBuildBaseIngressRules verifies the shared intra-cluster ingress rule used by both head and workers.
func TestBuildBaseIngressRules(t *testing.T) {
	setupNetworkPolicyTest(t)

	rules := testNetworkPolicyController.buildBaseIngressRules(testRayClusterBasic)
	require.Len(t, rules, 1)

	intraClusterRule := rules[0]
	require.Len(t, intraClusterRule.From, 1)
	assert.Equal(t, map[string]string{utils.RayClusterLabelKey: "test-cluster"},
		intraClusterRule.From[0].PodSelector.MatchLabels)
	assert.Empty(t, intraClusterRule.Ports, "Intra-cluster rule must allow all ports (no Ports field)")
}

// TestBuildHeadIngressRules verifies the 2 head-specific base ingress rules:
// intra-cluster and operator access.
func TestBuildHeadIngressRules(t *testing.T) {
	setupNetworkPolicyTest(t)

	rules := testNetworkPolicyController.buildHeadIngressRules(testRayClusterBasic)
	require.Len(t, rules, 2)

	// Rule 0: intra-cluster — no ports, pod selector matching the cluster label.
	intraClusterRule := rules[0]
	require.Len(t, intraClusterRule.From, 1)
	assert.Equal(t, map[string]string{utils.RayClusterLabelKey: "test-cluster"},
		intraClusterRule.From[0].PodSelector.MatchLabels)
	assert.Empty(t, intraClusterRule.Ports, "Intra-cluster rule must allow all ports (no Ports field)")

	// Rule 1: operator — two ports (dashboard + client), operator pod selector with
	// namespace selector restricted to the operator's namespace.
	operatorRule := rules[1]
	require.Len(t, operatorRule.From, 1)
	assert.Equal(t, map[string]string{
		utils.KubernetesComponentLabelKey: utils.ComponentName,
	}, operatorRule.From[0].PodSelector.MatchLabels)
	require.NotNil(t, operatorRule.From[0].NamespaceSelector)
	assert.Equal(t, map[string]string{
		corev1.LabelMetadataName: "kuberay-system",
	}, operatorRule.From[0].NamespaceSelector.MatchLabels, "Namespace selector must restrict to operator namespace")
	require.Len(t, operatorRule.Ports, 2)
	dashboardPort := intstr.FromInt32(utils.DefaultDashboardPort)
	clientPort := intstr.FromInt32(utils.DefaultClientPort)
	assert.Equal(t, &dashboardPort, operatorRule.Ports[0].Port)
	assert.Equal(t, &clientPort, operatorRule.Ports[1].Port)
}

// TestBuildRayJobPeer verifies that buildRayJobPeer returns a peer for RayJob-owned clusters
// and nil for standalone clusters.
func TestBuildRayJobPeer(t *testing.T) {
	setupNetworkPolicyTest(t)

	peer := testNetworkPolicyController.buildRayJobPeer(testRayClusterBasic)
	assert.Nil(t, peer, "Standalone cluster should not have a RayJob peer")

	peer = testNetworkPolicyController.buildRayJobPeer(testRayClusterWithRayJob)
	require.NotNil(t, peer, "RayJob-owned cluster should have a RayJob peer")
	assert.Equal(t, map[string]string{
		utils.RayOriginatedFromCRNameLabelKey: "test-job",
		utils.RayOriginatedFromCRDLabelKey:    utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD),
	}, peer.PodSelector.MatchLabels)
}

// TestBuildHeadIngressRules_WithRayJobOwner verifies that a RayJob-owned cluster gets
// a per-job submitter ingress rule (3 rules total).
func TestBuildHeadIngressRules_WithRayJobOwner(t *testing.T) {
	setupNetworkPolicyTest(t)

	rules := testNetworkPolicyController.buildHeadIngressRules(testRayClusterWithRayJob)
	require.Len(t, rules, 3)

	submitterRule := rules[2]
	require.Len(t, submitterRule.From, 1)
	assert.Equal(t, map[string]string{
		utils.RayOriginatedFromCRNameLabelKey: "test-job",
		utils.RayOriginatedFromCRDLabelKey:    utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD),
	}, submitterRule.From[0].PodSelector.MatchLabels)
	require.Len(t, submitterRule.Ports, 1)
	dashboardPort := intstr.FromInt32(utils.DefaultDashboardPort)
	assert.Equal(t, &dashboardPort, submitterRule.Ports[0].Port)
}

// TestBuildHeadIngressRules_AllowAllRayJobSubmitters verifies the broad submitter rule
// when ALLOW_ALL_RAYJOB_SUBMITTERS is enabled.
func TestBuildHeadIngressRules_AllowAllRayJobSubmitters(t *testing.T) {
	setupNetworkPolicyTest(t)
	t.Setenv(utils.ALLOW_ALL_RAYJOB_SUBMITTERS, "true")

	rules := testNetworkPolicyController.buildHeadIngressRules(testRayClusterBasic)
	require.Len(t, rules, 3)

	submitterRule := rules[2]
	require.Len(t, submitterRule.From, 1)
	assert.Equal(t, map[string]string{
		utils.RayOriginatedFromCRDLabelKey: utils.RayOriginatedFromCRDLabelValue(utils.RayJobCRD),
	}, submitterRule.From[0].PodSelector.MatchLabels)
	require.Len(t, submitterRule.Ports, 1)
	dashboardPort := intstr.FromInt32(utils.DefaultDashboardPort)
	assert.Equal(t, &dashboardPort, submitterRule.Ports[0].Port)
}

// TestBuildBaseEgressRules verifies the 2 base egress rules (intra-cluster + DNS).
func TestBuildBaseEgressRules(t *testing.T) {
	setupNetworkPolicyTest(t)

	rules := testNetworkPolicyController.buildBaseEgressRules(testRayClusterBasic)
	require.Len(t, rules, 2)

	// Rule 0: intra-cluster egress — no ports, pod selector matching the cluster label.
	intraClusterRule := rules[0]
	require.Len(t, intraClusterRule.To, 1)
	assert.Equal(t, map[string]string{utils.RayClusterLabelKey: "test-cluster"},
		intraClusterRule.To[0].PodSelector.MatchLabels)
	assert.Empty(t, intraClusterRule.Ports, "Intra-cluster egress must allow all ports (no Ports field)")

	// Rule 1: DNS egress — UDP+TCP port 53, no namespace restriction.
	dnsRule := rules[1]
	assert.Empty(t, dnsRule.To, "DNS rule must not restrict destination namespaces")

	require.Len(t, dnsRule.Ports, 2)
	dnsPort := intstr.FromInt(53)
	assert.Equal(t, &dnsPort, dnsRule.Ports[0].Port)
	assert.Equal(t, &dnsPort, dnsRule.Ports[1].Port)
	protocols := []corev1.Protocol{*dnsRule.Ports[0].Protocol, *dnsRule.Ports[1].Protocol}
	assert.ElementsMatch(t, []corev1.Protocol{corev1.ProtocolUDP, corev1.ProtocolTCP}, protocols)
}

// TestBuildHeadNetworkPolicy_WithRayJob verifies that a RayJob-owned cluster gets
// 3 ingress rules: 2 base + 1 per-job submitter.
func TestBuildHeadNetworkPolicy_WithRayJob(t *testing.T) {
	setupNetworkPolicyTest(t)

	policy := testNetworkPolicyController.buildHeadNetworkPolicy(context.Background(), testRayClusterWithRayJob, rayv1.NetworkIsolationDenyAll)

	require.Len(t, policy.Spec.Ingress, 3)
}

// TestBuildHeadNetworkPolicy_CustomIngressRules verifies that custom IngressRules are appended after base rules.
func TestBuildHeadNetworkPolicy_CustomIngressRules(t *testing.T) {
	setupNetworkPolicyTest(t)

	customPort := intstr.FromInt32(9999)
	tcpProto := corev1.ProtocolTCP
	cluster := testRayClusterBasic.DeepCopy()
	cluster.Spec.NetworkIsolation.IngressRules = []networkingv1.NetworkPolicyIngressRule{
		{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &tcpProto, Port: &customPort},
			},
		},
	}

	policy := testNetworkPolicyController.buildHeadNetworkPolicy(context.Background(), cluster, rayv1.NetworkIsolationDenyAll)

	// 2 base + 1 custom = 3.
	require.Len(t, policy.Spec.Ingress, 3)
	require.Len(t, policy.Spec.Ingress[2].Ports, 1)
	assert.Equal(t, &customPort, policy.Spec.Ingress[2].Ports[0].Port)
}

// TestBuildHeadNetworkPolicy_CustomEgressRules verifies that custom EgressRules are appended after base egress.
func TestBuildHeadNetworkPolicy_CustomEgressRules(t *testing.T) {
	setupNetworkPolicyTest(t)

	customPort := intstr.FromInt32(8080)
	tcpProto := corev1.ProtocolTCP
	cluster := testRayClusterBasic.DeepCopy()
	cluster.Spec.NetworkIsolation.EgressRules = []networkingv1.NetworkPolicyEgressRule{
		{
			Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &tcpProto, Port: &customPort},
			},
		},
	}

	policy := testNetworkPolicyController.buildHeadNetworkPolicy(context.Background(), cluster, rayv1.NetworkIsolationDenyAll)

	// 2 base egress + 1 custom = 3.
	require.Len(t, policy.Spec.Egress, 3)
	require.Len(t, policy.Spec.Egress[2].Ports, 1)
	assert.Equal(t, &customPort, policy.Spec.Egress[2].Ports[0].Port)
}

// TestGetHeadPort_DefaultFallback verifies the default port is returned when rayStartParams has no override.
func TestGetHeadPort_DefaultFallback(t *testing.T) {
	setupNetworkPolicyTest(t)

	cluster := testRayClusterBasic.DeepCopy()
	cluster.Spec.HeadGroupSpec.RayStartParams = map[string]string{}

	port := testNetworkPolicyController.getHeadPort(cluster, "dashboard-port", utils.DefaultDashboardPort)
	assert.Equal(t, int32(utils.DefaultDashboardPort), port)
}

// TestGetHeadPort_CustomPort verifies that a port set via rayStartParams is returned.
func TestGetHeadPort_CustomPort(t *testing.T) {
	setupNetworkPolicyTest(t)

	cluster := testRayClusterBasic.DeepCopy()
	cluster.Spec.HeadGroupSpec.RayStartParams = map[string]string{
		"dashboard-port": "9265",
	}

	port := testNetworkPolicyController.getHeadPort(cluster, "dashboard-port", utils.DefaultDashboardPort)
	assert.Equal(t, int32(9265), port)
}

// TestBuildHeadNetworkPolicy_AutoscalingAddsAPIServerEgress verifies that the head policy
// includes a Kubernetes API server egress rule when autoscaling is enabled and egress is restricted.
func TestBuildHeadNetworkPolicy_AutoscalingAddsAPIServerEgress(t *testing.T) {
	setupNetworkPolicyTest(t)

	testNetworkPolicyController.Client = clientFake.NewClientBuilder().
		WithScheme(testNetworkPolicyController.Scheme).
		WithRuntimeObjects(&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default", Name: "kubernetes",
				Labels: map[string]string{discoveryv1.LabelServiceName: "kubernetes"},
			},
			Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"192.168.1.10"}}},
			Ports:     []discoveryv1.EndpointPort{{Port: ptr.To[int32](6443), Protocol: ptr.To(corev1.ProtocolTCP)}},
		}).Build()

	cluster := testRayClusterBasic.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = ptr.To(true)

	policy := testNetworkPolicyController.buildHeadNetworkPolicy(context.Background(), cluster, rayv1.NetworkIsolationDenyAll)

	// 3 egress rules: intra-cluster, DNS, and API server.
	require.Len(t, policy.Spec.Egress, 3)

	apiRule := policy.Spec.Egress[2]
	require.Len(t, apiRule.To, 1)
	require.NotNil(t, apiRule.To[0].IPBlock)
	assert.Equal(t, "192.168.1.10/32", apiRule.To[0].IPBlock.CIDR)

	require.Len(t, apiRule.Ports, 1)
	tcpProtocol := corev1.ProtocolTCP
	expectedPort := intstr.FromInt32(6443)
	assert.Equal(t, &tcpProtocol, apiRule.Ports[0].Protocol)
	assert.Equal(t, &expectedPort, apiRule.Ports[0].Port)
}

// TestBuildKubeAPIServerEgressRule_IPv6 verifies correct CIDR for IPv6 API server addresses.
func TestBuildKubeAPIServerEgressRule_IPv6(t *testing.T) {
	setupNetworkPolicyTest(t)

	testNetworkPolicyController.Client = clientFake.NewClientBuilder().
		WithScheme(testNetworkPolicyController.Scheme).
		WithRuntimeObjects(&discoveryv1.EndpointSlice{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default", Name: "kubernetes",
				Labels: map[string]string{discoveryv1.LabelServiceName: "kubernetes"},
			},
			Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"fd00::1"}}},
			Ports:     []discoveryv1.EndpointPort{{Port: ptr.To[int32](6443), Protocol: ptr.To(corev1.ProtocolTCP)}},
		}).Build()

	rules := testNetworkPolicyController.buildKubeAPIServerEgressRule(context.Background())
	require.Len(t, rules, 1)
	assert.Equal(t, "fd00::1/128", rules[0].To[0].IPBlock.CIDR)

	expectedPort := intstr.FromInt32(6443)
	assert.Equal(t, &expectedPort, rules[0].Ports[0].Port)
}

// TestBuildNetworkPolicy_LongClusterName verifies NetworkPolicy names are constructed correctly for long names.
func TestBuildNetworkPolicy_LongClusterName(t *testing.T) {
	setupNetworkPolicyTest(t)

	cluster := testRayClusterBasic.DeepCopy()
	cluster.Name = strings.Repeat("a", utils.MaxRayClusterNameLength)

	headPolicy := testNetworkPolicyController.buildHeadNetworkPolicy(context.Background(), cluster, rayv1.NetworkIsolationDenyAll)
	workerPolicy := testNetworkPolicyController.buildWorkerNetworkPolicy(cluster, rayv1.NetworkIsolationDenyAll)

	assert.Equal(t, cluster.Name+"-head", headPolicy.Name)
	assert.Equal(t, cluster.Name+"-workers", workerPolicy.Name)
}
