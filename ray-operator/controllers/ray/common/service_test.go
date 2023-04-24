package common

import (
	"fmt"
	"reflect"
	"testing"

	rayiov1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"

	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var instanceWithWrongSvc = &rayiov1alpha1.RayCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "raycluster-sample",
		Namespace: "default",
	},
	Spec: rayiov1alpha1.RayClusterSpec{
		RayVersion: "1.0",
		HeadGroupSpec: rayiov1alpha1.HeadGroupSpec{
			Replicas: pointer.Int32Ptr(1),
			RayStartParams: map[string]string{
				"port":                "6379",
				"object-manager-port": "12345",
				"node-manager-port":   "12346",
				"object-store-memory": "100000000",
				"num-cpus":            "1",
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Labels: map[string]string{
						"rayCluster": "raycluster-sample",
						"groupName":  "headgroup",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "ray-head",
							Image: "rayproject/autoscaler",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 6379,
									Name:          "gcs",
								},
								{
									ContainerPort: 8265,
								},
							},
							Command: []string{"python"},
							Args:    []string{"/opt/code.py"},
							Env: []corev1.EnvVar{
								{
									Name: "MY_POD_IP",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "status.podIP",
										},
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

func TestBuildServiceForHeadPod(t *testing.T) {
	svc, err := BuildServiceForHeadPod(*instanceWithWrongSvc, nil, nil)
	assert.Nil(t, err)

	actualResult := svc.Spec.Selector[RayClusterLabelKey]
	expectedResult := string(instanceWithWrongSvc.Name)
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	actualResult = svc.Spec.Selector[RayNodeTypeLabelKey]
	expectedResult = string(rayiov1alpha1.HeadNode)
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	actualResult = svc.Spec.Selector[KubernetesApplicationNameLabelKey]
	expectedResult = ApplicationName
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	ports := svc.Spec.Ports
	expectedResult = DefaultServiceAppProtocol
	for _, port := range ports {
		if *port.AppProtocol != DefaultServiceAppProtocol {
			t.Fatalf("Expected `%v` but got `%v`", expectedResult, *port.AppProtocol)
		}
	}
}

func TestBuildServiceForHeadPodWithAppNameLabel(t *testing.T) {
	labels := make(map[string]string)
	labels[KubernetesApplicationNameLabelKey] = "testname"

	svc, err := BuildServiceForHeadPod(*instanceWithWrongSvc, labels, nil)
	assert.Nil(t, err)

	actualResult := svc.Spec.Selector[KubernetesApplicationNameLabelKey]
	expectedResult := "testname"
	if !reflect.DeepEqual(expectedResult, actualResult) {
		t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
	}

	actualLength := len(svc.Spec.Selector)
	// We have 5 default labels in `BuildServiceForHeadPod`, and `KubernetesApplicationNameLabelKey`
	// is one of the default labels. Hence, `expectedLength` should also be 5.
	expectedLength := 5
	if actualLength != expectedLength {
		t.Fatalf("Expected `%v` but got `%v`", expectedLength, actualLength)
	}
}

func TestBuildServiceForHeadPodWithAnnotations(t *testing.T) {
	annotations := make(map[string]string)
	annotations["key1"] = "testvalue1"
	annotations["key2"] = "testvalue2"
	svc, err := BuildServiceForHeadPod(*instanceWithWrongSvc, nil, annotations)
	assert.Nil(t, err)

	if !reflect.DeepEqual(svc.ObjectMeta.Annotations, annotations) {
		t.Fatalf("Expected `%v` but got `%v`", annotations, svc.ObjectMeta.Annotations)
	}
}

func TestGetPortsFromCluster(t *testing.T) {
	svcPorts, err := getPortsFromCluster(*instanceWithWrongSvc)
	assert.Nil(t, err)

	// getPortsFromCluster creates service ports based on the container ports.
	// It will assign a generated service port name if the container port name
	// is not defined. To compare created service ports with container ports,
	// all generated service port names need to be reverted to empty strings.
	svcNames := map[int32]string{}
	for name, port := range svcPorts {
		if name == (fmt.Sprint(port) + "-port") {
			name = ""
		}
		svcNames[port] = name
	}

	index := utils.FindRayContainerIndex(instanceWithWrongSvc.Spec.HeadGroupSpec.Template.Spec)
	cPorts := instanceWithWrongSvc.Spec.HeadGroupSpec.Template.Spec.Containers[index].Ports

	for _, cPort := range cPorts {
		expectedResult := cPort.Name
		actualResult := svcNames[cPort.ContainerPort]
		if !reflect.DeepEqual(expectedResult, actualResult) {
			t.Fatalf("Expected `%v` but got `%v`", expectedResult, actualResult)
		}
	}
}

func TestGetServicePortsWithMetricsPort(t *testing.T) {
	cluster := instanceWithWrongSvc.DeepCopy()

	// Test case 1: No ports are specified by the user.
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{}
	ports := getServicePorts(*cluster)
	// Verify that getServicePorts sets the default metrics port when the user doesn't specify any ports.
	if ports[DefaultMetricsName] != int32(DefaultMetricsPort) {
		t.Fatalf("Expected `%v` but got `%v`", int32(DefaultMetricsPort), ports[DefaultMetricsName])
	}

	// Test case 2: Only a random port is specified by the user.
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
		{
			Name:          "random",
			ContainerPort: 1234,
		},
	}
	ports = getServicePorts(*cluster)
	// Verify that getServicePorts sets the default metrics port when the user doesn't specify the metrics port but specifies other ports.
	if ports[DefaultMetricsName] != int32(DefaultMetricsPort) {
		t.Fatalf("Expected `%v` but got `%v`", int32(DefaultMetricsPort), ports[DefaultMetricsName])
	}

	// Test case 3: A custom metrics port is specified by the user.
	customMetricsPort := int32(DefaultMetricsPort) + 1
	metricsPort := corev1.ContainerPort{
		Name:          DefaultMetricsName,
		ContainerPort: customMetricsPort,
	}
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Ports = append(cluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Ports, metricsPort)
	ports = getServicePorts(*cluster)
	// Verify that getServicePorts uses the user's custom metrics port when the user specifies the metrics port.
	if ports[DefaultMetricsName] != customMetricsPort {
		t.Fatalf("Expected `%v` but got `%v`", customMetricsPort, ports[DefaultMetricsName])
	}
}

func TestUserSpecifiedHeadService(t *testing.T) {
	// Use any RayCluster instance as a base for the test.
	testRayClusterWithHeadService := instanceWithWrongSvc.DeepCopy()

	// Set user-specified head service with user-specified labels, annotations, and ports.
	userLabels := map[string]string{"userLabelKey": "userLabelValue", RayClusterLabelKey: "userClusterName"} // Override default cluster name
	userAnnotations := map[string]string{"userAnnotationKey": "userAnnotationValue"}
	userPort := corev1.ServicePort{Name: "userPort", Port: 12345}
	userPortOverride := corev1.ServicePort{Name: DefaultClientPortName, Port: 98765} // Override default client port (10001)
	userPorts := []corev1.ServicePort{userPort, userPortOverride}
	testRayClusterWithHeadService.Spec.HeadGroupSpec.HeadService = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      userLabels,
			Annotations: userAnnotations,
		},
		Spec: corev1.ServiceSpec{
			Ports: userPorts,
		},
	}

	headService, err := BuildServiceForHeadPod(*testRayClusterWithHeadService, nil, nil)
	if err != nil {
		t.Errorf("failed to build head service: %v", err)
	}

	// Test merged labels. The user-defined head service should have priority.
	for k, v := range userLabels {
		if headService.ObjectMeta.Labels[k] != v {
			t.Errorf("User label not found or incorrect value: key=%s, expected value=%s, actual value=%s", k, v, headService.ObjectMeta.Labels[k])
		}
	}

	// Test merged annotations
	for k, v := range userAnnotations {
		if headService.ObjectMeta.Annotations[k] != v {
			t.Errorf("User annotation not found or incorrect value: key=%s, expected value=%s, actual value=%s", k, v, headService.ObjectMeta.Annotations[k])
		}
	}

	// Test merged ports
	for _, p := range userPorts {
		found := false
		for _, hp := range headService.Spec.Ports {
			if p.Name == hp.Name && p.Port == hp.Port {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("User port not found: %v", p)
		}
	}
}
