package dashboardclient

import (
	"context"
	"errors"
	"sync"
	"time"

	cmap "github.com/orcaman/concurrent-map/v2"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	utiltypes "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/types"
)

var ErrAgain = errors.New("EAGAIN")

var (
	initWorkPool sync.Once
	taskQueue    chan Task

	// TODO: make queue size and worker size configurable.
	taskQueueSize = 128
	workerSize    = 8

	cacheStorage *cmap.ConcurrentMap[string, *JobInfoCache]

	queryInterval = 3 * time.Second // TODO: make it configurable
)

type (
	Task         func() bool
	JobInfoCache struct {
		JobInfo  *utiltypes.RayJobInfo
		Err      error
		UpdateAt *time.Time
	}
)

var _ RayDashboardClientInterface = (*RayDashboardCacheClient)(nil)

type RayDashboardCacheClient struct {
	client RayDashboardClientInterface
}

func (r *RayDashboardCacheClient) InitClient(client RayDashboardClientInterface) {
	initWorkPool.Do(func() {
		if taskQueue == nil {
			taskQueue = make(chan Task, taskQueueSize)

			// TODO: should we have observability for these goroutine?
			for i := 0; i < workerSize; i++ {
				// TODO: should we consider the stop ?
				go func() {
					for task := range taskQueue {
						again := task()

						if again {
							time.AfterFunc(queryInterval, func() {
								taskQueue <- task
							})
						}
					}
				}()
			}
		}

		if cacheStorage == nil {
			tmp := cmap.New[*JobInfoCache]()
			cacheStorage = &tmp
		}
	})

	r.client = client
}

func (r *RayDashboardCacheClient) UpdateDeployments(ctx context.Context, configJson []byte) error {
	return r.client.UpdateDeployments(ctx, configJson)
}

func (r *RayDashboardCacheClient) GetServeDetails(ctx context.Context) (*utiltypes.ServeDetails, error) {
	return r.client.GetServeDetails(ctx)
}

func (r *RayDashboardCacheClient) GetMultiApplicationStatus(ctx context.Context) (map[string]*utiltypes.ServeApplicationStatus, error) {
	return r.client.GetMultiApplicationStatus(ctx)
}

func (r *RayDashboardCacheClient) GetJobInfo(ctx context.Context, jobId string) (*utiltypes.RayJobInfo, error) {
	if cached, ok := cacheStorage.Get(jobId); ok {
		return cached.JobInfo, cached.Err
	}
	cached := &JobInfoCache{Err: ErrAgain}
	cacheStorage.Set(jobId, cached)

	// send to worker pool
	task := func() bool {
		jobInfoCache, _ := cacheStorage.Get(jobId)
		// TODO: should we handle cache not exist here, which it shouldn't happen

		jobInfoCache.JobInfo, jobInfoCache.Err = r.client.GetJobInfo(ctx, jobId)
		currentTime := time.Now()
		jobInfoCache.UpdateAt = &currentTime

		cacheStorage.Set(jobId, jobInfoCache)
		// handle not found(ex: rayjob has deleted)

		return !rayv1.IsJobTerminal(jobInfoCache.JobInfo.JobStatus)
	}

	taskQueue <- task

	return nil, ErrAgain
}

func (r *RayDashboardCacheClient) ListJobs(ctx context.Context) (*[]utiltypes.RayJobInfo, error) {
	return r.client.ListJobs(ctx)
}

func (r *RayDashboardCacheClient) SubmitJob(ctx context.Context, rayJob *rayv1.RayJob) (string, error) {
	return r.client.SubmitJob(ctx, rayJob)
}

func (r *RayDashboardCacheClient) SubmitJobReq(ctx context.Context, request *utiltypes.RayJobRequest) (string, error) {
	return r.client.SubmitJobReq(ctx, request)
}

func (r *RayDashboardCacheClient) GetJobLog(ctx context.Context, jobName string) (*string, error) {
	return r.client.GetJobLog(ctx, jobName)
}

func (r *RayDashboardCacheClient) StopJob(ctx context.Context, jobName string) error {
	return r.client.StopJob(ctx, jobName)
}

func (r *RayDashboardCacheClient) DeleteJob(ctx context.Context, jobName string) error {
	return r.client.DeleteJob(ctx, jobName)
}
