package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStatsTrackProcessedAndFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	q := New(4, 2, time.Second)
	q.Start(ctx)

	done := make(chan struct{})
	fail := make(chan struct{})

	q.Enqueue(Job{ID: "ok", Source: "watcher", Work: func(context.Context) error { close(done); return nil }})
	q.Enqueue(Job{ID: "fail", Source: "backfill", Work: func(context.Context) error { close(fail); return errors.New("boom") }})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("success job did not finish")
	}
	select {
	case <-fail:
	case <-time.After(2 * time.Second):
		t.Fatalf("failure job did not finish")
	}

	stats := q.Stats()
	if stats.Processed < 2 {
		t.Fatalf("expected processed to be >=2, got %d", stats.Processed)
	}
	if stats.Failed == 0 {
		t.Fatalf("expected at least one failure recorded")
	}
}
