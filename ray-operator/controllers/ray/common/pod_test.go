package common

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/stretchr/testify/assert"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var testMemoryLimit = resource.MustParse("1Gi")

var instance = rayiov1alpha1.RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
	},
	Spec: rayiov1alpha1.RayClusterSpec{
		RayVersion: "2.0.0",
		HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
			ServiceType: "ClusterIP",
			Replicas:    pointer.Int32Ptr(1),
			RayStartParams: map[string]string{
				"port":                "6379",
				"object-manager-port": "12345",
				"node-manager-port":   "12346",
				"object-store-memory": "100000000",
				"redis-password":      "LetMeInRay",
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
		WorkerGroupSpecs: []rayiov1alpha1.WorkerGroupSpec{
			{
				Replicas:    pointer.Int32Ptr(3),
				MinReplicas: pointer.Int32Ptr(0),
				MaxReplicas: pointer.Int32Ptr(10000),
				GroupName:   "small-group",
				RayStartParams: map[string]string{
					"port":           "6379",
					"redis-password": "LetMeInRay",
					"num-cpus":       "1",
					"block":          "true",
				},
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "default",
						Labels: map[string]string{
							"ray.io/cluster": "raycluster-sample",
							"ray.io/group":   "small-group",
						},
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
	ImagePullPolicy: v1.PullAlways,
	Env: []v1.EnvVar{
		{
			Name: "RAY_CLUSTER_NAME",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					FieldPath: "metadata.labels['ray.io/cluster']",
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

func TestGetAutoscalerImage(t *testing.T) {
	// rayVersion strings for which we judge autoscaler support is stable and thus
	// use the same image for the autoscaler as for the Ray container.
	newRayVersions := []string{"2.0.0", "2.0.0rc0", "2.0", "2", "latest", "nightly", "what's this"}
	rayImage := "repo/image:tag"
	for _, rayVersion := range newRayVersions {
		expectedAutoscalerImage := rayImage
		actualAutoscalerImage := getAutoscalerImage(rayImage, rayVersion)
		if actualAutoscalerImage != expectedAutoscalerImage {
			t.Fatalf("Expected `%v` but got `%v`", expectedAutoscalerImage, actualAutoscalerImage)
		}
	}

	// rayVersion strings for which we judge autoscaler support is not stable and thus
	// use the default Ray 2.0.0 image to run the autoscaler.
	oldRayVersions := []string{"1", "1.13", "1.13.0"}
	for _, rayVersion := range oldRayVersions {
		expectedAutoscalerImage := "rayproject/ray:2.0.0"
		actualAutoscalerImage := getAutoscalerImage(rayImage, rayVersion)
		if actualAutoscalerImage != expectedAutoscalerImage {
			t.Fatalf("Expected `%v` but got `%v`", expectedAutoscalerImage, actualAutoscalerImage)
		}
	}
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

func checkPodEnv(t *testing.T, pod v1.Pod, envName string, expectedValue string) {
	foundEnv := false
	for _, env := range pod.Spec.Containers[0].Env {
		if env.Name == envName {
			if !(env.Value == expectedValue) {
				t.Fatalf("Expected `%v` but got `%v`", expectedValue, env.Value)
			}
			foundEnv = true
			break
		}
	}
	if !foundEnv {
		t.Fatalf("Couldn't find `%v` env on pod.", envName)
	}
}

func TestBuildPod(t *testing.T) {
	cluster := instance.DeepCopy()

	// Test head pod
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName, "6379")
	pod := BuildPod(podTemplateSpec, rayiov1alpha1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, svcName, "6379", nil, "")

	// Check RAY_ADDRESS env.
	checkPodEnv(t, pod, RAY_ADDRESS, "127.0.0.1:6379")
	// Check usage stats env.
	checkPodEnv(t, pod, RAY_USAGE_STATS_KUBERAY_IN_USE, "1")

	// Check labels.
	actualResult := pod.Labels[RayClusterLabelKey]
	expectedResult := cluster.Name
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[RayNodeTypeLabelKey]
	expectedResult = string(rayiov1alpha1.HeadNode)
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
	podName = cluster.Name + DashSymbol + string(rayiov1alpha1.WorkerNode) + DashSymbol + worker.GroupName + DashSymbol + utils.FormatInt32(0)
	podTemplateSpec = DefaultWorkerPodTemplate(*cluster, worker, podName, svcName, "6379")
	pod = BuildPod(podTemplateSpec, rayiov1alpha1.WorkerNode, worker.RayStartParams, svcName, "6379", nil, "")

	// Check RAY_ADDRESS env
	checkPodEnv(t, pod, RAY_ADDRESS, "raycluster-sample-head-svc:6379")

	// Check RayStartParams
	expectedResult = fmt.Sprintf("%s:6379", svcName)
	actualResult = cluster.Spec.WorkerGroupSpecs[0].RayStartParams["address"]

	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	expectedCommandArg := splitAndSort("ulimit -n 65536; ray start --block --memory=1073741824 --num-cpus=1 --num-gpus=3 --address=raycluster-sample-head-svc:6379 --port=6379 --redis-password=LetMeInRay --metrics-export-port=8080")
	actualCommandArg := splitAndSort(pod.Spec.Containers[0].Args[0])
	if !reflect.DeepEqual(expectedCommandArg, actualCommandArg) {
		t.Fatalf("Expected `%v` but got `%v`", expectedCommandArg, actualCommandArg)
	}

	// Check Envs
	checkPodEnv(t, pod, "TEST_ENV_NAME", "TEST_ENV_VALUE")
}

func TestBuildPod_WithAutoscalerEnabled(t *testing.T) {
	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName, "6379")
	pod := BuildPod(podTemplateSpec, rayiov1alpha1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, svcName, "6379", &trueFlag, "")

	actualResult := pod.Labels[RayClusterLabelKey]
	expectedResult := cluster.Name
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
	actualResult = pod.Labels[RayNodeTypeLabelKey]
	expectedResult = string(rayiov1alpha1.HeadNode)
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
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName, "6379")
	pod := BuildPod(podTemplateSpec, rayiov1alpha1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, svcName, "6379", &trueFlag, RayServiceCreatorLabelValue)

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

// Check that autoscaler container overrides work as expected.
func TestBuildPodWithAutoscalerOptions(t *testing.T) {
	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)

	customAutoscalerImage := "custom-autoscaler-xxx"
	customPullPolicy := v1.PullIfNotPresent
	customTimeout := int32(100)
	customUpscaling := rayiov1alpha1.UpscalingMode("Aggressive")
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

	cluster.Spec.AutoscalerOptions = &rayiov1alpha1.AutoscalerOptions{
		UpscalingMode:      &customUpscaling,
		IdleTimeoutSeconds: &customTimeout,
		Image:              &customAutoscalerImage,
		ImagePullPolicy:    &customPullPolicy,
		Resources:          &customResources,
		Env:                customEnv,
		EnvFrom:            customEnvFrom,
	}
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName, "6379")
	pod := BuildPod(podTemplateSpec, rayiov1alpha1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, svcName, "6379", &trueFlag, "")
	expectedContainer := *autoscalerContainer.DeepCopy()
	expectedContainer.Image = customAutoscalerImage
	expectedContainer.ImagePullPolicy = customPullPolicy
	expectedContainer.Resources = customResources
	expectedContainer.EnvFrom = customEnvFrom
	expectedContainer.Env = append(expectedContainer.Env, customEnv...)
	index := getAutoscalerContainerIndex(pod)
	actualContainer := pod.Spec.Containers[index]
	if !reflect.DeepEqual(expectedContainer, actualContainer) {
		t.Fatalf("Expected `%v` but got `%v`", expectedContainer, actualContainer)
	}
}

func TestHeadPodTemplate_WithAutoscalingEnabled(t *testing.T) {
	cluster := instance.DeepCopy()
	cluster.Spec.EnableInTreeAutoscaling = &trueFlag
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName, "6379")

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
	podTemplateSpec = DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName, "6379")
	actualResult = podTemplateSpec.Spec.ServiceAccountName
	expectedResult = shortString(t) // 50 chars long, truncated by utils.CheckName
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}
}

