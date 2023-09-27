package util

import (
	"reflect"
	"testing"

	api "github.com/ray-project/kuberay/proto/go_client"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var sizelimit = resource.MustParse("100Gi")

var testVolume = &api.Volume{
	Name:       "hdfs",
	VolumeType: api.Volume_HOST_PATH,
	Source:     "/opt/hdfs",
	MountPath:  "/mnt/hdfs",
	ReadOnly:   true,
}

// There is only an fake case for test both MountPropagationMode and file type
// in real case hostToContainer mode may only valid for directory
var testFileVolume = &api.Volume{
	Name:                 "test-file",
	VolumeType:           api.Volume_HOST_PATH,
	MountPropagationMode: api.Volume_HOSTTOCONTAINER,
	Source:               "/root/proc/stat",
	MountPath:            "/proc/stat",
	HostPathType:         api.Volume_FILE,
	ReadOnly:             true,
}

var testPVCVolume = &api.Volume{
	Name:       "test-pvc",
	VolumeType: api.Volume_PERSISTENT_VOLUME_CLAIM,
	MountPath:  "/pvc/dir",
	ReadOnly:   true,
}

var testEphemeralVolume = &api.Volume{
	Name:       "test-ephemeral",
	VolumeType: api.Volume_EPHEMERAL,
	MountPath:  "/ephimeral/dir",
	Storage:    "10Gi",
}

var testConfigMapVolume = &api.Volume{
	Name:       "configMap",
	MountPath:  "/tmp/configmap",
	VolumeType: api.Volume_CONFIGMAP,
	Source:     "my-config-map",
	Items: map[string]string{
		"key": "path",
	},
}

var testSecretVolume = &api.Volume{
	Name:       "secret",
	MountPath:  "/tmp/secret",
	VolumeType: api.Volume_SECRET,
	Source:     "my-secret",
}

var testEmptyDirVolume = &api.Volume{
	Name:       "emptyDir",
	MountPath:  "/tmp/emptydir",
	VolumeType: api.Volume_EMPTY_DIR,
	Storage:    "100Gi",
}

// Spec for testing
var headGroup = api.HeadGroupSpec{
	ComputeTemplate: "foo",
	Image:           "bar",
	ServiceType:     "ClusterIP",
	RayStartParams: map[string]string{
		"dashboard-host":      "0.0.0.0",
		"metrics-export-port": "8080",
		"num-cpus":            "0",
	},
	ServiceAccount:  "account",
	ImagePullSecret: "foo",
	EnableIngress:   true,
	Environment: map[string]string{
		"foo": "bar",
	},
	Annotations: map[string]string{
		"foo": "bar",
	},
	Labels: map[string]string{
		"foo": "bar",
	},
}

var workerGroup = api.WorkerGroupSpec{
	GroupName:       "wg",
	ComputeTemplate: "foo",
	Image:           "bar",
	Replicas:        5,
	MinReplicas:     5,
	MaxReplicas:     5,
	RayStartParams: map[string]string{
		"node-ip-address": "$MY_POD_IP",
	},
	ServiceAccount:  "account",
	ImagePullSecret: "foo",
	Environment: map[string]string{
		"foo": "bar",
	},
	Annotations: map[string]string{
		"foo": "bar",
	},
	Labels: map[string]string{
		"foo": "bar",
	},
}

var rayCluster = api.Cluster{
	Name:      "test_cluster",
	Namespace: "foo",
	Annotations: map[string]string{
		"kubernetes.io/ingress.class": "nginx",
	},
	ClusterSpec: &api.ClusterSpec{
		HeadGroupSpec: &headGroup,
		WorkerGroupSpec: []*api.WorkerGroupSpec{
			&workerGroup,
		},
	},
}

var template = api.ComputeTemplate{
	Name:      "",
	Namespace: "",
	Cpu:       2,
	Memory:    8,
	Tolerations: []*api.PodToleration{
		{
			Key:      "blah1",
			Operator: "Exists",
			Effect:   "NoExecute",
		},
	},
}

var expectedToleration = v1.Toleration{
	Key:      "blah1",
	Operator: "Exists",
	Effect:   "NoExecute",
}

var expectedLabels = map[string]string{
	"foo": "bar",
}

func TestBuildVolumes(t *testing.T) {
	targetVolume := v1.Volume{
		Name: testVolume.Name,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: testVolume.Source,
				Type: newHostPathType(string(v1.HostPathDirectory)),
			},
		},
	}
	targetFileVolume := v1.Volume{
		Name: testFileVolume.Name,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: testFileVolume.Source,
				Type: newHostPathType(string(v1.HostPathFile)),
			},
		},
	}

	targetPVCVolume := v1.Volume{
		Name: testPVCVolume.Name,
		VolumeSource: v1.VolumeSource{
			PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
				ClaimName: testPVCVolume.Name,
				ReadOnly:  testPVCVolume.ReadOnly,
			},
		},
	}

	targetEphemeralVolume := v1.Volume{
		Name: testEphemeralVolume.Name,
		VolumeSource: v1.VolumeSource{
			Ephemeral: &v1.EphemeralVolumeSource{
				VolumeClaimTemplate: &v1.PersistentVolumeClaimTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app.kubernetes.io/managed-by": "kuberay-apiserver",
						},
					},
					Spec: v1.PersistentVolumeClaimSpec{
						AccessModes: []v1.PersistentVolumeAccessMode{
							v1.ReadWriteOnce,
						},
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceStorage: resource.MustParse(testEphemeralVolume.Storage),
							},
						},
					},
				},
			},
		},
	}

	targetConfigMapVolume := v1.Volume{
		Name: "configMap",
		VolumeSource: v1.VolumeSource{
			ConfigMap: &v1.ConfigMapVolumeSource{
				LocalObjectReference: v1.LocalObjectReference{
					Name: "my-config-map",
				},
				Items: []v1.KeyToPath{
					{
						Key:  "key",
						Path: "path",
					},
				},
			},
		},
	}

	targetSecretVolume := v1.Volume{
		Name: "secret",
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName: "my-secret",
			},
		},
	}

	targetEmptyDirVolume := v1.Volume{
		Name: "emptyDir",
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{
				SizeLimit: &sizelimit,
			},
		},
	}

	tests := []struct {
		name      string
		apiVolume []*api.Volume
		expect    []v1.Volume
	}{
		{
			"normal test",
			[]*api.Volume{
				testVolume, testFileVolume,
			},
			[]v1.Volume{targetVolume, targetFileVolume},
		},
		{
			"pvc test",
			[]*api.Volume{testPVCVolume},
			[]v1.Volume{targetPVCVolume},
		},
		{
			"ephemeral test",
			[]*api.Volume{testEphemeralVolume},
			[]v1.Volume{targetEphemeralVolume},
		},
		{
			"configmap test",
			[]*api.Volume{testConfigMapVolume},
			[]v1.Volume{targetConfigMapVolume},
		},
		{
			"secret test",
			[]*api.Volume{testSecretVolume},
			[]v1.Volume{targetSecretVolume},
		},
		{
			"empty dir test",
			[]*api.Volume{testEmptyDirVolume},
			[]v1.Volume{targetEmptyDirVolume},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildVols(tt.apiVolume)
			assert.Nil(t, err)
			if !reflect.DeepEqual(got, tt.expect) {
				t.Errorf("failed for %s ..., got %v, expected %v", tt.name, got, tt.expect)
			}
		})
	}
}

