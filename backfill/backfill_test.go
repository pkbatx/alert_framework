package backfill

import (
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

	pending := SelectPending(records, 15)
	if len(pending) != 15 {
		t.Fatalf("expected 15 pending records, got %d", len(pending))
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
