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
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
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
)

var (
	testNetworkPolicyController  *NetworkPolicyController
	testRayClusterBasic          *rayv1.RayCluster
	testRayClusterDenyAllIngress *rayv1.RayCluster
	testRayClusterDenyAllEgress  *rayv1.RayCluster
	testRayClusterWithRayJob     *rayv1.RayCluster
	testRayClusterWithOtherOwner *rayv1.RayCluster
)

func setupNetworkPolicyTest(_ *testing.T) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	testScheme := runtime.NewScheme()
	testNetworkPolicyController = &NetworkPolicyController{
		Scheme: testScheme,
		Client: clientFake.NewClientBuilder().
			WithScheme(testScheme).
			Build(),
	}

	// Basic cluster — denyAll mode (default).
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

	// Cluster configured with denyAllIngress mode.
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

	// Cluster configured with denyAllEgress mode.
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

	// Cluster owned by something other than a RayJob.
	testRayClusterWithOtherOwner = &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-other",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "test-deployment",
					UID:        "67890",
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

// TestBuildHeadNetworkPolicy_DenyAll verifies the head NetworkPolicy in default denyAll mode.
func TestBuildHeadNetworkPolicy_DenyAll(t *testing.T) {
	setupNetworkPolicyTest(t)

	policy := testNetworkPolicyController.buildHeadNetworkPolicy(testRayClusterBasic, rayv1.NetworkIsolationDenyAll)

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

	// denyAll restricts both directions.
	assert.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	assert.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)

	// 3 base ingress rules: intra-cluster, operator, same-namespace.
	assert.Len(t, policy.Spec.Ingress, 3)

	// 2 base egress rules: intra-cluster and DNS.
	assert.Len(t, policy.Spec.Egress, 2)
}

// TestBuildHeadNetworkPolicy_DenyAllIngress verifies that denyAllIngress omits the Egress policy type.
func TestBuildHeadNetworkPolicy_DenyAllIngress(t *testing.T) {
	setupNetworkPolicyTest(t)

	policy := testNetworkPolicyController.buildHeadNetworkPolicy(testRayClusterDenyAllIngress, rayv1.NetworkIsolationDenyAllIngress)

	assert.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	assert.NotContains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
	assert.Len(t, policy.Spec.Ingress, 3)
	assert.Empty(t, policy.Spec.Egress)
}

// TestBuildHeadNetworkPolicy_DenyAllEgress verifies that denyAllEgress includes both ingress and egress.
func TestBuildHeadNetworkPolicy_DenyAllEgress(t *testing.T) {
	setupNetworkPolicyTest(t)

	policy := testNetworkPolicyController.buildHeadNetworkPolicy(testRayClusterDenyAllEgress, rayv1.NetworkIsolationDenyAllEgress)

	assert.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	assert.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
	assert.Len(t, policy.Spec.Ingress, 3)
	assert.Len(t, policy.Spec.Egress, 2)
}

// TestBuildWorkerNetworkPolicy_DenyAll verifies the worker NetworkPolicy in denyAll mode.
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

// TestBuildWorkerNetworkPolicy_DenyAllIngress verifies no egress is added for denyAllIngress mode.
func TestBuildWorkerNetworkPolicy_DenyAllIngress(t *testing.T) {
	setupNetworkPolicyTest(t)

	policy := testNetworkPolicyController.buildWorkerNetworkPolicy(testRayClusterDenyAllIngress, rayv1.NetworkIsolationDenyAllIngress)

	assert.Contains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeIngress)
	assert.NotContains(t, policy.Spec.PolicyTypes, networkingv1.PolicyTypeEgress)
	assert.Len(t, policy.Spec.Ingress, 1)
	assert.Empty(t, policy.Spec.Egress)
}

