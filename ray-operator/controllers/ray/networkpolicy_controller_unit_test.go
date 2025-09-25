package ray

import (
	"context"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

var (
	testNetworkPolicyController  *NetworkPolicyController
	testRayClusterBasic          *rayv1.RayCluster
	testRayClusterWithRayJob     *rayv1.RayCluster
	testRayClusterWithOtherOwner *rayv1.RayCluster
)

func setupNetworkPolicyTest(_ *testing.T) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	// Initialize NetworkPolicy controller
	testNetworkPolicyController = &NetworkPolicyController{
		Scheme: runtime.NewScheme(),
	}

	// Basic RayCluster without owner
	testRayClusterBasic = &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-head",
								Image: "rayproject/ray:latest",
							},
						},
					},
				},
			},
		},
	}

	// RayCluster owned by RayJob
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
			HeadGroupSpec: rayv1.HeadGroupSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-head",
								Image: "rayproject/ray:latest",
							},
						},
					},
				},
			},
		},
	}

	// RayCluster owned by something other than RayJob
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
			HeadGroupSpec: rayv1.HeadGroupSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-head",
								Image: "rayproject/ray:latest",
							},
						},
					},
				},
			},
		},
	}
}

func TestBuildHeadNetworkPolicy_BasicCluster(t *testing.T) {
	setupNetworkPolicyTest(t)

	// Set environment for testing
	originalEnv := os.Getenv("POD_NAMESPACE")
	os.Setenv("POD_NAMESPACE", "ray-system")
	defer os.Setenv("POD_NAMESPACE", originalEnv)

	// Test building head NetworkPolicy for basic cluster
	kubeRayNamespaces := []string{"ray-system"}
	policy := testNetworkPolicyController.buildHeadNetworkPolicy(testRayClusterBasic, kubeRayNamespaces)

	// Verify basic properties
	expectedName := testRayClusterBasic.Name + "-head"
	assert.Equal(t, expectedName, policy.Name)
	assert.Equal(t, testRayClusterBasic.Namespace, policy.Namespace)

	// Verify labels
	expectedLabels := map[string]string{
		utils.RayClusterLabelKey:                testRayClusterBasic.Name,
		utils.KubernetesApplicationNameLabelKey: utils.ApplicationName,
		utils.KubernetesCreatedByLabelKey:       utils.ComponentName,
	}
	assert.Equal(t, expectedLabels, policy.Labels)

	// Verify policy type
	assert.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}, policy.Spec.PolicyTypes)

	// Verify pod selector targets head pods only
	expectedPodSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			utils.RayClusterLabelKey:  testRayClusterBasic.Name,
			utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
		},
	}
	assert.Equal(t, expectedPodSelector, policy.Spec.PodSelector)

	// Verify ingress rules - CodeFlare 5-rule pattern (+ optional RayJob rule)
	assert.GreaterOrEqual(t, len(policy.Spec.Ingress), 5, "Should have at least 5 ingress rules")
	assert.LessOrEqual(t, len(policy.Spec.Ingress), 6, "Should have at most 6 ingress rules (including optional RayJob)")

	// Verify Rule 1: Intra-cluster communication - NO PORTS (allows all ports)
	intraClusterRule := policy.Spec.Ingress[0]
	assert.Len(t, intraClusterRule.From, 1, "Intra-cluster rule should have one peer")
	assert.Empty(t, intraClusterRule.Ports, "Intra-cluster rule should have NO ports (allows all)")

	expectedIntraClusterPeer := networkingv1.NetworkPolicyPeer{
		PodSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				utils.RayClusterLabelKey: testRayClusterBasic.Name,
			},
		},
	}
	assert.Equal(t, expectedIntraClusterPeer, intraClusterRule.From[0], "Should allow cluster members")

	// Verify Rule 2: External access to dashboard and client ports from any pod in namespace
	externalRule := policy.Spec.Ingress[1]
	assert.Len(t, externalRule.From, 1, "External rule should have one peer")
	assert.Len(t, externalRule.Ports, 2, "External rule should have 2 ports (10001, 8265)")

	// Verify empty pod selector (any pod in namespace)
	expectedAnyPodPeer := networkingv1.NetworkPolicyPeer{
		PodSelector: &metav1.LabelSelector{
			// Empty MatchLabels = any pod in same namespace
		},
	}
	assert.Equal(t, expectedAnyPodPeer, externalRule.From[0], "Should allow any pod in namespace")

	// Check ports (10001, 8265)
	portFound10001 := false
	portFound8265 := false
	for _, port := range externalRule.Ports {
		switch port.Port.IntVal {
		case 10001:
			portFound10001 = true
		case 8265:
			portFound8265 = true
		}
	}
	assert.True(t, portFound10001, "Should include client port 10001")
	assert.True(t, portFound8265, "Should include dashboard port 8265")

	// Verify Rule 3: KubeRay operator access
	operatorRule := policy.Spec.Ingress[2]
	assert.Len(t, operatorRule.From, 1, "Operator rule should have one peer")
	assert.Len(t, operatorRule.Ports, 2, "Operator rule should have 2 ports (8265, 10001)")

	// Verify Rule 4: Monitoring access
	monitoringRule := policy.Spec.Ingress[3]
	assert.Len(t, monitoringRule.From, 1, "Monitoring rule should have one peer")
	assert.Len(t, monitoringRule.Ports, 1, "Monitoring rule should have 1 port (8080 only)")
	assert.Equal(t, int32(8080), monitoringRule.Ports[0].Port.IntVal, "Should be monitoring port 8080")

	// Verify Rule 5: Secured ports - NO FROM (allows all)
	securedRule := policy.Spec.Ingress[4]
	assert.Empty(t, securedRule.From, "Secured ports rule should have NO from (allows all)")
	assert.GreaterOrEqual(t, len(securedRule.Ports), 1, "Secured ports rule should have at least 1 port (8443)")

	// Check for mTLS port 8443 (always present)
	portFound8443 := false
	for _, port := range securedRule.Ports {
		if port.Port.IntVal == 8443 {
			portFound8443 = true
		}
	}
	assert.True(t, portFound8443, "Should include mTLS port 8443")
}

