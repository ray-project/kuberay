package utils

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

func TestGetClusterDomainName(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{{
		name: "all good from env",
		env:  "abc.com",
		want: "abc.com",
	}, {
		name: "No env set",
		env:  "",
		want: DefaultDomainName,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.env) > 0 {
				t.Setenv(ClusterDomainEnvKey, tt.env)
			}
			got := GetClusterDomainName()
			if got != tt.want {
				t.Errorf("Test %s failed expected: %s but got: %s", tt.name, tt.want, got)
			}
		})
	}
}

func TestStatus(t *testing.T) {
	pod := createSomePod()
	pod.Status.Phase = corev1.PodPending
	if !IsCreated(pod) {
		t.Fail()
	}
}

func TestCheckAllPodsRunning(t *testing.T) {
	tests := []struct {
		name     string
		pods     corev1.PodList
		expected bool
	}{
		{
			name: "should return true if all Pods are running",
			pods: corev1.PodList{
				Items: []corev1.Pod{
					*createSomePodWithPhase(corev1.PodRunning),
					*createSomePodWithPhase(corev1.PodRunning),
				},
			},
			expected: true,
		},
		{
			name: "should return false if there are no Pods",
			pods: corev1.PodList{
				Items: []corev1.Pod{},
			},
			expected: false,
		},
		{
			name: "should return false if any Pods don't have .status.phase Running",
			pods: corev1.PodList{
				Items: []corev1.Pod{
					*createSomePodWithPhase(corev1.PodPending),
					*createSomePodWithPhase(corev1.PodRunning),
				},
			},
			expected: false,
		},
		{
			name: "should return false if any Pods have a .status.condition of type: Ready that's not status: True",
			pods: corev1.PodList{
				Items: []corev1.Pod{
					*createSomePodWithPhase(corev1.PodRunning),
					*createSomePodWithCondition(corev1.PodReady, corev1.ConditionFalse),
				},
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, CheckAllPodsRunning(context.Background(), tc.pods))
		})
	}
}

func TestPodName(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		nodeType rayv1.RayNodeType
		expected string
	}{
		{
			name:     "short cluster name, head pod",
			prefix:   "ray-cluster-01",
			nodeType: rayv1.HeadNode,
			expected: "ray-cluster-01-head-",
		},
		{
			name:     "short cluster name, worker pod",
			prefix:   "ray-cluster-group-name-01",
			nodeType: rayv1.WorkerNode,
			expected: "ray-cluster-group-name-01-worker-",
		},
		{
			name:     "long cluster name, head pod",
			prefix:   "ray-cluster-0000000000000000000000011111111122222233333333333333",
			nodeType: rayv1.HeadNode,
			expected: "ray-cluster-00000000000000000000000111111111222222-head-",
		},
		{
			name:     "long cluster name, worker pod",
			prefix:   "ray-cluster-0000000000000000000000011111111122222233333333333333-group-name",
			nodeType: rayv1.WorkerNode,
			expected: "ray-cluster-00000000000000000000000111111111222222-worker-",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			str := PodName(test.prefix, test.nodeType, true)
			if str != test.expected {
				t.Logf("expected: %q", test.expected)
				t.Logf("actual: %q", str)
				t.Error("PodName returned an unexpected string")
			}

			// 63 (max pod name length) - 5 random hexadecimal characters from generateName
			if len(str) > 58 {
				t.Error("Generated pod name is too long")
			}
		})
	}
}

func TestCheckName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "shorten long string starting with numeric character",
			input:    "72fbcc7e-a661-4b18e-ca41-e903-fc3ae634b18e-lazer090scholar-director-s",
			expected: "rca41-e903-fc3ae634b18e-lazer090scholar-director-s",
		},
		{
			name:     "shorten long string starting with special character",
			input:    "--------566666--------444433-----------222222----------4444",
			expected: "r6666--------444433-----------222222----------4444",
		},
		{
			name:     "unchanged",
			input:    "acceptable-name-head-12345",
			expected: "acceptable-name-head-12345",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			str := CheckName(test.input)
			if str != test.expected {
				t.Logf("expected: %q", test.expected)
				t.Logf("actual: %q", str)
				t.Error("CheckName returned an unexpected string")
			}
		})
	}
}

