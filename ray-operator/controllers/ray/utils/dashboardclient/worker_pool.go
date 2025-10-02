package dashboardclient

import (
	"sync"

	cmap "github.com/orcaman/concurrent-map/v2"
)

type WorkerPool struct {
	channelContent cmap.ConcurrentMap[string, struct{}]
	taskQueue      chan func()
	stop           chan struct{}
	wg             sync.WaitGroup
	workers        int
}

func NewWorkerPool(taskQueue chan func()) *WorkerPool {
	wp := &WorkerPool{
		taskQueue:      taskQueue,
		workers:        10,
		stop:           make(chan struct{}),
		channelContent: cmap.New[struct{}](),
	}

	// Start workers immediately
	wp.Start()
	return wp
}

// Start launches worker goroutines to consume from queue
func (wp *WorkerPool) Start() {
	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}
}

// worker consumes and executes tasks from the queue
func (wp *WorkerPool) worker() {
	defer wp.wg.Done()

	for {
		select {
		case <-wp.stop:
			return
		case task := <-wp.taskQueue:
			if task != nil {
				task() // Execute the job
			}
		}
	}
}

// Stop shuts down all workers
func (wp *WorkerPool) Stop() {
	close(wp.stop)
	wp.wg.Wait()
}
