package ray

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/scheme"
	"github.com/ray-project/kuberay/ray-operator/test/support"
)

func TestGenerateHashWithoutReplicasAndWorkersToDelete(t *testing.T) {
	// `generateRayClusterJsonHash` will mute fields that will not trigger new RayCluster preparation. For example,
	// Autoscaler will update `Replicas` and `WorkersToDelete` when scaling up/down. Hence, `hash1` should be equal to
	// `hash2` in this case.
	cluster := rayv1.RayCluster{
		Spec: rayv1.RayClusterSpec{
			RayVersion: support.GetRayVersion(),
			WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
				{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{},
					},
					Replicas:    ptr.To[int32](2),
					MinReplicas: ptr.To[int32](1),
					MaxReplicas: ptr.To[int32](4),
				},
			},
		},
	}

	hash1, err := generateHashWithoutReplicasAndWorkersToDelete(cluster.Spec)
	require.NoError(t, err)

	*cluster.Spec.WorkerGroupSpecs[0].Replicas++
	hash2, err := generateHashWithoutReplicasAndWorkersToDelete(cluster.Spec)
	require.NoError(t, err)
	assert.Equal(t, hash1, hash2)

	// RayVersion will not be muted, so `hash3` should not be equal to `hash1`.
	cluster.Spec.RayVersion = "2.100.0"
	hash3, err := generateHashWithoutReplicasAndWorkersToDelete(cluster.Spec)
	require.NoError(t, err)
	assert.NotEqual(t, hash1, hash3)
}

func TestInconsistentRayServiceStatuses(t *testing.T) {
	oldStatus := rayv1.RayServiceStatuses{
		ActiveServiceStatus: rayv1.RayServiceStatus{
			RayClusterName: "new-cluster",
			Applications: map[string]rayv1.AppStatus{
				utils.DefaultServeAppName: {
					Status:  rayv1.ApplicationStatusEnum.RUNNING,
					Message: "OK",
					Deployments: map[string]rayv1.ServeDeploymentStatus{
						"serve-1": {
							Status:  rayv1.DeploymentStatusEnum.UNHEALTHY,
							Message: "error",
						},
					},
				},
			},
		},
		PendingServiceStatus: rayv1.RayServiceStatus{
			RayClusterName: "old-cluster",
			Applications: map[string]rayv1.AppStatus{
				utils.DefaultServeAppName: {
					Status:  rayv1.ApplicationStatusEnum.NOT_STARTED,
					Message: "application not started yet",
					Deployments: map[string]rayv1.ServeDeploymentStatus{
						"serve-1": {
							Status:  rayv1.DeploymentStatusEnum.HEALTHY,
							Message: "Serve is healthy",
						},
					},
				},
			},
		},
		ServiceStatus: rayv1.NotRunning,
	}
	ctx := context.Background()

	// Test 1: Update ServiceStatus only.
	newStatus := oldStatus.DeepCopy()
	newStatus.ServiceStatus = rayv1.Running
	assert.True(t, inconsistentRayServiceStatuses(ctx, oldStatus, *newStatus))

	// Test 2: Test RayServiceStatus
	newStatus = oldStatus.DeepCopy()
	assert.False(t, inconsistentRayServiceStatuses(ctx, oldStatus, *newStatus))
}

func TestIsHeadPodRunningAndReady(t *testing.T) {
	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Mock data
	cluster := rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	headPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "head-pod",
			Namespace: cluster.ObjectMeta.Namespace,
			Labels: map[string]string{
				utils.RayClusterLabelKey:  cluster.ObjectMeta.Name,
				utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	// Initialize a fake client with newScheme and runtimeObjects.
	runtimeObjects := []runtime.Object{}
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	ctx := context.TODO()

	// Initialize RayService reconciler.
	r := &RayServiceReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
	}

	// Test 1: There is no head pod. `isHeadPodRunningAndReady` should return false.
	// In addition, an error should be returned if the number of head pods is not 1.
	isReady, err := r.isHeadPodRunningAndReady(ctx, &cluster)
	require.Error(t, err)
	assert.False(t, isReady)

	// Test 2: There is one head pod, but the pod is not running and ready.
	// `isHeadPodRunningAndReady` should return false, and no error should be returned.
	runtimeObjects = []runtime.Object{headPod}
	fakeClient = clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	r.Client = fakeClient
	isReady, err = r.isHeadPodRunningAndReady(ctx, &cluster)
	require.NoError(t, err)
	assert.False(t, isReady)

	// Test 3: There is one head pod, and the pod is running and ready.
	// `isHeadPodRunningAndReady` should return true, and no error should be returned.
	runningHeadPod := headPod.DeepCopy()
	runningHeadPod.Status = corev1.PodStatus{
		Phase: corev1.PodRunning,
		Conditions: []corev1.PodCondition{
			{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			},
		},
	}
	runtimeObjects = []runtime.Object{runningHeadPod}
	fakeClient = clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	r.Client = fakeClient
	isReady, err = r.isHeadPodRunningAndReady(ctx, &cluster)
	require.NoError(t, err)
	assert.True(t, isReady)
}

