package dashboardclient

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"k8s.io/apimachinery/pkg/types"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/dashboardclient/mocks"
	utiltypes "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/types"
)

func TestAsyncJobInfoQuery(t *testing.T) {
	var mockClient *mocks.MockRayDashboardClientInterface

	ctrl := gomock.NewController(t)
	mockClient = mocks.NewMockRayDashboardClientInterface(ctrl)

	synctest.Test(t, func(t *testing.T) {
		ctx := logr.NewContext(t.Context(), logr.Discard())

		clusterName := types.NamespacedName{
			Namespace: "test-namespace",
			Name:      "raycluster-async-job-info-query",
		}
		asyncJobInfoQueryClient := RayDashboardCacheClient{}
		asyncJobInfoQueryClient.InitClient(ctx, clusterName, mockClient)
		synctest.Wait()

		jobId := "test-job-id"

		// earlier set up the mock expectation for the second call to avoid flaky test.
		mockJobInfo := &utiltypes.RayJobInfo{
			JobId: jobId,
		}
		mockClient.EXPECT().GetJobInfo(ctx, jobId).Return(mockJobInfo, nil)

		// First call, the job info is not in cache, so it should return ErrAgain
		jobInfo, err := asyncJobInfoQueryClient.GetJobInfo(ctx, jobId)
		assert.Nil(t, jobInfo)
		assert.Equal(t, ErrAgain, err)

		synctest.Wait()

		// Second call, after GetJobInfo has called in background , the job info should be in cache now.
		jobInfo, err = asyncJobInfoQueryClient.GetJobInfo(ctx, jobId)
		require.NoError(t, err)
		assert.Equal(t, mockJobInfo, jobInfo)

		expectedError := errors.New("test error")
		mockClient.EXPECT().GetJobInfo(ctx, jobId).Return(nil, expectedError)

		// Wait for longer than queryInterval to ensure the task has been re-queued and executed.
		time.Sleep(queryInterval + time.Millisecond)
		synctest.Wait()

		// Third call, after GetJobInfo has called in background and returned error, the error should be in cache now.
		jobInfo, err = asyncJobInfoQueryClient.GetJobInfo(ctx, jobId)
		assert.Nil(t, jobInfo)
		assert.Equal(t, expectedError, err)

		// set up the mock expectation for the fourth call.
		mockJobInfo = &utiltypes.RayJobInfo{
			JobId:     jobId,
			JobStatus: rayv1.JobStatusSucceeded,
		}
		mockClient.EXPECT().GetJobInfo(ctx, jobId).Return(mockJobInfo, nil)

		// After error has been consumed previously, the job info is not in cache so it should return ErrAgain.
		jobInfo, err = asyncJobInfoQueryClient.GetJobInfo(ctx, jobId)
		assert.Nil(t, jobInfo)
		assert.Equal(t, ErrAgain, err)

		// Wait for longer than queryInterval to ensure the task has been re-queued and executed.
		time.Sleep(queryInterval + time.Millisecond)
		synctest.Wait()

		// Fourth call, the job has reached the terminal status.
		jobInfo, err = asyncJobInfoQueryClient.GetJobInfo(ctx, jobId)
		require.NoError(t, err)
		assert.Equal(t, mockJobInfo, jobInfo)

		// Wait for longer than queryInterval to ensure the task has been re-queued.
		time.Sleep(queryInterval + time.Millisecond)
		synctest.Wait()

		// Fifth call, since the job has reached the terminal status, the task has removed from the worker.
		// The GetJobInfo underneath should not be called again, and the cached job info should be returned.
		jobInfo, err = asyncJobInfoQueryClient.GetJobInfo(ctx, jobId)
		require.NoError(t, err)
		assert.Equal(t, mockJobInfo, jobInfo)

		// Wait for longer than cacheExpiry to ensure the cache has been expired and removed.
		time.Sleep(cacheExpiry + 10*queryInterval)
		synctest.Wait()

		cached, ok := cacheStorage.Get(cacheKey(clusterName, jobId))
		assert.Nil(t, cached)
		assert.False(t, ok)

		// Test with getting a persistent error, the cache should be removed eventually.
		nonExistedJobId := "not-existed-job-id"

		// earlier set up the mock expectation for the second call to avoid flaky test.
		expectedError = errors.New("no such host")
		mockClient.EXPECT().GetJobInfo(ctx, nonExistedJobId).Return(nil, expectedError).AnyTimes()

		jobInfo, err = asyncJobInfoQueryClient.GetJobInfo(ctx, nonExistedJobId)
		assert.Nil(t, jobInfo)
		assert.Equal(t, ErrAgain, err)

		time.Sleep(queryInterval + time.Millisecond)
		synctest.Wait()

		jobInfo, err = asyncJobInfoQueryClient.GetJobInfo(ctx, nonExistedJobId)
		assert.Nil(t, jobInfo)
		assert.Equal(t, expectedError, err)

		// After error has been consumed previously, the job info is not in cache so it should return ErrAgain.
		jobInfo, err = asyncJobInfoQueryClient.GetJobInfo(ctx, nonExistedJobId)
		assert.Nil(t, jobInfo)
		assert.Equal(t, ErrAgain, err)

		time.Sleep(queryInterval + time.Millisecond)
		synctest.Wait()

		// Get the same error again without continuing to requeue the task.
		jobInfo, err = asyncJobInfoQueryClient.GetJobInfo(ctx, nonExistedJobId)
		assert.Nil(t, jobInfo)
		assert.Equal(t, expectedError, err)

		// The cache should be removed after previous GetJobInfo.
		cached, ok = cacheStorage.Get(cacheKey(clusterName, nonExistedJobId))
		assert.Nil(t, cached)
		assert.False(t, ok)
	})
}
