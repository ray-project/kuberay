package ray

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	cmap "github.com/orcaman/concurrent-map"
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils"
	"github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/scheme"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGenerateRayClusterJsonHash(t *testing.T) {
	// `generateRayClusterJsonHash` will mute fields that will not trigger new RayCluster preparation. For example,
	// Autoscaler will update `Replicas` and `WorkersToDelete` when scaling up/down. Hence, `hash1` should be equal to
	// `hash2` in this case.
	cluster := rayv1.RayCluster{
		Spec: rayv1.RayClusterSpec{
			RayVersion: "2.7.0",
			WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
				{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{},
					},
					Replicas:    pointer.Int32Ptr(2),
					MinReplicas: pointer.Int32Ptr(1),
					MaxReplicas: pointer.Int32Ptr(4),
				},
			},
		},
	}

	hash1, err := generateRayClusterJsonHash(cluster.Spec)
	assert.Nil(t, err)

	*cluster.Spec.WorkerGroupSpecs[0].Replicas++
	hash2, err := generateRayClusterJsonHash(cluster.Spec)
	assert.Nil(t, err)
	assert.Equal(t, hash1, hash2)

	// RayVersion will not be muted, so `hash3` should not be equal to `hash1`.
	cluster.Spec.RayVersion = "2.100.0"
	hash3, err := generateRayClusterJsonHash(cluster.Spec)
	assert.Nil(t, err)
	assert.NotEqual(t, hash1, hash3)
}

func TestCompareRayClusterJsonHash(t *testing.T) {
	cluster1 := rayv1.RayCluster{
		Spec: rayv1.RayClusterSpec{
			RayVersion: "2.7.0",
		},
	}
	cluster2 := cluster1.DeepCopy()
	cluster2.Spec.RayVersion = "2.100.0"
	equal, err := compareRayClusterJsonHash(cluster1.Spec, cluster2.Spec)
	assert.Nil(t, err)
	assert.False(t, equal)

	equal, err = compareRayClusterJsonHash(cluster1.Spec, cluster1.Spec)
	assert.Nil(t, err)
	assert.True(t, equal)
}

func TestInconsistentRayServiceStatuses(t *testing.T) {
	r := &RayServiceReconciler{
		Log: ctrl.Log.WithName("controllers").WithName("RayService"),
	}

	timeNow := metav1.Now()
	oldStatus := rayv1.RayServiceStatuses{
		ActiveServiceStatus: rayv1.RayServiceStatus{
			RayClusterName: "new-cluster",
			DashboardStatus: rayv1.DashboardStatus{
				IsHealthy:            true,
				LastUpdateTime:       &timeNow,
				HealthLastUpdateTime: &timeNow,
			},
			Applications: map[string]rayv1.AppStatus{
				common.DefaultServeAppName: {
					Status:               rayv1.ApplicationStatusEnum.RUNNING,
					Message:              "OK",
					LastUpdateTime:       &timeNow,
					HealthLastUpdateTime: &timeNow,
					Deployments: map[string]rayv1.ServeDeploymentStatus{
						"serve-1": {
							Status:               rayv1.DeploymentStatusEnum.UNHEALTHY,
							Message:              "error",
							LastUpdateTime:       &timeNow,
							HealthLastUpdateTime: &timeNow,
						},
					},
				},
			},
		},
		PendingServiceStatus: rayv1.RayServiceStatus{
			RayClusterName: "old-cluster",
			DashboardStatus: rayv1.DashboardStatus{
				IsHealthy:            true,
				LastUpdateTime:       &timeNow,
				HealthLastUpdateTime: &timeNow,
			},
			Applications: map[string]rayv1.AppStatus{
				common.DefaultServeAppName: {
					Status:               rayv1.ApplicationStatusEnum.NOT_STARTED,
					Message:              "application not started yet",
					LastUpdateTime:       &timeNow,
					HealthLastUpdateTime: &timeNow,
					Deployments: map[string]rayv1.ServeDeploymentStatus{
						"serve-1": {
							Status:               rayv1.DeploymentStatusEnum.HEALTHY,
							Message:              "Serve is healthy",
							LastUpdateTime:       &timeNow,
							HealthLastUpdateTime: &timeNow,
						},
					},
				},
			},
		},
		ServiceStatus: rayv1.WaitForDashboard,
	}

	// Test 1: Update ServiceStatus only.
	newStatus := oldStatus.DeepCopy()
	newStatus.ServiceStatus = rayv1.WaitForServeDeploymentReady
	assert.True(t, r.inconsistentRayServiceStatuses(oldStatus, *newStatus))

	// Test 2: Test RayServiceStatus
	newStatus = oldStatus.DeepCopy()
	newStatus.ActiveServiceStatus.DashboardStatus.LastUpdateTime = &metav1.Time{Time: timeNow.Add(1)}
	assert.False(t, r.inconsistentRayServiceStatuses(oldStatus, *newStatus))

	newStatus.ActiveServiceStatus.DashboardStatus.IsHealthy = !oldStatus.ActiveServiceStatus.DashboardStatus.IsHealthy
	assert.True(t, r.inconsistentRayServiceStatuses(oldStatus, *newStatus))
}