func TestCheckRouteName(t *testing.T) {
	tests := []struct {
		name      string
		routeName string
		namespace string
		want      string
	}{{
		name:      "long route name truncated",
		routeName: "cv-traffic-training-202402090958",
		namespace: "development-namespace",
		want:      "cv-traffic-training-2024020909",
	}, {
		name:      "long route name w/number start truncated and number replaced",
		routeName: "2-step-cv-training-network-revisited",
		namespace: "development-namespace",
		want:      "r-step-cv-training-network-rev",
	}, {
		name:      "well-formatted and well-sized route name unaffected",
		routeName: "acceptable-name-head-12345",
		namespace: "development-namespace",
		want:      "acceptable-name-head-12345",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name := CheckRouteName(context.Background(), tc.routeName, tc.namespace)
			assert.Equal(t, tc.want, name)
		})
	}
}

func createSomePod() (pod *corev1.Pod) {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-sample-small-group-worker-0",
			Namespace: "default",
		},
	}
}

func createSomePodWithPhase(phase corev1.PodPhase) (pod *corev1.Pod) {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-sample-small-group-worker-0",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			Phase: phase,
		},
	}
}

func createSomePodWithCondition(typ corev1.PodConditionType, status corev1.ConditionStatus) (pod *corev1.Pod) {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-sample-small-group-worker-0",
			Namespace: "default",
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   typ,
					Status: status,
				},
			},
		},
	}
}

func createRayHeadPodWithPhaseAndCondition(phase corev1.PodPhase, typ corev1.PodConditionType, status corev1.ConditionStatus) (pod *corev1.Pod) {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-sample-head",
			Namespace: "default",
			Labels: map[string]string{
				"ray.io/node-type": string(rayv1.HeadNode),
			},
		},
		Status: corev1.PodStatus{
			Phase: phase,
			Conditions: []corev1.PodCondition{
				{
					Type:   typ,
					Status: status,
				},
			},
		},
	}
}

func TestGetHeadGroupServiceAccountName(t *testing.T) {
	tests := []struct {
		name  string
		input *rayv1.RayCluster
		want  string
	}{
		{
			name: "Ray cluster with head group service account",
			input: &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "raycluster-sample",
					Namespace: "default",
				},
				Spec: rayv1.RayClusterSpec{
					HeadGroupSpec: rayv1.HeadGroupSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								ServiceAccountName: "my-service-account",
							},
						},
					},
				},
			},
			want: "my-service-account",
		},
		{
			name: "Ray cluster without head group service account",
			input: &rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "raycluster-sample",
					Namespace: "default",
				},
				Spec: rayv1.RayClusterSpec{
					HeadGroupSpec: rayv1.HeadGroupSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{},
						},
					},
				},
			},
			want: "raycluster-sample",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, GetHeadGroupServiceAccountName(tc.input))
		})
	}
}

