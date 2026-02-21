package dashboardclient

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/maypok86/otter/v2"
	"github.com/smallnest/chanx"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	utiltypes "github.com/ray-project/kuberay/ray-operator/controllers/ray/utils/types"
)

// ErrAgain EAGAIN means "there is no data available right now, try again later"
// https://stackoverflow.com/questions/4058368/what-does-eagain-mean
var ErrAgain = errors.New("EAGAIN")

const (
	initBufferSize = 128
)

var (
	// The singleton worker pool instance.
	// Use the global variable to avoid passing the worker pool instance through multiple layers of function calls,
	// which would require changing many function signatures.
	// Use the singleton to avoid initializing multiple worker pools.
	initWorkPool sync.Once
	pool         *WorkerPool
)

var _ manager.Runnable = (*WorkerPool)(nil)

type (
	JobInfoCache struct {
		JobInfo *utiltypes.RayJobInfo
		Err     error
	}

	WorkerPool struct {
		cacheReader         client.Reader
		taskQueue           *chanx.UnboundedChan[*rayv1.RayJob]
		existInQueue        sync.Map
		dashboardClientFunc func(rayCluster *rayv1.RayCluster, url string) (RayDashboardClientInterface, error)
		cacheStorage        *otter.Cache[string, *JobInfoCache]
		logger              logr.Logger
		numWorkers          int
		queryInterval       time.Duration
	}
)

func InitWorkerPool(ctx context.Context,
	cacheReader client.Reader,
	numWorkers int,
	queryInterval time.Duration,
	cacheExpiry time.Duration,
	dashboardClientFunc func(rayCluster *rayv1.RayCluster, url string) (RayDashboardClientInterface, error),
) (*WorkerPool, error) {
	var err error
	initWorkPool.Do(func() {
		logger := ctrl.LoggerFrom(ctx).WithName("WorkerPool")

		// It might be better to give a channel capacity because there would be a batch send after listing RayJobs from cache.
		// Using zero capacity channel would be a bit of inefficient because each send operation would block.
		taskQueue := chanx.NewUnboundedChanSize[*rayv1.RayJob](ctx, initBufferSize, initBufferSize, initBufferSize)

		var cacheStorage *otter.Cache[string, *JobInfoCache]
		cacheStorage, err = otter.New(&otter.Options[string, *JobInfoCache]{
			ExpiryCalculator: otter.ExpiryAccessing[string, *JobInfoCache](cacheExpiry), // Reset timer on reads/writes
			OnDeletion: func(e otter.DeletionEvent[string, *JobInfoCache]) {
				if !e.WasEvicted() {
					return
				}
				logger.WithName("cacheStorage").Info("Evict cache for key.", "key", e.Key, "cause", e.Cause.String())
			},
		})
		if err != nil {
			return
		}

		pool = &WorkerPool{
			taskQueue:           taskQueue,
			cacheReader:         cacheReader,
			dashboardClientFunc: dashboardClientFunc,
			cacheStorage:        cacheStorage,
			numWorkers:          numWorkers,
			queryInterval:       queryInterval,
			logger:              logger,
		}
	})

	return pool, err
}

func (w *WorkerPool) Start(ctx context.Context) error {
	var wg sync.WaitGroup
	logger := w.logger

	wg.Go(func() {
		ticker := time.NewTicker(w.queryInterval)
		defer ticker.Stop()
		defer close(w.taskQueue.In)

		for {
			select {
			case <-ctx.Done():
				logger.Info("RayJob listing goroutine exiting...")
				return
			case <-ticker.C:
				var rayJobs rayv1.RayJobList
				err := w.cacheReader.List(ctx, &rayJobs, client.InNamespace("")) // List all namespaces
				if err != nil {
					logger.Error(err, "Error listing RayJobs from cache")
					continue
				}

				logger.V(1).Info("Listing RayJobs from cache", "total", len(rayJobs.Items))

				for _, rayJob := range rayJobs.Items {
					if len(rayJob.Status.DashboardURL) == 0 ||
						len(rayJob.Status.JobId) == 0 ||
						rayv1.IsJobTerminal(rayJob.Status.JobStatus) ||
						rayv1.IsJobDeploymentTerminal(rayJob.Status.JobDeploymentStatus) ||
						(rayJob.ObjectMeta.DeletionTimestamp != nil && !rayJob.ObjectMeta.DeletionTimestamp.IsZero()) {
						continue
					}

					// If the RayJob is in the channel, skip to enqueue.
					// In the worst case of the current implementation, we could have the number of worker working on getting JobInfo and
					// the number of all of RayJobs in the cluster waiting in the task queue. It would not be unbounded.
					if _, ok := w.existInQueue.LoadOrStore(cacheKey(namespacedNameFromRayJob(&rayJob), rayJob.Status.JobId), struct{}{}); ok {
						continue
					}

					// The task queue is unbounded, so the send operation will never block.
					copiedRayJob := rayJob.DeepCopy()
					select {
					case <-ctx.Done():
						logger.Info("Async job info query producer goroutine exiting...")
						return
					case w.taskQueue.In <- copiedRayJob:
					}
				}
			}
		}
	})

	for i := range w.numWorkers {
		wg.Go(func() {
			func(workerID int) {
				for {
					select {
					case <-ctx.Done():
						logger.Info("worker exiting...", "workerID", workerID)
						return
					case rayJobInstance, ok := <-w.taskQueue.Out:
						if !ok {
							logger.Info("worker exiting from a closed channel", "workerID", workerID)
							return
						}

						w.processRayJob(ctx, rayJobInstance)
					}
				}
			}(i)
		})
	}
	logger.Info(fmt.Sprintf("Initialize a worker pool with %d goroutines and query interval is %v.", w.numWorkers, w.queryInterval))

	wg.Wait()
	return ctx.Err()
}

