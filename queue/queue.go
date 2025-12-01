package queue

import (
	"context"
	"log"
	"sync"
	"time"
)

// Job encapsulates a unit of work processed by the worker pool.
type Job struct {
	ID       string
	Work     func(context.Context) error
	OnFinish func(error)
}

// Queue represents a bounded job queue with a fixed worker pool.
type Queue struct {
	jobs        chan Job
	workerCount int
	timeout     time.Duration
	started     bool
	mu          sync.RWMutex
	wg          sync.WaitGroup
}

// New creates a new Queue with the provided capacity, worker count, and per-job timeout.
func New(capacity, workerCount int, timeout time.Duration) *Queue {
	return &Queue{
		jobs:        make(chan Job, capacity),
		workerCount: workerCount,
		timeout:     timeout,
	}
}

// Start launches the worker pool.
func (q *Queue) Start(ctx context.Context) {
	q.mu.Lock()
	if q.started {
		q.mu.Unlock()
		return
	}
	q.started = true
	q.mu.Unlock()
	for i := 0; i < q.workerCount; i++ {
		q.wg.Add(1)
		go q.worker(ctx)
	}
}

// Enqueue attempts to queue a job without blocking. Returns false if queue is full or not started.
func (q *Queue) Enqueue(j Job) bool {
	q.mu.RLock()
	started := q.started
	q.mu.RUnlock()
	if !started {
		log.Printf("enqueue called before queue started for job %s", j.ID)
		return false
	}
	select {
	case q.jobs <- j:
		return true
	default:
		log.Printf("job queue full, dropping job %s", j.ID)
		return false
	}
}

// Stop stops accepting new jobs and waits for workers to drain until context is done.
func (q *Queue) Stop(ctx context.Context) {
	q.mu.Lock()
	if !q.started {
		q.mu.Unlock()
		return
	}
	if q.jobs != nil {
		close(q.jobs)
	}
	q.mu.Unlock()

	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (q *Queue) worker(ctx context.Context) {
	defer q.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case j, ok := <-q.jobs:
			if !ok {
				return
			}
			q.handleJob(ctx, j)
		}
	}
}

func (q *Queue) handleJob(ctx context.Context, j Job) {
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("job %s panic recovered: %v", j.ID, r)
		}
	}()

	jobCtx, cancel := context.WithTimeout(ctx, q.timeout)
	err := j.Work(jobCtx)
	cancel()
	if j.OnFinish != nil {
		j.OnFinish(err)
	}
	status := "success"
	if err != nil {
		status = err.Error()
	}
	log.Printf("job %s finished in %s (%s)", j.ID, time.Since(start), status)
}

// Healthy returns true if the queue has been started.
func (q *Queue) Healthy() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.started
}
