package model

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	util "github.com/ray-project/kuberay/apiserver/pkg/util"
	api "github.com/ray-project/kuberay/proto/go_client"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

var (
	enableIngress                  = true
	workerReplicas           int32 = 5
	workerMinReplicas        int32 = 1
	workerMaxReplicas        int32 = 3
	unhealthySecondThreshold int32 = 900
	secondsValue             int32 = 100
)

var headSpecTest = rayv1api.HeadGroupSpec{
	ServiceType:   "ClusterIP",
	EnableIngress: &enableIngress,
	RayStartParams: map[string]string{
		"dashboard-host":      "0.0.0.0",
		"metrics-export-port": "8080",
		"num-cpus":            "0",
	},
	Template: corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"openshift.io/scc":    "restricted",
				"ray.io/ft-enabled":   "false",
				"ray.io/health-state": "",
				"custom":              "value",
			},
			Labels: map[string]string{
				"app.kubernetes.io/created-by": "kuberay-operator",
				"app.kubernetes.io/name":       "kuberay",
				"ray.io/cluster":               "boris-cluster",
				"ray.io/cluster-dashboard":     "boris-cluster-dashboard",
				"ray.io/group":                 utils.RayNodeHeadGroupLabelValue,
				"ray.io/identifier":            "boris-cluster-head",
				"ray.io/is-ray-node":           "yes",
				"ray.io/node-type":             "head",
				"test":                         "value",
			},
			Name:      "boris-cluster-head-f7zx2",
			Namespace: "max",
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "account",
			ImagePullSecrets: []corev1.LocalObjectReference{
				{Name: "foo"},
			},
			Tolerations: []corev1.Toleration{
				{
					Key:      "blah1",
					Operator: "Exists",
					Effect:   "NoExecute",
				},
			},
			Containers: []corev1.Container{
				{
					Name:            "ray-head",
					Image:           "blublinsky1/ray310:2.5.0",
					ImagePullPolicy: "Always",
					Env: []corev1.EnvVar{
						{
							Name:  "AWS_KEY",
							Value: "123",
						},
						{
							Name: "REDIS_PASSWORD",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "redis-password-secret",
									},
									Key: "password",
								},
							},
						},
						{
							Name: "CONFIGMAP",
							ValueFrom: &corev1.EnvVarSource{
								ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "special-config",
									},
									Key: "special.how",
								},
							},
						},
						{
							Name: "ResourceFieldRef",
							ValueFrom: &corev1.EnvVarSource{
								ResourceFieldRef: &corev1.ResourceFieldSelector{
									ContainerName: "my-container",
									Resource:      "resource",
								},
							},
						},
						{
							Name: "FieldRef",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: "path",
								},
							},
						},
					},
					SecurityContext: &corev1.SecurityContext{
						Capabilities: &corev1.Capabilities{
							Add: []corev1.Capability{
								"SYS_PTRACE",
							},
						},
					},
				},
			},
		},
	},
}

var configMapWithoutTolerations = corev1.ConfigMap{
	Data: map[string]string{
		"cpu":                "4",
		"gpu":                "0",
		"gpu_accelerator":    "",
		"memory":             "8",
		"extended_resources": "{\"vpc.amazonaws.com/efa\": 32}",
		"name":               "head-node-template",
		"namespace":          "max",
	},
}

var configMapWithTolerations = corev1.ConfigMap{
	Data: map[string]string{
		"cpu":                "4",
		"gpu":                "0",
		"gpu_accelerator":    "",
		"memory":             "8",
		"extended_resources": "{\"vpc.amazonaws.com/efa\": 32}",
		"name":               "head-node-template",
		"namespace":          "max",
		"tolerations":        "[{\"key\":\"blah1\",\"operator\":\"Exists\",\"effect\":\"NoExecute\"}]",
	},
}