func TestInconsistentRayServiceStatus(t *testing.T) {
	timeNow := metav1.Now()
	oldStatus := rayv1.RayServiceStatus{
		RayClusterName: "cluster-1",
		DashboardStatus: rayv1.DashboardStatus{
			IsHealthy:            true,
			LastUpdateTime:       &timeNow,
			HealthLastUpdateTime: &timeNow,
		},
		Applications: map[string]rayv1.AppStatus{
			"app1": {
				Status:               rayv1.ApplicationStatusEnum.RUNNING,
				Message:              "Application is running",
				LastUpdateTime:       &timeNow,
				HealthLastUpdateTime: &timeNow,
				Deployments: map[string]rayv1.ServeDeploymentStatus{
					"serve-1": {
						Status:               rayv1.DeploymentStatusEnum.HEALTHY,
						Message:              "Serve is healthy",
						LastUpdateTime:       &timeNow,
						HealthLastUpdateTime: &timeNow,
					},
				},
			},
			"app2": {
				Status:               rayv1.ApplicationStatusEnum.RUNNING,
				Message:              "Application is running",
				LastUpdateTime:       &timeNow,
				HealthLastUpdateTime: &timeNow,
				Deployments: map[string]rayv1.ServeDeploymentStatus{
					"serve-1": {
						Status:               rayv1.DeploymentStatusEnum.HEALTHY,
						Message:              "Serve is healthy",
						LastUpdateTime:       &timeNow,
						HealthLastUpdateTime: &timeNow,
					},
				},
			},
		},
	}

	r := &RayServiceReconciler{
		Log: ctrl.Log.WithName("controllers").WithName("RayService"),
	}

	// Test 1: Only LastUpdateTime and HealthLastUpdateTime are updated.
	newStatus := oldStatus.DeepCopy()
	newStatus.DashboardStatus.LastUpdateTime = &metav1.Time{Time: timeNow.Add(1)}
	for appName, application := range newStatus.Applications {
		application.HealthLastUpdateTime = &metav1.Time{Time: timeNow.Add(1)}
		application.LastUpdateTime = &metav1.Time{Time: timeNow.Add(2)}
		newStatus.Applications[appName] = application
	}
	assert.False(t, r.inconsistentRayServiceStatus(oldStatus, *newStatus))

	// Test 2: Not only LastUpdateTime and HealthLastUpdateTime are updated.
	newStatus = oldStatus.DeepCopy()
	newStatus.DashboardStatus.LastUpdateTime = &metav1.Time{Time: timeNow.Add(1)}
	newStatus.DashboardStatus.IsHealthy = !oldStatus.DashboardStatus.IsHealthy
	assert.True(t, r.inconsistentRayServiceStatus(oldStatus, *newStatus))
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
				common.RayClusterLabelKey:  cluster.ObjectMeta.Name,
				common.RayNodeTypeLabelKey: string(rayv1.HeadNode),
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
		Log:      ctrl.Log.WithName("controllers").WithName("RayService"),
	}

	// Test 1: There is no head pod. `isHeadPodRunningAndReady` should return false.
	// In addition, an error should be returned if the number of head pods is not 1.
	isReady, err := r.isHeadPodRunningAndReady(ctx, &cluster)
	assert.NotNil(t, err)
	assert.False(t, isReady)

	// Test 2: There is one head pod, but the pod is not running and ready.
	// `isHeadPodRunningAndReady` should return false, and no error should be returned.
	runtimeObjects = []runtime.Object{headPod}
	fakeClient = clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	r.Client = fakeClient
	isReady, err = r.isHeadPodRunningAndReady(ctx, &cluster)
	assert.Nil(t, err)
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
	assert.Nil(t, err)
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
		Log:      ctrl.Log.WithName("controllers").WithName("RayService"),
	}

	ctx := context.TODO()
	// Create a head service.
	err := r.reconcileServices(ctx, &rayService, &cluster, common.HeadService)
	assert.Nil(t, err, "Fail to reconcile service")

	svcList := corev1.ServiceList{}
	err = fakeClient.List(ctx, &svcList, client.InNamespace(namespace))
	assert.Nil(t, err, "Fail to get service list")
	assert.Equal(t, 1, len(svcList.Items), "Service list should have one item")
	oldSvc := svcList.Items[0].DeepCopy()

	// Test 1: When the service for the RayCluster already exists, it should not be updated.
	cluster.Spec.HeadGroupSpec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
		{
			Name:          "test-port",
			ContainerPort: 9999,
		},
	}
	err = r.reconcileServices(ctx, &rayService, &cluster, common.HeadService)
	assert.Nil(t, err, "Fail to reconcile service")

	svcList = corev1.ServiceList{}
	err = fakeClient.List(ctx, &svcList, client.InNamespace(namespace))
	assert.Nil(t, err, "Fail to get service list")
	assert.Equal(t, 1, len(svcList.Items), "Service list should have one item")
	assert.True(t, reflect.DeepEqual(*oldSvc, svcList.Items[0]))

	// Test 2: When the RayCluster switches, the service should be updated.
	cluster.Name = "new-cluster"
	err = r.reconcileServices(ctx, &rayService, &cluster, common.HeadService)
	assert.Nil(t, err, "Fail to reconcile service")

	svcList = corev1.ServiceList{}
	err = fakeClient.List(ctx, &svcList, client.InNamespace(namespace))
	assert.Nil(t, err, "Fail to get service list")
	assert.Equal(t, 1, len(svcList.Items), "Service list should have one item")
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
	assert.Nil(t, err, "Fail to generate head service name")
	headSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      headSvcName,
			Namespace: cluster.ObjectMeta.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: common.DashboardPortName,
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
		Log:      ctrl.Log.WithName("controllers").WithName("RayService"),
	}

	url, err := utils.FetchHeadServiceURL(ctx, &r.Log, r.Client, &cluster, common.DashboardPortName)
	assert.Nil(t, err, "Fail to fetch head service url")
	assert.Equal(t, fmt.Sprintf("test-cluster-head-svc.%s.svc.cluster.local:%d", namespace, dashboardPort), url, "Head service url is not correct")
}

