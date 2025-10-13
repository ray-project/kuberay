package dashboardclient

import (
	"sync"
)

type WorkerPool struct {
	taskQueue chan func()
	stopChan  chan struct{}
	wg        sync.WaitGroup
	workers   int
}

func NewWorkerPool(taskQueue chan func()) *WorkerPool {
	wp := &WorkerPool{
		taskQueue: taskQueue,
		workers:   10,
		stopChan:  make(chan struct{}),
	}

	// Start workers immediately
	wp.start()
	return wp
}

// Start launches worker goroutines to consume from queue
func (wp *WorkerPool) start() {
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
		case <-wp.stopChan:
			return
		case task := <-wp.taskQueue:
			if task != nil {
				task() // Execute the job
			}
		}
	}
}