func TestReconcileServices_UpdateService(t *testing.T) {
	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Mock data
	namespace := "ray"
	cluster := rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: namespace,
		},
		Spec: rayv1.RayClusterSpec{
			HeadGroupSpec: rayv1.HeadGroupSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "ray-head",
								Ports: []corev1.ContainerPort{},
							},
						},
					},
				},
			},
		},
	}
	rayService := rayv1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: cluster.ObjectMeta.Namespace,
		},
	}

	// Initialize a fake client with newScheme and runtimeObjects.
	runtimeObjects := []runtime.Object{}
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()

	// Initialize RayCluster reconciler.
	r := &RayServiceReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
	}

	ctx := context.TODO()
	// Create a head service.
	_, err := r.reconcileServices(ctx, &rayService, &cluster, utils.HeadService)
	require.NoError(t, err, "Fail to reconcile service")

	svcList := corev1.ServiceList{}
	err = fakeClient.List(ctx, &svcList, client.InNamespace(namespace))
	require.NoError(t, err, "Fail to get service list")
	assert.Len(t, svcList.Items, 1, "Service list should have one item")
	oldSvc := svcList.Items[0].DeepCopy()

	// Test 1: When the service for the RayCluster already exists, it should not be updated.
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
		{
			Name:          "test-port",
			ContainerPort: 9999,
		},
	}
	_, err = r.reconcileServices(ctx, &rayService, &cluster, utils.HeadService)
	require.NoError(t, err, "Fail to reconcile service")

	svcList = corev1.ServiceList{}
	err = fakeClient.List(ctx, &svcList, client.InNamespace(namespace))
	require.NoError(t, err, "Fail to get service list")
	assert.Len(t, svcList.Items, 1, "Service list should have one item")
	assert.True(t, reflect.DeepEqual(*oldSvc, svcList.Items[0]))

	// Test 2: When the RayCluster switches, the service should be updated.
	cluster.Name = "new-cluster"
	_, err = r.reconcileServices(ctx, &rayService, &cluster, utils.HeadService)
	require.NoError(t, err, "Fail to reconcile service")

	svcList = corev1.ServiceList{}
	err = fakeClient.List(ctx, &svcList, client.InNamespace(namespace))
	require.NoError(t, err, "Fail to get service list")
	assert.Len(t, svcList.Items, 1, "Service list should have one item")
	assert.False(t, reflect.DeepEqual(*oldSvc, svcList.Items[0]))
}

func TestFetchHeadServiceURL(t *testing.T) {
	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Mock data
	namespace := "ray"
	dashboardPort := int32(9999)
	cluster := rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: namespace,
		},
	}

	headSvcName, err := utils.GenerateHeadServiceName(utils.RayClusterCRD, cluster.Spec, cluster.Name)
	require.NoError(t, err, "Fail to generate head service name")
	headSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      headSvcName,
			Namespace: cluster.ObjectMeta.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: utils.DashboardPortName,
					Port: dashboardPort,
				},
			},
		},
	}

	// Initialize a fake client with newScheme and runtimeObjects.
	runtimeObjects := []runtime.Object{&headSvc}
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()

	// Initialize RayService reconciler.
	ctx := context.TODO()
	r := RayServiceReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
	}

	url, err := utils.FetchHeadServiceURL(ctx, r.Client, &cluster, utils.DashboardPortName)
	require.NoError(t, err, "Fail to fetch head service url")
	assert.Equal(t, fmt.Sprintf("test-cluster-head-svc.%s.svc.cluster.local:%d", namespace, dashboardPort), url, "Head service url is not correct")
}