func TestBuildHeadNetworkPolicy_WithMTLS(t *testing.T) {
	setupNetworkPolicyTest(t)

	// Create RayCluster with mTLS configuration
	rayClusterWithMTLS := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster-mtls",
			Namespace: "default",
		},
		Spec: rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-head",
								Image: "rayproject/ray:latest",
								Env: []corev1.EnvVar{
									{Name: "RAY_USE_TLS", Value: "1"},
									{Name: "RAY_TLS_SERVER_CERT", Value: "/etc/tls/server.crt"},
									{Name: "RAY_TLS_SERVER_KEY", Value: "/etc/tls/server.key"},
								},
							},
						},
					},
				},
			},
		},
	}

	// Test building head NetworkPolicy with mTLS enabled
	policy := testNetworkPolicyController.buildHeadNetworkPolicy(rayClusterWithMTLS, []string{"ray-system"})

	// Verify Rule 5: Secured ports should include both 8443 and 10001 when mTLS is enabled
	securedRule := policy.Spec.Ingress[4]
	assert.Empty(t, securedRule.From, "Secured ports rule should have NO from (allows all)")
	assert.Len(t, securedRule.Ports, 2, "Secured ports rule should have 2 ports when mTLS enabled (8443, 10001)")

	// Check for both mTLS ports
	portFound8443 := false
	portFound10001 := false
	for _, port := range securedRule.Ports {
		switch port.Port.IntVal {
		case 8443:
			portFound8443 = true
		case 10001:
			portFound10001 = true
		}
	}
	assert.True(t, portFound8443, "Should include mTLS port 8443")
	assert.True(t, portFound10001, "Should include client port 10001 when mTLS enabled")
}

