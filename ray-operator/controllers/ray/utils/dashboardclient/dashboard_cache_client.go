package dashboardclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
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
	workerSize = 8

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
	Task         func(taskCTX context.Context) bool
	JobInfoCache struct {
		JobInfo   *utiltypes.RayJobInfo
		Err       error
		UpdatedAt *time.Time
	}

	workerPool struct {
		taskQueue ExtendableChannel[Task]
	}
)

func (w *workerPool) init(ctx context.Context, workerSize int, queryInterval time.Duration) {
	logger := ctrl.LoggerFrom(ctx).WithName("RayDashboardCacheClient").WithName("WorkerPool")
	w.taskQueue = NewExtendableChannel[Task]()

	for i := 0; i < workerSize; i++ {
		go func(workerID int) {
			for {
				select {
				case <-ctx.Done():
					logger.Info("worker exiting...", "workerID", workerID)
					return
				case task := <-w.taskQueue.Out:
					again := task(ctx)

					if again && ctx.Err() == nil {
						time.AfterFunc(queryInterval, func() {
							w.taskQueue.In <- task
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
	case w.taskQueue.In <- task:
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
		pool.init(ctx, workerSize, queryInterval)
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
					keys := cacheStorage.Keys()

					expiredThreshold := time.Now().Add(-cacheExpiry)
					loggerForGC.Info(fmt.Sprintf("Found %d keys to verify,", len(keys)), "expiredThreshold", expiredThreshold, "tick at", t)

					removed := keys[:0]
					for _, key := range keys {
						if cached, ok := cacheStorage.Peek(key); ok {
							if cached.UpdatedAt.Before(expiredThreshold) {
								cacheStorage.Remove(key)
								removed = append(removed, key)
							}
						}
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

	if cached, ok := cacheStorage.Get(jobId); ok {
		if cached.Err != nil && !errors.Is(cached.Err, ErrAgain) {
			// Consume the error.
			// If the RayJob is still exists, the next Reconcile iteration will put the task back for updating JobInfo
			cacheStorage.Remove(jobId)
			logger.Info("Consume the cached error for jobId", "jobId", jobId, "error", cached.Err)
		}
		return cached.JobInfo, cached.Err
	}

	currentTime := time.Now()
	placeholder := &JobInfoCache{Err: ErrAgain, UpdatedAt: &currentTime}

	// Put a placeholder in storage. The cache will be updated only if the placeholder exists.
	// The placeholder will be removed when StopJob or DeleteJob.
	if cached, existed, _ := cacheStorage.PeekOrAdd(jobId, placeholder); existed {
		return cached.JobInfo, cached.Err
	}

	task := func(taskCTX context.Context) bool {
		if _, existed := cacheStorage.Get(jobId); !existed {
			logger.Info("The placeholder is removed for jobId", "jobId", jobId)
			return false
		}

		jobInfo, err := r.client.GetJobInfo(taskCTX, jobId)
		currentTime := time.Now()

		// Make this cache immutable to avoid data race between pointer updates and read operations.
		newJobInfoCache := &JobInfoCache{
			JobInfo:   jobInfo,
			Err:       err,
			UpdatedAt: &currentTime,
		}

		if existed := cacheStorage.Contains(jobId); !existed {
			logger.Info("The placeholder is removed before updating for jobId", "jobId", jobId)
			return false
		}
		cacheStorage.Add(jobId, newJobInfoCache)

		if err != nil {
			// Exits the updating loop after getting an error.
			// If the RayJob still exists, Reconcile will consume the error and put the JobId back to updating loop again.
			logger.Info("Failed to fetch job info for jobId", "jobId", jobId, "error", err)
			return false
		}
		if newJobInfoCache.JobInfo == nil {
			return true
		}
		if rayv1.IsJobTerminal(newJobInfoCache.JobInfo.JobStatus) {
			logger.Info("The job reaches terminal status for jobId", "jobId", jobId, "status", newJobInfoCache.JobInfo.JobStatus)
			return false
		}
		return true
	}

	if err := pool.PutTask(task); err != nil {
		logger.Error(err, "Cannot queue more job info fetching tasks.", "jobId", jobId)
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
	cacheStorage.Remove(jobName)
	return r.client.StopJob(ctx, jobName)
}

func (r *RayDashboardCacheClient) DeleteJob(ctx context.Context, jobName string) error {
	cacheStorage.Remove(jobName)
	return r.client.DeleteJob(ctx, jobName)
}

type ExtendableChannel[T any] struct {
	In  chan<- T
	Out <-chan T
}

func NewExtendableChannel[T any]() ExtendableChannel[T] {
	in := make(chan T)
	out := make(chan T)

	go func() {
		defer close(out)
		var buffer []T

		for {
			if len(buffer) == 0 {
				v, ok := <-in
				if !ok {
					return
				}
				buffer = append(buffer, v)
			}

			select {
			case v, ok := <-in:
				if !ok {
					// Inbound closed; drain the buffer
					for _, b := range buffer {
						out <- b
					}
					return
				}

				// TODO: this is not memory efficient.
				buffer = append(buffer, v)
			case out <- buffer[0]:
				buffer = buffer[1:]
			}
		}
	}()

	return ExtendableChannel[T]{In: in, Out: out}
}