func TestGetAndCheckServeStatus(t *testing.T) {
	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Initialize RayService reconciler.
	ctx := context.TODO()
	serveAppName := "serve-app-1"

	// Here are the key representing Ray Serve deployment and application statuses.
	const (
		// Ray Serve deployment status
		DeploymentStatus = "DeploymentStatus"
		// Ray Serve application status
		ApplicationStatus = "ApplicationStatus"
	)

	tests := []struct {
		rayServiceStatus map[string]string
		applications     map[string]rayv1.AppStatus
		name             string
		expectedReady    bool
	}{
		// Test 1: There is no pre-existing RayServiceStatus in the RayService CR. Create a new Ray Serve application, and the application is still deploying.
		{
			rayServiceStatus: map[string]string{
				DeploymentStatus:  rayv1.DeploymentStatusEnum.UPDATING,
				ApplicationStatus: rayv1.ApplicationStatusEnum.DEPLOYING,
			},
			applications:  map[string]rayv1.AppStatus{},
			name:          "Create a new Ray Serve application",
			expectedReady: false,
		},
		// Test 2: The Ray Serve application finishes the deployment process and becomes "RUNNING".
		{
			rayServiceStatus: map[string]string{
				DeploymentStatus:  rayv1.DeploymentStatusEnum.HEALTHY,
				ApplicationStatus: rayv1.ApplicationStatusEnum.RUNNING,
			},
			applications: map[string]rayv1.AppStatus{
				serveAppName: {
					Status: rayv1.ApplicationStatusEnum.RUNNING,
				},
			},
			name:          "Finishes the deployment process and becomes RUNNING",
			expectedReady: true,
		},
		// Test 3: Both the current Ray Serve application and RayService status are unhealthy.
		{
			rayServiceStatus: map[string]string{
				DeploymentStatus:  rayv1.DeploymentStatusEnum.UNHEALTHY,
				ApplicationStatus: rayv1.ApplicationStatusEnum.UNHEALTHY,
			},
			applications: map[string]rayv1.AppStatus{
				serveAppName: {
					Status: rayv1.ApplicationStatusEnum.UNHEALTHY,
				},
			},
			name:          "Both the current Ray Serve application and RayService status are unhealthy",
			expectedReady: false,
		},
		// Test 4: Both the current Ray Serve application and RayService status are DEPLOY_FAILED.
		{
			rayServiceStatus: map[string]string{
				DeploymentStatus:  rayv1.DeploymentStatusEnum.UPDATING,
				ApplicationStatus: rayv1.ApplicationStatusEnum.DEPLOY_FAILED,
			},
			applications: map[string]rayv1.AppStatus{
				serveAppName: {
					Status: rayv1.ApplicationStatusEnum.DEPLOY_FAILED,
				},
			},
			name:          "Both the current Ray Serve application and RayService status are DEPLOY_FAILED",
			expectedReady: false,
		},
		// Test 5: If the Ray Serve application is not found, the RayCluster is not ready to serve requests.
		{
			rayServiceStatus: map[string]string{},
			applications:     map[string]rayv1.AppStatus{},
			name:             "Ray Serve application is not found",
			expectedReady:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var dashboardClient utils.RayDashboardClientInterface
			if len(tc.rayServiceStatus) != 0 {
				dashboardClient = initFakeDashboardClient(serveAppName, tc.rayServiceStatus[DeploymentStatus], tc.rayServiceStatus[ApplicationStatus])
			} else {
				dashboardClient = &utils.FakeRayDashboardClient{}
			}
			isReady, _, err := getAndCheckServeStatus(ctx, dashboardClient)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedReady, isReady)
		})
	}
}

func TestCheckIfNeedSubmitServeApplications(t *testing.T) {
	serveConfigV2_1 := "serve-config-1"
	serveConfigV2_2 := "serve-config-2"

	serveApplications := map[string]rayv1.AppStatus{
		"myapp": {
			Status: rayv1.ApplicationStatusEnum.RUNNING,
		},
	}
	emptyServeApplications := map[string]rayv1.AppStatus{}

	// Test 1: The cached Serve config is empty, and the new Serve config is not empty.
	// This happens when the RayCluster is new, and the serve application has not been created yet.
	shouldCreate, _ := checkIfNeedSubmitServeApplications("", serveConfigV2_1, emptyServeApplications)
	assert.True(t, shouldCreate)

	// Test 2: The cached Serve config and the new Serve config are the same.
	// This happens when the serve application is already created, and users do not update the serve config.
	shouldCreate, _ = checkIfNeedSubmitServeApplications(serveConfigV2_1, serveConfigV2_1, serveApplications)
	assert.False(t, shouldCreate)

	// Test 3: The cached Serve config and the new Serve config are different.
	// This happens when the serve application is already created, and users update the serve config.
	shouldCreate, _ = checkIfNeedSubmitServeApplications(serveConfigV2_1, serveConfigV2_2, serveApplications)
	assert.True(t, shouldCreate)

	// Test 4: Both the cached Serve config and the new Serve config are the same, but the RayService CR status is empty.
	// This happens when the head Pod crashed and GCS FT was not enabled
	shouldCreate, _ = checkIfNeedSubmitServeApplications(serveConfigV2_1, serveConfigV2_1, emptyServeApplications)
	assert.True(t, shouldCreate)

	// Test 5: The cached Serve config is empty, but the new Serve config is not empty.
	// This happens when KubeRay operator crashes and restarts. Submit the request for safety.
	shouldCreate, _ = checkIfNeedSubmitServeApplications("", serveConfigV2_1, serveApplications)
	assert.True(t, shouldCreate)
}

func TestReconcileRayCluster_CreatePendingCluster(t *testing.T) {
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)

	ctx := context.TODO()
	namespace := "ray"
	rayService := rayv1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: namespace,
		},
		Status: rayv1.RayServiceStatuses{
			PendingServiceStatus: rayv1.RayServiceStatus{
				RayClusterName: "pending-cluster",
			},
		},
	}

	runtimeObjects := []runtime.Object{}
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	r := RayServiceReconciler{
		Client:   fakeClient,
		Scheme:   newScheme,
		Recorder: record.NewFakeRecorder(1),
	}

	activeRayCluster, pendingRayCluster, err := r.reconcileRayCluster(ctx, &rayService)
	require.NoError(t, err)
	assert.Nil(t, activeRayCluster)
	assert.Equal(t, "pending-cluster", pendingRayCluster.Name)
}

