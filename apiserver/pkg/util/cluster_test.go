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

func TestBuildVolumeMounts(t *testing.T) {
	targetVolume := v1.Volume{
		Name: testVolume.Name,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: testVolume.Source,
				Type: newHostPathType(string(v1.HostPathDirectory)),
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
				testVolume,
			},
			[]v1.Volume{targetVolume},
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

func TestBuildVolumes(t *testing.T) {
	targetVolumeMount := v1.VolumeMount{
		Name:      testVolume.Name,
		ReadOnly:  testVolume.ReadOnly,
		MountPath: testVolume.MountPath,
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
			},
			[]v1.VolumeMount{
				targetVolumeMount,
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