func TestBuildVolumeMounts(t *testing.T) {
	hostToContainer := v1.MountPropagationHostToContainer
	targetVolumeMount := v1.VolumeMount{
		Name:      testVolume.Name,
		ReadOnly:  testVolume.ReadOnly,
		MountPath: testVolume.MountPath,
	}
	targetFileVolumeMount := v1.VolumeMount{
		Name:             testFileVolume.Name,
		ReadOnly:         testFileVolume.ReadOnly,
		MountPath:        testFileVolume.MountPath,
		MountPropagation: &hostToContainer,
	}
	targetPVCVolumeMount := v1.VolumeMount{
		Name:      testPVCVolume.Name,
		ReadOnly:  testPVCVolume.ReadOnly,
		MountPath: testPVCVolume.MountPath,
	}
	tests := []struct {
		name      string
		apiVolume []*api.Volume
		expect    []v1.VolumeMount
	}{
		{
			"normal test",
			[]*api.Volume{
				testVolume,
				testFileVolume,
			},
			[]v1.VolumeMount{
				targetVolumeMount,
				targetFileVolumeMount,
			},
		},
		{
			"pvc test",
			[]*api.Volume{testPVCVolume},
			[]v1.VolumeMount{targetPVCVolumeMount},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildVolumeMounts(tt.apiVolume)
			if !reflect.DeepEqual(got, tt.expect) {
				t.Errorf("failed for %s ..., got %v, expected %v", tt.name, got, tt.expect)
			}
		})
	}
}