func TestCalculateAvailableReplicas(t *testing.T) {
	podList := corev1.PodList{
		Items: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod1",
					Labels: map[string]string{
						"ray.io/node-type": string(rayv1.HeadNode),
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod2",
					Labels: map[string]string{
						"ray.io/node-type": string(rayv1.WorkerNode),
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod2",
					Labels: map[string]string{
						"ray.io/node-type": string(rayv1.WorkerNode),
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod2",
					Labels: map[string]string{
						"ray.io/node-type": string(rayv1.WorkerNode),
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodFailed,
				},
			},
		},
	}

	availableCount := CalculateAvailableReplicas(podList)
	assert.Equal(t, int32(1), availableCount, "expect 1 available replica")

	readyCount := CalculateReadyReplicas(podList)
	assert.Equal(t, int32(1), readyCount, "expect 1 ready replica")
}

func TestFindContainerPort(t *testing.T) {
	container := corev1.Container{
		Name: "ray-head",
		Ports: []corev1.ContainerPort{
			{
				Name:          "port1",
				ContainerPort: 10001,
			},
			{
				Name:          "port2",
				ContainerPort: 10002,
			},
		},
	}
	port := FindContainerPort(&container, "port1", -1)
	assert.NotEqual(t, port, -1, "expect port1 found")
	port = FindContainerPort(&container, "port2", -1)
	assert.NotEqual(t, port, -1, "expect port2 found")
	port = FindContainerPort(&container, "port3", -1)
	assert.Equal(t, port, -1, "expect port3 not found")
}

func TestGenerateHeadServiceName(t *testing.T) {
	// GenerateHeadServiceName generates a Ray head service name. Note that there are two types of head services:
	//
	// (1) For RayCluster: If `HeadService.Name` in the cluster spec is not empty, it will be used as the head service name.
	// Otherwise, the name is generated based on the RayCluster CR's name.
	// (2) For RayService: It's important to note that the RayService CR not only possesses a head service owned by its RayCluster CR
	// but also maintains a separate head service for itself to facilitate zero-downtime upgrades. The name of the head service owned
	// by the RayService CR is generated based on the RayService CR's name.

	// [RayCluster]
	// Test 1: `HeadService.Name` is empty.
	headSvcName, err := GenerateHeadServiceName(RayClusterCRD, rayv1.RayClusterSpec{}, "raycluster-sample")
	expectedGeneratedSvcName := "raycluster-sample-head-svc"
	require.NoError(t, err)
	assert.Equal(t, expectedGeneratedSvcName, headSvcName)

	// Test 2: `HeadService.Name` is not empty.
	clusterSpecWithHeadService := rayv1.RayClusterSpec{
		HeadGroupSpec: rayv1.HeadGroupSpec{
			HeadService: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-head-svc",
				},
			},
		},
	}

	headSvcName, err = GenerateHeadServiceName(RayClusterCRD, *clusterSpecWithHeadService.DeepCopy(), "raycluster-sample")
	require.NoError(t, err)
	assert.Equal(t, "my-head-svc", headSvcName)

	// [RayService]
	// Test 3: `HeadService.Name` is empty.
	headSvcName, err = GenerateHeadServiceName(RayServiceCRD, rayv1.RayClusterSpec{}, "rayservice-sample")
	expectedGeneratedSvcName = "rayservice-sample-head-svc"
	require.NoError(t, err)
	assert.Equal(t, expectedGeneratedSvcName, headSvcName)

	// Test 4: `HeadService.Name` is not empty.
	headSvcName, err = GenerateHeadServiceName(RayServiceCRD, *clusterSpecWithHeadService.DeepCopy(), "rayservice-sample")
	require.NoError(t, err)
	assert.Equal(t, expectedGeneratedSvcName, headSvcName)

	// Invalid CRD type
	_, err = GenerateHeadServiceName(RayJobCRD, rayv1.RayClusterSpec{}, "rayjob-sample")
	require.Error(t, err)
}

