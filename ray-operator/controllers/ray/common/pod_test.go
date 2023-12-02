package common

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/stretchr/testify/assert"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var testMemoryLimit = resource.MustParse("1Gi")

var instance = rayv1.RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
	},
	Spec: rayv1.RayClusterSpec{
		HeadGroupSpec: rayv1.HeadGroupSpec{
			RayStartParams: map[string]string{
				"port":                "6379",
				"object-manager-port": "12345",
				"node-manager-port":   "12346",
				"object-store-memory": "100000000",
				"num-cpus":            "1",
				"include-dashboard":   "true",
				"log-color":           "true",
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Labels: map[string]string{
						"ray.io/cluster": "raycluster-sample",
						"ray.io/group":   "headgroup",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "ray-head",
							Image: "repo/image:custom",
							Env: []v1.EnvVar{
								{
									Name: "MY_POD_IP",
									ValueFrom: &v1.EnvVarSource{
										FieldRef: &v1.ObjectFieldSelector{
											FieldPath: "status.podIP",
										},
									},
								},
								{
									Name:  "TEST_ENV_NAME",
									Value: "TEST_ENV_VALUE",
								},
							},
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: testMemoryLimit,
								},
								Limits: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: testMemoryLimit,
								},
							},
						},
					},
				},
			},
		},
		WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
			{
				Replicas:    pointer.Int32(3),
				MinReplicas: pointer.Int32(0),
				MaxReplicas: pointer.Int32(10000),
				GroupName:   "small-group",
				RayStartParams: map[string]string{
					"port":     "6379",
					"num-cpus": "1",
				},
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "ray-worker",
								Image: "repo/image:custom",
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("1"),
										v1.ResourceMemory: testMemoryLimit,
										"nvidia.com/gpu":  resource.MustParse("3"),
									},
								},
								Env: []v1.EnvVar{
									{
										Name: "MY_POD_IP",
										ValueFrom: &v1.EnvVarSource{
											FieldRef: &v1.ObjectFieldSelector{
												FieldPath: "status.podIP",
											},
										},
									},
									{
										Name:  "TEST_ENV_NAME",
										Value: "TEST_ENV_VALUE",
									},
								},
							},
						},
					},
				},
			},
		},
	},
}

var volumesNoAutoscaler = []v1.Volume{
	{
		Name: "shared-mem",
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{
				Medium:    v1.StorageMediumMemory,
				SizeLimit: &testMemoryLimit,
			},
		},
	},
}

var volumesWithAutoscaler = []v1.Volume{
	{
		Name: "shared-mem",
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{
				Medium:    v1.StorageMediumMemory,
				SizeLimit: &testMemoryLimit,
			},
		},
	},
	{
		Name: "ray-logs",
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{
				Medium: v1.StorageMediumDefault,
			},
		},
	},
}

var volumeMountsNoAutoscaler = []v1.VolumeMount{
	{
		Name:      "shared-mem",
		MountPath: "/dev/shm",
		ReadOnly:  false,
	},
}

var volumeMountsWithAutoscaler = []v1.VolumeMount{
	{
		Name:      "shared-mem",
		MountPath: "/dev/shm",
		ReadOnly:  false,
	},
	{
		Name:      "ray-logs",
		MountPath: "/tmp/ray",
		ReadOnly:  false,
	},
}

var autoscalerContainer = v1.Container{
	Name:            "autoscaler",
	Image:           "repo/image:custom",
	ImagePullPolicy: v1.PullIfNotPresent,
	Env: []v1.EnvVar{
		{
			Name: RAY_CLUSTER_NAME,
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					FieldPath: fmt.Sprintf("metadata.labels['%s']", RayClusterLabelKey),
				},
			},
		},
		{
			Name: "RAY_CLUSTER_NAMESPACE",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
		{
			Name: "RAY_HEAD_POD_NAME",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name:  "KUBERAY_CRD_VER",
			Value: "v1",
		},
	},
	Command: []string{
		"ray",
	},
	Args: []string{
		"kuberay-autoscaler",
		"--cluster-name",
		"$(RAY_CLUSTER_NAME)",
		"--cluster-namespace",
		"$(RAY_CLUSTER_NAMESPACE)",
	},
	Resources: v1.ResourceRequirements{
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("500m"),
			v1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("500m"),
			v1.ResourceMemory: resource.MustParse("512Mi"),
		},
	},
	VolumeMounts: []v1.VolumeMount{
		{
			MountPath: "/tmp/ray",
			Name:      "ray-logs",
		},
	},
}

var trueFlag = true

func TestAddEmptyDirVolumes(t *testing.T) {
	testPod := &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "ray-worker",
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      "shared-mem",
							MountPath: "/dev/shm",
						},
					},
				},
			},
			Volumes: []v1.Volume{
				{
					Name: "shared-mem",
					VolumeSource: v1.VolumeSource{
						EmptyDir: &v1.EmptyDirVolumeSource{
							Medium: v1.StorageMediumMemory,
						},
					},
				},
			},
		},
	}
	assert.Equal(t, len(testPod.Spec.Containers[0].VolumeMounts), 1)
	assert.Equal(t, len(testPod.Spec.Volumes), 1)
	addEmptyDir(&testPod.Spec.Containers[0], testPod, "shared-mem2", "/dev/shm2", v1.StorageMediumDefault)
	assert.Equal(t, len(testPod.Spec.Containers[0].VolumeMounts), 2)
	assert.Equal(t, len(testPod.Spec.Volumes), 2)
	addEmptyDir(&testPod.Spec.Containers[0], testPod, "shared-mem2", "/dev/shm2", v1.StorageMediumDefault)
	assert.Equal(t, len(testPod.Spec.Containers[0].VolumeMounts), 2)
	assert.Equal(t, len(testPod.Spec.Volumes), 2)
}

