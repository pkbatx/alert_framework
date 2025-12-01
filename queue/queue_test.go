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
		ID: "job1",
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

	ok := q.Enqueue(Job{ID: "slow", Work: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}})
	if !ok {
		t.Fatalf("expected first enqueue to succeed")
	}

	if ok := q.Enqueue(Job{ID: "drop", Work: func(ctx context.Context) error { return nil }}); ok {
		t.Fatalf("expected enqueue to be rejected when queue is full")
	}
}