func TestGetWorkerGroupDesiredReplicas(t *testing.T) {
	ctx := context.Background()
	// Test 1: `WorkerGroupSpec.Replicas` is nil.
	// `Replicas` is impossible to be nil in a real RayCluster CR as it has a default value assigned in the CRD.
	numOfHosts := int32(1)
	minReplicas := int32(1)
	maxReplicas := int32(5)

	workerGroupSpec := rayv1.WorkerGroupSpec{
		NumOfHosts:  numOfHosts,
		MinReplicas: &minReplicas,
		MaxReplicas: &maxReplicas,
	}
	assert.Equal(t, GetWorkerGroupDesiredReplicas(ctx, workerGroupSpec), minReplicas)

	// Test 2: `WorkerGroupSpec.Replicas` is not nil and is within the range.
	replicas := int32(3)
	workerGroupSpec.Replicas = &replicas
	assert.Equal(t, GetWorkerGroupDesiredReplicas(ctx, workerGroupSpec), replicas)

	// Test 3: `WorkerGroupSpec.Replicas` is not nil but is more than maxReplicas.
	replicas = int32(6)
	workerGroupSpec.Replicas = &replicas
	assert.Equal(t, GetWorkerGroupDesiredReplicas(ctx, workerGroupSpec), maxReplicas)

	// Test 4: `WorkerGroupSpec.Replicas` is not nil but is less than minReplicas.
	replicas = int32(0)
	workerGroupSpec.Replicas = &replicas
	assert.Equal(t, GetWorkerGroupDesiredReplicas(ctx, workerGroupSpec), minReplicas)

	// Test 5: `WorkerGroupSpec.Replicas` is nil and minReplicas is less than maxReplicas.
	workerGroupSpec.Replicas = nil
	workerGroupSpec.MinReplicas = &maxReplicas
	workerGroupSpec.MaxReplicas = &minReplicas
	assert.Equal(t, GetWorkerGroupDesiredReplicas(ctx, workerGroupSpec), *workerGroupSpec.MaxReplicas)

	// Test 6: `WorkerGroupSpec.Suspend` is true.
	suspend := true
	workerGroupSpec.MinReplicas = &maxReplicas
	workerGroupSpec.MaxReplicas = &minReplicas
	workerGroupSpec.Suspend = &suspend
	assert.Zero(t, GetWorkerGroupDesiredReplicas(ctx, workerGroupSpec))

	// Test 7: `WorkerGroupSpec.NumOfHosts` is 4.
	numOfHosts = int32(4)
	replicas = int32(5)
	suspend = false
	workerGroupSpec.NumOfHosts = numOfHosts
	workerGroupSpec.Replicas = &replicas
	workerGroupSpec.Suspend = &suspend
	workerGroupSpec.MinReplicas = &minReplicas
	workerGroupSpec.MaxReplicas = &maxReplicas
	assert.Equal(t, GetWorkerGroupDesiredReplicas(ctx, workerGroupSpec), replicas*numOfHosts)
}

func TestCalculateMinAndMaxReplicas(t *testing.T) {
	suspend := true

	tests := []struct {
		name     string
		specs    []rayv1.WorkerGroupSpec
		expected struct {
			minReplicas int32
			maxReplicas int32
		}
	}{
		{
			name: "Single group with one host",
			specs: []rayv1.WorkerGroupSpec{
				{
					NumOfHosts:  1,
					MinReplicas: ptr.To[int32](2),
					MaxReplicas: ptr.To[int32](3),
				},
			},
			expected: struct {
				minReplicas int32
				maxReplicas int32
			}{
				minReplicas: 2,
				maxReplicas: 3,
			},
		},
		{
			name: "Single group with four hosts",
			specs: []rayv1.WorkerGroupSpec{
				{
					NumOfHosts:  4,
					MinReplicas: ptr.To[int32](2),
					MaxReplicas: ptr.To[int32](3),
				},
			},
			expected: struct {
				minReplicas int32
				maxReplicas int32
			}{
				minReplicas: 8,
				maxReplicas: 12,
			},
		},
		{
			name: "Two worker groups: one with a single host, one with two hosts",
			specs: []rayv1.WorkerGroupSpec{
				{
					NumOfHosts:  1,
					MinReplicas: ptr.To[int32](4),
					MaxReplicas: ptr.To[int32](4),
				},
				{
					NumOfHosts:  2,
					MinReplicas: ptr.To[int32](3),
					MaxReplicas: ptr.To[int32](3),
				},
			},
			expected: struct {
				minReplicas int32
				maxReplicas int32
			}{
				minReplicas: 10,
				maxReplicas: 10,
			},
		},
		{
			name: "Two groups with suspended",
			specs: []rayv1.WorkerGroupSpec{
				{
					NumOfHosts:  1,
					MinReplicas: ptr.To[int32](3),
					MaxReplicas: ptr.To[int32](3),
					Suspend:     &suspend,
				},
				{
					NumOfHosts:  1,
					MinReplicas: ptr.To[int32](1),
					MaxReplicas: ptr.To[int32](1),
					Suspend:     &suspend,
				},
			},
			expected: struct {
				minReplicas int32
				maxReplicas int32
			}{
				minReplicas: 0,
				maxReplicas: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster := &rayv1.RayCluster{
				Spec: rayv1.RayClusterSpec{
					WorkerGroupSpecs: tt.specs,
				},
			}

			// Check min replicas
			assert.Equal(t, tt.expected.minReplicas, CalculateMinReplicas(cluster))
			// Check max replicas
			assert.Equal(t, tt.expected.maxReplicas, CalculateMaxReplicas(cluster))
		})
	}
}

