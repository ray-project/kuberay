package util

import (
	"reflect"
	"testing"

	api "github.com/ray-project/kuberay/proto/go_client"
	v1 "k8s.io/api/core/v1"
)

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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildVols(tt.apiVolume)
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