func TestGetHeadPort(t *testing.T) {
	headStartParams := make(map[string]string)
	actualResult := GetHeadPort(headStartParams)
	expectedResult := "6379"
	if !(actualResult == expectedResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	headStartParams["port"] = "9999"
	actualResult = GetHeadPort(headStartParams)
	expectedResult = "9999"
	if actualResult != expectedResult {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
}

func getEnvVar(container v1.Container, envName string) *v1.EnvVar {
	for _, env := range container.Env {
		if env.Name == envName {
			return &env
		}
	}
	return nil
}

func checkContainerEnv(t *testing.T, container v1.Container, envName string, expectedValue string) {
	env := getEnvVar(container, envName)
	if env != nil {
		if env.Value != "" {
			if env.Value != expectedValue {
				t.Fatalf("Expected `%v` but got `%v`", expectedValue, env.Value)
			}
		} else {
			// env.ValueFrom is the source for the environment variable's value. Cannot be used if value is not empty.
			// See https://pkg.go.dev/k8s.io/api/core/v1#EnvVar for more details.
			if env.ValueFrom.FieldRef.FieldPath != expectedValue {
				t.Fatalf("Expected `%v` but got `%v`", expectedValue, env.ValueFrom.FieldRef.FieldPath)
			}
		}
	} else {
		t.Fatalf("Couldn't find `%v` env on pod.", envName)
	}
}

func TestBuildPod(t *testing.T) {
	cluster := instance.DeepCopy()

	// Test head pod
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayv1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod := BuildPod(podTemplateSpec, rayv1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, "6379", nil, "", "", false)

	// Check environment variables
	rayContainer := pod.Spec.Containers[RayContainerIndex]
	checkContainerEnv(t, rayContainer, RAY_ADDRESS, "127.0.0.1:6379")
	checkContainerEnv(t, rayContainer, RAY_USAGE_STATS_KUBERAY_IN_USE, "1")
	checkContainerEnv(t, rayContainer, RAY_CLUSTER_NAME, fmt.Sprintf("metadata.labels['%s']", RayClusterLabelKey))
	checkContainerEnv(t, rayContainer, RAY_DASHBOARD_ENABLE_K8S_DISK_USAGE, "1")
	headRayStartCommandEnv := getEnvVar(rayContainer, KUBERAY_GEN_RAY_START_CMD)
	assert.True(t, strings.Contains(headRayStartCommandEnv.Value, "ray start"))

	// In head, init container needs FQ_RAY_IP to create a self-signed certificate for its TLS authenticate.
	for _, initContainer := range pod.Spec.InitContainers {
		checkContainerEnv(t, initContainer, FQ_RAY_IP, "raycluster-sample-head-svc.default.svc.cluster.local")
		checkContainerEnv(t, rayContainer, RAY_IP, "raycluster-sample-head-svc")
	}

	// Check labels.
	actualResult := pod.Labels[RayClusterLabelKey]
	expectedResult := cluster.Name
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[RayNodeTypeLabelKey]
	expectedResult = string(rayv1.HeadNode)
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[RayNodeGroupLabelKey]
	expectedResult = "headgroup"
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	// Check volumes.
	actualVolumes := pod.Spec.Volumes
	expectedVolumes := volumesNoAutoscaler
	if !reflect.DeepEqual(actualVolumes, expectedVolumes) {
		t.Fatalf("Expected `%v` but got `%v`", expectedVolumes, actualVolumes)
	}

	// Check volume mounts.
	actualVolumeMounts := pod.Spec.Containers[0].VolumeMounts
	expectedVolumeMounts := volumeMountsNoAutoscaler
	if !reflect.DeepEqual(actualVolumeMounts, expectedVolumeMounts) {
		t.Fatalf("Expected `%v` but got `%v`", expectedVolumeMounts, actualVolumeMounts)
	}

	// testing worker pod
	worker := cluster.Spec.WorkerGroupSpecs[0]
	podName = cluster.Name + DashSymbol + string(rayv1.WorkerNode) + DashSymbol + worker.GroupName + DashSymbol + utils.FormatInt32(0)
	fqdnRayIP := utils.GenerateFQDNServiceName(*cluster, cluster.Namespace)
	podTemplateSpec = DefaultWorkerPodTemplate(*cluster, worker, podName, fqdnRayIP, "6379")
	pod = BuildPod(podTemplateSpec, rayv1.WorkerNode, worker.RayStartParams, "6379", nil, "", fqdnRayIP, false)

	// Check environment variables
	rayContainer = pod.Spec.Containers[RayContainerIndex]
	checkContainerEnv(t, rayContainer, RAY_ADDRESS, "raycluster-sample-head-svc.default.svc.cluster.local:6379")
	checkContainerEnv(t, rayContainer, FQ_RAY_IP, "raycluster-sample-head-svc.default.svc.cluster.local")
	checkContainerEnv(t, rayContainer, RAY_IP, "raycluster-sample-head-svc")
	checkContainerEnv(t, rayContainer, RAY_CLUSTER_NAME, fmt.Sprintf("metadata.labels['%s']", RayClusterLabelKey))
	checkContainerEnv(t, rayContainer, RAY_DASHBOARD_ENABLE_K8S_DISK_USAGE, "1")
	workerRayStartCommandEnv := getEnvVar(rayContainer, KUBERAY_GEN_RAY_START_CMD)
	assert.True(t, strings.Contains(workerRayStartCommandEnv.Value, "ray start"))

	expectedCommandArg := splitAndSort("ulimit -n 65536; ray start --block --memory=1073741824 --num-cpus=1 --num-gpus=3 --address=raycluster-sample-head-svc.default.svc.cluster.local:6379 --port=6379 --metrics-export-port=8080")
	actualCommandArg := splitAndSort(pod.Spec.Containers[0].Args[0])
	if !reflect.DeepEqual(expectedCommandArg, actualCommandArg) {
		t.Fatalf("Expected `%v` but got `%v`", expectedCommandArg, actualCommandArg)
	}

	// Check Envs
	rayContainer = pod.Spec.Containers[RayContainerIndex]
	checkContainerEnv(t, rayContainer, "TEST_ENV_NAME", "TEST_ENV_VALUE")

	// Try to build pod for serve
	pod = BuildPod(podTemplateSpec, rayv1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, "6379", nil, "", "", true)
	val, ok := pod.Labels[RayClusterServingServiceLabelKey]
	assert.True(t, ok, "Expected serve label is not present")
	assert.Equal(t, EnableRayClusterServingServiceTrue, val, "Wrong serve label value")
}

func TestBuildPod_WithOverwriteCommand(t *testing.T) {
	cluster := instance.DeepCopy()
	cluster.Annotations = map[string]string{
		// When the value of the annotation is "true", KubeRay will not generate the command and args for the container.
		// Instead, it will use the command and args specified by the use.
		RayOverwriteContainerCmdAnnotationKey: "true",
	}
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[RayContainerIndex].Command = []string{"I am head"}
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[RayContainerIndex].Args = []string{"I am head again"}
	cluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[RayContainerIndex].Command = []string{"I am worker"}
	cluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[RayContainerIndex].Args = []string{"I am worker again"}

	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayv1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	headPod := BuildPod(podTemplateSpec, rayv1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, "6379", nil, "", "", false)
	headContainer := headPod.Spec.Containers[RayContainerIndex]
	assert.Equal(t, headContainer.Command, []string{"I am head"})
	assert.Equal(t, headContainer.Args, []string{"I am head again"})

	worker := cluster.Spec.WorkerGroupSpecs[0]
	podName = cluster.Name + DashSymbol + string(rayv1.WorkerNode) + DashSymbol + worker.GroupName + DashSymbol + utils.FormatInt32(0)
	fqdnRayIP := utils.GenerateFQDNServiceName(*cluster, cluster.Namespace)
	podTemplateSpec = DefaultWorkerPodTemplate(*cluster, worker, podName, fqdnRayIP, "6379")
	workerPod := BuildPod(podTemplateSpec, rayv1.WorkerNode, worker.RayStartParams, "6379", nil, "", fqdnRayIP, false)
	workerContainer := workerPod.Spec.Containers[RayContainerIndex]
	assert.Equal(t, workerContainer.Command, []string{"I am worker"})
	assert.Equal(t, workerContainer.Args, []string{"I am worker again"})
}

func TestBuildPod_WithAutoscalerEnabled(t *testing.T) {
	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayv1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod := BuildPod(podTemplateSpec, rayv1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, "6379", &trueFlag, "", "", false)

	actualResult := pod.Labels[RayClusterLabelKey]
	expectedResult := cluster.Name
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[RayNodeTypeLabelKey]
	expectedResult = string(rayv1.HeadNode)
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[RayNodeGroupLabelKey]
	expectedResult = "headgroup"
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	// verify no-monitoring is set. conversion only happens in BuildPod so we can only check it here.
	expectedResult = "--no-monitor"
	if !strings.Contains(pod.Spec.Containers[0].Args[0], expectedResult) {
		t.Fatalf("Expected `%v` in `%v` but doesn't have the config", expectedResult, pod.Spec.Containers[0].Args[0])
	}

	actualVolumes := pod.Spec.Volumes
	expectedVolumes := volumesWithAutoscaler
	if !reflect.DeepEqual(actualVolumes, expectedVolumes) {
		t.Fatalf("Expected `%v` but got `%v`", actualVolumes, expectedVolumes)
	}

	actualVolumeMounts := pod.Spec.Containers[0].VolumeMounts
	expectedVolumeMounts := volumeMountsWithAutoscaler
	if !reflect.DeepEqual(actualVolumeMounts, expectedVolumeMounts) {
		t.Fatalf("Expected `%v` but got `%v`", expectedVolumeMounts, actualVolumeMounts)
	}

	// Make sure autoscaler container was formatted correctly.
	numContainers := len(pod.Spec.Containers)
	expectedNumContainers := 2
	if !(numContainers == expectedNumContainers) {
		t.Fatalf("Expected `%v` container but got `%v`", expectedNumContainers, numContainers)
	}
	index := getAutoscalerContainerIndex(pod)
	actualContainer := pod.Spec.Containers[index]
	expectedContainer := autoscalerContainer
	if !reflect.DeepEqual(expectedContainer, actualContainer) {
		t.Fatalf("Expected `%v` but got `%v`", expectedContainer, actualContainer)
	}
}

func TestBuildPod_WithCreatedByRayService(t *testing.T) {
	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayv1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod := BuildPod(podTemplateSpec, rayv1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, "6379", &trueFlag, RayServiceCreatorLabelValue, "", false)

	hasCorrectDeathEnv := false
	for _, container := range pod.Spec.Containers {
		if container.Name != "ray-head" {
			continue
		}
		if container.Env == nil || len(container.Env) == 0 {
			t.Fatalf("Expected death env `%v`", container)
		}
		for _, env := range container.Env {
			if env.Name == RAY_TIMEOUT_MS_TASK_WAIT_FOR_DEATH_INFO {
				assert.Equal(t, "0", env.Value)
				hasCorrectDeathEnv = true
				break
			}
		}
	}
	assert.True(t, hasCorrectDeathEnv)
}

func TestBuildPod_WithGcsFtEnabled(t *testing.T) {
	// Test 1
	cluster := instance.DeepCopy()
	cluster.Annotations = map[string]string{
		RayFTEnabledAnnotationKey: "true",
	}

	// Build a head Pod.
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayv1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod := BuildPod(podTemplateSpec, rayv1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, "6379", nil, "", "", false)

	// Check environment variable "RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S"
	rayContainer := pod.Spec.Containers[RayContainerIndex]

	// "RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S" should not be set on the head Pod by default
	assert.True(t, !envVarExists(RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S, rayContainer.Env))

	// Test 2
	cluster = instance.DeepCopy()
	cluster.Annotations = map[string]string{
		RayFTEnabledAnnotationKey: "true",
	}

	// Add "RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S" env var in the head group spec.
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[RayContainerIndex].Env = append(cluster.Spec.HeadGroupSpec.Template.Spec.Containers[RayContainerIndex].Env,
		v1.EnvVar{Name: RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S, Value: "60"})
	podTemplateSpec = DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod = BuildPod(podTemplateSpec, rayv1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, "6379", nil, "", "", false)
	rayContainer = pod.Spec.Containers[RayContainerIndex]

	// Check environment variable "RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S"
	checkContainerEnv(t, rayContainer, RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S, "60")

	// Test 3
	cluster = instance.DeepCopy()
	cluster.Annotations = map[string]string{
		RayFTEnabledAnnotationKey: "true",
	}

	// Build a worker pod
	worker := cluster.Spec.WorkerGroupSpecs[0]
	podName = cluster.Name + DashSymbol + string(rayv1.WorkerNode) + DashSymbol + worker.GroupName + DashSymbol + utils.FormatInt32(0)
	fqdnRayIP := utils.GenerateFQDNServiceName(*cluster, cluster.Namespace)
	podTemplateSpec = DefaultWorkerPodTemplate(*cluster, worker, podName, fqdnRayIP, "6379")
	pod = BuildPod(podTemplateSpec, rayv1.WorkerNode, worker.RayStartParams, "6379", nil, "", fqdnRayIP, false)

	// Check the default value of "RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S"
	rayContainer = pod.Spec.Containers[RayContainerIndex]
	checkContainerEnv(t, rayContainer, RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S, DefaultWorkerRayGcsReconnectTimeoutS)

	// Test 4
	cluster = instance.DeepCopy()
	cluster.Annotations = map[string]string{
		RayFTEnabledAnnotationKey: "true",
	}

	// Add "RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S" env var in the worker group spec.
	cluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[RayContainerIndex].Env = append(cluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[RayContainerIndex].Env,
		v1.EnvVar{Name: RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S, Value: "120"})
	worker = cluster.Spec.WorkerGroupSpecs[0]
	podTemplateSpec = DefaultWorkerPodTemplate(*cluster, worker, podName, fqdnRayIP, "6379")
	pod = BuildPod(podTemplateSpec, rayv1.WorkerNode, worker.RayStartParams, "6379", nil, "", fqdnRayIP, false)

	// Check the default value of "RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S"
	rayContainer = pod.Spec.Containers[RayContainerIndex]
	checkContainerEnv(t, rayContainer, RAY_GCS_RPC_SERVER_RECONNECT_TIMEOUT_S, "120")
}

// Check that autoscaler container overrides work as expected.
func TestBuildPodWithAutoscalerOptions(t *testing.T) {
	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayv1.HeadNode) + DashSymbol + utils.FormatInt32(0))

	customAutoscalerImage := "custom-autoscaler-xxx"
	customPullPolicy := v1.PullIfNotPresent
	customTimeout := int32(100)
	customUpscaling := rayv1.UpscalingMode("Aggressive")
	customResources := v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("1"),
			v1.ResourceMemory: testMemoryLimit,
		},
		Limits: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("1"),
			v1.ResourceMemory: testMemoryLimit,
		},
	}
	customEnv := []v1.EnvVar{{Name: "fooEnv", Value: "fooValue"}}
	customEnvFrom := []v1.EnvFromSource{{Prefix: "Pre"}}
	customVolumeMounts := []v1.VolumeMount{
		{
			Name:      "ca-tls",
			MountPath: "/etc/ca/tls",
			ReadOnly:  true,
		},
		{
			Name:      "ray-tls",
			MountPath: "/etc/ray/tls",
		},
	}

	// Define a custom security profile.
	allowPrivilegeEscalation := false
	capabilities := v1.Capabilities{
		Drop: []v1.Capability{"ALL"},
	}
	seccompProfile := v1.SeccompProfile{
		Type: v1.SeccompProfileTypeRuntimeDefault,
	}
	runAsNonRoot := true
	customSecurityContext := v1.SecurityContext{
		AllowPrivilegeEscalation: &allowPrivilegeEscalation,
		Capabilities:             &capabilities,
		RunAsNonRoot:             &runAsNonRoot,
		SeccompProfile:           &seccompProfile,
	}

	cluster.Spec.AutoscalerOptions = &rayv1.AutoscalerOptions{
		UpscalingMode:      &customUpscaling,
		IdleTimeoutSeconds: &customTimeout,
		Image:              &customAutoscalerImage,
		ImagePullPolicy:    &customPullPolicy,
		Resources:          &customResources,
		Env:                customEnv,
		EnvFrom:            customEnvFrom,
		VolumeMounts:       customVolumeMounts,
		SecurityContext:    &customSecurityContext,
	}
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod := BuildPod(podTemplateSpec, rayv1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, "6379", &trueFlag, "", "", false)
	expectedContainer := *autoscalerContainer.DeepCopy()
	expectedContainer.Image = customAutoscalerImage
	expectedContainer.ImagePullPolicy = customPullPolicy
	expectedContainer.Resources = customResources
	expectedContainer.EnvFrom = customEnvFrom
	expectedContainer.Env = append(expectedContainer.Env, customEnv...)
	expectedContainer.VolumeMounts = append(customVolumeMounts, expectedContainer.VolumeMounts...)
	expectedContainer.SecurityContext = &customSecurityContext
	index := getAutoscalerContainerIndex(pod)
	actualContainer := pod.Spec.Containers[index]
	if !reflect.DeepEqual(expectedContainer, actualContainer) {
		t.Fatalf("Expected `%v` but got `%v`", expectedContainer, actualContainer)
	}
}