func TestCalculateDesiredReplicas(t *testing.T) {
	tests := []struct {
		group1Replicas    *int32
		group1MinReplicas *int32
		group1MaxReplicas *int32
		group2Replicas    *int32
		group2MinReplicas *int32
		group2MaxReplicas *int32
		name              string
		group1NumOfHosts  int32
		group2NumOfHosts  int32
		answer            int32
	}{
		{
			group1Replicas:    nil,
			group1NumOfHosts:  1,
			group1MinReplicas: ptr.To[int32](1),
			group1MaxReplicas: ptr.To[int32](5),
			group2Replicas:    nil,
			group2NumOfHosts:  1,
			group2MinReplicas: ptr.To[int32](2),
			group2MaxReplicas: ptr.To[int32](5),
			name:              "Both groups' Replicas are nil",
			answer:            3,
		},
		{
			group1Replicas:    ptr.To[int32](0),
			group1NumOfHosts:  1,
			group1MinReplicas: ptr.To[int32](2),
			group1MaxReplicas: ptr.To[int32](5),
			group2Replicas:    ptr.To[int32](6),
			group2NumOfHosts:  1,
			group2MinReplicas: ptr.To[int32](2),
			group2MaxReplicas: ptr.To[int32](5),
			name:              "Group1's Replicas is smaller than MinReplicas, and Group2's Replicas is more than MaxReplicas.",
			answer:            7,
		},
		{
			group1Replicas:    ptr.To[int32](6),
			group1NumOfHosts:  1,
			group1MinReplicas: ptr.To[int32](2),
			group1MaxReplicas: ptr.To[int32](5),
			group2Replicas:    ptr.To[int32](3),
			group2NumOfHosts:  1,
			group2MinReplicas: ptr.To[int32](2),
			group2MaxReplicas: ptr.To[int32](5),
			name:              "Group1's Replicas is more than MaxReplicas.",
			answer:            8,
		},
		{
			group1Replicas:    ptr.To[int32](3),
			group1NumOfHosts:  4,
			group1MinReplicas: ptr.To[int32](1),
			group1MaxReplicas: ptr.To[int32](6),
			group2Replicas:    ptr.To[int32](3),
			group2NumOfHosts:  1,
			group2MinReplicas: ptr.To[int32](2),
			group2MaxReplicas: ptr.To[int32](5),
			name:              "Group1's NumOfHosts is 4, and Group2's Replicas is 1.",
			answer:            15,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cluster := rayv1.RayCluster{
				Spec: rayv1.RayClusterSpec{
					WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
						{
							GroupName:   "group1",
							Replicas:    tc.group1Replicas,
							NumOfHosts:  tc.group1NumOfHosts,
							MinReplicas: tc.group1MinReplicas,
							MaxReplicas: tc.group1MaxReplicas,
						},
						{
							GroupName:   "group2",
							Replicas:    tc.group2Replicas,
							NumOfHosts:  tc.group2NumOfHosts,
							MinReplicas: tc.group2MinReplicas,
							MaxReplicas: tc.group2MaxReplicas,
						},
					},
				},
			}
			assert.Equal(t, CalculateDesiredReplicas(context.Background(), &cluster), tc.answer)
		})
	}
}