func TestReconcileRayCluster_UpdateActiveCluster(t *testing.T) {
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)

	ctx := context.TODO()
	namespace := "ray"
	rayServiceTemplate := rayv1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: namespace,
		},
		Status: rayv1.RayServiceStatuses{
			ActiveServiceStatus: rayv1.RayServiceStatus{
				RayClusterName: "active-cluster",
			},
		},
	}

	hash, err := generateHashWithoutReplicasAndWorkersToDelete(rayServiceTemplate.Spec.RayClusterSpec)
	require.NoError(t, err)
	activeClusterTemplate := rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "active-cluster",
			Namespace: namespace,
			Annotations: map[string]string{
				utils.HashWithoutReplicasAndWorkersToDeleteKey: hash,
				utils.NumWorkerGroupsKey:                       strconv.Itoa(len(rayServiceTemplate.Spec.RayClusterSpec.WorkerGroupSpecs)),
				utils.KubeRayVersion:                           utils.KUBERAY_VERSION,
			},
		},
	}

	tests := []struct {
		name                 string
		updateKubeRayVersion bool
		addNewWorkerGroup    bool
	}{
		{
			name:                 "Update KubeRay version",
			updateKubeRayVersion: true,
			addNewWorkerGroup:    false,
		},
		{
			name:                 "Add new worker group",
			updateKubeRayVersion: false,
			addNewWorkerGroup:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cluster := activeClusterTemplate.DeepCopy()
			service := rayServiceTemplate.DeepCopy()
			if test.updateKubeRayVersion {
				cluster.Annotations[utils.KubeRayVersion] = "new-version"
			}
			if test.addNewWorkerGroup {
				service.Spec.RayClusterSpec.WorkerGroupSpecs = append(service.Spec.RayClusterSpec.WorkerGroupSpecs, rayv1.WorkerGroupSpec{
					GroupName: "new-worker-group",
				})
			}
			expectedWorkerGroupCount := len(service.Spec.RayClusterSpec.WorkerGroupSpecs)

			runtimeObjects := []runtime.Object{cluster}
			fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
			r := RayServiceReconciler{
				Client:   fakeClient,
				Scheme:   newScheme,
				Recorder: record.NewFakeRecorder(1),
			}

			activeCluster, pendingCluster, err := r.reconcileRayCluster(ctx, service)
			require.NoError(t, err)
			assert.Equal(t, cluster.Name, activeCluster.Name)
			assert.Nil(t, pendingCluster)

			if test.updateKubeRayVersion {
				assert.Equal(t, utils.KUBERAY_VERSION, activeCluster.Annotations[utils.KubeRayVersion])
			}
			assert.Len(t, activeCluster.Spec.WorkerGroupSpecs, expectedWorkerGroupCount)
		})
	}
}

func TestReconcileRayCluster_UpdatePendingCluster(t *testing.T) {
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)

	ctx := context.TODO()
	namespace := "ray"
	rayServiceTemplate := rayv1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: namespace,
		},
		Status: rayv1.RayServiceStatuses{
			PendingServiceStatus: rayv1.RayServiceStatus{
				RayClusterName: "pending-cluster",
			},
		},
	}

	hash, err := generateHashWithoutReplicasAndWorkersToDelete(rayServiceTemplate.Spec.RayClusterSpec)
	require.NoError(t, err)
	cluster := rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pending-cluster",
			Namespace: namespace,
			Annotations: map[string]string{
				utils.HashWithoutReplicasAndWorkersToDeleteKey: hash,
				utils.NumWorkerGroupsKey:                       strconv.Itoa(len(rayServiceTemplate.Spec.RayClusterSpec.WorkerGroupSpecs)),
				utils.KubeRayVersion:                           utils.KUBERAY_VERSION,
			},
		},
	}

	service := rayServiceTemplate.DeepCopy()
	service.Spec.RayClusterSpec.WorkerGroupSpecs = append(service.Spec.RayClusterSpec.WorkerGroupSpecs, rayv1.WorkerGroupSpec{
		GroupName: "new-worker-group",
	})
	expectedWorkerGroupCount := len(service.Spec.RayClusterSpec.WorkerGroupSpecs)

	runtimeObjects := []runtime.Object{&cluster}
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	r := RayServiceReconciler{
		Client:   fakeClient,
		Scheme:   newScheme,
		Recorder: record.NewFakeRecorder(1),
	}

	activeCluster, pendingCluster, err := r.reconcileRayCluster(ctx, service)
	require.NoError(t, err)
	assert.Nil(t, activeCluster)
	assert.Equal(t, cluster.Name, pendingCluster.Name)
	assert.Len(t, pendingCluster.Spec.WorkerGroupSpecs, expectedWorkerGroupCount)
}

func initFakeDashboardClient(appName string, deploymentStatus string, appStatus string) utils.RayDashboardClientInterface {
	fakeDashboardClient := utils.FakeRayDashboardClient{}
	status := generateServeStatus(deploymentStatus, appStatus)
	fakeDashboardClient.SetMultiApplicationStatuses(map[string]*utils.ServeApplicationStatus{appName: &status})
	return &fakeDashboardClient
}

func initFakeRayHttpProxyClient(isHealthy bool) utils.RayHttpProxyClientInterface {
	return &utils.FakeRayHttpProxyClient{
		IsHealthy: isHealthy,
	}
}

