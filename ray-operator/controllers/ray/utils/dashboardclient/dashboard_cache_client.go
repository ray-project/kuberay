package dashboardclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	utiltypes "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/types"
)

var (
	ErrAgain         = errors.New("EAGAIN")
	ErrTaskQueueFull = errors.New("task queue is full")
)

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
	cacheLock        sync.RWMutex
)

type (
	Task         func(taskCTX context.Context) bool
	JobInfoCache struct {
		JobInfo  *utiltypes.RayJobInfo
		Err      error
		UpdateAt *time.Time
	}

	workerPool struct {
		taskQueue chan Task
	}
)

func (w *workerPool) init(ctx context.Context, taskQueueSize int, workerSize int, queryInterval time.Duration) {
	logger := ctrl.LoggerFrom(ctx).WithName("RayDashboardCacheClient").WithName("WorkerPool")
	w.taskQueue = make(chan Task, taskQueueSize)

	for i := 0; i < workerSize; i++ {
		go func(workerID int) {
			for {
				select {
				case <-ctx.Done():
					logger.Info("worker exiting...", "workerID", workerID)
					return
				case task := <-w.taskQueue:
					again := task(ctx)

					if again && ctx.Err() == nil {
						time.AfterFunc(queryInterval, func() {
							w.taskQueue <- task
						})
					}
				}
			}
		}(i)
	}
	logger.Info(fmt.Sprintf("Initialize a worker pool with %d goroutine and queryInterval is %v.", workerSize, queryInterval))
}

func (w *workerPool) PutTask(task Task) error {
	select {
	case w.taskQueue <- task:
		return nil
	default:
		return ErrTaskQueueFull
	}
}

var _ RayDashboardClientInterface = (*RayDashboardCacheClient)(nil)

type RayDashboardCacheClient struct {
	client RayDashboardClientInterface
}

func (r *RayDashboardCacheClient) InitClient(ctx context.Context, client RayDashboardClientInterface) {
	logger := ctrl.LoggerFrom(ctx).WithName("RayDashboardCacheClient")

	initWorkPool.Do(func() {
		pool.init(ctx, taskQueueSize, workerSize, queryInterval)
	})

	initCacheStorage.Do(func() {
		// The NewWithEvict() returns error only if the cacheSize is less than or equal to zero.
		// While we set cacheSize as constant, we can ignore the error here.
		cacheStorage, _ = lru.NewWithEvict[string, *JobInfoCache](cacheSize, func(key string, _ *JobInfoCache) {
			logger.WithName("cacheStorage").Info("Evict cache for key.", "key", key)
		})

		// expiry cache cleanup
		go func() {
			ticker := time.NewTicker(queryInterval * 10)
			defer ticker.Stop()

			loggerForGC := logger.WithName("CacheCleanup")
			loggerForGC.Info(fmt.Sprintf("Initialize a cache cleanup goroutine with interval %v.", queryInterval*10))

			for {
				select {
				case <-ctx.Done():
					loggerForGC.Info("clean up goroutine exiting...")
					return
				case t := <-ticker.C:
					cacheLock.RLock()
					keys := cacheStorage.Keys()
					cacheLock.RUnlock()

					expiredThreshold := time.Now().Add(-cacheExpiry)
					loggerForGC.Info(fmt.Sprintf("Found %d keys to verify,", len(keys)), "expiredThreshold", expiredThreshold, "tick at", t)

					removed := keys[:0]
					for _, key := range keys {
						cacheLock.Lock()
						if cached, ok := cacheStorage.Peek(key); ok {
							if cached.UpdateAt.Before(expiredThreshold) {
								cacheStorage.Remove(key)
								removed = append(removed, key)
							}
						}
						cacheLock.Unlock()
					}
					loggerForGC.Info(fmt.Sprintf("clean up %d cache.", len(removed)), "expiredThreshold", expiredThreshold, "removed keys", removed)
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
	logger := ctrl.LoggerFrom(ctx).WithName("RayDashboardCacheClient")

	cacheLock.RLock()
	if cached, ok := cacheStorage.Get(jobId); ok {
		cacheLock.RUnlock()
		return cached.JobInfo, cached.Err
	}
	cacheLock.RUnlock()

	currentTime := time.Now()
	placeholder := &JobInfoCache{Err: ErrAgain, UpdateAt: &currentTime}

	// Put a placeholder in storage. The cache will be updated only if the placeholder exists.
	// The placeholder will be removed when StopJob or DeleteJob.
	cacheLock.Lock()
	if cached, existed, _ := cacheStorage.PeekOrAdd(jobId, placeholder); existed {
		cacheLock.Unlock()
		return cached.JobInfo, cached.Err
	}
	cacheLock.Unlock()

	task := func(taskCTX context.Context) bool {
		cacheLock.RLock()
		jobInfoCache, existed := cacheStorage.Get(jobId)
		if !existed {
			cacheLock.RUnlock()
			logger.Info("The placeholder is removed for jobId", "jobId", jobId)
			return false
		}
		cacheLock.RUnlock()

		var statusErr *k8serrors.StatusError
		jobInfo, err := r.client.GetJobInfo(taskCTX, jobId)
		if err != nil && !errors.As(err, &statusErr) {
			if jobInfoCache.Err != nil && err.Error() == jobInfoCache.Err.Error() {
				// The error is the same as last time, no need to update, just put the task to execute later.
				// If the error is not fixed, eventually the cache will be expired and removed.
				logger.Info("The error is the same as last time for jobId", "jobId", jobId, "error", err)
				return true
			}
		}
		jobInfoCache.JobInfo = jobInfo
		jobInfoCache.Err = err
		currentTime := time.Now()
		jobInfoCache.UpdateAt = &currentTime

		cacheLock.Lock()
		if existed := cacheStorage.Contains(jobId); !existed {
			cacheLock.Unlock()
			logger.Info("The placeholder is removed before updating for jobId", "jobId", jobId)
			return false
		}
		cacheStorage.Add(jobId, jobInfoCache)
		cacheLock.Unlock()

		if jobInfoCache.JobInfo == nil {
			return true
		}
		if rayv1.IsJobTerminal(jobInfoCache.JobInfo.JobStatus) {
			logger.Info("The job reaches terminal status for jobId", "jobId", jobId, "status", jobInfoCache.JobInfo.JobStatus)
			return false
		}
		return true
	}

	if err := pool.PutTask(task); err != nil {
		logger.Error(err, "Cannot queue more jobInfo fetching tasks.", "jobId", jobId)
		return nil, ErrAgain
	}
	logger.Info("Put a task to fetch job info in background for jobId ", "jobId", jobId)

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
	cacheLock.Lock()
	defer cacheLock.Unlock()

	cacheStorage.Remove(jobName)
	return r.client.StopJob(ctx, jobName)
}

func (r *RayDashboardCacheClient) DeleteJob(ctx context.Context, jobName string) error {
	cacheLock.Lock()
	defer cacheLock.Unlock()

	cacheStorage.Remove(jobName)
	return r.client.DeleteJob(ctx, jobName)
}