var workerSpecTest = rayv1api.WorkerGroupSpec{
	GroupName:   "",
	Replicas:    &workerReplicas,
	MinReplicas: &workerMinReplicas,
	MaxReplicas: &workerMaxReplicas,
	RayStartParams: map[string]string{
		"node-ip-address": "$MY_POD_IP",
	},
	Template: corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"cni.projectcalico.org/containerID": "cce862a899455385e98e3453ba9ef5a376e85ad45c3e95b18e04e001204af728",
				"cni.projectcalico.org/podIP":       "172.17.60.2/32",
				"cni.projectcalico.org/podIPs":      "172.17.60.2/32",
				"openshift.io/scc":                  "restricted",
				"ray.io/ft-enabled":                 "false",
				"ray.io/health-state":               "",
				"custom":                            "value",
			},
			Labels: map[string]string{
				"app.kubernetes.io/created-by": "kuberay-operator",
				"app.kubernetes.io/name":       "kuberay",
				"ray.io/cluster":               "boris-cluster",
				"ray.io/cluster-dashboard":     "boris-cluster-dashboard",
				"ray.io/group":                 "8-CPUs",
				"ray.io/identifier":            "boris-cluster-worker",
				"ray.io/is-ray-node":           "yes",
				"ray.io/node-type":             "worker",
				"test":                         "value",
			},
			Name:      "boris-cluster-worker-8-cpus-4dp9v",
			Namespace: "max",
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: "account",
			ImagePullSecrets: []corev1.LocalObjectReference{
				{Name: "foo"},
			},
			Tolerations: []corev1.Toleration{
				{
					Key:      "blah1",
					Operator: "Exists",
					Effect:   "NoExecute",
				},
			},
			Containers: []corev1.Container{
				{
					Name:  "ray-worker",
					Image: "blublinsky1/ray310:2.5.0",
					Env: []corev1.EnvVar{
						{
							Name:  "AWS_KEY",
							Value: "123",
						},
						{
							Name:  "AWS_SECRET",
							Value: "1234",
						},
						{
							Name:  "RAY_DISABLE_DOCKER_CPU_WARNING",
							Value: "1",
						},
						{
							Name:  "TYPE",
							Value: "worker",
						},
						{
							Name:  "RAY_IP",
							Value: "boris-cluster-head-svc",
						},
						{
							Name:  "RAY_USAGE_STATS_KUBERAY_IN_USE",
							Value: "1",
						},
					},
					SecurityContext: &corev1.SecurityContext{
						Capabilities: &corev1.Capabilities{
							Add: []corev1.Capability{
								"SYS_PTRACE",
							},
						},
					},
				},
			},
		},
	},
}

var ClusterSpecTest = rayv1api.RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
		Annotations: map[string]string{
			"kubernetes.io/ingress.class": "nginx",
		},
	},
	Spec: rayv1api.RayClusterSpec{
		HeadGroupSpec: headSpecTest,
		WorkerGroupSpecs: []rayv1api.WorkerGroupSpec{
			workerSpecTest,
		},
	},
}

var ClusterSpecAutoscalerTest = rayv1api.RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
		Annotations: map[string]string{
			"kubernetes.io/ingress.class": "nginx",
		},
	},
	Spec: rayv1api.RayClusterSpec{
		HeadGroupSpec: headSpecTest,
		WorkerGroupSpecs: []rayv1api.WorkerGroupSpec{
			workerSpecTest,
		},
		EnableInTreeAutoscaling: ptr.To(true),
		AutoscalerOptions: &rayv1api.AutoscalerOptions{
			IdleTimeoutSeconds: ptr.To[int32](int32(60)),
			UpscalingMode:      (*rayv1api.UpscalingMode)(ptr.To("Default")),
			ImagePullPolicy:    (*corev1.PullPolicy)(ptr.To("Always")),
			Resources: &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
			},
		},
	},
}

var JobNewClusterTest = rayv1api.RayJob{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test",
		Namespace: "test",
		Labels: map[string]string{
			"ray.io/user": "user",
		},
	},
	Spec: rayv1api.RayJobSpec{
		Entrypoint: "python /home/ray/samples/sample_code.py",
		Metadata: map[string]string{
			"job_submission_id": "123",
		},
		RuntimeEnvYAML:          "mytest yaml",
		TTLSecondsAfterFinished: secondsValue,
		RayClusterSpec:          &ClusterSpecTest.Spec,
	},
}

var JobExistingClusterTest = rayv1api.RayJob{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test",
		Namespace: "test",
		Labels: map[string]string{
			"ray.io/user": "user",
		},
	},
	Spec: rayv1api.RayJobSpec{
		Entrypoint:              "python /home/ray/samples/sample_code.py",
		RuntimeEnvYAML:          "mytest yaml",
		TTLSecondsAfterFinished: secondsValue,
		ClusterSelector: map[string]string{
			util.RayClusterUserLabelKey: "test",
		},
	},
}

var JobExistingClusterSubmitterTest = rayv1api.RayJob{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test",
		Namespace: "test",
		Labels: map[string]string{
			"ray.io/user": "user",
		},
	},
	Spec: rayv1api.RayJobSpec{
		Entrypoint:              "python /home/ray/samples/sample_code.py",
		RuntimeEnvYAML:          "mytest yaml",
		TTLSecondsAfterFinished: secondsValue,
		ClusterSelector: map[string]string{
			util.RayClusterUserLabelKey: "test",
		},
		SubmitterPodTemplate: &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-submitter",
						Image: "image",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("2"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("200Mi"),
							},
						},
					},
				},
				RestartPolicy: corev1.RestartPolicyNever,
			},
		},
	},
}