func TestHeadPodTemplate_WithAutoscalingEnabled(t *testing.T) {
	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayv1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")

	// autoscaler container is injected into head pod
	actualContainerCount := len(podTemplateSpec.Spec.Containers)
	expectedContainerCount := 2
	if !reflect.DeepEqual(expectedContainerCount, actualContainerCount) {
		t.Fatalf("Expected `%v` but got `%v`", expectedContainerCount, actualContainerCount)
	}

	actualResult := podTemplateSpec.Spec.ServiceAccountName
	expectedResult := cluster.Name

	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	// Repeat ServiceAccountName check with long cluster name.
	cluster.Name = longString(t) // 200 chars long
	podTemplateSpec = DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	actualResult = podTemplateSpec.Spec.ServiceAccountName
	expectedResult = shortString(t) // 50 chars long, truncated by utils.CheckName
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
}

func TestHeadPodTemplate_AutoscalerImage(t *testing.T) {
	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	cluster.Spec.AutoscalerOptions = nil
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayv1.HeadNode) + DashSymbol + utils.FormatInt32(0))

	// Case 1: If `AutoscalerOptions.Image` is not set, the Autoscaler container should use the Ray head container's image by default.
	expectedAutoscalerImage := cluster.Spec.HeadGroupSpec.Template.Spec.Containers[RayContainerIndex].Image
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod := v1.Pod{
		Spec: podTemplateSpec.Spec,
	}
	autoscalerContainerIndex := getAutoscalerContainerIndex(pod)
	assert.Equal(t, expectedAutoscalerImage, podTemplateSpec.Spec.Containers[autoscalerContainerIndex].Image)

	// Case 2: If `AutoscalerOptions.Image` is set, the Autoscaler container should use the specified image.
	customAutoscalerImage := "custom-autoscaler-xxx"
	cluster = instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	cluster.Spec.AutoscalerOptions = &rayv1.AutoscalerOptions{
		Image: &customAutoscalerImage,
	}
	podTemplateSpec = DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod.Spec = podTemplateSpec.Spec
	autoscalerContainerIndex = getAutoscalerContainerIndex(pod)
	assert.Equal(t, customAutoscalerImage, podTemplateSpec.Spec.Containers[autoscalerContainerIndex].Image)
}