// processRayJob fetches job info from Ray Dashboard and stores it in the cache.
// It uses defer to ensure existInQueue is cleaned up after processing completes,
// preventing the same RayJob from being enqueued again while still being processed.
func (w *WorkerPool) processRayJob(ctx context.Context, rayJobInstance *rayv1.RayJob) {
	logger := w.logger

	// cannot use common.RayJobRayClusterNamespacedName because of cyclic import.
	rayClusterNamespacedName := namespacedNameFromRayJob(rayJobInstance)
	key := cacheKey(rayClusterNamespacedName, rayJobInstance.Status.JobId)

	// Use defer to ensure the key is deleted from existInQueue after processing completes.
	// This prevents the same RayJob from being processed by multiple workers simultaneously.
	defer w.existInQueue.Delete(key)

	// get RayCluster instance from informer cache
	var rayClusterInstance rayv1.RayCluster
	err := w.cacheReader.Get(ctx, rayClusterNamespacedName, &rayClusterInstance)
	if err != nil {
		logger.Error(err, "failed to get RayCluster instance from informer cache", "rayCluster", rayClusterNamespacedName.String())
		return
	}

	rayDashboardClient, err := w.dashboardClientFunc(&rayClusterInstance, rayJobInstance.Status.DashboardURL)
	if err != nil {
		logger.Error(err, "failed to get dashboard client", "rayCluster", rayClusterNamespacedName.Name, "dashboardURL", rayJobInstance.Status.DashboardURL)
		return
	}

	jobInfo, err := rayDashboardClient.GetJobInfo(ctx, rayJobInstance.Status.JobId)

	w.cacheStorage.Set(key, &JobInfoCache{
		JobInfo: jobInfo,
		Err:     err,
	})
}

// GetCachedDashboardClientFunc returns a function that creates a RayDashboardCacheClient.
// This should be called after InitWorkerPool to ensure the worker pool is initialized.
func GetCachedDashboardClientFunc() func(rayCluster *rayv1.RayCluster, url string) (RayDashboardClientInterface, error) {
	if pool == nil {
		panic("WorkerPool is not initialized. Please call InitWorkerPool first.")
	}
	return func(rayCluster *rayv1.RayCluster, url string) (RayDashboardClientInterface, error) {
		rayDashboardClient, err := pool.dashboardClientFunc(rayCluster, url)
		if err != nil {
			return nil, err
		}

		// not use common.RayJobRayClusterNamespacedName to avoid import cycle
		rayClusterNamespacedName := types.NamespacedName{
			Name:      rayCluster.Name,
			Namespace: rayCluster.Namespace,
		}

		return &RayDashboardCacheClient{
			client:         rayDashboardClient,
			cacheStorage:   pool.cacheStorage,
			namespacedName: rayClusterNamespacedName,
		}, nil
	}
}

var _ RayDashboardClientInterface = (*RayDashboardCacheClient)(nil)

type RayDashboardCacheClient struct {
	client         RayDashboardClientInterface
	cacheStorage   *otter.Cache[string, *JobInfoCache]
	namespacedName types.NamespacedName
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

	var err error
	if cached, ok := r.cacheStorage.ComputeIfPresent(cacheKey(r.namespacedName, jobId),
		func(oldValue *JobInfoCache) (newValue *JobInfoCache, op otter.ComputeOp) {
			// If the cache has error, we populate it and invalidate the cache
			// so that the reconcile would not repeatedly return the same error to trigger the rate limiter,
			// which would cause exponential backoff and delay the recovery.
			if oldValue.Err != nil {
				err = oldValue.Err
				return oldValue, otter.InvalidateOp
			}
			return oldValue, otter.CancelOp
		},
	); ok {
		return cached.JobInfo, nil
	}
	if err != nil {
		logger.Error(err, "Got an error on the job info cache, invalidating the cache", "jobId", jobId, "cacheKey", cacheKey(r.namespacedName, jobId))
		return nil, err
	}

	logger.Info("Cache miss for jobId", "jobId", jobId, "cacheKey", cacheKey(r.namespacedName, jobId))
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

func cacheKey(namespacedName types.NamespacedName, jobId string) string {
	return namespacedName.String() + string(types.Separator) + jobId
}

// namespacedNameFromRayJob is duplicated from common.RayJobRayClusterNamespacedName to avoid import cycle
func namespacedNameFromRayJob(rayJob *rayv1.RayJob) types.NamespacedName {
	return types.NamespacedName{
		Name:      rayJob.Status.RayClusterName,
		Namespace: rayJob.Namespace,
	}
}