func TestUnmarshalRuntimeEnv(t *testing.T) {
	tests := []struct {
		name           string
		runtimeEnvYAML string
		isErrorNil     bool
	}{
		{
			name:           "Empty runtimeEnvYAML",
			runtimeEnvYAML: "",
			isErrorNil:     true,
		},
		{
			name: "Valid runtimeEnvYAML",
			runtimeEnvYAML: `
env_vars:
  counter_name: test_counter
`,
			isErrorNil: true,
		},
		{
			name:           "Invalid runtimeEnvYAML",
			runtimeEnvYAML: `invalid_yaml_str`,
			isErrorNil:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := UnmarshalRuntimeEnvYAML(tc.runtimeEnvYAML)
			if tc.isErrorNil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestFindHeadPodReadyCondition(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected metav1.Condition
	}{
		{
			name: "condition true if Ray head pod is running and ready",
			pod:  createRayHeadPodWithPhaseAndCondition(corev1.PodRunning, corev1.PodReady, corev1.ConditionTrue),
			expected: metav1.Condition{
				Type:   string(rayv1.HeadPodReady),
				Status: metav1.ConditionTrue,
			},
		},
		{
			name: "condition false if Ray head pod is not running",
			pod:  createRayHeadPodWithPhaseAndCondition(corev1.PodPending, corev1.PodReady, corev1.ConditionFalse),
			expected: metav1.Condition{
				Type:   string(rayv1.HeadPodReady),
				Status: metav1.ConditionFalse,
			},
		},
		{
			name: "condition false if Ray head pod is not ready",
			pod:  createRayHeadPodWithPhaseAndCondition(corev1.PodRunning, corev1.PodReady, corev1.ConditionFalse),
			expected: metav1.Condition{
				Type:   string(rayv1.HeadPodReady),
				Status: metav1.ConditionFalse,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			headPodReadyCondition := FindHeadPodReadyCondition(tc.pod)
			assert.Equal(t, tc.expected.Status, headPodReadyCondition.Status)
		})
	}
}

func TestErrRayClusterReplicaFailureReason(t *testing.T) {
	assert.Equal(t, "FailedDeleteAllPods", RayClusterReplicaFailureReason(ErrFailedDeleteAllPods))
	assert.Equal(t, "FailedDeleteHeadPod", RayClusterReplicaFailureReason(ErrFailedDeleteHeadPod))
	assert.Equal(t, "FailedCreateHeadPod", RayClusterReplicaFailureReason(ErrFailedCreateHeadPod))
	assert.Equal(t, "FailedDeleteWorkerPod", RayClusterReplicaFailureReason(ErrFailedDeleteWorkerPod))
	assert.Equal(t, "FailedCreateWorkerPod", RayClusterReplicaFailureReason(ErrFailedCreateWorkerPod))
	assert.Equal(t, "FailedDeleteAllPods", RayClusterReplicaFailureReason(errors.Join(ErrFailedDeleteAllPods, errors.New("other error"))))
	assert.Equal(t, "FailedDeleteHeadPod", RayClusterReplicaFailureReason(errors.Join(ErrFailedDeleteHeadPod, errors.New("other error"))))
	assert.Equal(t, "FailedCreateHeadPod", RayClusterReplicaFailureReason(errors.Join(ErrFailedCreateHeadPod, errors.New("other error"))))
	assert.Equal(t, "FailedDeleteWorkerPod", RayClusterReplicaFailureReason(errors.Join(ErrFailedDeleteWorkerPod, errors.New("other error"))))
	assert.Equal(t, "FailedCreateWorkerPod", RayClusterReplicaFailureReason(errors.Join(ErrFailedCreateWorkerPod, errors.New("other error"))))
	assert.Empty(t, RayClusterReplicaFailureReason(errors.New("other error")))
}

func TestIsAutoscalingEnabled(t *testing.T) {
	tests := map[string]struct {
		spec     *rayv1.RayClusterSpec
		expected bool
	}{
		"should be false when spec is nil": {
			spec:     nil,
			expected: false,
		},
		"should be false when enableInTreeAutoscaling is nil": {
			spec: &rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: nil,
			},
			expected: false,
		},
		"should be false when enableInTreeAutoscaling is false": {
			spec: &rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(false),
			},
			expected: false,
		},
		"should be true when enableInTreeAutoscaling is true": {
			spec: &rayv1.RayClusterSpec{
				EnableInTreeAutoscaling: ptr.To(true),
			},
			expected: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.expected, IsAutoscalingEnabled(tc.spec))
		})
	}
}

