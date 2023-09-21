package util

import (
	"reflect"
	"testing"

	api "github.com/ray-project/kuberay/proto/go_client"
	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"

	"github.com/stretchr/testify/assert"
)

var apiServiceNoServe = &api.RayService{
	Name:        "test",
	Namespace:   "test",
	User:        "test",
	ClusterSpec: rayCluster.ClusterSpec,
}

var apiService2Serve = &api.RayService{
	Name:           "test",
	Namespace:      "test",
	User:           "test",
	ClusterSpec:    rayCluster.ClusterSpec,
	ServeConfig_V2: "Fake Yaml file",
	ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{
		ImportPath: "Some path",
	},
}

var apiServiceV1 = &api.RayService{
	Name:        "test",
	Namespace:   "test",
	User:        "test",
	ClusterSpec: rayCluster.ClusterSpec,
	ServeDeploymentGraphSpec: &api.ServeDeploymentGraphSpec{
		ImportPath: "Some path",
		RuntimeEnv: "Some environment",
		ServeConfigs: []*api.ServeConfig{
			{
				DeploymentName:       "test",
				Replicas:             2,
				MaxConcurrentQueries: 2,
			},
		},
	},
}

var apiServiceV2 = &api.RayService{
	Name:                            "test",
	Namespace:                       "test",
	User:                            "test",
	ServeConfig_V2:                  "Fake Yaml file",
	ClusterSpec:                     rayCluster.ClusterSpec,
	ServiceUnhealthySecondThreshold: 100,
}

func TestBuildService(t *testing.T) {
	_, err := NewRayService(apiServiceNoServe, map[string]*api.ComputeTemplate{"foo": &template})
	assert.NotNil(t, err)
	if err.Error() != "serve configuration is not defined" {
		t.Errorf("wrong error returned")
	}
	_, err = NewRayService(apiService2Serve, map[string]*api.ComputeTemplate{"foo": &template})
	assert.NotNil(t, err)
	if err.Error() != "two serve configuration are defined, only one is allowed" {
		t.Errorf("wrong error returned")
	}
	got, err := NewRayService(apiServiceV1, map[string]*api.ComputeTemplate{"foo": &template})
	assert.Nil(t, err)
	if reflect.DeepEqual(got.Spec.ServeDeploymentGraphSpec, rayv1alpha1.ServeDeploymentGraphSpec{}) {
		t.Errorf("Got empty V1")
	}
	if got.RayService.Spec.ServeConfigV2 != "" {
		t.Errorf("Got non empty V2")
	}
	assert.Equal(t, int32(2), *got.Spec.ServeDeploymentGraphSpec.ServeConfigSpecs[0].MaxConcurrentQueries)
	assert.Nil(t, got.Spec.ServiceUnhealthySecondThreshold)
	assert.Nil(t, got.Spec.DeploymentUnhealthySecondThreshold)

	got, err = NewRayService(apiServiceV2, map[string]*api.ComputeTemplate{"foo": &template})
	assert.Nil(t, err)
	if got.RayService.Spec.ServeConfigV2 == "" {
		t.Errorf("Got empty V2")
	}
	if !reflect.DeepEqual(got.Spec.ServeDeploymentGraphSpec, rayv1alpha1.ServeDeploymentGraphSpec{}) {
		t.Errorf("Got non empty V1")
	}
	assert.NotNil(t, got.Spec.ServiceUnhealthySecondThreshold)
	assert.Nil(t, got.Spec.DeploymentUnhealthySecondThreshold)
}