var JobWithOutputTest = rayv1api.RayJob{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test",
		Namespace: "test",
		Labels: map[string]string{
			"ray.io/user": "user",
		},
	},
	Spec: rayv1api.RayJobSpec{
		Entrypoint:              "python /home/ray/samples/sample_code.py",
		RuntimeEnvYAML:          "mytest yaml",
		TTLSecondsAfterFinished: secondsValue,
		RayClusterSpec:          &ClusterSpecTest.Spec,
	},
	Status: rayv1api.RayJobStatus{
		JobStatus:           "RUNNING",
		JobDeploymentStatus: "Initializing",
		Message:             "Job is currently running",
		RayClusterName:      "raycluster-sample-xxxxx",
		StartTime:           &metav1.Time{Time: time.Date(2024, 0o7, 25, 0, 0, 0, 0, time.UTC)},
		EndTime:             nil,
	},
}

var ServiceV2Test = rayv1api.RayService{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test",
		Namespace: "test",
		Labels: map[string]string{
			"ray.io/user": "user",
		},
	},
	Spec: rayv1api.RayServiceSpec{
		ServeConfigV2:                      "Some yaml value",
		RayClusterSpec:                     ClusterSpecTest.Spec,
		DeploymentUnhealthySecondThreshold: &unhealthySecondThreshold,
	},
}

var autoscalerOptions = &rayv1api.AutoscalerOptions{
	IdleTimeoutSeconds: ptr.To[int32](int32(60)),
	UpscalingMode:      (*rayv1api.UpscalingMode)(ptr.To("Default")),
	Image:              ptr.To("Some Image"),
	ImagePullPolicy:    (*corev1.PullPolicy)(ptr.To("Always")),
	Env: []corev1.EnvVar{
		{
			Name:  "n1",
			Value: "v1",
		},
	},
	EnvFrom: []corev1.EnvFromSource{
		{
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "ConfigMap",
				},
			},
		},
		{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "Secret",
				},
			},
		},
	},
	VolumeMounts: []corev1.VolumeMount{
		{
			Name:             "vmount1",
			MountPath:        "path1",
			ReadOnly:         false,
			MountPropagation: (*corev1.MountPropagationMode)(ptr.To("None")),
		},
		{
			Name:             "vmount2",
			MountPath:        "path2",
			ReadOnly:         true,
			MountPropagation: (*corev1.MountPropagationMode)(ptr.To("HostToContainer")),
		},
	},
	Resources: &corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
	},
}

var expectedAnnotations = map[string]string{
	"custom": "value",
}

var expectedLabels = map[string]string{
	"test": "value",
}

var expectedHeadEnv = &api.EnvironmentVariables{
	Values: map[string]string{
		"AWS_KEY": "123",
	},
	ValuesFrom: map[string]*api.EnvValueFrom{
		"REDIS_PASSWORD": {
			Source: api.EnvValueFrom_SECRET,
			Name:   "redis-password-secret",
			Key:    "password",
		},
		"CONFIGMAP": {
			Source: api.EnvValueFrom_CONFIGMAP,
			Name:   "special-config",
			Key:    "special.how",
		},
		"ResourceFieldRef": {
			Source: api.EnvValueFrom_RESOURCEFIELD,
			Name:   "my-container",
			Key:    "resource",
		},
		"FieldRef": {
			Source: api.EnvValueFrom_FIELD,
			Key:    "path",
		},
	},
}

var expectedEnv = &api.EnvironmentVariables{
	Values: map[string]string{
		"AWS_KEY":    "123",
		"AWS_SECRET": "1234",
	},
	ValuesFrom: map[string]*api.EnvValueFrom{},
}

var expectedTolerations = api.PodToleration{
	Key:      "blah1",
	Operator: "Exists",
	Effect:   "NoExecute",
}