func TestLabelHeadPodForServeStatus(t *testing.T) {
	tests := []struct {
		name                       string
		expectServeResult          string
		excludeHeadPodFromServeSvc bool
		isHealthy                  bool
	}{
		{
			name:                       "Ray serve application is running, excludeHeadPodFromServeSvc is true",
			expectServeResult:          "false",
			excludeHeadPodFromServeSvc: true,
			isHealthy:                  true,
		},
		{
			name:                       "Ray serve application is running, excludeHeadPodFromServeSvc is false",
			expectServeResult:          "true",
			excludeHeadPodFromServeSvc: false,
			isHealthy:                  true,
		},
		{
			name:                       "Ray serve application is unhealthy, excludeHeadPodFromServeSvc is true",
			expectServeResult:          "false",
			excludeHeadPodFromServeSvc: true,
			isHealthy:                  false,
		},
		{
			name:                       "Ray serve application is unhealthy, excludeHeadPodFromServeSvc is false",
			expectServeResult:          "false",
			excludeHeadPodFromServeSvc: false,
			isHealthy:                  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			newScheme := runtime.NewScheme()
			_ = corev1.AddToScheme(newScheme)

			namespace := "mock-ray-namespace"
			cluster := rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: namespace,
				},
			}
			headPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "head-pod",
					Namespace: cluster.ObjectMeta.Namespace,
					Labels: map[string]string{
						utils.RayClusterLabelKey:  cluster.ObjectMeta.Name,
						utils.RayNodeTypeLabelKey: string(rayv1.HeadNode),
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "test-container",
						},
					},
				},
			}
			// Initialize a fake client with newScheme and runtimeObjects.
			runtimeObjects := []runtime.Object{headPod}
			fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
			ctx := context.TODO()

			fakeRayHttpProxyClient := initFakeRayHttpProxyClient(tc.isHealthy)
			// Initialize RayService reconciler.
			r := &RayServiceReconciler{
				Client:   fakeClient,
				Recorder: &record.FakeRecorder{},
				Scheme:   newScheme,
				httpProxyClientFunc: func() utils.RayHttpProxyClientInterface {
					return fakeRayHttpProxyClient
				},
			}

			err := r.updateHeadPodServeLabel(ctx, &rayv1.RayService{}, &cluster, tc.excludeHeadPodFromServeSvc)
			require.NoError(t, err)
			// Get latest headPod status
			headPod, err = common.GetRayClusterHeadPod(ctx, r, &cluster)
			assert.Equal(t, headPod.Labels[utils.RayClusterServingServiceLabelKey], tc.expectServeResult)
			require.NoError(t, err)
		})
	}
}

func TestCalculateConditions(t *testing.T) {
	tests := []struct {
		name                    string
		conditionType           rayv1.RayServiceConditionType
		originalConditionStatus metav1.ConditionStatus
		originalReason          string
		expectedConditionStatus metav1.ConditionStatus
		expectedReason          string
		rayServiceInstance      rayv1.RayService
	}{
		{
			name:                    "initial RayServiceReady",
			rayServiceInstance:      rayv1.RayService{},
			conditionType:           rayv1.RayServiceReady,
			originalConditionStatus: metav1.ConditionFalse,
			originalReason:          string(rayv1.RayServiceInitializing),
			expectedConditionStatus: metav1.ConditionFalse,
			expectedReason:          string(rayv1.RayServiceInitializing),
		},
		{
			name:                    "initial RayServiceInitializing",
			rayServiceInstance:      rayv1.RayService{},
			conditionType:           rayv1.UpgradeInProgress,
			originalConditionStatus: metav1.ConditionFalse,
			originalReason:          string(rayv1.RayServiceInitializing),
			expectedConditionStatus: metav1.ConditionFalse,
			expectedReason:          string(rayv1.RayServiceInitializing),
		},
		{
			name: "Ready condition remains false unchanged",
			rayServiceInstance: rayv1.RayService{
				Status: rayv1.RayServiceStatuses{
					NumServeEndpoints: 0,
				},
			},
			conditionType:           rayv1.RayServiceReady,
			originalConditionStatus: metav1.ConditionFalse,
			originalReason:          "WhateverReason",
			expectedConditionStatus: metav1.ConditionFalse,
			expectedReason:          "WhateverReason",
		},
		{
			name: "Ready condition remains true always has NonZeroServeEndPoints reason",
			rayServiceInstance: rayv1.RayService{
				Status: rayv1.RayServiceStatuses{
					NumServeEndpoints: 1,
				},
			},
			conditionType:           rayv1.RayServiceReady,
			originalConditionStatus: metav1.ConditionTrue,
			originalReason:          "WhateverReason",
			expectedConditionStatus: metav1.ConditionTrue,
			expectedReason:          string(rayv1.NonZeroServeEndpoints),
		},
		{
			name: "Ready condition becomes true",
			rayServiceInstance: rayv1.RayService{
				Status: rayv1.RayServiceStatuses{
					NumServeEndpoints: 1,
				},
			},
			conditionType:           rayv1.RayServiceReady,
			originalConditionStatus: metav1.ConditionFalse,
			originalReason:          "WhateverReason",
			expectedConditionStatus: metav1.ConditionTrue,
			expectedReason:          string(rayv1.NonZeroServeEndpoints),
		},
		{
			name: "Ready condition becomes false",
			rayServiceInstance: rayv1.RayService{
				Status: rayv1.RayServiceStatuses{
					NumServeEndpoints: 0,
				},
			},
			conditionType:           rayv1.RayServiceReady,
			originalConditionStatus: metav1.ConditionTrue,
			originalReason:          string(rayv1.NonZeroServeEndpoints),
			expectedConditionStatus: metav1.ConditionFalse,
			expectedReason:          string(rayv1.ZeroServeEndpoints),
		},
		{
			name: "UpgradeInProgress condition is true if both active and pending clusters exist",
			rayServiceInstance: rayv1.RayService{
				Status: rayv1.RayServiceStatuses{
					ActiveServiceStatus: rayv1.RayServiceStatus{
						RayClusterName: "active-cluster",
					},
					PendingServiceStatus: rayv1.RayServiceStatus{
						RayClusterName: "pending-cluster",
					},
				},
			},
			conditionType:           rayv1.UpgradeInProgress,
			originalConditionStatus: metav1.ConditionFalse,
			originalReason:          "WhateverReason",
			expectedConditionStatus: metav1.ConditionTrue,
			expectedReason:          string(rayv1.BothActivePendingClustersExist),
		},
		{
			name: "UpgradeInProgress condition is false if only active cluster exists",
			rayServiceInstance: rayv1.RayService{
				Status: rayv1.RayServiceStatuses{
					ActiveServiceStatus: rayv1.RayServiceStatus{
						RayClusterName: "active-cluster",
					},
				},
			},
			conditionType:           rayv1.UpgradeInProgress,
			originalConditionStatus: metav1.ConditionTrue,
			originalReason:          string(rayv1.BothActivePendingClustersExist),
			expectedConditionStatus: metav1.ConditionFalse,
			expectedReason:          string(rayv1.NoPendingCluster),
		},
		{
			name:                    "UpgradeInProgress condition is unknown if no active cluster exists and RayService is not initializing",
			rayServiceInstance:      rayv1.RayService{},
			conditionType:           rayv1.UpgradeInProgress,
			originalConditionStatus: metav1.ConditionTrue,
			originalReason:          string(rayv1.BothActivePendingClustersExist),
			expectedConditionStatus: metav1.ConditionUnknown,
			expectedReason:          string(rayv1.NoActiveCluster),
		},
		{
			name: "UpgradeInProgress condition is false if RayService is initializing",
			rayServiceInstance: rayv1.RayService{
				Status: rayv1.RayServiceStatuses{
					PendingServiceStatus: rayv1.RayServiceStatus{
						RayClusterName: "pending-cluster",
					},
				},
			},
			conditionType:           rayv1.UpgradeInProgress,
			originalConditionStatus: metav1.ConditionFalse,
			originalReason:          string(rayv1.RayServiceInitializing),
			expectedConditionStatus: metav1.ConditionFalse,
			expectedReason:          string(rayv1.RayServiceInitializing),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta.SetStatusCondition(&tt.rayServiceInstance.Status.Conditions, metav1.Condition{
				Type:   string(tt.conditionType),
				Status: tt.originalConditionStatus,
				Reason: tt.originalReason,
			})
			calculateConditions(&tt.rayServiceInstance)
			condition := meta.FindStatusCondition(tt.rayServiceInstance.Status.Conditions, string(tt.conditionType))
			assert.Equal(t, tt.expectedConditionStatus, condition.Status)
			assert.Equal(t, tt.expectedReason, condition.Reason)
		})
	}
}

