package e2e

import (
	"testing"
	"time"

	api "github.com/ray-project/kuberay/proto/go_client"

	"github.com/stretchr/testify/require"

	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

// TestCreateClusterAutoscalerEndpoint sequentially iterates over the create cluster endpoint
// with valid and invalid requests
func TestCreateClusterAutoscaler(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	// create config map and register a cleanup hook upon success
	configMapName := tCtx.CreateConfigMap(t, map[string]string{
		"detached_actor.py":           ReadFileAsString(t, "resources/create_detached_actor.py"),
		"terminate_detached_actor.py": ReadFileAsString(t, "resources/terminate_detached_actor.py"),
	})
	t.Cleanup(func() {
		tCtx.DeleteConfigMap(t, configMapName)
	})

	volumes := []*api.Volume{
		{
			MountPath:  "/home/ray/samples",
			VolumeType: api.Volume_CONFIGMAP,
			Name:       "detached-actor",
			Source:     tCtx.GetConfigMapName(),
			Items: map[string]string{
				"detached_actor.py":           "detached_actor.py",
				"terminate_detached_actor.py": "terminate_detached_actor.py",
			},
		},
	}

	clusterReq := api.CreateClusterRequest{
		Namespace: tCtx.GetNamespaceName(),
		Cluster: &api.Cluster{
			Name:        tCtx.GetNextName(),
			Namespace:   tCtx.GetNamespaceName(),
			User:        "boris",
			Version:     tCtx.GetRayVersion(),
			Environment: api.Cluster_DEV,
			ClusterSpec: &api.ClusterSpec{
				EnableInTreeAutoscaling: true,
				AutoscalerOptions: &api.AutoscalerOptions{
					UpscalingMode:      "Default",
					IdleTimeoutSeconds: 30,
					Cpu:                "500m",
					Memory:             "512Mi",
				},
				HeadGroupSpec: &api.HeadGroupSpec{
					ComputeTemplate: tCtx.GetComputeTemplateName(),
					Image:           tCtx.GetRayImage(),
					ServiceType:     "NodePort",
					RayStartParams: map[string]string{
						"dashboard-host":      "0.0.0.0",
						"metrics-export-port": "8080",
						"num-cpus":            "0",
					},
					Volumes: volumes,
				},
				WorkerGroupSpec: []*api.WorkerGroupSpec{
					{
						GroupName:       "small-wg",
						ComputeTemplate: tCtx.GetComputeTemplateName(),
						Image:           tCtx.GetRayImage(),
						Replicas:        0,
						MinReplicas:     0,
						MaxReplicas:     5,
						RayStartParams: map[string]string{
							"node-ip-address": "$MY_POD_IP",
						},
						Volumes: volumes,
					},
				},
			},
		},
	}

	// Create cluster
	actualCluster, actualRpcStatus, err := tCtx.GetRayApiServerClient().CreateCluster(&clusterReq)
	require.NoError(t, err, "No error expected")
	require.Nil(t, actualRpcStatus, "No RPC status expected")
	require.NotNil(t, actualCluster, "A cluster is expected")
	waitForRunningCluster(t, tCtx, actualCluster.Name)

	// Get number of workers
	rayCluster, err := tCtx.GetRayClusterByName(actualCluster.Name)
	require.NoError(t, err, "No error expected")
	require.Equal(t, int32(0), rayCluster.Status.AvailableWorkerReplicas)

	// Create actor
	createActorRequest := api.CreateRayJobRequest{
		Namespace: tCtx.GetNamespaceName(),
		Job: &api.RayJob{
			Name:       tCtx.GetNextName(),
			Namespace:  tCtx.GetNamespaceName(),
			User:       "natacha",
			Entrypoint: "python /home/ray/samples/detached_actor.py",
			ClusterSelector: map[string]string{
				"ray.io/cluster": actualCluster.Name,
			},
		},
	}

	actualJob, actualRpcStatus, err := tCtx.GetRayApiServerClient().CreateRayJob(&createActorRequest)
	require.NoError(t, err, "No error expected")
	require.Nil(t, actualRpcStatus, "No RPC status expected")
	require.NotNil(t, actualJob, "A job is expected")
	waitForRayJob(t, tCtx, createActorRequest.Job.Name, rayv1api.JobStatusSucceeded)

	// worker pod should be created as part of job execution
	time.Sleep(20 * time.Second)

	// Get number of workers
	rayCluster, err = tCtx.GetRayClusterByName(actualCluster.Name)
	require.NoError(t, err, "No error expected")
	require.Equal(t, int32(1), rayCluster.Status.AvailableWorkerReplicas)

	// Delete actor
	deleteActorRequest := api.CreateRayJobRequest{
		Namespace: tCtx.GetNamespaceName(),
		Job: &api.RayJob{
			Name:       tCtx.GetNextName(),
			Namespace:  tCtx.GetNamespaceName(),
			User:       "natacha",
			Entrypoint: "python /home/ray/samples/terminate_detached_actor.py",
			ClusterSelector: map[string]string{
				"ray.io/cluster": actualCluster.Name,
			},
		},
	}
	actualJob, actualRpcStatus, err = tCtx.GetRayApiServerClient().CreateRayJob(&deleteActorRequest)
	require.NoError(t, err, "No error expected")
	require.Nil(t, actualRpcStatus, "No RPC status expected")
	require.NotNil(t, actualJob, "A job is expected")
	waitForRayJob(t, tCtx, createActorRequest.Job.Name, rayv1api.JobStatusSucceeded)

	// Sleep for a while to ensure that the worker pod is deleted
	time.Sleep(100 * time.Second)

	// Get number of workers
	rayCluster, err = tCtx.GetRayClusterByName(actualCluster.Name)
	require.NoError(t, err, "No error expected")
	require.Equal(t, int32(0), rayCluster.Status.AvailableWorkerReplicas)
}
