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
	assert.Nil(t, err)

	*cluster.Spec.WorkerGroupSpecs[0].Replicas++
	hash2, err := generateHashWithoutReplicasAndWorkersToDelete(cluster.Spec)
	assert.Nil(t, err)
	assert.Equal(t, hash1, hash2)

	// RayVersion will not be muted, so `hash3` should not be equal to `hash1`.
	cluster.Spec.RayVersion = "2.100.0"
	hash3, err := generateHashWithoutReplicasAndWorkersToDelete(cluster.Spec)
	assert.Nil(t, err)
	assert.NotEqual(t, hash1, hash3)
}

func TestDecideClusterAction(t *testing.T) {
	ctx := context.TODO()

	fillAnnotations := func(rayCluster *rayv1.RayCluster) {
		hash, _ := generateHashWithoutReplicasAndWorkersToDelete(rayCluster.Spec)
		rayCluster.ObjectMeta.Annotations[utils.HashWithoutReplicasAndWorkersToDeleteKey] = hash
		rayCluster.ObjectMeta.Annotations[utils.NumWorkerGroupsKey] = strconv.Itoa(len(rayCluster.Spec.WorkerGroupSpecs))
	}

	rayServiceStatusWithPendingCluster := rayv1.RayServiceStatuses{
		PendingServiceStatus: rayv1.RayServiceStatus{
			RayClusterName: "new-cluster",
		},
	}

	rayClusterBase := &rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				utils.KubeRayVersion: utils.KUBERAY_VERSION,
			},
		},
		Spec: rayv1.RayClusterSpec{
			RayVersion: "1.0.0",
			WorkerGroupSpecs: []rayv1.WorkerGroupSpec{
				{
					Replicas:    ptr.To[int32](2),
					MinReplicas: ptr.To[int32](1),
					MaxReplicas: ptr.To[int32](4),
					GroupName:   "worker-group-1",
					ScaleStrategy: rayv1.ScaleStrategy{
						WorkersToDelete: []string{"worker-1", "worker-2"},
					},
				},
			},
		},
	}
	fillAnnotations(rayClusterBase)

	rayClusterDifferentRayVersion := rayClusterBase.DeepCopy()
	rayClusterDifferentRayVersion.Spec.RayVersion = "2.0.0"
	fillAnnotations(rayClusterDifferentRayVersion)

	rayClusterDifferentReplicasAndWorkersToDelete := rayClusterBase.DeepCopy()
	rayClusterDifferentReplicasAndWorkersToDelete.Spec.WorkerGroupSpecs[0].Replicas = ptr.To[int32](3)
	rayClusterDifferentReplicasAndWorkersToDelete.Spec.WorkerGroupSpecs[0].ScaleStrategy.WorkersToDelete = []string{"worker-3", "worker-4"}
	fillAnnotations(rayClusterDifferentReplicasAndWorkersToDelete)

	rayClusterDifferentWorkerGroup := rayClusterBase.DeepCopy()
	rayClusterDifferentWorkerGroup.Spec.WorkerGroupSpecs[0].GroupName = "worker-group-2"
	fillAnnotations(rayClusterDifferentWorkerGroup)

	rayClusterAdditionalWorkerGroup := rayClusterBase.DeepCopy()
	rayClusterAdditionalWorkerGroup.Spec.WorkerGroupSpecs = append(rayClusterAdditionalWorkerGroup.Spec.WorkerGroupSpecs, rayv1.WorkerGroupSpec{
		Replicas:    ptr.To[int32](3),
		MinReplicas: ptr.To[int32](2),
		MaxReplicas: ptr.To[int32](5),
		GroupName:   "worker-group-2",
	})
	fillAnnotations(rayClusterAdditionalWorkerGroup)

	rayClusterWorkerGroupRemoved := rayClusterBase.DeepCopy()
	rayClusterWorkerGroupRemoved.Spec.WorkerGroupSpecs = []rayv1.WorkerGroupSpec{}
	fillAnnotations(rayClusterWorkerGroupRemoved)

	rayClusterDifferentKubeRayVersion := rayClusterBase.DeepCopy()
	rayClusterDifferentKubeRayVersion.ObjectMeta.Annotations[utils.KubeRayVersion] = "some-other-version"

	tests := []struct {
		rayService        *rayv1.RayService
		activeRayCluster  *rayv1.RayCluster
		pendingRayCluster *rayv1.RayCluster
		name              string
		expectedAction    ClusterAction
	}{
		{
			name: "Has pending cluster name and cluster spec is the same",
			rayService: &rayv1.RayService{
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayClusterBase.Spec,
				},
				Status: rayServiceStatusWithPendingCluster,
			},
			activeRayCluster:  nil,
			pendingRayCluster: rayClusterBase,
			expectedAction:    DoNothing,
		},
		{
			name: "Has pending cluster name and cluster spec has different Ray version",
			rayService: &rayv1.RayService{
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayClusterDifferentRayVersion.Spec,
				},
				Status: rayServiceStatusWithPendingCluster,
			},
			activeRayCluster:  nil,
			pendingRayCluster: rayClusterBase,
			expectedAction:    CreatePendingCluster,
		},
		{
			name: "Has pending cluster name and cluster spec has different replicas and workers to delete",
			rayService: &rayv1.RayService{
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayClusterDifferentReplicasAndWorkersToDelete.Spec,
				},
				Status: rayServiceStatusWithPendingCluster,
			},
			activeRayCluster:  nil,
			pendingRayCluster: rayClusterBase,
			expectedAction:    DoNothing,
		},
		{
			name: "Has pending cluster name and cluster spec has different worker group name",
			rayService: &rayv1.RayService{
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayClusterDifferentWorkerGroup.Spec,
				},
				Status: rayServiceStatusWithPendingCluster,
			},
			activeRayCluster:  nil,
			pendingRayCluster: rayClusterBase,
			expectedAction:    CreatePendingCluster,
		},
		{
			name: "Has pending cluster name and cluster spec has additional worker group",
			rayService: &rayv1.RayService{
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayClusterAdditionalWorkerGroup.Spec,
				},
				Status: rayServiceStatusWithPendingCluster,
			},
			activeRayCluster:  nil,
			pendingRayCluster: rayClusterBase,
			expectedAction:    UpdatePendingCluster,
		},
		{
			name: "Has pending cluster name and cluster spec has no worker group",
			rayService: &rayv1.RayService{
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayClusterWorkerGroupRemoved.Spec,
				},
				Status: rayServiceStatusWithPendingCluster,
			},
			activeRayCluster:  nil,
			pendingRayCluster: rayClusterBase,
			expectedAction:    CreatePendingCluster,
		},
		{
			name:              "No pending cluster name and no active cluster",
			rayService:        &rayv1.RayService{},
			activeRayCluster:  nil,
			pendingRayCluster: nil,
			expectedAction:    GeneratePendingClusterName,
		},
		{
			name:              "No pending cluster name and active cluster has different KubeRay version",
			rayService:        &rayv1.RayService{},
			activeRayCluster:  rayClusterDifferentKubeRayVersion,
			pendingRayCluster: nil,
			expectedAction:    UpdateActiveCluster,
		},
		{
			name: "No pending cluster name and cluster spec is the same",
			rayService: &rayv1.RayService{
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayClusterBase.Spec,
				},
			},
			activeRayCluster:  rayClusterBase,
			pendingRayCluster: nil,
			expectedAction:    DoNothing,
		},
		{
			name: "No pending cluster name and cluster spec has different Ray version",
			rayService: &rayv1.RayService{
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayClusterDifferentRayVersion.Spec,
				},
			},
			activeRayCluster:  rayClusterBase,
			pendingRayCluster: nil,
			expectedAction:    GeneratePendingClusterName,
		},
		{
			name: "No pending cluster name and cluster spec has different replicas and workers to delete",
			rayService: &rayv1.RayService{
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayClusterDifferentReplicasAndWorkersToDelete.Spec,
				},
			},
			activeRayCluster:  rayClusterBase,
			pendingRayCluster: nil,
			expectedAction:    DoNothing,
		},
		{
			name: "No pending cluster name and cluster spec has different worker group name",
			rayService: &rayv1.RayService{
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayClusterDifferentWorkerGroup.Spec,
				},
			},
			activeRayCluster:  rayClusterBase,
			pendingRayCluster: nil,
			expectedAction:    GeneratePendingClusterName,
		},
		{
			name: "No pending cluster name and cluster spec has additional worker group",
			rayService: &rayv1.RayService{
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayClusterAdditionalWorkerGroup.Spec,
				},
			},
			activeRayCluster:  rayClusterBase,
			pendingRayCluster: nil,
			expectedAction:    UpdateActiveCluster,
		},
		{
			name: "No pending cluster name and cluster spec has no worker group",
			rayService: &rayv1.RayService{
				Spec: rayv1.RayServiceSpec{
					RayClusterSpec: rayClusterWorkerGroupRemoved.Spec,
				},
			},
			activeRayCluster:  rayClusterBase,
			pendingRayCluster: nil,
			expectedAction:    GeneratePendingClusterName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := decideClusterAction(ctx, tt.rayService, tt.activeRayCluster, tt.pendingRayCluster)
			assert.Equal(t, tt.expectedAction, action)
		})
	}
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
		ServiceStatus: rayv1.PreparingNewCluster,
	}
	ctx := context.Background()

	// Test 1: Update ServiceStatus only.
	newStatus := oldStatus.DeepCopy()
	newStatus.ServiceStatus = rayv1.WaitForServeDeploymentReady
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
	}

	ctx := context.TODO()
	// Create a head service.
	_, err := r.reconcileServices(ctx, &rayService, &cluster, utils.HeadService)
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
	_, err = r.reconcileServices(ctx, &rayService, &cluster, utils.HeadService)
	assert.Nil(t, err, "Fail to reconcile service")

	svcList = corev1.ServiceList{}
	err = fakeClient.List(ctx, &svcList, client.InNamespace(namespace))
	assert.Nil(t, err, "Fail to get service list")
	assert.Equal(t, 1, len(svcList.Items), "Service list should have one item")
	assert.True(t, reflect.DeepEqual(*oldSvc, svcList.Items[0]))

	// Test 2: When the RayCluster switches, the service should be updated.
	cluster.Name = "new-cluster"
	_, err = r.reconcileServices(ctx, &rayService, &cluster, utils.HeadService)
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
	assert.Nil(t, err, "Fail to fetch head service url")
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

	tests := map[string]struct {
		rayServiceStatus map[string]string
		applications     map[string]rayv1.AppStatus
		expectedReady    bool
	}{
		// Test 1: There is no pre-existing RayServiceStatus in the RayService CR. Create a new Ray Serve application, and the application is still deploying.
		"Create a new Ray Serve application": {
			rayServiceStatus: map[string]string{
				DeploymentStatus:  rayv1.DeploymentStatusEnum.UPDATING,
				ApplicationStatus: rayv1.ApplicationStatusEnum.DEPLOYING,
			},
			applications:  map[string]rayv1.AppStatus{},
			expectedReady: false,
		},
		// Test 2: The Ray Serve application finishes the deployment process and becomes "RUNNING".
		"Finishes the deployment process and becomes RUNNING": {
			rayServiceStatus: map[string]string{
				DeploymentStatus:  rayv1.DeploymentStatusEnum.HEALTHY,
				ApplicationStatus: rayv1.ApplicationStatusEnum.RUNNING,
			},
			applications: map[string]rayv1.AppStatus{
				serveAppName: {
					Status: rayv1.ApplicationStatusEnum.RUNNING,
				},
			},
			expectedReady: true,
		},
		// Test 3: Both the current Ray Serve application and RayService status are unhealthy.
		"Both the current Ray Serve application and RayService status are unhealthy": {
			rayServiceStatus: map[string]string{
				DeploymentStatus:  rayv1.DeploymentStatusEnum.UNHEALTHY,
				ApplicationStatus: rayv1.ApplicationStatusEnum.UNHEALTHY,
			},
			applications: map[string]rayv1.AppStatus{
				serveAppName: {
					Status: rayv1.ApplicationStatusEnum.UNHEALTHY,
				},
			},
			expectedReady: false,
		},
		// Test 4: Both the current Ray Serve application and RayService status are DEPLOY_FAILED.
		"Both the current Ray Serve application and RayService status are DEPLOY_FAILED": {
			rayServiceStatus: map[string]string{
				DeploymentStatus:  rayv1.DeploymentStatusEnum.UPDATING,
				ApplicationStatus: rayv1.ApplicationStatusEnum.DEPLOY_FAILED,
			},
			applications: map[string]rayv1.AppStatus{
				serveAppName: {
					Status: rayv1.ApplicationStatusEnum.DEPLOY_FAILED,
				},
			},
			expectedReady: false,
		},
		// Test 5: If the Ray Serve application is not found, the RayCluster is not ready to serve requests.
		"Ray Serve application is not found": {
			rayServiceStatus: map[string]string{},
			applications:     map[string]rayv1.AppStatus{},
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
			isReady, err := getAndCheckServeStatus(ctx, dashboardClient, &prevRayServiceStatus)
			assert.Nil(t, err)
			assert.Equal(t, tc.expectedReady, isReady)
		})
	}
}