func TestGetAndCheckServeStatus(t *testing.T) {
	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Initialize a fake client with newScheme and runtimeObjects.
	runtimeObjects := []runtime.Object{}
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()

	// Initialize RayService reconciler.
	ctx := context.TODO()
	r := RayServiceReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayService"),
	}
	serviceUnhealthySecondThreshold := int32(ServiceUnhealthySecondThreshold) // The threshold is 900 seconds by default.
	serveAppName := "serve-app-1"

	// Here are the key representing Ray Serve deployment and application statuses.
	const (
		// Ray Serve deployment status
		DeploymentStatus = "DeploymentStatus"
		// Ray Serve application status
		ApplicationStatus = "ApplicationStatus"
	)

	tests := map[string]struct {
		rayServiceStatus map[string]string
		applications     map[string]rayv1.AppStatus
		expectedHealthy  bool
		expectedReady    bool
	}{
		// Test 1: There is no pre-existing RayServiceStatus in the RayService CR. Create a new Ray Serve application, and the application is still deploying.
		"Create a new Ray Serve application": {
			rayServiceStatus: map[string]string{
				DeploymentStatus:  rayv1.DeploymentStatusEnum.UPDATING,
				ApplicationStatus: rayv1.ApplicationStatusEnum.DEPLOYING,
			},
			applications:    map[string]rayv1.AppStatus{},
			expectedHealthy: true,
			expectedReady:   false,
		},
		// Test 2: The Ray Serve application takes more than `serviceUnhealthySecondThreshold` seconds to be "RUNNING".
		// This may happen when `runtime_env` installation takes a long time or the cluster does not have enough resources
		// for autoscaling. Note that the cluster will not be marked as unhealthy if the application is still deploying.
		"Take longer than threshold to be RUNNING while deploying": {
			rayServiceStatus: map[string]string{
				DeploymentStatus:  rayv1.DeploymentStatusEnum.UPDATING,
				ApplicationStatus: rayv1.ApplicationStatusEnum.DEPLOYING,
			},
			applications: map[string]rayv1.AppStatus{
				serveAppName: {
					Status:               rayv1.ApplicationStatusEnum.DEPLOYING,
					HealthLastUpdateTime: &metav1.Time{Time: metav1.Now().Add(-time.Second * time.Duration(serviceUnhealthySecondThreshold+1))},
				},
			},
			expectedHealthy: true,
			expectedReady:   false,
		},
		// Test 3: The Ray Serve application finishes the deployment process and becomes "RUNNING".
		"Finishes the deployment process and becomes RUNNING": {
			rayServiceStatus: map[string]string{
				DeploymentStatus:  rayv1.DeploymentStatusEnum.HEALTHY,
				ApplicationStatus: rayv1.ApplicationStatusEnum.RUNNING,
			},
			applications: map[string]rayv1.AppStatus{
				serveAppName: {
					Status:               rayv1.ApplicationStatusEnum.DEPLOYING,
					HealthLastUpdateTime: &metav1.Time{Time: metav1.Now().Time},
				},
			},
			expectedHealthy: true,
			expectedReady:   true,
		},
		// Test 4: The Ray Serve application lasts "UNHEALTHY" for more than `serviceUnhealthySecondThreshold` seconds.
		// The RayCluster will be marked as unhealthy.
		"UNHEALTHY status lasts longer than threshold": {
			rayServiceStatus: map[string]string{
				DeploymentStatus:  rayv1.DeploymentStatusEnum.UNHEALTHY,
				ApplicationStatus: rayv1.ApplicationStatusEnum.UNHEALTHY,
			},
			applications: map[string]rayv1.AppStatus{
				serveAppName: {
					Status:               rayv1.ApplicationStatusEnum.UNHEALTHY,
					HealthLastUpdateTime: &metav1.Time{Time: metav1.Now().Add(-time.Second * time.Duration(serviceUnhealthySecondThreshold+1))},
				},
			},
			expectedHealthy: false,
			expectedReady:   false,
		},
		// Test 5: The Ray Serve application lasts "UNHEALTHY" for less than `serviceUnhealthySecondThreshold` seconds.
		// The RayCluster will not be marked as unhealthy.
		"UNHEALTHY status lasts less than threshold": {
			rayServiceStatus: map[string]string{
				DeploymentStatus:  rayv1.DeploymentStatusEnum.UNHEALTHY,
				ApplicationStatus: rayv1.ApplicationStatusEnum.UNHEALTHY,
			},
			applications: map[string]rayv1.AppStatus{
				serveAppName: {
					Status:               rayv1.ApplicationStatusEnum.UNHEALTHY,
					HealthLastUpdateTime: &metav1.Time{Time: metav1.Now().Add(-time.Second * time.Duration(serviceUnhealthySecondThreshold-1))},
				},
			},
			expectedHealthy: true,
			expectedReady:   false,
		},
		// Test 6: The Ray Serve application lasts "DEPLOY_FAILED" for more than `serviceUnhealthySecondThreshold` seconds.
		// The RayCluster will be marked as unhealthy.
		"DEPLOY_FAILED status lasts longer than threshold": {
			rayServiceStatus: map[string]string{
				DeploymentStatus:  rayv1.DeploymentStatusEnum.UPDATING,
				ApplicationStatus: rayv1.ApplicationStatusEnum.DEPLOY_FAILED,
			},
			applications: map[string]rayv1.AppStatus{
				serveAppName: {
					Status:               rayv1.ApplicationStatusEnum.DEPLOY_FAILED,
					HealthLastUpdateTime: &metav1.Time{Time: metav1.Now().Add(-time.Second * time.Duration(serviceUnhealthySecondThreshold+1))},
				},
			},
			expectedHealthy: false,
			expectedReady:   false,
		},
		// Test 7: The Ray Serve application lasts "DEPLOY_FAILED" for less than `serviceUnhealthySecondThreshold` seconds.
		// The RayCluster will not be marked as unhealthy.
		"DEPLOY_FAILED status less longer than threshold": {
			rayServiceStatus: map[string]string{
				DeploymentStatus:  rayv1.DeploymentStatusEnum.UPDATING,
				ApplicationStatus: rayv1.ApplicationStatusEnum.DEPLOY_FAILED,
			},
			applications: map[string]rayv1.AppStatus{
				serveAppName: {
					Status:               rayv1.ApplicationStatusEnum.DEPLOY_FAILED,
					HealthLastUpdateTime: &metav1.Time{Time: metav1.Now().Add(-time.Second * time.Duration(serviceUnhealthySecondThreshold-1))},
				},
			},
			expectedHealthy: true,
			expectedReady:   false,
		},
		// Test 8: If the Ray Serve application is not found, the RayCluster is not ready to serve requests.
		"Ray Serve application is not found": {
			rayServiceStatus: map[string]string{},
			applications:     map[string]rayv1.AppStatus{},
			expectedHealthy:  true,
			expectedReady:    false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var dashboardClient utils.RayDashboardClientInterface
			if len(tc.rayServiceStatus) != 0 {
				dashboardClient = initFakeDashboardClient(serveAppName, tc.rayServiceStatus[DeploymentStatus], tc.rayServiceStatus[ApplicationStatus])
			} else {
				dashboardClient = &utils.FakeRayDashboardClient{}
			}
			prevRayServiceStatus := rayv1.RayServiceStatus{Applications: tc.applications}
			isHealthy, isReady, err := r.getAndCheckServeStatus(ctx, dashboardClient, &prevRayServiceStatus, utils.MULTI_APP, &serviceUnhealthySecondThreshold)
			assert.Nil(t, err)
			assert.Equal(t, tc.expectedHealthy, isHealthy)
			assert.Equal(t, tc.expectedReady, isReady)
		})
	}
}