func TestBuildHeadNetworkPolicy_WithoutMTLS(t *testing.T) {
	setupNetworkPolicyTest(t)

	// Use basic cluster without mTLS configuration
	policy := testNetworkPolicyController.buildHeadNetworkPolicy(testRayClusterBasic, []string{"ray-system"})

	// Verify Rule 5: Secured ports should only include 8443 when mTLS is disabled
	securedRule := policy.Spec.Ingress[4]
	assert.Empty(t, securedRule.From, "Secured ports rule should have NO from (allows all)")
	assert.Len(t, securedRule.Ports, 1, "Secured ports rule should have 1 port when mTLS disabled (8443 only)")

	// Check for only mTLS port
	assert.Equal(t, int32(8443), securedRule.Ports[0].Port.IntVal, "Should only include mTLS port 8443")
}

func TestBuildWorkerNetworkPolicy_BasicCluster(t *testing.T) {
	setupNetworkPolicyTest(t)

	// Test building worker NetworkPolicy for basic cluster
	policy := testNetworkPolicyController.buildWorkerNetworkPolicy(testRayClusterBasic)

	// Verify basic properties
	expectedName := testRayClusterBasic.Name + "-workers"
	assert.Equal(t, expectedName, policy.Name)
	assert.Equal(t, testRayClusterBasic.Namespace, policy.Namespace)

	// Verify labels
	expectedLabels := map[string]string{
		utils.RayClusterLabelKey:                testRayClusterBasic.Name,
		utils.KubernetesApplicationNameLabelKey: utils.ApplicationName,
		utils.KubernetesCreatedByLabelKey:       utils.ComponentName,
	}
	assert.Equal(t, expectedLabels, policy.Labels)

	// Verify pod selector targets worker pods only
	expectedPodSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			utils.RayClusterLabelKey:  testRayClusterBasic.Name,
			utils.RayNodeTypeLabelKey: string(rayv1.WorkerNode),
		},
	}
	assert.Equal(t, expectedPodSelector, policy.Spec.PodSelector)

	// Verify ingress rules - workers only allow intra-cluster communication
	require.Len(t, policy.Spec.Ingress, 1)
	require.Len(t, policy.Spec.Ingress[0].From, 1)

	// Verify intra-cluster peer
	intraClusterPeer := policy.Spec.Ingress[0].From[0]
	expectedIntraClusterPeer := networkingv1.NetworkPolicyPeer{
		PodSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				utils.RayClusterLabelKey: testRayClusterBasic.Name,
			},
		},
	}
	assert.Equal(t, expectedIntraClusterPeer, intraClusterPeer)
}

func TestBuildHeadNetworkPolicy_ClusterWithRayJob(t *testing.T) {
	setupNetworkPolicyTest(t)

	// Set environment for testing
	originalEnv := os.Getenv("POD_NAMESPACE")
	os.Setenv("POD_NAMESPACE", "ray-system")
	defer os.Setenv("POD_NAMESPACE", originalEnv)

	// Test building head NetworkPolicy for cluster owned by RayJob
	kubeRayNamespaces := []string{"ray-system"}
	policy := testNetworkPolicyController.buildHeadNetworkPolicy(testRayClusterWithRayJob, kubeRayNamespaces)

	// Verify basic properties
	expectedName := testRayClusterWithRayJob.Name + "-head"
	assert.Equal(t, expectedName, policy.Name)

	// Verify ingress rules - should have additional RayJob rule
	assert.Greater(t, len(policy.Spec.Ingress), 4, "Should have additional RayJob ingress rule")

	// Find the RayJob rule (should be the last rule)
	rayJobRule := policy.Spec.Ingress[len(policy.Spec.Ingress)-1]
	require.Len(t, rayJobRule.From, 1, "RayJob rule should have one peer")

	// Verify RayJob peer
	rayJobPeer := rayJobRule.From[0]
	expectedRayJobPeer := networkingv1.NetworkPolicyPeer{
		PodSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"batch.kubernetes.io/job-name": "test-job",
			},
		},
	}
	assert.Equal(t, expectedRayJobPeer, rayJobPeer)
}