// If no service account is specified in the RayCluster,
// the head pod's service account should be an empty string.
func TestHeadPodTemplate_WithNoServiceAccount(t *testing.T) {
	cluster := instance.DeepCopy()
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayv1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	pod := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")

	actualResult := pod.Spec.ServiceAccountName
	expectedResult := ""
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
}

// If a service account is specified in the RayCluster and EnableInTreeAutoscaling is set to false,
// the head pod's service account should be the same.
func TestHeadPodTemplate_WithServiceAccountNoAutoscaling(t *testing.T) {
	cluster := instance.DeepCopy()
	serviceAccount := "head-service-account"
	cluster.Spec.HeadGroupSpec.Template.Spec.ServiceAccountName = serviceAccount
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayv1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	pod := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")

	actualResult := pod.Spec.ServiceAccountName
	expectedResult := serviceAccount
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
}

// If a service account is specified in the RayCluster and EnableInTreeAutoscaling is set to true,
// the head pod's service account should be the same.
func TestHeadPodTemplate_WithServiceAccount(t *testing.T) {
	cluster := instance.DeepCopy()
	serviceAccount := "head-service-account"
	cluster.Spec.HeadGroupSpec.Template.Spec.ServiceAccountName = serviceAccount
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayv1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	pod := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")

	actualResult := pod.Spec.ServiceAccountName
	expectedResult := serviceAccount
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
}