func TestCheckIfNeedSubmitServeDeployment(t *testing.T) {
	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Initialize a fake client with newScheme and runtimeObjects.
	runtimeObjects := []runtime.Object{}
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()

	// Initialize RayService reconciler.
	r := RayServiceReconciler{
		Client:       fakeClient,
		Recorder:     &record.FakeRecorder{},
		Scheme:       scheme.Scheme,
		Log:          ctrl.Log.WithName("controllers").WithName("RayService"),
		ServeConfigs: cmap.New(),
	}

	namespace := "ray"
	cluster := rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: namespace,
		},
	}
	rayService := rayv1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: cluster.ObjectMeta.Namespace,
		},
		Spec: rayv1.RayServiceSpec{
			ServeConfigV2: `
applications:
- name: myapp
  import_path: fruit.deployment_graph
  runtime_env:
  working_dir: "https://github.com/ray-project/test_dag/archive/41d09119cbdf8450599f993f51318e9e27c59098.zip"
  deployments:
  - name: MangoStand
	num_replicas: 1
	user_config:
	price: 3
	ray_actor_options:
	num_cpus: 0.1`,
		},
	}

	// Test 1: The RayCluster is new, and this is the first reconciliation after the RayCluster becomes ready.
	// No Serve application has been created yet, so the RayService's serve configuration has not been cached in
	// `r.ServeConfigs`.
	cacheKey := r.generateConfigKey(&rayService, cluster.Name)
	_, exist := r.ServeConfigs.Get(cacheKey)
	assert.False(t, exist)
	shouldCreate := r.checkIfNeedSubmitServeDeployment(&rayService, &cluster, &rayv1.RayServiceStatus{})
	assert.True(t, shouldCreate)

	// Test 2: The RayCluster is not new, but the head Pod without GCS FT-enabled crashes and restarts.
	// Hence, the RayService's Serve application status is empty, but the KubeRay operator has cached the Serve
	// application's configuration.
	r.ServeConfigs.Set(cacheKey, rayService.Spec.ServeConfigV2) // Simulate the Serve application's configuration has been cached.
	shouldCreate = r.checkIfNeedSubmitServeDeployment(&rayService, &cluster, &rayv1.RayServiceStatus{})
	assert.True(t, shouldCreate)

	// Test 3: The Serve application has been created, and the RayService's status has been updated.
	_, exist = r.ServeConfigs.Get(cacheKey)
	assert.True(t, exist)
	serveStatus := rayv1.RayServiceStatus{
		Applications: map[string]rayv1.AppStatus{
			"myapp": {
				Status: rayv1.ApplicationStatusEnum.RUNNING,
			},
		},
	}
	shouldCreate = r.checkIfNeedSubmitServeDeployment(&rayService, &cluster, &serveStatus)
	assert.False(t, shouldCreate)

	// Test 4: The Serve application has been created, but the Serve config has been updated.
	// Therefore, the Serve in-place update should be triggered.
	rayService.Spec.ServeConfigV2 = `
applications:
- name: new_app_name
  import_path: fruit.deployment_graph`
	shouldCreate = r.checkIfNeedSubmitServeDeployment(&rayService, &cluster, &serveStatus)
	assert.True(t, shouldCreate)
}

