package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/wait"

	kuberayHTTP "github.com/ray-project/kuberay/apiserver/pkg/http"
	api "github.com/ray-project/kuberay/proto/go_client"

	rayv1api "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
)

func TestCreateJobWithDisposableClusters(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	// create config map and register a cleanup hook upon success
	configMapName := tCtx.CreateConfigMap(t, map[string]string{
		"counter_sample.py": ReadFileAsString(t, "resources/counter_sample.py"),
		"fail_fast.py":      ReadFileAsString(t, "resources/fail_fast_sample.py"),
	})
	t.Cleanup(func() {
		tCtx.DeleteConfigMap(t, configMapName)
	})

	items := map[string]string{
		"counter_sample.py": "counter_sample.py",
		"fail_fast.py":      "fail_fast_sample.py",
	}

	startParams := map[string]string{
		"dashboard-host":      "0.0.0.0",
		"metrics-export-port": "8080",
	}
	volume := &api.Volume{
		MountPath:  "/home/ray/samples",
		VolumeType: api.Volume_CONFIGMAP,
		Name:       "code-sample",
		Source:     tCtx.GetConfigMapName(),
		Items:      items,
	}

	clusterSpec := &api.ClusterSpec{
		HeadGroupSpec: &api.HeadGroupSpec{
			ComputeTemplate: tCtx.GetComputeTemplateName(),
			Image:           tCtx.GetRayImage(),
			ServiceType:     "NodePort",
			EnableIngress:   false,
			RayStartParams:  startParams,
			Volumes:         []*api.Volume{volume},
		},
		WorkerGroupSpec: []*api.WorkerGroupSpec{
			{
				GroupName:       "small-wg",
				ComputeTemplate: tCtx.GetComputeTemplateName(),
				Image:           tCtx.GetRayImage(),
				Replicas:        1,
				MinReplicas:     1,
				MaxReplicas:     5,
				RayStartParams:  startParams,
				Volumes:         []*api.Volume{volume},
			},
		},
	}

	tests := []struct {
		Name              string
		Input             *api.CreateRayJobRequest
		ExpectedError     error
		ExpectedJobStatus rayv1api.JobStatus
	}{
		{
			Name: "Create a running sample job",
			Input: &api.CreateRayJobRequest{
				Job: &api.RayJob{
					Name:                     tCtx.GetNextName(),
					Namespace:                tCtx.GetNamespaceName(),
					User:                     "natacha",
					Version:                  tCtx.GetRayVersion(),
					Entrypoint:               "python /home/ray/samples/counter_sample.py",
					RuntimeEnv:               "pip:\n  - requests==2.26.0\n  - pendulum==2.1.2\nenv_vars:\n  counter_name: test_counter\n",
					ShutdownAfterJobFinishes: true,
					ClusterSpec:              clusterSpec,
					TtlSecondsAfterFinished:  60,
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError:     nil,
			ExpectedJobStatus: rayv1api.JobStatusSucceeded,
		},
		{
			Name: "Create a failing sample job",
			Input: &api.CreateRayJobRequest{
				Job: &api.RayJob{
					Name:                     tCtx.GetNextName(),
					Namespace:                tCtx.GetNamespaceName(),
					User:                     "natacha",
					Version:                  tCtx.GetRayVersion(),
					Entrypoint:               "python /home/ray/samples/fail_fast.py",
					ShutdownAfterJobFinishes: true,
					ClusterSpec:              clusterSpec,
					TtlSecondsAfterFinished:  60,
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError:     nil,
			ExpectedJobStatus: rayv1api.JobStatusFailed,
		},
		{
			Name: "Create a job request without providing a namespace",
			Input: &api.CreateRayJobRequest{
				Job:       nil,
				Namespace: "",
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusNotFound,
			},
		},
		{
			Name: "Create a job request with nil job spec",
			Input: &api.CreateRayJobRequest{
				Job:       nil,
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create a job request with no namespace in the job spec",
			Input: &api.CreateRayJobRequest{
				Job: &api.RayJob{
					Name: tCtx.GetNextName(),
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create a job request with no name",
			Input: &api.CreateRayJobRequest{
				Job: &api.RayJob{
					Namespace: tCtx.GetNamespaceName(),
					Name:      "",
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create a job request with no user name",
			Input: &api.CreateRayJobRequest{
				Job: &api.RayJob{
					Namespace: tCtx.GetNamespaceName(),
					Name:      tCtx.GetNextName(),
					User:      "",
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create a job request with entrypoint",
			Input: &api.CreateRayJobRequest{
				Job: &api.RayJob{
					Namespace: tCtx.GetNamespaceName(),
					Name:      tCtx.GetNextName(),
					User:      "bullwinkle",
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Create a job request with nil cluster spec",
			Input: &api.CreateRayJobRequest{
				Job: &api.RayJob{
					Namespace:                tCtx.GetNamespaceName(),
					Name:                     tCtx.GetNextName(),
					User:                     "bullwinkle",
					Version:                  tCtx.GetRayVersion(),
					Entrypoint:               "python /home/ray/samples/counter_sample.py",
					RuntimeEnv:               "pip:\n  - requests==2.26.0\n  - pendulum==2.1.2\nenv_vars:\n  counter_name: test_counter\n",
					ShutdownAfterJobFinishes: true,
					ClusterSpec:              nil,
					TtlSecondsAfterFinished:  60,
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
	}
	// Execute tests sequentially
	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.Name, func(t *testing.T) {
			actualJob, actualRpcStatus, err := tCtx.GetRayApiServerClient().CreateRayJob(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRpcStatus, "No RPC status expected")
				require.NotNil(t, actualJob, "A job is expected")
				waitForRayJob(t, tCtx, tc.Input.Job.Name, tc.ExpectedJobStatus)
				tCtx.DeleteRayJobByName(t, actualJob.Name)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRpcStatus, "A not nill RPC status is required")
			}
		})
	}
}

func TestDeleteJob(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	testJobRequest := createTestJob(t, tCtx)

	tests := []GenericEnd2EndTest[*api.DeleteRayJobRequest]{
		{
			Name: "Delete an existing job",
			Input: &api.DeleteRayJobRequest{
				Name:      testJobRequest.Job.Name,
				Namespace: testJobRequest.Namespace,
			},
			ExpectedError: nil,
		},
		{
			Name: "Delete a non existing job",
			Input: &api.DeleteRayJobRequest{
				Name:      "a-bogus-job-name",
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusNotFound,
			},
		},
		{
			Name: "Delete a job without providing a namespace",
			Input: &api.DeleteRayJobRequest{
				Name:      testJobRequest.Job.Name,
				Namespace: "",
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusNotFound,
			},
		},
	}
	// Execute tests sequentially
	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.Name, func(t *testing.T) {
			actualRpcStatus, err := tCtx.GetRayApiServerClient().DeleteRayJob(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRpcStatus, "No RPC status expected")
				waitForDeletedRayJob(t, tCtx, testJobRequest.Job.Name)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRpcStatus, "A not nill RPC status is required")
			}
		})
	}
}

func TestGetAllJobs(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	testJobRequest := createTestJob(t, tCtx)
	t.Cleanup(func() {
		tCtx.DeleteRayJobByName(t, testJobRequest.Job.Name)
	})

	response, actualRpcStatus, err := tCtx.GetRayApiServerClient().ListAllRayJobs()
	require.NoError(t, err, "No error expected")
	require.Nil(t, actualRpcStatus, "No RPC status expected")
	require.NotNil(t, response, "A response is expected")
	require.NotEmpty(t, response.Jobs, "A list of jobs is required")
	foundName := false
	for _, job := range response.Jobs {
		if testJobRequest.Job.Name == job.Name && tCtx.GetNamespaceName() == job.Namespace {
			foundName = true
			break
		}
	}
	require.Equal(t, foundName, true)
}

func TestGetJobsInNamespace(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	testJobRequest := createTestJob(t, tCtx)
	t.Cleanup(func() {
		tCtx.DeleteRayJobByName(t, testJobRequest.Job.Name)
	})

	response, actualRpcStatus, err := tCtx.GetRayApiServerClient().ListRayJobs(&api.ListRayJobsRequest{
		Namespace: tCtx.GetNamespaceName(),
	})
	require.NoError(t, err, "No error expected")
	require.Nil(t, actualRpcStatus, "No RPC status expected")
	require.NotNil(t, response, "A response is expected")
	require.NotEmpty(t, response.Jobs, "A list of jobs is required")
	foundName := false
	for _, job := range response.Jobs {
		if testJobRequest.Job.Name == job.Name && tCtx.GetNamespaceName() == job.Namespace {
			foundName = true
			break
		}
	}
	require.Equal(t, foundName, true)
}

func TestGetJob(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})
	testJobRequest := createTestJob(t, tCtx)
	t.Cleanup(func() {
		tCtx.DeleteRayJobByName(t, testJobRequest.Job.Name)
	})
	tests := []GenericEnd2EndTest[*api.GetRayJobRequest]{
		{
			Name: "Get job by name in a namespace",
			Input: &api.GetRayJobRequest{
				Name:      testJobRequest.Job.Name,
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: nil,
		},
		{
			Name: "Get non existing job",
			Input: &api.GetRayJobRequest{
				Name:      "a-bogus-cluster-name",
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusNotFound,
			},
		},
		{
			Name: "Get a job with no Name",
			Input: &api.GetRayJobRequest{
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusBadRequest,
			},
		},
		{
			Name: "Get a job with no namespace",
			Input: &api.GetRayJobRequest{
				Name: "some-Name",
			},
			ExpectedError: &kuberayHTTP.KuberayAPIServerClientError{
				HTTPStatusCode: http.StatusNotFound,
			},
		},
	}
	// Execute tests sequentially
	for _, tc := range tests {
		tc := tc // capture range variable
		t.Run(tc.Name, func(t *testing.T) {
			actualJob, actualRpcStatus, err := tCtx.GetRayApiServerClient().GetRayJob(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRpcStatus, "No RPC status expected")
				require.Equal(t, tc.Input.Name, actualJob.Name)
				require.Equal(t, tCtx.GetNamespaceName(), actualJob.Namespace)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRpcStatus, "A not nill RPC status is required")
			}
		})
	}
}

func TestCreateJobWithClusterSelector(t *testing.T) {
	tCtx, err := NewEnd2EndTestingContext(t)
	require.NoError(t, err, "No error expected when creating testing context")

	tCtx.CreateComputeTemplate(t)
	t.Cleanup(func() {
		tCtx.DeleteComputeTemplate(t)
	})

	cluster, configMapName := tCtx.CreateRayClusterWithConfigMaps(t,
		map[string]string{
			"counter_sample.py": ReadFileAsString(t, "resources/counter_sample.py"),
			"fail_fast.py":      ReadFileAsString(t, "resources/fail_fast_sample.py"),
		})
	t.Cleanup(func() {
		tCtx.DeleteRayCluster(t, cluster.Name)
		tCtx.DeleteConfigMap(t, configMapName)
	})

	tests := []struct {
		Name              string
		Input             *api.CreateRayJobRequest
		ExpectedError     error
		ExpectedJobStatus rayv1api.JobStatus
	}{
		{
			Name: "Submit a correct job on an already running cluster",
			Input: &api.CreateRayJobRequest{
				Job: &api.RayJob{
					Name:                    tCtx.GetNextName(),
					Namespace:               tCtx.GetNamespaceName(),
					User:                    "r2d2",
					Version:                 tCtx.GetRayVersion(),
					Entrypoint:              "python /home/ray/samples/counter_sample.py",
					Metadata:                map[string]string{},
					RuntimeEnv:              "pip:\n  - requests==2.26.0\n  - pendulum==2.1.2\nenv_vars:\n  counter_name: test_counter\n",
					ClusterSelector:         map[string]string{"ray.io/cluster": cluster.Name},
					TtlSecondsAfterFinished: 60,
					JobSubmitter: &api.RayJobSubmitter{
						Image: cluster.ClusterSpec.HeadGroupSpec.Image,
					},
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedJobStatus: rayv1api.JobStatusSucceeded,
			ExpectedError:     nil,
		},
		{
			Name: "Submit a failing job on an already running cluster",
			Input: &api.CreateRayJobRequest{
				Job: &api.RayJob{
					Name:                     tCtx.GetNextName(),
					Namespace:                tCtx.GetNamespaceName(),
					User:                     "r2d2",
					Version:                  tCtx.GetRayVersion(),
					Entrypoint:               "python /home/ray/samples/fail_fast.py",
					RuntimeEnv:               "pip:\n  - requests==2.26.0\n  - pendulum==2.1.2\nenv_vars:\n  counter_name: test_counter\n",
					ShutdownAfterJobFinishes: true,
					TtlSecondsAfterFinished:  60,
					ClusterSelector:          map[string]string{"ray.io/cluster": cluster.Name},
					JobSubmitter: &api.RayJobSubmitter{
						Image: cluster.ClusterSpec.HeadGroupSpec.Image,
					},
				},
				Namespace: tCtx.GetNamespaceName(),
			},
			ExpectedJobStatus: rayv1api.JobStatusFailed,
			ExpectedError:     nil,
		},
	}
	// Execute tests sequentially
	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			actualJob, actualRpcStatus, err := tCtx.GetRayApiServerClient().CreateRayJob(tc.Input)
			if tc.ExpectedError == nil {
				require.NoError(t, err, "No error expected")
				require.Nil(t, actualRpcStatus, "No RPC status expected")
				require.NotNil(t, actualJob, "A job is expected")
				waitForRayJob(t, tCtx, tc.Input.Job.Name, tc.ExpectedJobStatus)
				tCtx.DeleteRayJobByName(t, actualJob.Name)
			} else {
				require.EqualError(t, err, tc.ExpectedError.Error(), "Matching error expected")
				require.NotNil(t, actualRpcStatus, "A not nill RPC status is required")
			}
		})
	}
}

func createTestJob(t *testing.T, tCtx *End2EndTestingContext) *api.CreateRayJobRequest {
	// create config map and register a cleanup hook upon success
	configMapName := tCtx.CreateConfigMap(t, map[string]string{
		"counter_sample.py": ReadFileAsString(t, "resources/counter_sample.py"),
		"fail_fast.py":      ReadFileAsString(t, "resources/fail_fast_sample.py"),
	})
	t.Cleanup(func() {
		tCtx.DeleteConfigMap(t, configMapName)
	})

	items := map[string]string{
		"counter_sample.py": "counter_sample.py",
		"fail_fast.py":      "fail_fast_sample.py",
	}

	startParams := map[string]string{
		"dashboard-host":      "0.0.0.0",
		"metrics-export-port": "8080",
	}
	volume := &api.Volume{
		MountPath:  "/home/ray/samples",
		VolumeType: api.Volume_CONFIGMAP,
		Name:       "code-sample",
		Source:     tCtx.GetConfigMapName(),
		Items:      items,
	}

	testJobRequest := &api.CreateRayJobRequest{
		Job: &api.RayJob{
			Name:                     tCtx.GetNextName(),
			Namespace:                tCtx.GetNamespaceName(),
			User:                     "natacha",
			Version:                  tCtx.GetRayVersion(),
			Entrypoint:               "python /home/ray/samples/counter_sample.py",
			RuntimeEnv:               "pip:\n  - requests==2.26.0\n  - pendulum==2.1.2\nenv_vars:\n  counter_name: test_counter\n",
			ShutdownAfterJobFinishes: true,
			ClusterSpec: &api.ClusterSpec{
				HeadGroupSpec: &api.HeadGroupSpec{
					ComputeTemplate: tCtx.GetComputeTemplateName(),
					Image:           tCtx.GetRayImage(),
					ServiceType:     "NodePort",
					EnableIngress:   false,
					RayStartParams:  startParams,
					Volumes:         []*api.Volume{volume},
				},
				WorkerGroupSpec: []*api.WorkerGroupSpec{
					{
						GroupName:       "small-wg",
						ComputeTemplate: tCtx.GetComputeTemplateName(),
						Image:           tCtx.GetRayImage(),
						Replicas:        1,
						MinReplicas:     1,
						MaxReplicas:     5,
						RayStartParams:  startParams,
						Volumes:         []*api.Volume{volume},
					},
				},
			},
			TtlSecondsAfterFinished: 60,
		},
		Namespace: tCtx.GetNamespaceName(),
	}

	actualJob, actualRpcStatus, err := tCtx.GetRayApiServerClient().CreateRayJob(testJobRequest)
	require.NoError(t, err, "No error expected")
	require.Nil(t, actualRpcStatus, "No RPC status expected")
	require.NotNil(t, actualJob, "A job is expected")
	waitForRayJob(t, tCtx, testJobRequest.Job.Name, rayv1api.JobStatusSucceeded)
	return testJobRequest
}

func waitForRayJob(t *testing.T, tCtx *End2EndTestingContext, rayJobName string, rayJobStatus rayv1api.JobStatus) {
	// wait for the job to be in a JobStatusSucceeded state for 3 minutes
	// if is not in that state, return an error
	err := wait.PollUntilContextTimeout(tCtx.ctx, 500*time.Millisecond, 3*time.Minute, false, func(_ context.Context) (done bool, err error) {
		rayJob, err00 := tCtx.GetRayJobByName(rayJobName)
		if err00 != nil {
			return true, err00
		}
		t.Logf("Found ray job with state '%s' for ray job '%s'", rayJob.Status.JobStatus, rayJobName)
		return rayJob.Status.JobStatus == rayJobStatus, nil
	})
	require.NoErrorf(t, err, "No error expected when getting status for ray job: '%s', err %v", rayJobName, err)
}

func waitForDeletedRayJob(t *testing.T, tCtx *End2EndTestingContext, jobName string) {
	// wait for the job to be deleted
	// if is not in that state, return an error
	err := wait.PollUntilContextTimeout(tCtx.ctx, 500*time.Millisecond, 3*time.Minute, false, func(_ context.Context) (done bool, err error) {
		rayJob, err00 := tCtx.GetRayJobByName(jobName)
		if err00 != nil &&
			assert.EqualError(t, err00, "rayjobs.ray.io \""+jobName+"\" not found") {
			return true, nil
		}
		t.Logf("Found status of '%s' for ray cluster '%s'", rayJob.Status.JobStatus, jobName)
		return false, err00
	})
	require.NoErrorf(t, err, "No error expected when deleting ray job: '%s', err %v", jobName, err)
}
