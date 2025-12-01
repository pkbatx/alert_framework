package queue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueueProcessesJob(t *testing.T) {
	q := New(10, 1, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	var processed int32
	done := make(chan struct{})
	ok := q.Enqueue(Job{
		ID:     "job1",
		Source: "test",
		Work: func(ctx context.Context) error {
			atomic.AddInt32(&processed, 1)
			close(done)
			return nil
		},
	})
	if !ok {
		t.Fatalf("expected enqueue to succeed")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("job did not complete")
	}
	if atomic.LoadInt32(&processed) != 1 {
		t.Fatalf("job not processed")
	}
}

func TestQueueTimeoutAndBounded(t *testing.T) {
	q := New(1, 0, 100*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	ok := q.Enqueue(Job{ID: "slow", Source: "test", Work: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}})
	if !ok {
		t.Fatalf("expected first enqueue to succeed")
	}

	if ok := q.Enqueue(Job{ID: "drop", Source: "test", Work: func(ctx context.Context) error { return nil }}); ok {
		t.Fatalf("expected enqueue to be rejected when queue is full")
	}
}

func TestEnqueueWithRetryDropsWhenFull(t *testing.T) {
	q := New(1, 0, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	// Fill the queue so the retry path triggers.
	first := q.Enqueue(Job{ID: "first", Source: "test", Work: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }})
	if !first {
		t.Fatalf("expected initial enqueue to succeed")
	}

	enqueued, dropped := q.EnqueueWithRetry(ctx, Job{ID: "retry", Source: "test", Work: func(ctx context.Context) error { return nil }}, 200*time.Millisecond, 50*time.Millisecond)
	if enqueued {
		t.Fatalf("expected enqueue to fail due to full queue")
	}
	if !dropped {
		t.Fatalf("expected enqueue to be reported as dropped after retries")
	}
}