func TestReconcileRayCluster(t *testing.T) {
	defer os.Unsetenv(ENABLE_ZERO_DOWNTIME)
	// Create a new scheme with CRDs schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1.AddToScheme(newScheme)

	ctx := context.TODO()
	namespace := "ray"
	rayService := rayv1.RayService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: namespace,
		},
		Status: rayv1.RayServiceStatuses{},
	}

	hash, err := generateRayClusterJsonHash(rayService.Spec.RayClusterSpec)
	assert.Nil(t, err)
	activeCluster := rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "active-cluster",
			Namespace: namespace,
			Annotations: map[string]string{
				common.RayServiceClusterHashKey: hash,
			},
		},
	}

	tests := map[string]struct {
		activeCluster           *rayv1.RayCluster
		updateRayClusterSpec    bool
		enableZeroDowntime      bool
		shouldPrepareNewCluster bool
	}{
		// Test 1: Neither active nor pending clusters exist. The `markRestart` function will be called, so the `PendingServiceStatus.RayClusterName` should be set.
		"Zero-downtime upgrade is enabled. Neither active nor pending clusters exist.": {
			activeCluster:           nil,
			updateRayClusterSpec:    false,
			enableZeroDowntime:      true,
			shouldPrepareNewCluster: true,
		},
		// Test 2: The active cluster exists, but the pending cluster does not exist.
		"Zero-downtime upgrade is enabled. The active cluster exists, but the pending cluster does not exist.": {
			activeCluster:           activeCluster.DeepCopy(),
			updateRayClusterSpec:    false,
			enableZeroDowntime:      true,
			shouldPrepareNewCluster: false,
		},
		// Test 3: The active cluster exists. Trigger the zero-downtime upgrade.
		"Zero-downtime upgrade is enabled. The active cluster exists. Trigger the zero-downtime upgrade.": {
			activeCluster:           activeCluster.DeepCopy(),
			updateRayClusterSpec:    true,
			enableZeroDowntime:      true,
			shouldPrepareNewCluster: true,
		},
		// Test 4: The active cluster exists. Trigger the zero-downtime upgrade.
		"Zero-downtime upgrade is disabled. The active cluster exists. Trigger the zero-downtime upgrade.": {
			activeCluster:           activeCluster.DeepCopy(),
			updateRayClusterSpec:    true,
			enableZeroDowntime:      false,
			shouldPrepareNewCluster: false,
		},
		// Test 5: Neither active nor pending clusters exist. The `markRestart` function will be called, so the `PendingServiceStatus.RayClusterName` should be set.
		"Zero-downtime upgrade is disabled. Neither active nor pending clusters exist.": {
			activeCluster:           nil,
			updateRayClusterSpec:    false,
			enableZeroDowntime:      false,
			shouldPrepareNewCluster: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Enable or disable zero-downtime upgrade.
			defer os.Unsetenv(ENABLE_ZERO_DOWNTIME)
			if !tc.enableZeroDowntime {
				os.Setenv(ENABLE_ZERO_DOWNTIME, "false")
			}
			runtimeObjects := []runtime.Object{}
			if tc.activeCluster != nil {
				runtimeObjects = append(runtimeObjects, tc.activeCluster.DeepCopy())
			}
			fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
			r := RayServiceReconciler{
				Client: fakeClient,
				Log:    ctrl.Log.WithName("controllers").WithName("RayService"),
			}
			service := rayService.DeepCopy()
			if tc.updateRayClusterSpec {
				service.Spec.RayClusterSpec.RayVersion = "new-version"
			}
			if tc.activeCluster != nil {
				service.Status.ActiveServiceStatus.RayClusterName = tc.activeCluster.Name
			}
			assert.Equal(t, "", service.Status.PendingServiceStatus.RayClusterName)
			_, _, err = r.reconcileRayCluster(ctx, service)
			assert.Nil(t, err)

			// If KubeRay operator is preparing a new cluster, the `PendingServiceStatus.RayClusterName` should be set by calling the function `markRestart`.
			if tc.shouldPrepareNewCluster {
				assert.NotEqual(t, "", service.Status.PendingServiceStatus.RayClusterName)
			} else {
				assert.Equal(t, "", service.Status.PendingServiceStatus.RayClusterName)
			}
		})
	}
}

