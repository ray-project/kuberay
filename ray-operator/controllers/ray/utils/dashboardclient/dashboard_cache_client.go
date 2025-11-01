package dashboardclient

import (
	"context"
	"errors"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	utiltypes "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/types"
)

var ErrAgain = errors.New("EAGAIN")

const (
	// TODO: make queue size and worker size configurable.
	taskQueueSize = 128
	workerSize    = 8

	queryInterval = 3 * time.Second

	// TODO: consider a proper size for accommodating the all live job info
	cacheSize   = 10000
	cacheExpiry = 10 * time.Minute
)

var (
	// singleton
	initWorkPool sync.Once
	pool         workerPool

	// singleton
	initCacheStorage sync.Once
	cacheStorage     *lru.Cache[string, *JobInfoCache]
)

type (
	Task         func() bool
	JobInfoCache struct {
		JobInfo  *utiltypes.RayJobInfo
		Err      error
		UpdateAt *time.Time
	}

	workerPool struct {
		taskQueue chan Task
	}
)

func (w *workerPool) init(taskQueueSize int, workerSize int, queryInterval time.Duration) {
	w.taskQueue = make(chan Task, taskQueueSize)

	for i := 0; i < workerSize; i++ {
		// TODO: observability for these goroutine
		// TODO: should we consider the stop ?
		go func() {
			for task := range w.taskQueue {
				again := task()

				if again {
					time.AfterFunc(queryInterval, func() {
						w.taskQueue <- task
					})
				}
			}
		}()
	}
}

func (w *workerPool) PutTask(task Task) {
	w.taskQueue <- task
}

var _ RayDashboardClientInterface = (*RayDashboardCacheClient)(nil)

type RayDashboardCacheClient struct {
	client RayDashboardClientInterface
}

func (r *RayDashboardCacheClient) InitClient(client RayDashboardClientInterface) {
	initWorkPool.Do(func() {
		pool.init(taskQueueSize, workerSize, queryInterval)
	})

	initCacheStorage.Do(func() {
		if cacheStorage == nil {
			// the New() returns error only if the size is less or equal than zero.
			cacheStorage, _ = lru.New[string, *JobInfoCache](cacheSize)
		}

		// expiry cache cleanup
		go func() {
			ticker := time.NewTicker(queryInterval * 10)
			defer ticker.Stop()

			// TODO: observability ?
			// TODO: should we consider the stop ?
			for range ticker.C {
				keys := cacheStorage.Keys()
				expiredThreshold := time.Now().Add(-cacheExpiry)
				for _, key := range keys {
					if cached, ok := cacheStorage.Peek(key); ok {
						if cached.UpdateAt.Before(expiredThreshold) {
							cacheStorage.Remove(key)
						}
					}
				}
			}
		}()
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
	currentTime := time.Now()
	placeholder := &JobInfoCache{Err: ErrAgain, UpdateAt: &currentTime}

	// Put a placeholder in storage. The cache will be updated only if the placeholder exists.
	// The placeholder will be removed when StopJob or DeleteJob.
	if cached, existed, _ := cacheStorage.PeekOrAdd(jobId, placeholder); existed {
		return cached.JobInfo, cached.Err
	}

	task := func() bool {
		jobInfoCache, existed := cacheStorage.Get(jobId)
		if !existed {
			return false
		}

		jobInfoCache.JobInfo, jobInfoCache.Err = r.client.GetJobInfo(ctx, jobId)
		currentTime := time.Now()
		jobInfoCache.UpdateAt = &currentTime

		if _, existed := cacheStorage.ContainsOrAdd(jobId, jobInfoCache); !existed {
			return false
		}

		return !rayv1.IsJobTerminal(jobInfoCache.JobInfo.JobStatus)
	}

	pool.PutTask(task)

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
	cacheStorage.Remove(jobName)
	return r.client.StopJob(ctx, jobName)
}

func (r *RayDashboardCacheClient) DeleteJob(ctx context.Context, jobName string) error {
	cacheStorage.Remove(jobName)
	return r.client.DeleteJob(ctx, jobName)
}