func TestIsAutoscalingV2Enabled(t *testing.T) {
	tests := map[string]struct {
		spec     *rayv1.RayClusterSpec
		expected bool
	}{
		"should be false when spec is nil": {
			spec:     nil,
			expected: false,
		},
		"should be false when autoscaler options is nil": {
			spec: &rayv1.RayClusterSpec{
				AutoscalerOptions: nil,
			},
			expected: false,
		},
		"should be false when autoscaler options is not v2": {
			spec: &rayv1.RayClusterSpec{
				AutoscalerOptions: &rayv1.AutoscalerOptions{Version: ptr.To(rayv1.AutoscalerVersionV1)},
			},
			expected: false,
		},
		"should be true when autoscaler options is v2": {
			spec: &rayv1.RayClusterSpec{
				AutoscalerOptions: &rayv1.AutoscalerOptions{Version: ptr.To(rayv1.AutoscalerVersionV2)},
			},
			expected: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.expected, IsAutoscalingV2Enabled(tc.spec))
		})
	}
}

func TestIsGCSFaultToleranceEnabled(t *testing.T) {
	tests := []struct {
		name     string
		instance rayv1.RayCluster
		expected bool
	}{
		{
			name: "ray.io/ft-enabled is true",
			instance: rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						RayFTEnabledAnnotationKey: "true",
					},
				},
			},
			expected: true,
		},
		{
			name: "ray.io/ft-enabled is not set and GcsFaultToleranceOptions is set",
			instance: rayv1.RayCluster{
				Spec: rayv1.RayClusterSpec{
					GcsFaultToleranceOptions: &rayv1.GcsFaultToleranceOptions{},
				},
			},
			expected: true,
		},
		{
			name: "ray.io/ft-enabled is false",
			instance: rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						RayFTEnabledAnnotationKey: "false",
					},
				},
			},
			expected: false,
		},
		{
			name: "ray.io/ft-enabled is not set and GcsFaultToleranceOptions is not set",
			instance: rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expected: false,
		},
		{
			name: "ray.io/ft-enabled is using uppercase true",
			instance: rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						RayFTEnabledAnnotationKey: "TRUE",
					},
				},
			},
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := IsGCSFaultToleranceEnabled(&test.instance.Spec, test.instance.Annotations)
			assert.Equal(t, test.expected, result)
		})
	}
}

func createPodSpec(cpu, memory string) corev1.PodSpec {
	return corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse(cpu),
						corev1.ResourceMemory: resource.MustParse(memory),
					},
				},
			},
		},
	}
}

func createRayClusterTemplate(
	head struct {
		cpu    string
		memory string
	},
	workers []struct {
		replicas    *int32
		minReplicas *int32
		suspend     *bool
		cpu         string
		memory      string
		numOfHosts  int32
	},
) *rayv1.RayCluster {
	cluster := &rayv1.RayCluster{
		Spec: rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{
				Template: corev1.PodTemplateSpec{
					Spec: createPodSpec(head.cpu, head.memory),
				},
			},
		},
	}

	for _, w := range workers {
		cluster.Spec.WorkerGroupSpecs = append(cluster.Spec.WorkerGroupSpecs, rayv1.WorkerGroupSpec{
			NumOfHosts:  w.numOfHosts,
			Replicas:    w.replicas,
			MinReplicas: w.minReplicas,
			Suspend:     w.suspend,
			Template: corev1.PodTemplateSpec{
				Spec: createPodSpec(w.cpu, w.memory),
			},
		})
	}

	return cluster
}