// If no service account is specified in the RayCluster,
// the head pod's service account should be an empty string.
func TestHeadPodTemplate_WithNoServiceAccount(t *testing.T) {
	cluster := instance.DeepCopy()
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)
	pod := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName, "6379")

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
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)
	pod := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName, "6379")

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
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)
	pod := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName, "6379")

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
	podName := strings.ToLower(cluster.Name + DashSymbol + string(rayiov1alpha1.HeadNode) + DashSymbol + utils.FormatInt32(0))
	svcName := utils.GenerateServiceName(cluster.Name)
	podTemplateSpec := DefaultHeadPodTemplate(*cluster, cluster.Spec.HeadGroupSpec, podName, svcName, "6379")
	pod := BuildPod(podTemplateSpec, rayiov1alpha1.HeadNode, cluster.Spec.HeadGroupSpec.RayStartParams, svcName, "6379", nil, "")

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
	svcName := utils.GenerateServiceName(cluster.Name)
	worker := cluster.Spec.WorkerGroupSpecs[0]
	worker.Template.ObjectMeta.Name = "ray-worker-test"
	podName := cluster.Name + DashSymbol + string(rayiov1alpha1.WorkerNode) + DashSymbol + worker.GroupName + DashSymbol + utils.FormatInt32(0)
	podTemplateSpec := DefaultWorkerPodTemplate(*cluster, worker, podName, svcName, "6379")
	assert.Equal(t, podTemplateSpec.ObjectMeta.Name, "")
}
