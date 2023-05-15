package common

import (
	"encoding/json"
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

var (
	headServiceAnnotationKey1   = "HeadServiceAnnotationKey1"
	headServiceAnnotationValue1 = "HeadServiceAnnotationValue1"
	headServiceAnnotationKey2   = "HeadServiceAnnotationKey2"
	headServiceAnnotationValue2 = "HeadServiceAnnotationValue2"
	instanceWithWrongSvc        = &rayiov1alpha1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "raycluster-sample",
			Namespace: "default",
		},
		Spec: rayiov1alpha1.RayClusterSpec{
			RayVersion: "1.0",
			HeadServiceAnnotations: map[string]string{
				headServiceAnnotationKey1: headServiceAnnotationValue1,
				headServiceAnnotationKey2: headServiceAnnotationValue2,
			},
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
							"groupName": "headgroup",
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
)

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
	userAnnotations := map[string]string{"userAnnotationKey": "userAnnotationValue", headServiceAnnotationKey1: "user_override"}
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

	headService, err := BuildServiceForHeadPod(*testRayClusterWithHeadService, nil, testRayClusterWithHeadService.Spec.HeadServiceAnnotations)
	if err != nil {
		t.Errorf("failed to build head service: %v", err)
	}

	// Test merged labels. In the case of overlap (RayClusterLabelKey) the user label should be ignored.
	for k, v := range userLabels {
		if headService.ObjectMeta.Labels[k] != v && k != RayClusterLabelKey {
			t.Errorf("User label not found or incorrect value: key=%s, expected value=%s, actual value=%s", k, v, headService.ObjectMeta.Labels[k])
		}
	}
	if headService.ObjectMeta.Labels[RayClusterLabelKey] != testRayClusterWithHeadService.ObjectMeta.Name {
		t.Errorf("User cluster name label not found or incorrect value: key=%s, expected value=%s, actual value=%s", RayClusterLabelKey, testRayClusterWithHeadService.ObjectMeta.Name, headService.ObjectMeta.Labels[RayClusterLabelKey])
	}
	// Test merged annotations. In the case of overlap (HeadServiceAnnotationKey1) the user annotation should be ignored.
	for k, v := range userAnnotations {
		if headService.ObjectMeta.Annotations[k] != v && k != headServiceAnnotationKey1 {
			t.Errorf("User annotation not found or incorrect value: key=%s, expected value=%s, actual value=%s", k, v, headService.ObjectMeta.Annotations[k])
		}
	}
	if headService.ObjectMeta.Annotations[headServiceAnnotationKey1] != headServiceAnnotationValue1 {
		t.Errorf("User annotation not found or incorrect value: key=%s, expected value=%s, actual value=%s", headServiceAnnotationKey1, headServiceAnnotationValue1, headService.ObjectMeta.Annotations[headServiceAnnotationKey1])
	}
	// HeadServiceAnnotationKey2 should be present with value HeadServiceAnnotationValue2 since it was only specified in HeadServiceAnnotations.
	if headService.ObjectMeta.Annotations[headServiceAnnotationKey2] != headServiceAnnotationValue2 {
		t.Errorf("User annotation not found or incorrect value: key=%s, expected value=%s, actual value=%s", headServiceAnnotationKey2, headServiceAnnotationValue2, headService.ObjectMeta.Annotations[headServiceAnnotationKey2])
	}

	// Test merged ports. In the case of overlap (DefaultClientPortName) the user port should be ignored.
	// DEBUG: Print out the entire head service to help with debugging.
	headServiceJSON, err := json.MarshalIndent(headService, "", "  ")
	if err != nil {
		t.Errorf("failed to marshal head service: %v", err)
	}
	t.Logf("head service: %s", string(headServiceJSON))

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

	// Test name and namespace are generated if not specified
	if headService.ObjectMeta.Name == "" {
		t.Errorf("Generated head service name is empty")
	}
	if headService.ObjectMeta.Namespace == "" {
		t.Errorf("Generated head service namespace is empty")
	}
}

func TestBuildServiceForHeadPodPortsOrder(t *testing.T) {
	svc1, err1 := BuildServiceForHeadPod(*instanceWithWrongSvc, nil, nil)
	svc2, err2 := BuildServiceForHeadPod(*instanceWithWrongSvc, nil, nil)
	assert.Nil(t, err1)
	assert.Nil(t, err2)

	ports1 := svc1.Spec.Ports
	ports2 := svc2.Spec.Ports

	// length should be same
	assert.Equal(t, len(ports1), len(ports2))
	for i := 0; i < len(ports1); i++ {
		// name should be same
		assert.Equal(t, ports1[i].Name, ports2[i].Name)
	}
}