func TestCheckIfNeedSubmitServeApplications(t *testing.T) {
	serveConfigV2_1 := "serve-config-1"
	serveConfigV2_2 := "serve-config-2"

	serveStatus := rayv1.RayServiceStatus{
		Applications: map[string]rayv1.AppStatus{
			"myapp": {
				Status: rayv1.ApplicationStatusEnum.RUNNING,
			},
		},
	}
	emptyServeStatus := rayv1.RayServiceStatus{}

	// Test 1: The cached Serve config is empty, and the new Serve config is not empty.
	// This happens when the RayCluster is new, and the serve application has not been created yet.
	shouldCreate, _ := checkIfNeedSubmitServeApplications("", serveConfigV2_1, &emptyServeStatus)
	assert.True(t, shouldCreate)

	// Test 2: The cached Serve config and the new Serve config are the same.
	// This happens when the serve application is already created, and users do not update the serve config.
	shouldCreate, _ = checkIfNeedSubmitServeApplications(serveConfigV2_1, serveConfigV2_1, &serveStatus)
	assert.False(t, shouldCreate)

	// Test 3: The cached Serve config and the new Serve config are different.
	// This happens when the serve application is already created, and users update the serve config.
	shouldCreate, _ = checkIfNeedSubmitServeApplications(serveConfigV2_1, serveConfigV2_2, &serveStatus)
	assert.True(t, shouldCreate)

	// Test 4: Both the cached Serve config and the new Serve config are the same, but the RayService CR status is empty.
	// This happens when the head Pod crashed and GCS FT was not enabled
	shouldCreate, _ = checkIfNeedSubmitServeApplications(serveConfigV2_1, serveConfigV2_1, &emptyServeStatus)
	assert.True(t, shouldCreate)

	// Test 5: The cached Serve config is empty, but the new Serve config is not empty.
	// This happens when KubeRay operator crashes and restarts. Submit the request for safety.
	shouldCreate, _ = checkIfNeedSubmitServeApplications("", serveConfigV2_1, &serveStatus)
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

	hash, err := generateHashWithoutReplicasAndWorkersToDelete(rayService.Spec.RayClusterSpec)
	assert.Nil(t, err)
	activeCluster := rayv1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "active-cluster",
			Namespace: namespace,
			Annotations: map[string]string{
				utils.HashWithoutReplicasAndWorkersToDeleteKey: hash,
				utils.NumWorkerGroupsKey:                       strconv.Itoa(len(rayService.Spec.RayClusterSpec.WorkerGroupSpecs)),
				utils.KubeRayVersion:                           utils.KUBERAY_VERSION,
			},
		},
	}

	tests := map[string]struct {
		activeCluster           *rayv1.RayCluster
		rayServiceUpgradeType   rayv1.RayServiceUpgradeType
		kubeRayVersion          string
		updateRayClusterSpec    bool
		enableZeroDowntime      bool
		shouldPrepareNewCluster bool
		updateKubeRayVersion    bool
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
		// Test 4: The active cluster exists. Zero-downtime upgrade is false, should not trigger zero-downtime upgrade.
		"Zero-downtime upgrade is disabled. The active cluster exists. Does not trigger the zero-downtime upgrade.": {
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
		// Test 6: If the active KubeRay version doesn't match the KubeRay version annotation on the RayCluster, update the RayCluster's hash and KubeRay version
		// annotations first before checking whether to trigger a zero downtime upgrade. This behavior occurs because when we upgrade the KubeRay CRD, the hash
		// generated by different KubeRay versions may differ, which can accidentally trigger a zero downtime upgrade.
		"Active RayCluster exists. KubeRay version is mismatched. Update the RayCluster.": {
			activeCluster:           activeCluster.DeepCopy(),
			updateRayClusterSpec:    true,
			enableZeroDowntime:      true,
			shouldPrepareNewCluster: false,
			updateKubeRayVersion:    true,
			kubeRayVersion:          "new-version",
		},
		// Test 7: Zero downtime upgrade is enabled, but is enabled through the RayServiceSpec
		"Zero-downtime upgrade enabled. The active cluster exist. Zero-downtime upgrade is triggered through RayServiceSpec.": {
			activeCluster:           activeCluster.DeepCopy(),
			updateRayClusterSpec:    true,
			enableZeroDowntime:      true,
			shouldPrepareNewCluster: true,
			rayServiceUpgradeType:   rayv1.NewCluster,
		},
		// Test 8: Zero downtime upgrade is enabled. Env var is set to false but RayServiceSpec is set to NewCluster. Trigger the zero-downtime upgrade.
		"Zero-downtime upgrade is enabled through RayServiceSpec and not through env var. Active cluster exist. Trigger the zero-downtime upgrade.": {
			activeCluster:           activeCluster.DeepCopy(),
			updateRayClusterSpec:    true,
			enableZeroDowntime:      false,
			shouldPrepareNewCluster: true,
			rayServiceUpgradeType:   rayv1.NewCluster,
		},
		// Test 9: Zero downtime upgrade is disabled. Env var is set to true but RayServiceSpec is set to None.
		"Zero-downtime upgrade is disabled. Env var is set to true but RayServiceSpec is set to None.": {
			activeCluster:           activeCluster.DeepCopy(),
			updateRayClusterSpec:    true,
			enableZeroDowntime:      true,
			shouldPrepareNewCluster: false,
			rayServiceUpgradeType:   rayv1.None,
		},
		// Test 10: Zero downtime upgrade is enabled. Neither the env var nor the RayServiceSpec is set. Trigger the zero-downtime upgrade.
		"Zero-downtime upgrade is enabled. Neither the env var nor the RayServiceSpec is set.": {
			activeCluster:           nil,
			updateRayClusterSpec:    true,
			shouldPrepareNewCluster: true,
			rayServiceUpgradeType:   "",
		},
		// Test 11: Zero downtime upgrade is disabled. Both the env var and the RayServiceSpec is set to disable zero-downtime upgrade.
		"Zero-downtime upgrade is disabled by both env var and RayServiceSpec.": {
			activeCluster:           activeCluster.DeepCopy(),
			updateRayClusterSpec:    true,
			enableZeroDowntime:      false,
			shouldPrepareNewCluster: false,
			rayServiceUpgradeType:   rayv1.None,
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
				// Update 'ray.io/kuberay-version' to a new version if kubeRayVersion is set.
				if tc.updateKubeRayVersion {
					tc.activeCluster.Annotations[utils.KubeRayVersion] = tc.kubeRayVersion
				}
				runtimeObjects = append(runtimeObjects, tc.activeCluster.DeepCopy())
			}
			fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
			r := RayServiceReconciler{
				Client:   fakeClient,
				Scheme:   newScheme,
				Recorder: record.NewFakeRecorder(1),
			}
			service := rayService.DeepCopy()
			service.Spec.UpgradeStrategy = &rayv1.RayServiceUpgradeStrategy{}
			if tc.rayServiceUpgradeType != "" {
				service.Spec.UpgradeStrategy.Type = &tc.rayServiceUpgradeType
			}
			if tc.updateRayClusterSpec {
				service.Spec.RayClusterSpec.RayVersion = "new-version"
			}
			if tc.activeCluster != nil {
				service.Status.ActiveServiceStatus.RayClusterName = tc.activeCluster.Name
			}
			assert.Equal(t, "", service.Status.PendingServiceStatus.RayClusterName)
			activeRayCluster, _, err := r.reconcileRayCluster(ctx, service)
			assert.Nil(t, err)

			// If the KubeRay version has changed, check that the RayCluster annotations have been updated to the correct version.
			if tc.updateKubeRayVersion && activeRayCluster != nil {
				assert.Equal(t, utils.KUBERAY_VERSION, activeRayCluster.Annotations[utils.KubeRayVersion])
			}

			// If KubeRay operator is preparing a new cluster, the `PendingServiceStatus.RayClusterName` should be set by calling the function `markRestart`.
			if tc.shouldPrepareNewCluster {
				assert.NotEqual(t, "", service.Status.PendingServiceStatus.RayClusterName)
			} else {
				assert.Equal(t, "", service.Status.PendingServiceStatus.RayClusterName)
			}
		})
	}
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
	tests := map[string]struct {
		expectServeResult          string
		excludeHeadPodFromServeSvc bool
		isHealthy                  bool
	}{
		"Ray serve application is running, excludeHeadPodFromServeSvc is true": {
			"false",
			true,
			true,
		},
		"Ray serve application is running, excludeHeadPodFromServeSvc is false": {
			"true",
			false,
			true,
		},
		"Ray serve application is unhealthy, excludeHeadPodFromServeSvc is true": {
			"false",
			true,
			false,
		},
		"Ray serve application is unhealthy, excludeHeadPodFromServeSvc is false": {
			"false",
			false,
			false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
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

			err := r.updateHeadPodServeLabel(ctx, &cluster, tc.excludeHeadPodFromServeSvc)
			assert.NoError(t, err)
			// Get latest headPod status
			headPod, err = common.GetRayClusterHeadPod(ctx, r, &cluster)
			assert.Equal(t, headPod.Labels[utils.RayClusterServingServiceLabelKey], tc.expectServeResult)
			assert.NoError(t, err)
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
			initConditions(&tt.rayServiceInstance)
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
