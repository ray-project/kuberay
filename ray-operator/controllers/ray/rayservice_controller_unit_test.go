package ray

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	rayv1alpha1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
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
	cluster := rayv1alpha1.RayCluster{
		Spec: rayv1alpha1.RayClusterSpec{
			RayVersion: "2.5.0",
			WorkerGroupSpecs: []rayv1alpha1.WorkerGroupSpec{
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
	cluster1 := rayv1alpha1.RayCluster{
		Spec: rayv1alpha1.RayClusterSpec{
			RayVersion: "2.5.0",
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
	oldStatus := rayv1alpha1.RayServiceStatuses{
		ActiveServiceStatus: rayv1alpha1.RayServiceStatus{
			RayClusterName: "new-cluster",
			DashboardStatus: rayv1alpha1.DashboardStatus{
				IsHealthy:            true,
				LastUpdateTime:       &timeNow,
				HealthLastUpdateTime: &timeNow,
			},
			Applications: map[string]rayv1alpha1.AppStatus{
				common.DefaultServeAppName: {
					Status:               rayv1alpha1.ApplicationStatusEnum.RUNNING,
					Message:              "OK",
					LastUpdateTime:       &timeNow,
					HealthLastUpdateTime: &timeNow,
					Deployments: map[string]rayv1alpha1.ServeDeploymentStatus{
						"serve-1": {
							Status:               rayv1alpha1.DeploymentStatusEnum.UNHEALTHY,
							Message:              "error",
							LastUpdateTime:       &timeNow,
							HealthLastUpdateTime: &timeNow,
						},
					},
				},
			},
		},
		PendingServiceStatus: rayv1alpha1.RayServiceStatus{
			RayClusterName: "old-cluster",
			DashboardStatus: rayv1alpha1.DashboardStatus{
				IsHealthy:            true,
				LastUpdateTime:       &timeNow,
				HealthLastUpdateTime: &timeNow,
			},
			Applications: map[string]rayv1alpha1.AppStatus{
				common.DefaultServeAppName: {
					Status:               rayv1alpha1.ApplicationStatusEnum.NOT_STARTED,
					Message:              "application not started yet",
					LastUpdateTime:       &timeNow,
					HealthLastUpdateTime: &timeNow,
					Deployments: map[string]rayv1alpha1.ServeDeploymentStatus{
						"serve-1": {
							Status:               rayv1alpha1.DeploymentStatusEnum.HEALTHY,
							Message:              "Serve is healthy",
							LastUpdateTime:       &timeNow,
							HealthLastUpdateTime: &timeNow,
						},
					},
				},
			},
		},
		ServiceStatus: rayv1alpha1.WaitForDashboard,
	}

	// Test 1: Update ServiceStatus only.
	newStatus := oldStatus.DeepCopy()
	newStatus.ServiceStatus = rayv1alpha1.WaitForServeDeploymentReady
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
	oldStatus := rayv1alpha1.RayServiceStatus{
		RayClusterName: "cluster-1",
		DashboardStatus: rayv1alpha1.DashboardStatus{
			IsHealthy:            true,
			LastUpdateTime:       &timeNow,
			HealthLastUpdateTime: &timeNow,
		},
		Applications: map[string]rayv1alpha1.AppStatus{
			"app1": {
				Status:               rayv1alpha1.ApplicationStatusEnum.RUNNING,
				Message:              "Application is running",
				LastUpdateTime:       &timeNow,
				HealthLastUpdateTime: &timeNow,
				Deployments: map[string]rayv1alpha1.ServeDeploymentStatus{
					"serve-1": {
						Status:               rayv1alpha1.DeploymentStatusEnum.HEALTHY,
						Message:              "Serve is healthy",
						LastUpdateTime:       &timeNow,
						HealthLastUpdateTime: &timeNow,
					},
				},
			},
			"app2": {
				Status:               rayv1alpha1.ApplicationStatusEnum.RUNNING,
				Message:              "Application is running",
				LastUpdateTime:       &timeNow,
				HealthLastUpdateTime: &timeNow,
				Deployments: map[string]rayv1alpha1.ServeDeploymentStatus{
					"serve-1": {
						Status:               rayv1alpha1.DeploymentStatusEnum.HEALTHY,
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
	_ = rayv1alpha1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Mock data
	cluster := rayv1alpha1.RayCluster{
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
				common.RayNodeTypeLabelKey: string(rayv1alpha1.HeadNode),
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
	_ = rayv1alpha1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Mock data
	namespace := "ray"
	cluster := rayv1alpha1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: namespace,
		},
		Spec: rayv1alpha1.RayClusterSpec{
			HeadGroupSpec: rayv1alpha1.HeadGroupSpec{
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
	rayService := rayv1alpha1.RayService{
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
	_ = rayv1alpha1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Mock data
	namespace := "ray"
	dashboardPort := int32(9999)
	cluster := rayv1alpha1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: namespace,
		},
	}
	headSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GenerateServiceName(cluster.Name),
			Namespace: cluster.ObjectMeta.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: common.DefaultDashboardName,
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

	url, err := utils.FetchHeadServiceURL(ctx, &r.Log, r.Client, &cluster, common.DefaultDashboardName)
	assert.Nil(t, err, "Fail to fetch head service url")
	assert.Equal(t, fmt.Sprintf("test-cluster-head-svc.%s.svc.cluster.local:%d", namespace, dashboardPort), url, "Head service url is not correct")
}

func TestGetAndCheckServeStatus(t *testing.T) {
	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = rayv1alpha1.AddToScheme(newScheme)
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

	// Test 1: There is no pre-existing RayServiceStatus in the RayService CR. Create a new Ray Serve application, and the application is still deploying.
	dashboardClient := initFakeDashboardClient(serveAppName, rayv1alpha1.DeploymentStatusEnum.UPDATING, rayv1alpha1.ApplicationStatusEnum.DEPLOYING)
	prevRayServiceStatus := rayv1alpha1.RayServiceStatus{
		Applications: map[string]rayv1alpha1.AppStatus{},
	}
	isHealthy, isReady, err := r.getAndCheckServeStatus(ctx, dashboardClient, &prevRayServiceStatus, utils.MULTI_APP, &serviceUnhealthySecondThreshold)
	assert.Nil(t, err)
	assert.True(t, isHealthy)
	assert.False(t, isReady)

	// Test 2: The Ray Serve application takes more than `serviceUnhealthySecondThreshold` seconds to be "RUNNING".
	// This may happen when `runtime_env` installation takes a long time or the cluster does not have enough resources
	// for autoscaling. Note that the cluster will not be marked as unhealthy if the application is still deploying.
	dashboardClient = initFakeDashboardClient(serveAppName, rayv1alpha1.DeploymentStatusEnum.UPDATING, rayv1alpha1.ApplicationStatusEnum.DEPLOYING)
	prevRayServiceStatus = rayv1alpha1.RayServiceStatus{
		Applications: map[string]rayv1alpha1.AppStatus{
			serveAppName: {
				Status:               rayv1alpha1.ApplicationStatusEnum.DEPLOYING,
				HealthLastUpdateTime: &metav1.Time{Time: metav1.Now().Add(-time.Second * time.Duration(serviceUnhealthySecondThreshold+1))},
			},
		},
	}
	isHealthy, isReady, err = r.getAndCheckServeStatus(ctx, dashboardClient, &prevRayServiceStatus, utils.MULTI_APP, &serviceUnhealthySecondThreshold)
	assert.Nil(t, err)
	assert.True(t, isHealthy)
	assert.False(t, isReady)

	// Test 3: The Ray Serve application finishes the deployment process and becomes "RUNNING".
	dashboardClient = initFakeDashboardClient(serveAppName, rayv1alpha1.DeploymentStatusEnum.HEALTHY, rayv1alpha1.ApplicationStatusEnum.RUNNING)
	prevRayServiceStatus = rayv1alpha1.RayServiceStatus{
		Applications: map[string]rayv1alpha1.AppStatus{
			serveAppName: {
				Status:               rayv1alpha1.ApplicationStatusEnum.DEPLOYING,
				HealthLastUpdateTime: &metav1.Time{Time: metav1.Now().Time},
			},
		},
	}
	isHealthy, isReady, err = r.getAndCheckServeStatus(ctx, dashboardClient, &prevRayServiceStatus, utils.MULTI_APP, &serviceUnhealthySecondThreshold)
	assert.Nil(t, err)
	assert.True(t, isHealthy)
	assert.True(t, isReady)

	// Test 4: The Ray Serve application lasts "UNHEALTHY" for more than `serviceUnhealthySecondThreshold` seconds.
	// The RayCluster will be marked as unhealthy.
	dashboardClient = initFakeDashboardClient(serveAppName, rayv1alpha1.DeploymentStatusEnum.UNHEALTHY, rayv1alpha1.ApplicationStatusEnum.UNHEALTHY)
	prevRayServiceStatus = rayv1alpha1.RayServiceStatus{
		Applications: map[string]rayv1alpha1.AppStatus{
			serveAppName: {
				Status:               rayv1alpha1.ApplicationStatusEnum.UNHEALTHY,
				HealthLastUpdateTime: &metav1.Time{Time: metav1.Now().Add(-time.Second * time.Duration(serviceUnhealthySecondThreshold+1))},
			},
		},
	}
	isHealthy, isReady, err = r.getAndCheckServeStatus(ctx, dashboardClient, &prevRayServiceStatus, utils.MULTI_APP, &serviceUnhealthySecondThreshold)
	assert.Nil(t, err)
	assert.False(t, isHealthy)
	assert.False(t, isReady)

	// Test 5: The Ray Serve application lasts "UNHEALTHY" for less than `serviceUnhealthySecondThreshold` seconds.
	// The RayCluster will not be marked as unhealthy.
	dashboardClient = initFakeDashboardClient(serveAppName, rayv1alpha1.DeploymentStatusEnum.UNHEALTHY, rayv1alpha1.ApplicationStatusEnum.UNHEALTHY)
	prevRayServiceStatus = rayv1alpha1.RayServiceStatus{
		Applications: map[string]rayv1alpha1.AppStatus{
			serveAppName: {
				Status:               rayv1alpha1.ApplicationStatusEnum.UNHEALTHY,
				HealthLastUpdateTime: &metav1.Time{Time: metav1.Now().Add(-time.Second * time.Duration(serviceUnhealthySecondThreshold-1))},
			},
		},
	}
	isHealthy, isReady, err = r.getAndCheckServeStatus(ctx, dashboardClient, &prevRayServiceStatus, utils.MULTI_APP, &serviceUnhealthySecondThreshold)
	assert.Nil(t, err)
	assert.True(t, isHealthy)
	assert.False(t, isReady)

	// Test 6: The Ray Serve application lasts "DEPLOY_FAILED" for more than `serviceUnhealthySecondThreshold` seconds.
	// The RayCluster will be marked as unhealthy.
	dashboardClient = initFakeDashboardClient(serveAppName, rayv1alpha1.DeploymentStatusEnum.UPDATING, rayv1alpha1.ApplicationStatusEnum.DEPLOY_FAILED)
	prevRayServiceStatus = rayv1alpha1.RayServiceStatus{
		Applications: map[string]rayv1alpha1.AppStatus{
			serveAppName: {
				Status:               rayv1alpha1.ApplicationStatusEnum.DEPLOY_FAILED,
				HealthLastUpdateTime: &metav1.Time{Time: metav1.Now().Add(-time.Second * time.Duration(serviceUnhealthySecondThreshold+1))},
			},
		},
	}
	isHealthy, isReady, err = r.getAndCheckServeStatus(ctx, dashboardClient, &prevRayServiceStatus, utils.MULTI_APP, &serviceUnhealthySecondThreshold)
	assert.Nil(t, err)
	assert.False(t, isHealthy)
	assert.False(t, isReady)

	// Test 7: The Ray Serve application lasts "DEPLOY_FAILED" for less than `serviceUnhealthySecondThreshold` seconds.
	// The RayCluster will not be marked as unhealthy.
	dashboardClient = initFakeDashboardClient(serveAppName, rayv1alpha1.DeploymentStatusEnum.UPDATING, rayv1alpha1.ApplicationStatusEnum.DEPLOY_FAILED)
	prevRayServiceStatus = rayv1alpha1.RayServiceStatus{
		Applications: map[string]rayv1alpha1.AppStatus{
			serveAppName: {
				Status:               rayv1alpha1.ApplicationStatusEnum.DEPLOY_FAILED,
				HealthLastUpdateTime: &metav1.Time{Time: metav1.Now().Add(-time.Second * time.Duration(serviceUnhealthySecondThreshold-1))},
			},
		},
	}
	isHealthy, isReady, err = r.getAndCheckServeStatus(ctx, dashboardClient, &prevRayServiceStatus, utils.MULTI_APP, &serviceUnhealthySecondThreshold)
	assert.Nil(t, err)
	assert.True(t, isHealthy)
	assert.False(t, isReady)
}

func initFakeDashboardClient(appName string, deploymentStatus string, appStatus string) utils.RayDashboardClientInterface {
	status := generateServeStatus(deploymentStatus, appStatus)
	fakeDashboardClient := utils.FakeRayDashboardClient{}
	fakeDashboardClient.SetMultiApplicationStatuses(map[string]*utils.ServeApplicationStatus{appName: &status})
	return &fakeDashboardClient
}