func TestValidateHeadRayStartParams_OK(t *testing.T) {
	input := instance.Spec.HeadGroupSpec.DeepCopy()
	isValid, err := ValidateHeadRayStartParams(*input)
	assert.Equal(t, true, isValid)
	assert.Nil(t, err)
	command := convertParamMap(input.RayStartParams)
	assert.True(t, strings.Contains(command, "--include-dashboard=true"))
	assert.True(t, strings.Contains(command, "--log-color=true"))
}

func TestValidateHeadRayStartParams_ValidWithObjectStoreMemoryError(t *testing.T) {
	input := instance.Spec.HeadGroupSpec.DeepCopy()
	input.RayStartParams[ObjectStoreMemoryKey] = "2000000000"
	input.Template.Spec.Containers[0].Env = append(input.Template.Spec.Containers[0].Env, v1.EnvVar{
		Name:  AllowSlowStorageEnvVar,
		Value: "1",
	})
	isValid, err := ValidateHeadRayStartParams(*input)
	assert.Equal(t, true, isValid)
	assert.True(t, errors.IsBadRequest(err))
}

func TestValidateHeadRayStartParams_InvalidObjectStoreMemory(t *testing.T) {
	input := instance.Spec.HeadGroupSpec.DeepCopy()
	input.RayStartParams[ObjectStoreMemoryKey] = "2000000000"
	isValid, err := ValidateHeadRayStartParams(*input)
	assert.Equal(t, false, isValid)
	assert.True(t, errors.IsBadRequest(err))
}

func splitAndSort(s string) []string {
	strs := strings.Split(s, " ")
	result := make([]string, 0, len(strs))
	for _, s := range strs {
		if len(s) > 0 {
			result = append(result, s)
		}
	}
	sort.Strings(result)
	return result
}

func TestCleanupInvalidVolumeMounts(t *testing.T) {
	cluster := instance.DeepCopy()

	// Test head pod
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayv1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	pod := BuildPod(podTemplateSpec, rayv1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, "6379", nil, "", "", false)

	pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, []v1.VolumeMount{
		{
			Name:      "mock-name1",
			MountPath: "/mock-path1",
			ReadOnly:  true,
		},
		{
			Name:      "mock-name2",
			MountPath: "/mock-path2",
			ReadOnly:  true,
		},
	}...)
	assert.Equal(t, len(pod.Spec.Containers[0].VolumeMounts), 3)
	cleanupInvalidVolumeMounts(&pod.Spec.Containers[0], &pod)
	assert.Equal(t, len(pod.Spec.Containers[0].VolumeMounts), 1)
}

func TestDefaultWorkerPodTemplateWithName(t *testing.T) {
	cluster := instance.DeepCopy()
	fqdnRayIP := utils.GenerateFQDNServiceName(*cluster, cluster.Namespace)
	worker := cluster.Spec.WorkerGroupSpecs[0]
	worker.Template.ObjectMeta.Name = "ray-worker-test"
	podName := cluster.Name + DashSymbol + string(rayv1.WorkerNode) + DashSymbol + worker.GroupName + DashSymbol + utils.FormatInt32(0)
	expectedWorker := *worker.DeepCopy()

	// Pass a deep copy of worker (*worker.DeepCopy()) to prevent "worker" from updating.
	podTemplateSpec := DefaultWorkerPodTemplate(*cluster, *worker.DeepCopy(), podName, fqdnRayIP, "6379")
	assert.Equal(t, podTemplateSpec.ObjectMeta.Name, "")
	assert.Equal(t, worker, expectedWorker)
}

func containerPortExists(ports []v1.ContainerPort, name string, containerPort int32) error {
	for _, port := range ports {
		if port.Name == name {
			if port.ContainerPort != containerPort {
				return fmt.Errorf("expected `%v` but got `%v` for `%v` port", containerPort, port.ContainerPort, name)
			}
			return nil
		}
	}
	return fmt.Errorf("couldn't find `%v` port", name)
}