func TestBuildHeadPodTemplate(t *testing.T) {
	podSpec, err := buildHeadPodTemplate("2.4", make(map[string]string), &headGroup, &template)
	assert.Nil(t, err)

	if podSpec.Spec.ServiceAccountName != "account" {
		t.Errorf("failed to propagate service account")
	}
	if podSpec.Spec.ImagePullSecrets[0].Name != "foo" {
		t.Errorf("failed to propagate image pull secret")
	}
	if !containsEnv(podSpec.Spec.Containers[0].Env, "foo", "bar") {
		t.Errorf("failed to propagate environment")
	}
	if len(podSpec.Spec.Tolerations) != 1 {
		t.Errorf("failed to propagate tolerations, expected 1, got %d", len(podSpec.Spec.Tolerations))
	}
	if !reflect.DeepEqual(podSpec.Spec.Tolerations[0], expectedToleration) {
		t.Errorf("failed to propagate annotations, got %v, expected %v", tolerationToString(&podSpec.Spec.Tolerations[0]),
			tolerationToString(&expectedToleration))
	}
	if val, exists := podSpec.Annotations["foo"]; !exists || val != "bar" {
		t.Errorf("failed to convert annotations")
	}
	if !reflect.DeepEqual(podSpec.Labels, expectedLabels) {
		t.Errorf("failed to convert labels, got %v, expected %v", podSpec.Labels, expectedLabels)
	}
}

func TestBuildRayCluster(t *testing.T) {
	cluster, err := NewRayCluster(&rayCluster, map[string]*api.ComputeTemplate{"foo": &template})
	assert.Nil(t, err)
	if len(cluster.ObjectMeta.Annotations) != 1 {
		t.Errorf("failed to propagate annotations")
	}
	if !(*cluster.Spec.HeadGroupSpec.EnableIngress) {
		t.Errorf("failed to propagate create Ingress")
	}
}

func TestBuilWorkerPodTemplate(t *testing.T) {
	podSpec, err := buildWorkerPodTemplate("2.4", make(map[string]string), &workerGroup, &template)
	assert.Nil(t, err)

	if podSpec.Spec.ServiceAccountName != "account" {
		t.Errorf("failed to propagate service account")
	}
	if podSpec.Spec.ImagePullSecrets[0].Name != "foo" {
		t.Errorf("failed to propagate image pull secret")
	}
	if !containsEnv(podSpec.Spec.Containers[0].Env, "foo", "bar") {
		t.Errorf("failed to propagate environment")
	}
	if len(podSpec.Spec.Tolerations) != 1 {
		t.Errorf("failed to propagate tolerations, expected 1, got %d", len(podSpec.Spec.Tolerations))
	}
	if !reflect.DeepEqual(podSpec.Spec.Tolerations[0], expectedToleration) {
		t.Errorf("failed to propagate annotations, got %v, expected %v", tolerationToString(&podSpec.Spec.Tolerations[0]),
			tolerationToString(&expectedToleration))
	}
	if val, exists := podSpec.Annotations["foo"]; !exists || val != "bar" {
		t.Errorf("failed to convert annotations")
	}
	if !reflect.DeepEqual(podSpec.Labels, expectedLabels) {
		t.Errorf("failed to convert labels, got %v, expected %v", podSpec.Labels, expectedLabels)
	}
}

func containsEnv(envs []v1.EnvVar, key string, val string) bool {
	for _, env := range envs {
		if env.Name == key && env.Value == val {
			return true
		}
	}
	return false
}

func tolerationToString(toleration *v1.Toleration) string {
	return "Key: " + toleration.Key + " Operator: " + string(toleration.Operator) + " Effect: " + string(toleration.Effect)
}