func TestBuildHeadNetworkPolicy_MonitoringAccess(t *testing.T) {
	setupNetworkPolicyTest(t)

	// Test building head NetworkPolicy with monitoring access
	kubeRayNamespaces := []string{"ray-system"}
	policy := testNetworkPolicyController.buildHeadNetworkPolicy(testRayClusterBasic, kubeRayNamespaces)

	// Find the monitoring rule (should have port 8080)
	var monitoringRule *networkingv1.NetworkPolicyIngressRule
	for _, rule := range policy.Spec.Ingress {
		for _, port := range rule.Ports {
			if port.Port != nil && port.Port.IntVal == 8080 {
				monitoringRule = &rule
				break
			}
		}
		if monitoringRule != nil {
			break
		}
	}

	require.NotNil(t, monitoringRule, "Should have monitoring rule with port 8080")
	assert.Len(t, monitoringRule.Ports, 1, "Monitoring rule should have one port")
	assert.Equal(t, int32(8080), monitoringRule.Ports[0].Port.IntVal, "Should be port 8080")

	// Should allow from monitoring sources (single From with multiple namespaces)
	assert.Len(t, monitoringRule.From, 1, "Should have one monitoring peer with multiple namespaces")

	// Check for both OpenShift monitoring and Prometheus namespaces
	foundOpenShiftMonitoring := false
	foundPrometheus := false
	for _, peer := range monitoringRule.From {
		if peer.NamespaceSelector != nil {
			for _, req := range peer.NamespaceSelector.MatchExpressions {
				if req.Key == "kubernetes.io/metadata.name" {
					if contains(req.Values, "openshift-monitoring") {
						foundOpenShiftMonitoring = true
					}
					if contains(req.Values, "prometheus") {
						foundPrometheus = true
					}
				}
			}
		}
	}
	assert.True(t, foundOpenShiftMonitoring, "Should allow OpenShift monitoring namespace")
	assert.True(t, foundPrometheus, "Should allow Prometheus namespace")
}

func TestBuildHeadNetworkPolicy_SecuredPorts(t *testing.T) {
	setupNetworkPolicyTest(t)

	// Test building head NetworkPolicy with secured ports (mTLS)
	kubeRayNamespaces := []string{"ray-system"}
	policy := testNetworkPolicyController.buildHeadNetworkPolicy(testRayClusterBasic, kubeRayNamespaces)

	// Find the secured ports rule
	var securedPortsRule *networkingv1.NetworkPolicyIngressRule
	for _, rule := range policy.Spec.Ingress {
		for _, port := range rule.Ports {
			if port.Port != nil && port.Port.IntVal == 8443 {
				securedPortsRule = &rule
				break
			}
		}
		if securedPortsRule != nil {
			break
		}
	}

	require.NotNil(t, securedPortsRule, "Should have secured ports rule")
	assert.Len(t, securedPortsRule.Ports, 1, "Should have 1 secured port (8443 only, no mTLS)")

	// Check for mTLS port 8443 (always present)
	portFound8443 := false
	for _, port := range securedPortsRule.Ports {
		if port.Port.IntVal == 8443 {
			portFound8443 = true
		}
	}
	assert.True(t, portFound8443, "Should include mTLS port 8443")
}

// Helper function to check if slice contains string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func TestGetKubeRayNamespaces_EnvironmentFallback(t *testing.T) {
	setupNetworkPolicyTest(t)

	// Test fallback when POD_NAMESPACE is not set
	originalEnv := os.Getenv("POD_NAMESPACE")
	os.Unsetenv("POD_NAMESPACE")
	defer os.Setenv("POD_NAMESPACE", originalEnv)

	namespaces := testNetworkPolicyController.getKubeRayNamespaces(context.Background())

	// Should fallback to "ray-system" namespace
	assert.Equal(t, []string{"ray-system"}, namespaces)
}

