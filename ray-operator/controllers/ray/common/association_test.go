package common

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
)

func TestRayServiceServeServiceNamespacedName(t *testing.T) {
	svc, err := BuildServeServiceForRayService(context.Background(), *serviceInstance, *instanceWithWrongSvc)
	assert.Nil(t, err)
	namespaced := RayServiceServeServiceNamespacedName(serviceInstance)
	if namespaced.Name != svc.Name {
		t.Fatalf("Expected `%v` but got `%v`", svc.Name, namespaced.Name)
	}
	if namespaced.Namespace != svc.Namespace {
		t.Fatalf("Expected `%v` but got `%v`", svc.Namespace, namespaced.Namespace)
	}
}

func TestRayServiceServeServiceNamespacedNameForUserSpecifiedServeService(t *testing.T) {
	testRayServiceWithServeService := serviceInstance.DeepCopy()
	testRayServiceWithServeService.Spec.ServeService = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "user-custom-name",
		},
	}
	svc, err := BuildServeServiceForRayService(context.Background(), *testRayServiceWithServeService, *instanceWithWrongSvc)
	assert.Nil(t, err)

	namespaced := RayServiceServeServiceNamespacedName(testRayServiceWithServeService)
	if namespaced.Namespace != svc.Namespace {
		t.Fatalf("Expected `%v` but got `%v`", svc.Namespace, namespaced.Namespace)
	}
	if namespaced.Name != svc.Name {
		t.Fatalf("Expected `%v` but got `%v`", svc.Name, namespaced.Name)
	}
	if namespaced.Name != "user-custom-name" {
		t.Fatalf("Expected `%v` but got `%v`", "user-custom-name", namespaced.Name)
	}
}

// TestRayClusterServeServiceNamespacedName tests the function for generating a NamespacedName for a RayCluster's serve service
func TestRayClusterServeServiceNamespacedName(t *testing.T) {
	instance := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-example",
			Namespace: "default",
		},
	}
	expected := types.NamespacedName{
		Namespace: "default",
		Name:      utils.GenerateServeServiceName(instance.Name),
	}
	result := RayClusterServeServiceNamespacedName(instance)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}

// TestRayClusterAutoscalerRoleNamespacedName tests the function for generating a NamespacedName for a RayCluster's autoscaler role
func TestRayClusterAutoscalerRoleNamespacedName(t *testing.T) {
	instance := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-autoscaler",
			Namespace: "default",
		},
	}
	expected := types.NamespacedName{
		Namespace: "default",
		Name:      instance.Name,
	}
	result := RayClusterAutoscalerRoleNamespacedName(instance)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}

// TestRayClusterAutoscalerRoleBindingNamespacedName tests the function for generating a NamespacedName for a RayCluster's autoscaler role binding
func TestRayClusterAutoscalerRoleBindingNamespacedName(t *testing.T) {
	instance := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-autoscaler-binding",
			Namespace: "default",
		},
	}
	expected := types.NamespacedName{
		Namespace: "default",
		Name:      instance.Name,
	}
	result := RayClusterAutoscalerRoleBindingNamespacedName(instance)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}

// TestRayClusterAutoscalerServiceAccountNamespacedName tests the function for generating a NamespacedName for a RayCluster's autoscaler service account
func TestRayClusterAutoscalerServiceAccountNamespacedName(t *testing.T) {
	instance := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-autoscaler-account",
			Namespace: "default",
		},
	}
	expected := types.NamespacedName{
		Namespace: "default",
		Name:      utils.GetHeadGroupServiceAccountName(instance),
	}
	result := RayClusterAutoscalerServiceAccountNamespacedName(instance)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}

func TestRayClusterHeadlessServiceListOptions(t *testing.T) {
	instance := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-headless",
			Namespace: "test-ns",
		},
	}
	headlessSvc, err := BuildHeadlessServiceForRayCluster(*instance)
	assert.Nil(t, err)

	rayClusterName := ""
	for k, v := range headlessSvc.Labels {
		if k == utils.RayClusterHeadlessServiceLabelKey {
			rayClusterName = v
			break
		}
	}
	assert.Equal(t, rayClusterName, instance.Name)

	expected := []client.ListOption{
		client.InNamespace(headlessSvc.Namespace),
		client.MatchingLabels(map[string]string{utils.RayClusterHeadlessServiceLabelKey: rayClusterName}),
	}
	result := RayClusterHeadlessServiceListOptions(instance)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}

func TestRayClusterHeadServiceListOptions(t *testing.T) {
	instance := rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster",
			Namespace: "test-ns",
		},
	}

	labels := HeadServiceLabels(instance)
	delete(labels, utils.KubernetesCreatedByLabelKey)
	delete(labels, utils.KubernetesApplicationNameLabelKey)

	expected := []client.ListOption{
		client.InNamespace(instance.Namespace),
		client.MatchingLabels(labels),
	}
	result := RayClusterHeadServiceListOptions(&instance)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}

// TestRayServiceActiveRayClusterNamespacedName tests the function for generating a NamespacedName for a RayService's active RayCluster
func TestRayServiceActiveRayClusterNamespacedName(t *testing.T) {
	rayService := &rayv1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rayservice-active",
			Namespace: "default",
		},
		Status: rayv1.RayServiceStatuses{
			ActiveServiceStatus: rayv1.RayServiceStatus{
				RayClusterName: "active-ray-cluster",
			},
		},
	}
	expected := types.NamespacedName{
		Namespace: "default",
		Name:      "active-ray-cluster",
	}
	result := RayServiceActiveRayClusterNamespacedName(rayService)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}

// TestRayServicePendingRayClusterNamespacedName tests the function for generating a NamespacedName for a RayService's pending RayCluster
func TestRayServicePendingRayClusterNamespacedName(t *testing.T) {
	rayService := &rayv1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rayservice-pending",
			Namespace: "default",
		},
		Status: rayv1.RayServiceStatuses{
			PendingServiceStatus: rayv1.RayServiceStatus{
				RayClusterName: "pending-ray-cluster",
			},
		},
	}
	expected := types.NamespacedName{
		Namespace: "default",
		Name:      "pending-ray-cluster",
	}
	result := RayServicePendingRayClusterNamespacedName(rayService)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}

// TestRayJobK8sJobNamespacedName tests the function for generating a NamespacedName for a RayJob's Kubernetes Job
func TestRayJobK8sJobNamespacedName(t *testing.T) {
	rayJob := &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rayjob-k8s",
			Namespace: "default",
		},
	}
	expected := types.NamespacedName{
		Namespace: "default",
		Name:      "rayjob-k8s",
	}
	result := RayJobK8sJobNamespacedName(rayJob)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}

// TestRayJobRayClusterNamespacedName tests the function for generating a NamespacedName for a RayJob's RayCluster
func TestRayJobRayClusterNamespacedName(t *testing.T) {
	rayJob := &rayv1.RayJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rayjob-cluster",
			Namespace: "default",
		},
		Status: rayv1.RayJobStatus{
			RayClusterName: "associated-ray-cluster",
		},
	}
	expected := types.NamespacedName{
		Namespace: "default",
		Name:      "associated-ray-cluster",
	}
	result := RayJobRayClusterNamespacedName(rayJob)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}