func TestConstructRayClusterForRayService(t *testing.T) {
	tests := []struct {
		name       string
		rayService rayv1.RayService
	}{
		{
			name: "RayClusterSpec with no worker groups",
			rayService: rayv1.RayService{
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayv1.RayClusterSpec{
						WorkerGroupSpecs: []rayv1.WorkerGroupSpec{},
					},
				},
			},
		},
		{
			name: "RayClusterSpec with two worker groups",
			rayService: rayv1.RayService{
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayv1.RayClusterSpec{
						WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
							{
								GroupName: "worker-group-1",
							},
							{
								GroupName: "worker-group-2",
							},
						},
					},
				},
			},
		},
		{
			name: "RayService with labels",
			rayService: rayv1.RayService{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"label-1": "value-1",
						"label-2": "value-2",
					},
				},
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayv1.RayClusterSpec{
						WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
							{
								GroupName: "worker-group-1",
							},
						},
					},
				},
			},
		},
		{
			name: "RayService with annotations",
			rayService: rayv1.RayService{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"annotation-1": "value-1",
						"annotation-2": "value-2",
					},
				},
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayv1.RayClusterSpec{
						WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
							{
								GroupName: "worker-group-1",
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rayService := tt.rayService
			rayService.Name = "test-service"
			rayService.Namespace = "test-namespace"
			clusterName := "test-cluster"
			rayCluster, err := constructRayClusterForRayService(&rayService, clusterName, scheme.Scheme)
			require.NoError(t, err)

			// Check ObjectMeta of the RayCluster
			assert.Equal(t, rayCluster.ObjectMeta.Name, clusterName)
			assert.Equal(t, rayCluster.ObjectMeta.Namespace, rayService.Namespace)

			// Check labels for metadata
			assert.Equal(t, rayCluster.Labels[utils.RayOriginatedFromCRNameLabelKey], rayService.Name)
			assert.Equal(t, rayCluster.Labels[utils.RayOriginatedFromCRDLabelKey], string(utils.RayServiceCRD))

			// Check annotations for metadata
			assert.NotEmpty(t, rayCluster.Annotations[utils.HashWithoutReplicasAndWorkersToDeleteKey])
			expectedNumWorkerGroups := strconv.Itoa(len(rayService.Spec.RayClusterSpec.WorkerGroupSpecs))
			assert.Equal(t, expectedNumWorkerGroups, rayCluster.Annotations[utils.NumWorkerGroupsKey])
			assert.Equal(t, utils.KUBERAY_VERSION, rayCluster.Annotations[utils.KubeRayVersion])

			// Check whether the RayService's labels are copied to the RayCluster
			for key, value := range rayService.Labels {
				assert.Equal(t, rayCluster.Labels[key], value)
			}

			// Check whether the RayService's annotations are copied to the RayCluster
			for key, value := range rayService.Annotations {
				assert.Equal(t, rayCluster.Annotations[key], value)
			}

			// Check owner reference
			assert.Equal(t, rayCluster.OwnerReferences[0].Name, rayService.Name)
			assert.Equal(t, rayCluster.OwnerReferences[0].UID, rayService.UID)
		})
	}
}