func TestGetKubeRayNamespaces_WithEnvironment(t *testing.T) {
	setupNetworkPolicyTest(t)

	// Test with POD_NAMESPACE set
	originalEnv := os.Getenv("POD_NAMESPACE")
	os.Setenv("POD_NAMESPACE", "custom-ray-system")
	defer os.Setenv("POD_NAMESPACE", originalEnv)

	namespaces := testNetworkPolicyController.getKubeRayNamespaces(context.Background())

	// Should use the custom namespace
	assert.Equal(t, []string{"custom-ray-system"}, namespaces)
}

func TestBuildRayJobPeer_NoOwner(t *testing.T) {
	setupNetworkPolicyTest(t)

	// Test RayCluster without owner
	peer := testNetworkPolicyController.buildRayJobPeer(testRayClusterBasic)
	assert.Nil(t, peer)
}

func TestBuildRayJobPeer_WithRayJobOwner(t *testing.T) {
	setupNetworkPolicyTest(t)

	// Test RayCluster with RayJob owner
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

func TestBuildRayJobPeer_WithOtherOwner(t *testing.T) {
	setupNetworkPolicyTest(t)

	// Test RayCluster with non-RayJob owner
	peer := testNetworkPolicyController.buildRayJobPeer(testRayClusterWithOtherOwner)
	assert.Nil(t, peer)
}

func TestBuildRayJobPeer_MultipleOwners(t *testing.T) {
	setupNetworkPolicyTest(t)

	// Create RayCluster with multiple owners, including RayJob
	rayCluster := testRayClusterBasic.DeepCopy()
	rayCluster.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "test-deployment",
			UID:        "67890",
		},
		{
			APIVersion: "ray.io/v1",
			Kind:       "RayJob",
			Name:       "test-job",
			UID:        "12345",
		},
	}

	peer := testNetworkPolicyController.buildRayJobPeer(rayCluster)
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

func TestBuildHeadNetworkPolicy_DifferentNamespace(t *testing.T) {
	setupNetworkPolicyTest(t)

	// Set custom operator namespace
	originalEnv := os.Getenv("POD_NAMESPACE")
	os.Setenv("POD_NAMESPACE", "custom-ray-system")
	defer os.Setenv("POD_NAMESPACE", originalEnv)

	// Create cluster in different namespace
	rayCluster := testRayClusterBasic.DeepCopy()
	rayCluster.Namespace = "custom-namespace"

	kubeRayNamespaces := []string{"custom-ray-system"}
	headPolicy := testNetworkPolicyController.buildHeadNetworkPolicy(rayCluster, kubeRayNamespaces)

	// Verify NetworkPolicy is created in the same namespace as RayCluster
	assert.Equal(t, "custom-namespace", headPolicy.Namespace)

	// Verify head policy name
	expectedHeadName := rayCluster.Name + "-head"
	assert.Equal(t, expectedHeadName, headPolicy.Name)
}

func TestBuildHeadNetworkPolicy_LongClusterName(t *testing.T) {
	setupNetworkPolicyTest(t)

	// Test with long cluster name
	longName := "very-long-cluster-name-that-might-cause-issues"
	rayCluster := testRayClusterBasic.DeepCopy()
	rayCluster.Name = longName

	kubeRayNamespaces := []string{"ray-system"}
	headPolicy := testNetworkPolicyController.buildHeadNetworkPolicy(rayCluster, kubeRayNamespaces)

	// Verify name is constructed correctly
	expectedHeadName := longName + "-head"
	assert.Equal(t, expectedHeadName, headPolicy.Name)

	// Verify pod selector uses correct cluster name and targets head pods
	expectedPodSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			utils.RayClusterLabelKey:  longName,
			utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
		},
	}
	assert.Equal(t, expectedPodSelector, headPolicy.Spec.PodSelector)
}