// TestBuildBaseIngressRules verifies the structure and content of the 3 base ingress rules.
func TestBuildBaseIngressRules(t *testing.T) {
	setupNetworkPolicyTest(t)

	rules := testNetworkPolicyController.buildBaseIngressRules(testRayClusterBasic)
	require.Len(t, rules, 3)

	// Rule 0: intra-cluster — no ports, pod selector matching the cluster label.
	intraClusterRule := rules[0]
	require.Len(t, intraClusterRule.From, 1)
	assert.Equal(t, map[string]string{utils.RayClusterLabelKey: "test-cluster"},
		intraClusterRule.From[0].PodSelector.MatchLabels)
	assert.Empty(t, intraClusterRule.Ports, "Intra-cluster rule must allow all ports (no Ports field)")

	// Rule 1: operator — two ports (dashboard + client), operator pod selector with
	// empty namespace selector so the operator is reachable from any namespace.
	operatorRule := rules[1]
	require.Len(t, operatorRule.From, 1)
	assert.Equal(t, map[string]string{
		"app.kubernetes.io/component": "kuberay-operator",
		"app.kubernetes.io/name":      utils.ApplicationName,
	}, operatorRule.From[0].PodSelector.MatchLabels)
	assert.NotNil(t, operatorRule.From[0].NamespaceSelector)
	assert.Empty(t, operatorRule.From[0].NamespaceSelector.MatchLabels, "Empty namespace selector must match all namespaces")
	require.Len(t, operatorRule.Ports, 2)
	dashboardPort := intstr.FromInt32(utils.DefaultDashboardPort)
	clientPort := intstr.FromInt32(utils.DefaultClientPort)
	assert.Equal(t, &dashboardPort, operatorRule.Ports[0].Port)
	assert.Equal(t, &clientPort, operatorRule.Ports[1].Port)

	// Rule 2: same-namespace — empty pod selector, two ports.
	sameNsRule := rules[2]
	require.Len(t, sameNsRule.From, 1)
	assert.NotNil(t, sameNsRule.From[0].PodSelector)
	assert.Empty(t, sameNsRule.From[0].PodSelector.MatchLabels, "Empty selector matches all pods in namespace")
	require.Len(t, sameNsRule.Ports, 2)
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

	// Rule 1: DNS egress — UDP+TCP port 53, kube-system/openshift-dns namespace selector.
	dnsRule := rules[1]
	require.Len(t, dnsRule.To, 1)
	require.NotNil(t, dnsRule.To[0].NamespaceSelector)
	require.Len(t, dnsRule.To[0].NamespaceSelector.MatchExpressions, 1)
	expr := dnsRule.To[0].NamespaceSelector.MatchExpressions[0]
	assert.Equal(t, corev1.LabelMetadataName, expr.Key)
	assert.Equal(t, metav1.LabelSelectorOpIn, expr.Operator)
	assert.ElementsMatch(t, []string{"kube-system", "openshift-dns"}, expr.Values)

	require.Len(t, dnsRule.Ports, 2)
	dnsPort := intstr.FromInt(53)
	assert.Equal(t, &dnsPort, dnsRule.Ports[0].Port)
	assert.Equal(t, &dnsPort, dnsRule.Ports[1].Port)
	protocols := []corev1.Protocol{*dnsRule.Ports[0].Protocol, *dnsRule.Ports[1].Protocol}
	assert.ElementsMatch(t, []corev1.Protocol{corev1.ProtocolUDP, corev1.ProtocolTCP}, protocols)
}

// TestBuildRayJobPeer_NoOwner verifies nil is returned when the cluster has no owner references.
func TestBuildRayJobPeer_NoOwner(t *testing.T) {
	setupNetworkPolicyTest(t)

	peer := testNetworkPolicyController.buildRayJobPeer(testRayClusterBasic)
	assert.Nil(t, peer)
}

// TestBuildRayJobPeer_WithRayJobOwner verifies the correct peer is built from a RayJob ownerRef.
func TestBuildRayJobPeer_WithRayJobOwner(t *testing.T) {
	setupNetworkPolicyTest(t)

	peer := testNetworkPolicyController.buildRayJobPeer(testRayClusterWithRayJob)
	require.NotNil(t, peer)

	expectedPeer := &networkingv1.NetworkPolicyPeer{
		PodSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"batch.kubernetes.io/job-name": "test-job",
			},
		},
	}
	assert.Equal(t, expectedPeer, peer)
}

// TestBuildRayJobPeer_WithOtherOwner verifies nil is returned for non-RayJob owner references.
func TestBuildRayJobPeer_WithOtherOwner(t *testing.T) {
	setupNetworkPolicyTest(t)

	peer := testNetworkPolicyController.buildRayJobPeer(testRayClusterWithOtherOwner)
	assert.Nil(t, peer)
}