func TestIsClusterSpecHashEqual(t *testing.T) {
	rayService := rayv1.RayService{
		Spec: rayv1.RayServiceSpec{
			RayClusterSpec: rayv1.RayClusterSpec{
				WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
					{
						GroupName: "worker-group-1",
						Replicas:  ptr.To[int32](1),
					},
				},
			},
		},
	}

	tests := []struct {
		name              string
		partial           bool
		diffReplicas      bool
		expected          bool
		addNewWorkerGroup bool
		updateClusterSpec bool
	}{
		{
			name:              "[full] diff replicas",
			partial:           false,
			diffReplicas:      true,
			addNewWorkerGroup: false,
			expected:          true,
		},
		{
			name:              "[full] completely identical",
			partial:           false,
			diffReplicas:      false,
			addNewWorkerGroup: false,
			expected:          true,
		},
		{
			name:              "[full] update cluster spec",
			partial:           false,
			diffReplicas:      false,
			addNewWorkerGroup: false,
			updateClusterSpec: true,
			expected:          false,
		},
		{
			name:              "[partial] new worker group",
			partial:           true,
			diffReplicas:      false,
			addNewWorkerGroup: true,
			expected:          true,
		},
		{
			name:              "[partial] diff replicas + new worker group",
			partial:           true,
			diffReplicas:      true,
			addNewWorkerGroup: true,
			expected:          true,
		},
		{
			name:              "[partial] diff replicas",
			partial:           true,
			diffReplicas:      true,
			addNewWorkerGroup: false,
			expected:          true,
		},
		{
			name:              "[partial] update cluster spec",
			partial:           true,
			updateClusterSpec: true,
			expected:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := rayService.DeepCopy()
			hash, err := generateHashWithoutReplicasAndWorkersToDelete(service.Spec.RayClusterSpec)
			require.NoError(t, err)
			cluster := rayv1.RayCluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						utils.HashWithoutReplicasAndWorkersToDeleteKey: hash,
						utils.NumWorkerGroupsKey:                       strconv.Itoa(len(rayService.Spec.RayClusterSpec.WorkerGroupSpecs)),
					},
				},
				Spec: rayService.Spec.RayClusterSpec,
			}
			if tt.diffReplicas {
				*service.Spec.RayClusterSpec.WorkerGroupSpecs[0].Replicas++
			}
			if tt.addNewWorkerGroup {
				service.Spec.RayClusterSpec.WorkerGroupSpecs = append(service.Spec.RayClusterSpec.WorkerGroupSpecs, rayv1.WorkerGroupSpec{
					GroupName: "worker-group-2",
					Replicas:  ptr.To[int32](1),
				})
			}
			if tt.updateClusterSpec {
				service.Spec.RayClusterSpec.RayVersion = "new-version"
			}

			isEqual := isClusterSpecHashEqual(service, &cluster, tt.partial)
			assert.Equal(t, tt.expected, isEqual)
		})
	}
}

func TestShouldPrepareNewCluster_PrepareNewCluster(t *testing.T) {
	// Prepare a new cluster when both active and pending clusters are nil.
	ctx := context.TODO()
	rayService := rayv1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test-namespace",
		},
	}

	shouldPrepareNewCluster := shouldPrepareNewCluster(ctx, &rayService, nil, nil, false)
	assert.True(t, shouldPrepareNewCluster)
}

func TestShouldPrepareNewCluster_ZeroDowntimeUpgrade(t *testing.T) {
	// Trigger a zero-downtime upgrade when the cluster spec in RayService differs
	// from the active cluster and no pending cluster exists.
	ctx := context.TODO()
	namespace := "test-namespace"
	activeClusterName := "active-cluster"

	rayService := rayv1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: namespace,
		},
		Spec: rayv1.RayServiceSpec{
			RayClusterSpec: rayv1.RayClusterSpec{
				RayVersion: "old-version",
			},
		},
	}

	hash, err := generateHashWithoutReplicasAndWorkersToDelete(rayService.Spec.RayClusterSpec)
	require.NoError(t, err)
	activeCluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      activeClusterName,
			Namespace: namespace,
			Annotations: map[string]string{
				utils.HashWithoutReplicasAndWorkersToDeleteKey: hash,
				utils.NumWorkerGroupsKey:                       strconv.Itoa(len(rayService.Spec.RayClusterSpec.WorkerGroupSpecs)),
				utils.KubeRayVersion:                           utils.KUBERAY_VERSION,
			},
		},
	}

	// Update cluster spec in RayService to trigger a zero downtime upgrade.
	rayService.Spec.RayClusterSpec.RayVersion = "new-version"
	shouldPrepareNewCluster := shouldPrepareNewCluster(ctx, &rayService, activeCluster, nil, false)
	assert.True(t, shouldPrepareNewCluster)
}