func TestDefaultHeadPodTemplateWithConfigurablePorts(t *testing.T) {
	cluster := instance.DeepCopy()
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Ports = []v1.ContainerPort{}
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayv1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	// DefaultHeadPodTemplate will add the default metrics port if user doesn't specify it.
	// Verify the default metrics port exists.
	if err := containerPortExists(podTemplateSpec.Spec.Containers[0].Ports, MetricsPortName, int32(DefaultMetricsPort)); err != nil {
		t.Fatal(err)
	}
	customMetricsPort := int32(DefaultMetricsPort) + 1
	metricsPort := v1.ContainerPort{
		Name:          MetricsPortName,
		ContainerPort: customMetricsPort,
	}
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Ports = []v1.ContainerPort{metricsPort}
	podTemplateSpec = DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, "6379")
	// Verify the custom metrics port exists.
	if err := containerPortExists(podTemplateSpec.Spec.Containers[0].Ports, MetricsPortName, customMetricsPort); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultWorkerPodTemplateWithConfigurablePorts(t *testing.T) {
	cluster := instance.DeepCopy()
	cluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Ports = []v1.ContainerPort{}
	worker := cluster.Spec.WorkerGroupSpecs[0]
	podName := cluster.Name + DashSymbol + string(rayv1.WorkerNode) + DashSymbol + worker.GroupName + DashSymbol + utils.FormatInt32(0)
	fqdnRayIP := utils.GenerateFQDNServiceName(*cluster, cluster.Namespace)
	podTemplateSpec := DefaultWorkerPodTemplate(*cluster, worker, podName, fqdnRayIP, "6379")
	// DefaultWorkerPodTemplate will add the default metrics port if user doesn't specify it.
	// Verify the default metrics port exists.
	if err := containerPortExists(podTemplateSpec.Spec.Containers[0].Ports, MetricsPortName, int32(DefaultMetricsPort)); err != nil {
		t.Fatal(err)
	}
	customMetricsPort := int32(DefaultMetricsPort) + 1
	metricsPort := v1.ContainerPort{
		Name:          MetricsPortName,
		ContainerPort: customMetricsPort,
	}
	cluster.Spec.WorkerGroupSpecs[0].Template.Spec.Containers[0].Ports = []v1.ContainerPort{metricsPort}
	podTemplateSpec = DefaultWorkerPodTemplate(*cluster, worker, podName, fqdnRayIP, "6379")
	// Verify the custom metrics port exists.
	if err := containerPortExists(podTemplateSpec.Spec.Containers[0].Ports, MetricsPortName, customMetricsPort); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultInitContainer(t *testing.T) {
	// A default init container to check the health of GCS is expected to be added.
	cluster := instance.DeepCopy()
	fqdnRayIP := utils.GenerateFQDNServiceName(*cluster, cluster.Namespace)
	worker := cluster.Spec.WorkerGroupSpecs[0]
	podName := cluster.Name + DashSymbol + string(rayv1.WorkerNode) + DashSymbol + worker.GroupName + DashSymbol + utils.FormatInt32(0)
	expectedResult := len(cluster.Spec.WorkerGroupSpecs[0].Template.Spec.InitContainers) + 1

	// Pass a deep copy of worker (*worker.DeepCopy()) to prevent "worker" from updating.
	podTemplateSpec := DefaultWorkerPodTemplate(*cluster, *worker.DeepCopy(), podName, fqdnRayIP, "6379")
	numInitContainers := len(podTemplateSpec.Spec.InitContainers)
	assert.Equal(t, expectedResult, numInitContainers, "A default init container is expected to be added.")

	// This health-check init container requires certain environment variables to establish a secure connection
	// with the Ray head using TLS authentication. Currently, we simply copied all environment variables from
	// Ray container to the init container. This may be changed in the future.
	healthCheckContainer := podTemplateSpec.Spec.InitContainers[numInitContainers-1]
	rayContainer := worker.Template.Spec.Containers[RayContainerIndex]

	assert.NotEqual(t, len(rayContainer.Env), 0, "The test only makes sense if the Ray container has environment variables.")
	assert.Equal(t, len(rayContainer.Env), len(healthCheckContainer.Env))
	for _, env := range rayContainer.Env {
		// env.ValueFrom is the source for the environment variable's value. Cannot be used if value is not empty.
		if env.Value != "" {
			checkContainerEnv(t, healthCheckContainer, env.Name, env.Value)
		} else {
			checkContainerEnv(t, healthCheckContainer, env.Name, env.ValueFrom.FieldRef.FieldPath)
		}
	}

	assert.NotEmpty(t, rayContainer.Resources, "The test only makes sense if the Ray container has resource limit/request.")
}

func TestDefaultInitContainerImagePullPolicy(t *testing.T) {
	cluster := instance.DeepCopy()
	fqdnRayIP := utils.GenerateFQDNServiceName(*cluster, cluster.Namespace)
	worker := cluster.Spec.WorkerGroupSpecs[0]
	podName := cluster.Name + DashSymbol + string(rayv1.WorkerNode) + DashSymbol + worker.GroupName + DashSymbol + utils.FormatInt32(0)

	cases := []struct {
		name               string
		imagePullPolicy    v1.PullPolicy
		expectedPullPolicy v1.PullPolicy
	}{
		{
			name:               "Always",
			imagePullPolicy:    v1.PullAlways,
			expectedPullPolicy: v1.PullAlways,
		},
		{
			name:               "IfNotPresent",
			imagePullPolicy:    v1.PullIfNotPresent,
			expectedPullPolicy: v1.PullIfNotPresent,
		},
		{
			name:               "Never",
			imagePullPolicy:    v1.PullNever,
			expectedPullPolicy: v1.PullNever,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// set ray container imagePullPolicy
			worker.Template.Spec.Containers[RayContainerIndex].ImagePullPolicy = tc.imagePullPolicy

			podTemplateSpec := DefaultWorkerPodTemplate(*cluster, *worker.DeepCopy(), podName, fqdnRayIP, "6379")

			healthCheckContainer := podTemplateSpec.Spec.InitContainers[len(podTemplateSpec.Spec.InitContainers)-1]
			assert.Equal(t, tc.expectedPullPolicy, healthCheckContainer.ImagePullPolicy, "The ImagePullPolicy of the init container should be the same as the Ray container.")
		})
	}
}

func TestSetMissingRayStartParamsAddress(t *testing.T) {
	// The address option is automatically injected into RayStartParams with a default value of <Fully Qualified Domain Name (FQDN)>:<headPort> for workers only, which is used to connect to the Ray cluster.
	// The head should not include the address option in RayStartParams, as it can access the GCS server, which also runs on the head, via localhost.
	// Although not recommended, users can manually set the address option for both head and workers.

	headPort := "6379"
	fqdnRayIP := "raycluster-kuberay-head-svc.default.svc.cluster.local"
	customAddress := "custom-address:1234"

	// Case 1: Head node with no address option set.
	rayStartParams := map[string]string{}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.HeadNode, headPort, "", nil)
	assert.NotContains(t, rayStartParams, "address", "Head node should not have an address option set by default.")

	// Case 2: Head node with custom address option set.
	rayStartParams = map[string]string{"address": customAddress}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.HeadNode, headPort, "", nil)
	assert.Equal(t, customAddress, rayStartParams["address"], fmt.Sprintf("Expected `%v` but got `%v`", customAddress, rayStartParams["address"]))

	// Case 3: Worker node with no address option set.
	rayStartParams = map[string]string{}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.WorkerNode, headPort, fqdnRayIP, nil)
	expectedAddress := fmt.Sprintf("%s:%s", fqdnRayIP, headPort)
	assert.Equal(t, expectedAddress, rayStartParams["address"], fmt.Sprintf("Expected `%v` but got `%v`", expectedAddress, rayStartParams["address"]))

	// Case 4: Worker node with custom address option set.
	rayStartParams = map[string]string{"address": customAddress}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.WorkerNode, headPort, fqdnRayIP, nil)
	assert.Equal(t, customAddress, rayStartParams["address"], fmt.Sprintf("Expected `%v` but got `%v`", customAddress, rayStartParams["address"]))
}

