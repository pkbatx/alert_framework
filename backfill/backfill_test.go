package backfill

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestSelectPendingRespectsLimitAndStatus(t *testing.T) {
	now := time.Now()
	var records []Record
	for i := 0; i < 30; i++ {
		status := StatusQueued
		if i%5 == 0 {
			status = StatusDone
		}
		records = append(records, Record{
			Filename:  fmt.Sprintf("file-%02d", i),
			ModTime:   now.Add(time.Duration(i) * time.Minute),
			Status:    status,
			UpdatedAt: now,
		})
	}

	pending, summary := SelectPending(records, 15)
	if len(pending) != 15 {
		t.Fatalf("expected 15 pending records, got %d", len(pending))
	}
	if summary.AlreadyProcessed != 6 {
		t.Fatalf("expected 6 already processed, got %d", summary.AlreadyProcessed)
	}
	if summary.Unprocessed != 24 {
		t.Fatalf("expected 24 unprocessed, got %d", summary.Unprocessed)
	}
	if summary.Selected != 15 {
		t.Fatalf("expected 15 selected, got %d", summary.Selected)
	}
	for _, rec := range pending {
		if rec.Status == StatusDone {
			t.Fatalf("unexpected done status in pending set: %v", rec.Filename)
		}
	}
	for i := 1; i < len(pending); i++ {
		if pending[i].ModTime.After(pending[i-1].ModTime) {
			t.Fatalf("records not sorted by recency")
		}
	}
}

func TestBackfillRunReportsDrops(t *testing.T) {
	now := time.Now()
	candidates := []Record{}
	for i := 0; i < 5; i++ {
		candidates = append(candidates, Record{Filename: fmt.Sprintf("call-%d", i), ModTime: now.Add(time.Duration(i) * time.Minute)})
	}

	summaryCh := make(chan Summary, 1)
	repo := &stubRepo{candidates: candidates, allowEnqueue: 2, summaries: summaryCh}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	Run(ctx, repo, 5)

	select {
	case summary := <-summaryCh:
		if summary.Enqueued != 2 {
			t.Fatalf("expected 2 enqueues, got %d", summary.Enqueued)
		}
		if summary.DroppedFull != 3 {
			t.Fatalf("expected 3 dropped jobs, got %d", summary.DroppedFull)
		}
		if summary.Selected != 5 {
			t.Fatalf("expected 5 selected, got %d", summary.Selected)
		}
		if summary.Unprocessed != 5 {
			t.Fatalf("expected unprocessed count, got %d", summary.Unprocessed)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for backfill summary")
	}
}

type stubRepo struct {
	candidates   []Record
	allowEnqueue int
	enqueued     int
	summaries    chan<- Summary
}

func (r *stubRepo) ListCandidates(ctx context.Context) ([]Record, error) {
	return r.candidates, nil
}

func (r *stubRepo) QueueRecord(ctx context.Context, rec Record) (EnqueueResult, error) {
	if r.enqueued < r.allowEnqueue {
		r.enqueued++
		return EnqueueResult{Enqueued: true}, nil
	}
	return EnqueueResult{DroppedFull: true}, nil
}

func (r *stubRepo) OnBackfillComplete(summary Summary) {
	r.summaries <- summary
}
