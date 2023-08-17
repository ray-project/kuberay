package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
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

func TestBefore(t *testing.T) {
	if Before("a", "b") != "" {
		t.Fail()
	}

	if Before("aaa", "a") != "" {
		t.Fail()
	}

	if Before("aab", "b") != "aa" {
		t.Fail()
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
	pods := []corev1.Pod{
		*createSomePod(),
		*createSomePod(),
	}
	pods[0].Status.Phase = corev1.PodPending
	pods[1].Status.Phase = corev1.PodRunning
	podList1 := corev1.PodList{
		Items: pods,
	}
	if CheckAllPodsRunning(podList1) {
		t.Fail()
	}
	podList2 := corev1.PodList{}
	if CheckAllPodsRunning(podList2) {
		t.Fail()
	}
}

func TestCheckName(t *testing.T) {
	// test 1 -> change
	str := "72fbcc7e-a661-4b18e-ca41-e903-fc3ae634b18e-lazer090scholar-director-s"
	str = CheckName(str)
	if str != "rca41-e903-fc3ae634b18e-lazer090scholar-director-s" {
		t.Fail()
	}
	// test 2 -> change
	str = "--------566666--------444433-----------222222----------4444"
	str = CheckName(str)
	if str != "r6666--------444433-----------222222----------4444" {
		t.Fail()
	}

	// test 3 -> keep
	str = "acceptable-name-head-12345"
	str = CheckName(str)
	if str != "acceptable-name-head-12345" {
		t.Fail()
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

func TestGetHeadGroupServiceAccountName(t *testing.T) {
	tests := map[string]struct {
		input *rayv1alpha1.RayCluster
		want  string
	}{
		"Ray cluster with head group service account": {
			input: &rayv1alpha1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "raycluster-sample",
					Namespace: "default",
				},
				Spec: rayv1alpha1.RayClusterSpec{
					HeadGroupSpec: rayv1alpha1.HeadGroupSpec{
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
		"Ray cluster without head group service account": {
			input: &rayv1alpha1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "raycluster-sample",
					Namespace: "default",
				},
				Spec: rayv1alpha1.RayClusterSpec{
					HeadGroupSpec: rayv1alpha1.HeadGroupSpec{
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{},
						},
					},
				},
			},
			want: "raycluster-sample",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := GetHeadGroupServiceAccountName(tc.input)
			if got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestReconcile_CheckNeedRemoveOldPod(t *testing.T) {
	namespaceStr := "default"

	headTemplate := corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "ray-head",
					Image:   "rayproject/autoscaler",
					Command: []string{"python"},
					Args:    []string{"/opt/code.py"},
					Env: []corev1.EnvVar{
						{
							Name: "MY_POD_IP",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: "status.podIP",
								},
							},
						},
					},
				},
			},
		},
	}

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "headNode",
			Namespace: namespaceStr,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "ray-head",
					Image:   "rayproject/autoscaler",
					Command: []string{"python"},
					Args:    []string{"/opt/code.py"},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	assert.Equal(t, PodNotMatchingTemplate(pod, headTemplate), false, "expect template & pod matching")

	pod.Spec.Containers = []corev1.Container{
		{
			Name:    "ray-head",
			Image:   "rayproject/autoscaler",
			Command: []string{"python"},
			Args:    []string{"/opt/code.py"},
		},
		{
			Name:    "ray-head",
			Image:   "rayproject/autoscaler",
			Command: []string{"python"},
			Args:    []string{"/opt/code.py"},
		},
	}

	assert.Equal(t, PodNotMatchingTemplate(pod, headTemplate), true, "expect template & pod with 2 containers not matching")

	workerTemplate := corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "ray-worker",
					Image:   "rayproject/autoscaler",
					Command: []string{"echo"},
					Args:    []string{"Hello Ray"},
					Env: []corev1.EnvVar{
						{
							Name: "MY_POD_IP",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: "status.podIP",
								},
							},
						},
					},
				},
			},
		},
	}

	pod = corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: namespaceStr,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "ray-worker",
					Image:   "rayproject/autoscaler",
					Command: []string{"echo"},
					Args:    []string{"Hello Ray"},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	assert.Equal(t, PodNotMatchingTemplate(pod, workerTemplate), false, "expect template & pod matching")

	workerTemplate = corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "ray-worker",
					Image:   "rayproject/autoscaler",
					Command: []string{"echo"},
					Args:    []string{"Hello Ray"},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("256m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
			},
		},
	}

	pod = corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod1",
			Namespace: namespaceStr,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "ray-worker",
					Image:   "rayproject/autoscaler",
					Command: []string{"echo"},
					Args:    []string{"Hello Ray"},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("256m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	assert.Equal(t, PodNotMatchingTemplate(pod, workerTemplate), false, "expect template & pod matching")

	pod.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("50m")

	assert.Equal(t, PodNotMatchingTemplate(pod, workerTemplate), true, "expect template & pod not matching")

	pod.Spec.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("500m")
	pod.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("250m")

	assert.Equal(t, PodNotMatchingTemplate(pod, workerTemplate), true, "expect template & pod not matching")
}

func TestCalculateAvailableReplicas(t *testing.T) {
	podList := corev1.PodList{
		Items: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod1",
					Labels: map[string]string{
						"ray.io/node-type": string(rayv1alpha1.HeadNode),
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
						"ray.io/node-type": string(rayv1alpha1.WorkerNode),
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
						"ray.io/node-type": string(rayv1alpha1.WorkerNode),
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pod2",
					Labels: map[string]string{
						"ray.io/node-type": string(rayv1alpha1.WorkerNode),
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodFailed,
				},
			},
		},
	}
	count := CalculateAvailableReplicas(podList)
	assert.Equal(t, count, int32(1), "expect 1 available replica")
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
	headSvcName, err := GenerateHeadServiceName(RayClusterCRD, rayv1alpha1.RayClusterSpec{}, "raycluster-sample")
	expectedGeneratedSvcName := "raycluster-sample-head-svc"
	assert.Nil(t, err)
	assert.Equal(t, headSvcName, expectedGeneratedSvcName)

	// Test 2: `HeadService.Name` is not empty.
	clusterSpecWithHeadService := rayv1alpha1.RayClusterSpec{
		HeadGroupSpec: rayv1alpha1.HeadGroupSpec{
			HeadService: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-head-svc",
				},
			},
		},
	}

	headSvcName, err = GenerateHeadServiceName(RayClusterCRD, *clusterSpecWithHeadService.DeepCopy(), "raycluster-sample")
	assert.Nil(t, err)
	assert.Equal(t, headSvcName, "my-head-svc")

	// [RayService]
	// Test 3: `HeadService.Name` is empty.
	headSvcName, err = GenerateHeadServiceName(RayServiceCRD, rayv1alpha1.RayClusterSpec{}, "rayservice-sample")
	expectedGeneratedSvcName = "rayservice-sample-head-svc"
	assert.Nil(t, err)
	assert.Equal(t, headSvcName, expectedGeneratedSvcName)

	// Test 4: `HeadService.Name` is not empty.
	headSvcName, err = GenerateHeadServiceName(RayServiceCRD, *clusterSpecWithHeadService.DeepCopy(), "rayservice-sample")
	assert.Nil(t, err)
	assert.Equal(t, headSvcName, expectedGeneratedSvcName)

	// Invalid CRD type
	_, err = GenerateHeadServiceName(RayJobCRD, rayv1alpha1.RayClusterSpec{}, "rayjob-sample")
	assert.NotNil(t, err)
}