func TestSetMissingRayStartParamsMetricsExportPort(t *testing.T) {
	// The metrics-export-port option is automatically injected into RayStartParams with a default value of DefaultMetricsPort for both head and workers.
	// Users can manually set the metrics-export-port option to customize the metrics export port and scrape Ray’s metrics using Prometheus via <ip>:<custom metrics export port>.
	// See https://github.com/ray-project/kuberay/pull/954 for more details.

	headPort := "6379"
	fqdnRayIP := "raycluster-kuberay-head-svc.default.svc.cluster.local"
	customMetricsPort := DefaultMetricsPort + 1

	// Case 1: Head node with no metrics-export-port option set.
	rayStartParams := map[string]string{}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.HeadNode, headPort, "", nil)
	assert.Equal(t, fmt.Sprint(DefaultMetricsPort), rayStartParams["metrics-export-port"], fmt.Sprintf("Expected `%v` but got `%v`", fmt.Sprint(DefaultMetricsPort), rayStartParams["metrics-export-port"]))

	// Case 2: Head node with custom metrics-export-port option set.
	rayStartParams = map[string]string{"metrics-export-port": fmt.Sprint(customMetricsPort)}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.HeadNode, headPort, "", nil)
	assert.Equal(t, fmt.Sprint(customMetricsPort), rayStartParams["metrics-export-port"], fmt.Sprintf("Expected `%v` but got `%v`", fmt.Sprint(customMetricsPort), rayStartParams["metrics-export-port"]))

	// Case 3: Worker node with no metrics-export-port option set.
	rayStartParams = map[string]string{}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.WorkerNode, headPort, fqdnRayIP, nil)
	assert.Equal(t, fmt.Sprint(DefaultMetricsPort), rayStartParams["metrics-export-port"], fmt.Sprintf("Expected `%v` but got `%v`", fmt.Sprint(DefaultMetricsPort), rayStartParams["metrics-export-port"]))

	// Case 4: Worker node with custom metrics-export-port option set.
	rayStartParams = map[string]string{"metrics-export-port": fmt.Sprint(customMetricsPort)}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.WorkerNode, headPort, fqdnRayIP, nil)
	assert.Equal(t, fmt.Sprint(customMetricsPort), rayStartParams["metrics-export-port"], fmt.Sprintf("Expected `%v` but got `%v`", fmt.Sprint(customMetricsPort), rayStartParams["metrics-export-port"]))
}

func TestSetMissingRayStartParamsBlock(t *testing.T) {
	// The --block option is automatically injected into RayStartParams with a default value of true for both head and workers.
	// Although not recommended, users can manually set the --block option.
	// See https://github.com/ray-project/kuberay/pull/675 for more details.

	headPort := "6379"
	fqdnRayIP := "raycluster-kuberay-head-svc.default.svc.cluster.local"

	// Case 1: Head node with no --block option set.
	rayStartParams := map[string]string{}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.HeadNode, headPort, "", nil)
	assert.Equal(t, "true", rayStartParams["block"], fmt.Sprintf("Expected `%v` but got `%v`", "true", rayStartParams["block"]))

	// Case 2: Head node with --block option set to false.
	rayStartParams = map[string]string{"block": "false"}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.HeadNode, headPort, "", nil)
	assert.Equal(t, "false", rayStartParams["block"], fmt.Sprintf("Expected `%v` but got `%v`", "false", rayStartParams["block"]))

	// Case 3: Worker node with no --block option set.
	rayStartParams = map[string]string{}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.WorkerNode, headPort, fqdnRayIP, nil)
	assert.Equal(t, "true", rayStartParams["block"], fmt.Sprintf("Expected `%v` but got `%v`", "true", rayStartParams["block"]))

	// Case 4: Worker node with --block option set to false.
	rayStartParams = map[string]string{"block": "false"}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.WorkerNode, headPort, fqdnRayIP, nil)
	assert.Equal(t, "false", rayStartParams["block"], fmt.Sprintf("Expected `%v` but got `%v`", "false", rayStartParams["block"]))
}

func TestSetMissingRayStartParamsDashboardHost(t *testing.T) {
	// The dashboard-host option is automatically injected into RayStartParams with a default value of "0.0.0.0" for head only as workers do not have dashborad server.
	// Users can manually set the dashboard-host option to customize the host the dashboard server binds to, either "localhost" (127.0.0.1) or "0.0.0.0" (available from all interfaces).
	headPort := "6379"
	fqdnRayIP := "raycluster-kuberay-head-svc.default.svc.cluster.local"

	// Case 1: Head node with no dashboard-host option set.
	rayStartParams := map[string]string{}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.HeadNode, headPort, "", nil)
	assert.Equal(t, "0.0.0.0", rayStartParams["dashboard-host"], fmt.Sprintf("Expected `%v` but got `%v`", "0.0.0.0", rayStartParams["dashboard-host"]))

	// Case 2: Head node with dashboard-host option set.
	rayStartParams = map[string]string{"dashboard-host": "localhost"}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.HeadNode, headPort, "", nil)
	assert.Equal(t, "localhost", rayStartParams["dashboard-host"], fmt.Sprintf("Expected `%v` but got `%v`", "localhost", rayStartParams["dashboard-host"]))

	// Case 3: Worker node with no dashboard-host option set.
	rayStartParams = map[string]string{}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.WorkerNode, headPort, fqdnRayIP, nil)
	assert.NotContains(t, rayStartParams, "dashboard-host", "workers should not have an dashboard-host option set.")

	// Case 4: Worker node with dashboard-host option set.
	// To maximize user empowerment, this option can be enabled. However, it is important to note that the dashboard is not available on worker nodes.
	rayStartParams = map[string]string{"dashboard-host": "localhost"}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.WorkerNode, headPort, fqdnRayIP, nil)
	assert.Equal(t, "localhost", rayStartParams["dashboard-host"], fmt.Sprintf("Expected `%v` but got `%v`", "localhost", rayStartParams["dashboard-host"]))
}