func TestShouldPrepareNewCluster_PendingCluster(t *testing.T) {
	// A new cluster will not be created if the K8s services are pointing to the pending cluster.
	ctx := context.TODO()
	namespace := "test-namespace"
	pendingClusterName := "pending-cluster"

	rayService := rayv1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: namespace,
		},
		Spec: rayv1.RayServiceSpec{
			RayClusterSpec: rayv1.RayClusterSpec{
				RayVersion: "old-version",
			},
		},
	}

	hash, err := generateHashWithoutReplicasAndWorkersToDelete(rayService.Spec.RayClusterSpec)
	require.NoError(t, err)
	pendingCluster := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pendingClusterName,
			Namespace: namespace,
			Annotations: map[string]string{
				utils.HashWithoutReplicasAndWorkersToDeleteKey: hash,
				utils.NumWorkerGroupsKey:                       strconv.Itoa(len(rayService.Spec.RayClusterSpec.WorkerGroupSpecs)),
				utils.KubeRayVersion:                           utils.KUBERAY_VERSION,
			},
		},
	}
	rayService.Spec.RayClusterSpec.RayVersion = "new-version"

	t.Run("override the pending cluster if it is not serving", func(t *testing.T) {
		shouldPrepareNewCluster := shouldPrepareNewCluster(ctx, &rayService, nil, pendingCluster, false)
		assert.True(t, shouldPrepareNewCluster)
	})

	t.Run("do not override the pending cluster if it is serving", func(t *testing.T) {
		shouldPrepareNewCluster := shouldPrepareNewCluster(ctx, &rayService, nil, pendingCluster, true)
		assert.False(t, shouldPrepareNewCluster)
	})
}

func TestIsZeroDowntimeUpgradeEnabled(t *testing.T) {
	tests := []struct {
		name                     string
		upgradeStrategy          *rayv1.RayServiceUpgradeStrategy
		enableZeroDowntimeEnvVar string // "true" or "false" or "" (not set)
		expected                 bool
	}{
		{
			// The most common case.
			name:                     "both upgrade strategy and env var are not set",
			upgradeStrategy:          nil,
			enableZeroDowntimeEnvVar: "",
			expected:                 true,
		},
		{
			name:                     "upgrade strategy is not set, but env var is set to true",
			upgradeStrategy:          nil,
			enableZeroDowntimeEnvVar: "true",
			expected:                 true,
		},
		{
			name:                     "upgrade strategy is not set, but env var is set to false",
			upgradeStrategy:          nil,
			enableZeroDowntimeEnvVar: "false",
			expected:                 false,
		},
		{
			name:                     "upgrade strategy is set to NewCluster",
			upgradeStrategy:          &rayv1.RayServiceUpgradeStrategy{Type: ptr.To(rayv1.NewCluster)},
			enableZeroDowntimeEnvVar: "",
			expected:                 true,
		},
		{
			name:                     "upgrade strategy is set to NewCluster, and env var is not set",
			upgradeStrategy:          &rayv1.RayServiceUpgradeStrategy{Type: ptr.To(rayv1.NewCluster)},
			enableZeroDowntimeEnvVar: "true",
			expected:                 true,
		},
		{
			name:                     "upgrade strategy is set to NewCluster, and env var is set to false",
			upgradeStrategy:          &rayv1.RayServiceUpgradeStrategy{Type: ptr.To(rayv1.NewCluster)},
			enableZeroDowntimeEnvVar: "false",
			expected:                 true,
		},
		{
			name:                     "upgrade strategy is set to None, and env var is not set",
			upgradeStrategy:          &rayv1.RayServiceUpgradeStrategy{Type: ptr.To(rayv1.None)},
			enableZeroDowntimeEnvVar: "",
			expected:                 false,
		},
		{
			name:                     "upgrade strategy is set to None, and env var is set to true",
			upgradeStrategy:          &rayv1.RayServiceUpgradeStrategy{Type: ptr.To(rayv1.None)},
			enableZeroDowntimeEnvVar: "true",
			expected:                 false,
		},
		{
			name:                     "upgrade strategy is set to None, and env var is set to false",
			upgradeStrategy:          &rayv1.RayServiceUpgradeStrategy{Type: ptr.To(rayv1.None)},
			enableZeroDowntimeEnvVar: "false",
			expected:                 false,
		},
	}

	ctx := context.TODO()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				os.Unsetenv(ENABLE_ZERO_DOWNTIME)
			}()

			os.Setenv(ENABLE_ZERO_DOWNTIME, tt.enableZeroDowntimeEnvVar)
			isEnabled := isZeroDowntimeUpgradeEnabled(ctx, tt.upgradeStrategy)
			assert.Equal(t, tt.expected, isEnabled)
		})
	}
}
