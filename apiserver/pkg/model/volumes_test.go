package model

import (
	"fmt"
	"reflect"
	"testing"

	api "github.com/ray-project/kuberay/proto/go_client"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	hostToContainer = v1.MountPropagationHostToContainer
	bidirectonal    = v1.MountPropagationBidirectional
)

var podTemplateTest = v1.PodTemplateSpec{
	Spec: v1.PodSpec{
		Containers: []v1.Container{
			{
				Name:  "ray-head-test",
				Image: "blah",
				VolumeMounts: []v1.VolumeMount{
					{
						Name:             "hostPath",
						MountPath:        "/tmp/hostPath",
						MountPropagation: &hostToContainer,
					},
					{
						Name:             "pvc",
						MountPath:        "/tmp/pvc",
						MountPropagation: &bidirectonal,
					},
					{
						Name:      "ephemeral",
						MountPath: "/tmp/ephemeral",
					},
				},
			},
		},
		Volumes: []v1.Volume{
			{
				Name: "hostPath",
				VolumeSource: v1.VolumeSource{
					HostPath: &v1.HostPathVolumeSource{
						Path: "/tmp",
						Type: newHostPathType(string(v1.HostPathDirectory)),
					},
				},
			},
			{
				Name: "pvc",
				VolumeSource: v1.VolumeSource{
					PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
						ClaimName: "pvc",
						ReadOnly:  false,
					},
				},
			},
			{
				Name: "ephemeral",
				VolumeSource: v1.VolumeSource{
					Ephemeral: &v1.EphemeralVolumeSource{
						VolumeClaimTemplate: &v1.PersistentVolumeClaimTemplate{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"app.kubernetes.io/managed-by": "kuberay-apiserver",
								},
							},
							Spec: v1.PersistentVolumeClaimSpec{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceStorage: resource.MustParse("5Gi"),
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

var expectedVolumes = []*api.Volume{
	{
		Name:                 "hostPath",
		Source:               "/tmp",
		MountPath:            "/tmp/hostPath",
		VolumeType:           api.Volume_HOST_PATH,
		HostPathType:         api.Volume_DIRECTORY,
		MountPropagationMode: api.Volume_HOSTTOCONTAINER,
	},
	{
		Name:                 "pvc",
		MountPath:            "/tmp/pvc",
		VolumeType:           api.Volume_PERSISTENT_VOLUME_CLAIM,
		MountPropagationMode: api.Volume_BIDIRECTIONAL,
		ReadOnly:             false,
	},
	{
		Name:                 "ephemeral",
		MountPath:            "/tmp/ephemeral",
		MountPropagationMode: api.Volume_NONE,
		VolumeType:           api.Volume_EPHEMERAL,
		Storage:              "5Gi",
		AccessMode:           api.Volume_RWO,
	},
}

// Build host path
func newHostPathType(pathType string) *v1.HostPathType {
	hostPathType := new(v1.HostPathType)
	*hostPathType = v1.HostPathType(pathType)
	return hostPathType
}

func TestPopulateVolumes(t *testing.T) {
	volumes := PopulateVolumes(&podTemplateTest)
	for i, vol := range volumes {
		fmt.Printf("volume = %#v\n", vol)
		if !reflect.DeepEqual(vol, expectedVolumes[i]) {
			t.Errorf("failed volumes conversion, got %v, expected %v", volumes, expectedVolumes)
		}
	}
}