// TestBuildRayJobPeer_MultipleOwners verifies the RayJob peer is found among multiple owner references.
func TestBuildRayJobPeer_MultipleOwners(t *testing.T) {
	setupNetworkPolicyTest(t)

	cluster := testRayClusterBasic.DeepCopy()
	cluster.OwnerReferences = []metav1.OwnerReference{
		{APIVersion: "apps/v1", Kind: "Deployment", Name: "test-deployment", UID: "67890"},
		{APIVersion: "ray.io/v1", Kind: "RayJob", Name: "test-job", UID: "12345"},
	}

	peer := testNetworkPolicyController.buildRayJobPeer(cluster)
	require.NotNil(t, peer)

	expectedPeer := &networkingv1.NetworkPolicyPeer{
		PodSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"batch.kubernetes.io/job-name": "test-job",
			},
		},
	}
	assert.Equal(t, expectedPeer, peer)
}

// TestBuildHeadNetworkPolicy_WithRayJob verifies the extra ingress rule added for a RayJob owner.
func TestBuildHeadNetworkPolicy_WithRayJob(t *testing.T) {
	setupNetworkPolicyTest(t)

	policy := testNetworkPolicyController.buildHeadNetworkPolicy(testRayClusterWithRayJob, rayv1.NetworkIsolationDenyAll)

	// 3 base rules + 1 RayJob peer rule = 4.
	require.Len(t, policy.Spec.Ingress, 4)

	// The last rule carries the RayJob submitter pod selector.
	lastRule := policy.Spec.Ingress[3]
	require.Len(t, lastRule.From, 1)
	assert.Equal(t, map[string]string{
		"batch.kubernetes.io/job-name": "test-job",
	}, lastRule.From[0].PodSelector.MatchLabels)
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

	policy := testNetworkPolicyController.buildHeadNetworkPolicy(cluster, rayv1.NetworkIsolationDenyAll)

	// 3 base + 1 custom = 4.
	require.Len(t, policy.Spec.Ingress, 4)
	require.Len(t, policy.Spec.Ingress[3].Ports, 1)
	assert.Equal(t, &customPort, policy.Spec.Ingress[3].Ports[0].Port)
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

	policy := testNetworkPolicyController.buildHeadNetworkPolicy(cluster, rayv1.NetworkIsolationDenyAll)

	// 2 base egress + 1 custom = 3.
	require.Len(t, policy.Spec.Egress, 3)
	require.Len(t, policy.Spec.Egress[2].Ports, 1)
	assert.Equal(t, &customPort, policy.Spec.Egress[2].Ports[0].Port)
}

// TestGetHeadPort_DefaultFallback verifies the default port is returned when the container list is empty.
func TestGetHeadPort_DefaultFallback(t *testing.T) {
	setupNetworkPolicyTest(t)

	cluster := testRayClusterBasic.DeepCopy()
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers = nil

	port := testNetworkPolicyController.getHeadPort(cluster, utils.DashboardPortName, utils.DefaultDashboardPort)
	assert.Equal(t, int32(utils.DefaultDashboardPort), port)
}

// TestGetHeadPort_CustomPort verifies that a named port found in the head container spec is returned.
func TestGetHeadPort_CustomPort(t *testing.T) {
	setupNetworkPolicyTest(t)

	cluster := testRayClusterBasic.DeepCopy()
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers = []corev1.Container{
		{
			Name: "ray-head",
			Ports: []corev1.ContainerPort{
				{Name: utils.DashboardPortName, ContainerPort: 9265},
			},
		},
	}

	port := testNetworkPolicyController.getHeadPort(cluster, utils.DashboardPortName, utils.DefaultDashboardPort)
	assert.Equal(t, int32(9265), port)
}

// TestBuildNetworkPolicy_LongClusterName verifies NetworkPolicy names are constructed correctly for long names.
func TestBuildNetworkPolicy_LongClusterName(t *testing.T) {
	setupNetworkPolicyTest(t)

	cluster := testRayClusterBasic.DeepCopy()
	cluster.Name = strings.Repeat("a", utils.MaxRayClusterNameLength)

	headPolicy := testNetworkPolicyController.buildHeadNetworkPolicy(cluster, rayv1.NetworkIsolationDenyAll)
	workerPolicy := testNetworkPolicyController.buildWorkerNetworkPolicy(cluster, rayv1.NetworkIsolationDenyAll)

	assert.Equal(t, cluster.Name+"-head", headPolicy.Name)
	assert.Equal(t, cluster.Name+"-workers", workerPolicy.Name)
}
