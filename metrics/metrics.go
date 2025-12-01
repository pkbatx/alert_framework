package metrics

import (
	"alert_framework/backfill"
	"sync"
	"sync/atomic"
)

// Metrics captures shared operational stats for the queue, workers, and backfill.
type Metrics struct {
	queueLength   int64
	queueCapacity int64
	workerCount   int64

	processedJobs int64
	failedJobs    int64

	mu             sync.RWMutex
	lastBackfill   backfill.Summary
	hasBackfillRun atomic.Bool
}

// Snapshot provides a consistent view of the current metrics.
type Snapshot struct {
	QueueLength    int
	QueueCapacity  int
	WorkerCount    int
	ProcessedJobs  int64
	FailedJobs     int64
	LastBackfill   backfill.Summary
	HasBackfillRun bool
}

// New creates a zeroed Metrics instance.
func New() *Metrics {
	return &Metrics{}
}

// UpdateQueue records the current queue stats.
func (m *Metrics) UpdateQueue(length, capacity, workers int) {
	atomic.StoreInt64(&m.queueLength, int64(length))
	atomic.StoreInt64(&m.queueCapacity, int64(capacity))
	atomic.StoreInt64(&m.workerCount, int64(workers))
}

// RecordJobCompletion increments processed/failed counters based on outcome.
func (m *Metrics) RecordJobCompletion(err error) {
	atomic.AddInt64(&m.processedJobs, 1)
	if err != nil {
		atomic.AddInt64(&m.failedJobs, 1)
	}
}

// SetBackfill marks the most recent backfill summary.
func (m *Metrics) SetBackfill(summary backfill.Summary) {
	m.mu.Lock()
	m.lastBackfill = summary
	m.mu.Unlock()
	m.hasBackfillRun.Store(true)
}

// Snapshot returns a read-only view of metrics.
func (m *Metrics) Snapshot() Snapshot {
	m.mu.RLock()
	last := m.lastBackfill
	m.mu.RUnlock()
	return Snapshot{
		QueueLength:    int(atomic.LoadInt64(&m.queueLength)),
		QueueCapacity:  int(atomic.LoadInt64(&m.queueCapacity)),
		WorkerCount:    int(atomic.LoadInt64(&m.workerCount)),
		ProcessedJobs:  atomic.LoadInt64(&m.processedJobs),
		FailedJobs:     atomic.LoadInt64(&m.failedJobs),
		LastBackfill:   last,
		HasBackfillRun: m.hasBackfillRun.Load(),
	}
}
