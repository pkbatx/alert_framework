package queue

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Job encapsulates a unit of work processed by the worker pool.
type Job struct {
	ID       string
	Source   string
	Work     func(context.Context) error
	OnFinish func(error)
}

// Stats exposes current queue metrics.
type Stats struct {
	Length      int
	Capacity    int
	WorkerCount int
	Processed   uint64
	Failed      uint64
}

// Queue represents a bounded job queue with a fixed worker pool.
type Queue struct {
	jobs        chan Job
	workerCount int
	timeout     time.Duration
	started     bool
	mu          sync.RWMutex
	wg          sync.WaitGroup
	processed   uint64
	failed      uint64
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
	return q.tryEnqueue(j, true)
}

// EnqueueWithRetry attempts to queue a job with a bounded retry window. Returns (enqueued, droppedFull).
func (q *Queue) EnqueueWithRetry(ctx context.Context, j Job, window time.Duration, interval time.Duration) (bool, bool) {
	deadline := time.Now().Add(window)
	attempt := func() bool {
		return q.tryEnqueue(j, false)
	}
	if attempt() {
		return true, false
	}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false, false
		case <-time.After(interval):
			if attempt() {
				return true, false
			}
		}
	}
	return false, true
}

func (q *Queue) tryEnqueue(j Job, logDrop bool) bool {
	q.mu.RLock()
	started := q.started
	q.mu.RUnlock()
	if !started {
		if logDrop {
			log.Printf("enqueue called before queue started for job %s", j.ID)
		}
		return false
	}
	select {
	case q.jobs <- j:
		return true
	default:
		if logDrop {
			log.Printf("job queue full, dropping job %s", j.ID)
		}
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

// Stats returns current queue metrics.
func (q *Queue) Stats() Stats {
	q.mu.RLock()
	defer q.mu.RUnlock()
	length := 0
	if q.jobs != nil {
		length = len(q.jobs)
	}
	return Stats{
		Length:      length,
		Capacity:    cap(q.jobs),
		WorkerCount: q.workerCount,
		Processed:   atomic.LoadUint64(&q.processed),
		Failed:      atomic.LoadUint64(&q.failed),
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
	atomic.AddUint64(&q.processed, 1)
	if err != nil {
		atomic.AddUint64(&q.failed, 1)
	}
	status := "success"
	if err != nil {
		status = err.Error()
	}
	log.Printf("job_source=%s job=%s duration_ms=%d status=%s", j.Source, j.ID, time.Since(start).Milliseconds(), status)
}

// Healthy returns true if the queue has been started.
func (q *Queue) Healthy() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.started
}