func TestSetMissingRayStartParamsAgentListenPort(t *testing.T) {
	// The "dashboard-agent-listen-port" port will be automatically injected into RayStartParams with a default
	// value of 52365 (i.e., DefaultDashboardAgentListenPort) when the annotation "ray.io/enable-serve-service"
	// is set to true. The behavior is the same for both head and workers.
	headPort := "6379"
	fqdnRayIP := "raycluster-kuberay-head-svc.default.svc.cluster.local"
	rayStartParams := map[string]string{}
	annotaions := map[string]string{}

	// Case 1: Head node without "ray.io/enable-serve-service=true".
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.HeadNode, headPort, "", annotaions)
	assert.NotContains(t, rayStartParams, "dashboard-agent-listen-port", "Head Pod should not have a dashboard-agent-listen-port option set by default.")

	// Case 2: Worker node without "ray.io/enable-serve-service=true".
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.WorkerNode, headPort, fqdnRayIP, annotaions)
	assert.NotContains(t, rayStartParams, "dashboard-agent-listen-port", "Worker Pod should not have a dashboard-agent-listen-port option set by default.")

	// Case 3: Head node with "ray.io/enable-serve-service=true" and users do not provide "dashboard-agent-listen-port".
	annotaions = map[string]string{EnableServeServiceKey: EnableServeServiceTrue}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.HeadNode, headPort, "", annotaions)
	assert.Equal(t, fmt.Sprint(DefaultDashboardAgentListenPort), rayStartParams["dashboard-agent-listen-port"], fmt.Sprintf("Expected `%v` but got `%v`", fmt.Sprint(DefaultDashboardAgentListenPort), rayStartParams["dashboard-agent-listen-port"]))

	// Case 4: Worker node with "ray.io/enable-serve-service=true" and users do not provide "dashboard-agent-listen-port".
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.WorkerNode, headPort, fqdnRayIP, annotaions)
	assert.Equal(t, fmt.Sprint(DefaultDashboardAgentListenPort), rayStartParams["dashboard-agent-listen-port"], fmt.Sprintf("Expected `%v` but got `%v`", fmt.Sprint(DefaultDashboardAgentListenPort), rayStartParams["dashboard-agent-listen-port"]))

	// Case 5: Head node with "ray.io/enable-serve-service=true" and users provide "dashboard-agent-listen-port".
	customDashboardAgentListenPort := DefaultDashboardAgentListenPort + 1
	rayStartParams = map[string]string{"dashboard-agent-listen-port": fmt.Sprint(customDashboardAgentListenPort)}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.HeadNode, headPort, "", annotaions)
	assert.Equal(t, fmt.Sprint(customDashboardAgentListenPort), rayStartParams["dashboard-agent-listen-port"], fmt.Sprintf("Expected `%v` but got `%v`", fmt.Sprint(customDashboardAgentListenPort), rayStartParams["dashboard-agent-listen-port"]))

	// Case 6: Worker node with "ray.io/enable-serve-service=true" and users provide "dashboard-agent-listen-port".
	rayStartParams = map[string]string{"dashboard-agent-listen-port": fmt.Sprint(customDashboardAgentListenPort)}
	rayStartParams = setMissingRayStartParams(rayStartParams, rayv1.WorkerNode, headPort, fqdnRayIP, annotaions)
	assert.Equal(t, fmt.Sprint(customDashboardAgentListenPort), rayStartParams["dashboard-agent-listen-port"], fmt.Sprintf("Expected `%v` but got `%v`", fmt.Sprint(customDashboardAgentListenPort), rayStartParams["dashboard-agent-listen-port"]))
}

func TestGetCustomWorkerInitImage(t *testing.T) {
	// cleanup
	defer os.Unsetenv(EnableInitContainerInjectionEnvKey)

	// not set the env
	b := getEnableInitContainerInjection()
	assert.True(t, b)
	// set the env with "true"
	os.Setenv(EnableInitContainerInjectionEnvKey, "true")
	b = getEnableInitContainerInjection()
	assert.True(t, b)
	// set the env with "True"
	os.Setenv(EnableInitContainerInjectionEnvKey, "True")
	b = getEnableInitContainerInjection()
	assert.True(t, b)
	// set the env with "false"
	os.Setenv(EnableInitContainerInjectionEnvKey, "false")
	b = getEnableInitContainerInjection()
	assert.False(t, b)
	// set the env with "False"
	os.Setenv(EnableInitContainerInjectionEnvKey, "False")
	b = getEnableInitContainerInjection()
	assert.False(t, b)
}

func TestGetEnableProbesInjection(t *testing.T) {
	// cleanup
	defer os.Unsetenv(ENABLE_PROBES_INJECTION)

	// not set the env
	os.Unsetenv(ENABLE_PROBES_INJECTION)
	b := getEnableProbesInjection()
	assert.True(t, b)
	// set the env with "true"
	os.Setenv(ENABLE_PROBES_INJECTION, "true")
	b = getEnableProbesInjection()
	assert.True(t, b)
	// set the env with "True"
	os.Setenv(ENABLE_PROBES_INJECTION, "True")
	b = getEnableProbesInjection()
	assert.True(t, b)
	// set the env with "false"
	os.Setenv(ENABLE_PROBES_INJECTION, "false")
	b = getEnableProbesInjection()
	assert.False(t, b)
	// set the env with "False"
	os.Setenv(ENABLE_PROBES_INJECTION, "False")
	b = getEnableProbesInjection()
	assert.False(t, b)
}

func TestInitHealthProbe(t *testing.T) {
	// Test 1: User defines a custom HTTPGet probe.
	httpGetProbe := v1.Probe{
		ProbeHandler: v1.ProbeHandler{
			HTTPGet: &v1.HTTPGetAction{
				// Check Raylet status
				Path: fmt.Sprintf("/%s", RayAgentRayletHealthPath),
				Port: intstr.FromInt(DefaultDashboardAgentListenPort),
			},
		},
	}
	initHealthProbe(&httpGetProbe, rayv1.HeadNode)
	assert.NotNil(t, httpGetProbe.HTTPGet)
	assert.Nil(t, httpGetProbe.Exec)

	// Test 2: User does not define a custom probe. KubeRay will inject a default Exec probe.
	probe := v1.Probe{}
	initHealthProbe(&probe, rayv1.HeadNode)
	assert.NotNil(t, probe.Exec)
}
