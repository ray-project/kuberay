package client

import (
	"context"
	"testing"

	"github.com/ray-project/kuberay/kubectl-plugin/pkg/util"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicFake "k8s.io/client-go/dynamic/fake"
	kubeFake "k8s.io/client-go/kubernetes/fake"
)

func TestGetRayHeadSvcNameByRayCluster(t *testing.T) {
	kubeObjects := []runtime.Object{}

	dynamicObjects := []runtime.Object{
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "ray.io/v1",
				"kind":       "RayCluster",
				"metadata": map[string]interface{}{
					"name":      "raycluster-default",
					"namespace": "default",
				},
				"status": map[string]interface{}{
					"head": map[string]interface{}{
						"serviceName": "raycluster-default-head-svc",
					},
				},
			},
		},
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "ray.io/v1",
				"kind":       "RayCluster",
				"metadata": map[string]interface{}{
					"name":      "raycluster-test",
					"namespace": "test",
				},
				"status": map[string]interface{}{
					"head": map[string]interface{}{
						"serviceName": "raycluster-test-head-svc",
					},
				},
			},
		},
	}

	kubeClientSet := kubeFake.NewSimpleClientset(kubeObjects...)
	dynamicClient := dynamicFake.NewSimpleDynamicClient(runtime.NewScheme(), dynamicObjects...)
	client := NewClientForTesting(kubeClientSet, dynamicClient)

	tests := []struct {
		name         string
		namespace    string
		resourceName string
		serviceName  string
	}{
		{
			name:         "find service name in default namespace",
			namespace:    "default",
			resourceName: "raycluster-default",
			serviceName:  "raycluster-default-head-svc",
		},
		{
			name:         "find service name in test namespace",
			namespace:    "test",
			resourceName: "raycluster-test",
			serviceName:  "raycluster-test-head-svc",
		},
		{
			name:         "resource not found",
			namespace:    "default",
			resourceName: "raycluster-not-found",
			serviceName:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svcName, err := client.GetRayHeadSvcName(context.Background(), tc.namespace, util.RayCluster, tc.resourceName)
			if tc.serviceName == "" {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tc.serviceName, svcName)
			}
		})
	}
}

func TestGetRayHeadSvcNameByRayJob(t *testing.T) {
	kubeObjects := []runtime.Object{}

	dynamicObjects := []runtime.Object{
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "ray.io/v1",
				"kind":       "RayJob",
				"metadata": map[string]interface{}{
					"name":      "rayjob-default",
					"namespace": "default",
				},
				"status": map[string]interface{}{
					"rayClusterStatus": map[string]interface{}{
						"head": map[string]interface{}{
							"serviceName": "rayjob-default-raycluster-xxxxx-head-svc",
						},
					},
				},
			},
		},
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "ray.io/v1",
				"kind":       "RayJob",
				"metadata": map[string]interface{}{
					"name":      "rayjob-test",
					"namespace": "test",
				},
				"status": map[string]interface{}{
					"rayClusterStatus": map[string]interface{}{
						"head": map[string]interface{}{
							"serviceName": "rayjob-test-raycluster-xxxxx-head-svc",
						},
					},
				},
			},
		},
	}

	kubeClientSet := kubeFake.NewSimpleClientset(kubeObjects...)
	dynamicClient := dynamicFake.NewSimpleDynamicClient(runtime.NewScheme(), dynamicObjects...)
	client := NewClientForTesting(kubeClientSet, dynamicClient)

	tests := []struct {
		name         string
		namespace    string
		resourceName string
		serviceName  string
	}{
		{
			name:         "find service name in default namespace",
			namespace:    "default",
			resourceName: "rayjob-default",
			serviceName:  "rayjob-default-raycluster-xxxxx-head-svc",
		},
		{
			name:         "find service name in test namespace",
			namespace:    "test",
			resourceName: "rayjob-test",
			serviceName:  "rayjob-test-raycluster-xxxxx-head-svc",
		},
		{
			name:         "resource not found",
			namespace:    "default",
			resourceName: "rayjob-not-found",
			serviceName:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svcName, err := client.GetRayHeadSvcName(context.Background(), tc.namespace, util.RayJob, tc.resourceName)
			if tc.serviceName == "" {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tc.serviceName, svcName)
			}
		})
	}
}

func TestGetRayHeadSvcNameByRayService(t *testing.T) {
	kubeObjects := []runtime.Object{}

	dynamicObjects := []runtime.Object{
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "ray.io/v1",
				"kind":       "RayService",
				"metadata": map[string]interface{}{
					"name":      "rayservice-default",
					"namespace": "default",
				},
				"status": map[string]interface{}{
					"activeServiceStatus": map[string]interface{}{
						"rayClusterStatus": map[string]interface{}{
							"head": map[string]interface{}{
								"serviceName": "rayservice-default-raycluster-xxxxx-head-svc",
							},
						},
					},
				},
			},
		},
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "ray.io/v1",
				"kind":       "RayService",
				"metadata": map[string]interface{}{
					"name":      "rayservice-test",
					"namespace": "test",
				},
				"status": map[string]interface{}{
					"activeServiceStatus": map[string]interface{}{
						"rayClusterStatus": map[string]interface{}{
							"head": map[string]interface{}{
								"serviceName": "rayservice-test-raycluster-xxxxx-head-svc",
							},
						},
					},
				},
			},
		},
	}

	kubeClientSet := kubeFake.NewSimpleClientset(kubeObjects...)
	dynamicClient := dynamicFake.NewSimpleDynamicClient(runtime.NewScheme(), dynamicObjects...)
	client := NewClientForTesting(kubeClientSet, dynamicClient)

	tests := []struct {
		name         string
		namespace    string
		resourceName string
		serviceName  string
	}{
		{
			name:         "find service name in default namespace",
			namespace:    "default",
			resourceName: "rayservice-default",
			serviceName:  "rayservice-default-raycluster-xxxxx-head-svc",
		},
		{
			name:         "find service name in test namespace",
			namespace:    "test",
			resourceName: "rayservice-test",
			serviceName:  "rayservice-test-raycluster-xxxxx-head-svc",
		},
		{
			name:         "resource not found",
			namespace:    "default",
			resourceName: "rayservice-not-found",
			serviceName:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svcName, err := client.GetRayHeadSvcName(context.Background(), tc.namespace, util.RayService, tc.resourceName)
			if tc.serviceName == "" {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
				assert.Equal(t, tc.serviceName, svcName)
			}
		})
	}
}
