package backfill

import (
	"context"
	"log"
	"sort"
	"time"
)

// Record represents a file and its processing state used for backfill decisions.
type Record struct {
	Filename  string
	ModTime   time.Time
	SizeBytes int64
	Status    string
	UpdatedAt time.Time
}

// Status constants used by selection logic.
const (
	StatusDone       = "done"
	StatusProcessing = "processing"
	StatusQueued     = "queued"
	StatusError      = "error"
)

// SelectPending returns up to limit records sorted by recency that are not fully processed.
func SelectPending(records []Record, limit int) []Record {
	sort.Slice(records, func(i, j int) bool {
		return records[i].ModTime.After(records[j].ModTime)
	})
	pending := make([]Record, 0, limit)
	for _, r := range records {
		if r.Status == StatusDone {
			continue
		}
		pending = append(pending, r)
		if len(pending) >= limit {
			break
		}
	}
	return pending
}

// Repository describes the data source needed for backfill.
type Repository interface {
	ListCandidates(ctx context.Context) ([]Record, error)
	QueueRecord(ctx context.Context, rec Record) bool
}

// Run executes the backfill asynchronously.
func Run(ctx context.Context, repo Repository, limit int) {
	go func() {
		log.Printf("starting backfill with limit %d", limit)
		records, err := repo.ListCandidates(ctx)
		if err != nil {
			log.Printf("backfill list failed: %v", err)
			return
		}
		pending := SelectPending(records, limit)
		var enqueued int
		for _, rec := range pending {
			if repo.QueueRecord(ctx, rec) {
				enqueued++
			}
		}
		log.Printf("backfill candidates=%d enqueued=%d", len(records), enqueued)
	}()
}