func TestPopulateHeadNodeSpec(t *testing.T) {
	groupSpec := PopulateHeadNodeSpec(headSpecTest)

	if groupSpec.ServiceAccount != "account" {
		t.Errorf("failed to convert service account")
	}
	if groupSpec.EnableIngress != *headSpecTest.EnableIngress {
		t.Errorf("failed to convert enableIngress")
	}
	if groupSpec.ImagePullSecret != "foo" {
		t.Errorf("failed to convert image pull secret")
	}
	if groupSpec.ImagePullPolicy != "Always" {
		t.Errorf("failed to convert image pull policy")
	}
	if !reflect.DeepEqual(groupSpec.Annotations, expectedAnnotations) {
		t.Errorf("failed to convert annotations, got %v, expected %v", groupSpec.Annotations, expectedAnnotations)
	}
	if !reflect.DeepEqual(groupSpec.Labels, expectedLabels) {
		t.Errorf("failed to convert labels, got %v, expected %v", groupSpec.Labels, expectedLabels)
	}
	if !reflect.DeepEqual(groupSpec.Environment, expectedHeadEnv) {
		t.Errorf("failed to convert environment, got %v, expected %v", groupSpec.Environment, expectedHeadEnv)
	}
	// Cannot use deep equal since protobuf locks copying
	if groupSpec.SecurityContext == nil || groupSpec.SecurityContext.Capabilities == nil || len(groupSpec.SecurityContext.Capabilities.Add) != 1 {
		t.Errorf("failed to convert security context")
	}
}

func TestPopulateWorkerNodeSpec(t *testing.T) {
	groupSpec := PopulateWorkerNodeSpec([]rayv1api.WorkerGroupSpec{workerSpecTest})[0]

	if groupSpec.ServiceAccount != "account" {
		t.Errorf("failed to convert service account")
	}
	if groupSpec.ImagePullSecret != "foo" {
		t.Errorf("failed to convert image pull secret")
	}
	if !reflect.DeepEqual(groupSpec.Annotations, expectedAnnotations) {
		t.Errorf("failed to convert annotations, got %v, expected %v", groupSpec.Annotations, expectedAnnotations)
	}
	if !reflect.DeepEqual(groupSpec.Labels, expectedLabels) {
		t.Errorf("failed to convert labels, got %v, expected %v", groupSpec.Labels, expectedLabels)
	}
	if !reflect.DeepEqual(groupSpec.Environment, expectedEnv) {
		t.Errorf("failed to convert environment, got %v, expected %v", groupSpec.Environment, expectedEnv)
	}
	if groupSpec.SecurityContext == nil || groupSpec.SecurityContext.Capabilities == nil || len(groupSpec.SecurityContext.Capabilities.Add) != 1 {
		t.Errorf("failed to convert security context")
	}
}

func TestAutoscalerOptions(t *testing.T) {
	options := convertAutoscalingOptions(autoscalerOptions)
	assert.Equal(t, options.IdleTimeoutSeconds, int32(60))
	assert.Equal(t, options.UpscalingMode, "Default")
	assert.Equal(t, options.Image, "Some Image")
	assert.Equal(t, options.ImagePullPolicy, "Always")
	assert.Equal(t, options.Cpu, "500m")
	assert.Equal(t, options.Memory, "512Mi")
	assert.Equal(t, len(options.Envs.Values), 1)
	assert.Equal(t, len(options.Envs.ValuesFrom), 2)
	assert.Equal(t, len(options.Volumes), 2)
}

func TestPopulateRayClusterSpec(t *testing.T) {
	cluster := FromCrdToApiCluster(&ClusterSpecTest, []corev1.Event{})
	if len(cluster.Annotations) != 1 {
		t.Errorf("failed to convert cluster's annotations")
	}
	assert.Equal(t, cluster.ClusterSpec.EnableInTreeAutoscaling, false)
	if cluster.ClusterSpec.AutoscalerOptions != nil {
		t.Errorf("unexpected autoscaler annotations")
	}
	cluster = FromCrdToApiCluster(&ClusterSpecAutoscalerTest, []corev1.Event{})
	assert.Equal(t, cluster.ClusterSpec.EnableInTreeAutoscaling, true)
	if cluster.ClusterSpec.AutoscalerOptions == nil {
		t.Errorf("autoscaler annotations not found")
	}
	assert.Equal(t, cluster.ClusterSpec.AutoscalerOptions.IdleTimeoutSeconds, int32(60))
	assert.Equal(t, cluster.ClusterSpec.AutoscalerOptions.UpscalingMode, "Default")
	assert.Equal(t, cluster.ClusterSpec.AutoscalerOptions.ImagePullPolicy, "Always")
	assert.Equal(t, cluster.ClusterSpec.AutoscalerOptions.Cpu, "500m")
	assert.Equal(t, cluster.ClusterSpec.AutoscalerOptions.Memory, "512Mi")
	assert.Equal(t, len(cluster.ClusterSpec.HeadGroupSpec.Environment.ValuesFrom), 4)
	for name, value := range cluster.ClusterSpec.HeadGroupSpec.Environment.ValuesFrom {
		switch name {
		case "REDIS_PASSWORD":
			assert.Equal(t, value.Source.String(), "SECRET")
			assert.Equal(t, value.Name, "redis-password-secret")
			assert.Equal(t, value.Key, "password")
		case "CONFIGMAP":
			assert.Equal(t, value.Source.String(), "CONFIGMAP")
			assert.Equal(t, value.Name, "special-config")
			assert.Equal(t, value.Key, "special.how")
		case "ResourceFieldRef":
			assert.Equal(t, value.Source.String(), "RESOURCEFIELD")
			assert.Equal(t, value.Name, "my-container")
			assert.Equal(t, value.Key, "resource")
		default:
			assert.Equal(t, value.Source.String(), "FIELD")
			assert.Equal(t, value.Name, "")
			assert.Equal(t, value.Key, "path")
		}
	}
}