func TestUpdateAndCheckDashboardStatus(t *testing.T) {
	timestamp := metav1.Now()
	rayServiceStatus := rayv1.RayServiceStatus{
		DashboardStatus: rayv1.DashboardStatus{
			IsHealthy:            true,
			HealthLastUpdateTime: &timestamp,
			LastUpdateTime:       &timestamp,
		},
	}

	// Test 1: The dashboard agent was healthy, and the dashboard agent is still healthy.
	svcStatusCopy := rayServiceStatus.DeepCopy()
	updateAndCheckDashboardStatus(svcStatusCopy, true)
	assert.NotEqual(t, svcStatusCopy.DashboardStatus.HealthLastUpdateTime, timestamp)

	// Test 2: The dashboard agent was healthy, and the dashboard agent becomes unhealthy.
	svcStatusCopy = rayServiceStatus.DeepCopy()
	updateAndCheckDashboardStatus(svcStatusCopy, false)
	assert.Equal(t, *svcStatusCopy.DashboardStatus.HealthLastUpdateTime, timestamp)

	// Test 3: The dashboard agent was unhealthy, and the dashboard agent is still unhealthy.
	svcStatusCopy = rayServiceStatus.DeepCopy()
	svcStatusCopy.DashboardStatus.IsHealthy = false
	updateAndCheckDashboardStatus(svcStatusCopy, false)
	// The `HealthLastUpdateTime` should not be updated.
	assert.Equal(t, *svcStatusCopy.DashboardStatus.HealthLastUpdateTime, timestamp)

	// Test 4: The dashboard agent was unhealthy, and the dashboard agent becomes healthy.
	svcStatusCopy = rayServiceStatus.DeepCopy()
	svcStatusCopy.DashboardStatus.IsHealthy = false
	updateAndCheckDashboardStatus(svcStatusCopy, true)
	assert.NotEqual(t, *svcStatusCopy.DashboardStatus.HealthLastUpdateTime, timestamp)
}

func initFakeDashboardClient(appName string, deploymentStatus string, appStatus string) utils.RayDashboardClientInterface {
	fakeDashboardClient := utils.FakeRayDashboardClient{}
	status := generateServeStatus(deploymentStatus, appStatus)
	fakeDashboardClient.SetMultiApplicationStatuses(map[string]*utils.ServeApplicationStatus{appName: &status})
	return &fakeDashboardClient
}