func TestCalculateResources(t *testing.T) {
	headStruct := struct {
		cpu    string
		memory string
	}{
		cpu:    "1",
		memory: "100Mi",
	}

	tests := []struct {
		expected struct {
			desiredResources corev1.ResourceList
			minResources     corev1.ResourceList
		}
		cluster *rayv1.RayCluster
		name    string
	}{
		{
			name:    "Single head pod with no worker groups",
			cluster: createRayClusterTemplate(headStruct, nil),
			expected: struct {
				desiredResources corev1.ResourceList
				minResources     corev1.ResourceList
			}{
				desiredResources: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("100Mi"),
				},
				minResources: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("100Mi"),
				},
			},
		},
		{
			name: "Head pod with one worker group",
			cluster: createRayClusterTemplate(headStruct, []struct {
				replicas    *int32
				minReplicas *int32
				suspend     *bool
				cpu         string
				memory      string
				numOfHosts  int32
			}{
				{
					numOfHosts:  2,
					replicas:    ptr.To[int32](4),
					minReplicas: ptr.To[int32](3),
					cpu:         "1",
					memory:      "200Mi",
					suspend:     nil,
				},
			}),
			expected: struct {
				desiredResources corev1.ResourceList
				minResources     corev1.ResourceList
			}{
				desiredResources: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("9"),
					corev1.ResourceMemory: resource.MustParse("1700Mi"),
				},
				minResources: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("7"),
					corev1.ResourceMemory: resource.MustParse("1300Mi"),
				},
			},
		},
		{
			name: "Head pod with two worker group with different resources",
			cluster: createRayClusterTemplate(headStruct, []struct {
				replicas    *int32
				minReplicas *int32
				suspend     *bool
				cpu         string
				memory      string
				numOfHosts  int32
			}{
				{
					numOfHosts:  2,
					replicas:    ptr.To[int32](2),
					minReplicas: ptr.To[int32](1),
					cpu:         "2",
					memory:      "100Mi",
					suspend:     nil,
				},
				{
					numOfHosts:  1,
					replicas:    ptr.To[int32](3),
					minReplicas: ptr.To[int32](0),
					cpu:         "1",
					memory:      "200Mi",
					suspend:     nil,
				},
			}),
			expected: struct {
				desiredResources corev1.ResourceList
				minResources     corev1.ResourceList
			}{
				desiredResources: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("12"),
					corev1.ResourceMemory: resource.MustParse("1100Mi"),
				},
				minResources: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("5"),
					corev1.ResourceMemory: resource.MustParse("300Mi"),
				},
			},
		},
		{
			name: "Head pod with suspended worker group",
			cluster: createRayClusterTemplate(headStruct, []struct {
				replicas    *int32
				minReplicas *int32
				suspend     *bool
				cpu         string
				memory      string
				numOfHosts  int32
			}{
				{
					numOfHosts:  2,
					replicas:    ptr.To[int32](3),
					minReplicas: ptr.To[int32](0),
					cpu:         "2",
					memory:      "200Mi",
					suspend:     ptr.To(true),
				},
			}),
			expected: struct {
				desiredResources corev1.ResourceList
				minResources     corev1.ResourceList
			}{
				desiredResources: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("100Mi"),
				},
				minResources: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("100Mi"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deepCopyCluster := tt.cluster.DeepCopy()

			desiredResource := CalculateDesiredResources(tt.cluster)
			assert.Equal(t, tt.expected.desiredResources.Cpu().String(), desiredResource.Cpu().String())
			assert.Equal(t, tt.expected.desiredResources.Memory().String(), desiredResource.Memory().String())
			assert.Equal(t, deepCopyCluster, tt.cluster)

			minResource := CalculateMinResources(tt.cluster)
			assert.Equal(t, tt.expected.minResources.Cpu().String(), minResource.Cpu().String())
			assert.Equal(t, tt.expected.minResources.Memory().String(), minResource.Memory().String())
			assert.Equal(t, deepCopyCluster, tt.cluster)
		})
	}
}

func TestGetContainerCommand(t *testing.T) {
	tests := []struct {
		name              string
		additionalOptions []string
		expected          []string
		enableLoginShell  bool
	}{
		{
			name:              "enable login shell is false",
			enableLoginShell:  false,
			additionalOptions: []string{},
			expected:          []string{"/bin/bash", "-c", "--"},
		},
		{
			name:              "enable login shell is true",
			enableLoginShell:  true,
			additionalOptions: []string{},
			expected:          []string{"/bin/bash", "-cl", "--"},
		},
		{
			name:              "enable login shell is false and additional options is not empty",
			enableLoginShell:  false,
			additionalOptions: []string{"e"},
			expected:          []string{"/bin/bash", "-ce", "--"},
		},
		{
			name:              "enable login shell is true and additional options is not empty",
			enableLoginShell:  true,
			additionalOptions: []string{"e"},
			expected:          []string{"/bin/bash", "-cel", "--"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.enableLoginShell {
				os.Setenv("ENABLE_LOGIN_SHELL", "true")
				defer os.Unsetenv("ENABLE_LOGIN_SHELL")
			}
			assert.Equal(t, test.expected, GetContainerCommand(test.additionalOptions))
		})
	}
}