func TestPopulateTemplate(t *testing.T) {
	template := FromKubeToAPIComputeTemplate(&configMapWithoutTolerations)
	if len(template.Tolerations) != 0 {
		t.Errorf("failed to convert config map, expected no tolerations, got %d", len(template.Tolerations))
	}

	template = FromKubeToAPIComputeTemplate(&configMapWithTolerations)
	if len(template.Tolerations) != 1 {
		t.Errorf("failed to convert config map, expected 1 toleration, got %d", len(template.Tolerations))
	}
	if template.Tolerations[0].Key != expectedTolerations.Key ||
		template.Tolerations[0].Operator != expectedTolerations.Operator ||
		template.Tolerations[0].Effect != expectedTolerations.Effect {
		t.Errorf("failed to convert config map, got %v, expected %v", tolerationToString(template.Tolerations[0]),
			tolerationToString(&expectedTolerations))
	}

	assert.Equal(t, uint32(4), template.Cpu, "CPU mismatch")
	assert.Equal(t, uint32(8), template.Memory, "Memory mismatch")
	assert.Equal(t, uint32(0), template.Gpu, "GPU mismatch")
	assert.Equal(
		t,
		map[string]uint32{"vpc.amazonaws.com/efa": 32},
		template.ExtendedResources,
		"Extended resources mismatch",
	)
}

func tolerationToString(toleration *api.PodToleration) string {
	return "Key: " + toleration.Key + " Operator: " + string(
		toleration.Operator,
	) + " Effect: " + string(
		toleration.Effect,
	)
}

func TestPopulateJob(t *testing.T) {
	job := FromCrdToApiJob(&JobNewClusterTest)
	fmt.Printf("jobWithCluster = %#v\n", job)
	assert.Equal(t, "test", job.Name)
	assert.Equal(t, "test", job.Namespace)
	assert.Equal(t, "user", job.User)
	assert.Greater(t, len(job.RuntimeEnv), 1)
	assert.Nil(t, job.ClusterSelector)
	assert.NotNil(t, job.ClusterSpec)

	job = FromCrdToApiJob(&JobExistingClusterTest)
	fmt.Printf("jobReferenceCluster = %#v\n", job)
	assert.Equal(t, "test", job.Name)
	assert.Equal(t, "test", job.Namespace)
	assert.Equal(t, "user", job.User)
	assert.Greater(t, len(job.RuntimeEnv), 1)
	assert.NotNil(t, job.ClusterSelector)
	assert.Nil(t, job.ClusterSpec)

	job = FromCrdToApiJob(&JobExistingClusterSubmitterTest)
	fmt.Printf("jobReferenceCluster = %#v\n", job)
	assert.Equal(t, "test", job.Name)
	assert.Equal(t, "test", job.Namespace)
	assert.Equal(t, "user", job.User)
	assert.Greater(t, len(job.RuntimeEnv), 1)
	assert.NotNil(t, job.ClusterSelector)
	assert.Nil(t, job.ClusterSpec)
	assert.Equal(t, "image", job.JobSubmitter.Image)
	assert.Equal(t, "2", job.JobSubmitter.Cpu)

	job = FromCrdToApiJob(&JobWithOutputTest)
	fmt.Printf("jobWithOutput = %#v\n", job)
	assert.Equal(t, time.Date(2024, 0o7, 25, 0, 0, 0, 0, time.UTC), job.StartTime.AsTime())
	assert.Nil(t, job.EndTime)
	assert.Equal(t, "RUNNING", job.JobStatus)
	assert.Equal(t, "Initializing", job.JobDeploymentStatus)
	assert.Equal(t, "Job is currently running", job.Message)
	assert.Equal(t, "raycluster-sample-xxxxx", job.RayClusterName)
}
