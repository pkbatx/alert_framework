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

// Summary captures backfill execution metrics.
type Summary struct {
	TotalCandidates  int `json:"total"`
	AlreadyProcessed int `json:"already_processed"`
	Unprocessed      int `json:"unprocessed"`
	Selected         int `json:"selected"`
	Enqueued         int `json:"enqueued"`
	DroppedFull      int `json:"dropped_full"`
	OtherErrors      int `json:"other_errors"`
}

// EnqueueResult captures queueing outcome for a record.
type EnqueueResult struct {
	Enqueued    bool
	DroppedFull bool
}

// Repository describes the data source needed for backfill.
type Repository interface {
	ListCandidates(ctx context.Context) ([]Record, error)
	QueueRecord(ctx context.Context, rec Record) (EnqueueResult, error)
	OnBackfillComplete(summary Summary)
}

// SelectPending returns up to limit records sorted by recency that are not fully processed.
// It also reports a summary of the candidate set.
func SelectPending(records []Record, limit int) ([]Record, Summary) {
	sort.Slice(records, func(i, j int) bool {
		return records[i].ModTime.After(records[j].ModTime)
	})

	summary := Summary{TotalCandidates: len(records)}
	if limit < 0 {
		limit = 0
	}
	unprocessed := make([]Record, 0, len(records))
	for _, r := range records {
		if r.Status == StatusDone {
			summary.AlreadyProcessed++
			continue
		}
		unprocessed = append(unprocessed, r)
	}

	summary.Unprocessed = len(unprocessed)
	if limit > 0 && limit < summary.Unprocessed {
		unprocessed = unprocessed[:limit]
	}
	summary.Selected = len(unprocessed)
	return unprocessed, summary
}

// Run executes the backfill asynchronously.
func Run(ctx context.Context, repo Repository, limit int) {
	go func() {
		select {
		case <-ctx.Done():
			repo.OnBackfillComplete(Summary{})
			return
		default:
		}

		records, err := repo.ListCandidates(ctx)
		if err != nil {
			log.Printf("backfill list failed: %v", err)
			repo.OnBackfillComplete(Summary{})
			return
		}

		selected, summary := SelectPending(records, limit)

		for _, rec := range selected {
			result, err := repo.QueueRecord(ctx, rec)
			if err != nil {
				log.Printf("backfill enqueue error for %s: %v", rec.Filename, err)
				summary.OtherErrors++
				continue
			}
			if result.Enqueued {
				summary.Enqueued++
			}
			if result.DroppedFull {
				summary.DroppedFull++
			}
		}

		log.Printf("backfill summary: total=%d unprocessed=%d selected=%d enqueued=%d dropped_full=%d other_errors=%d already_processed=%d", summary.TotalCandidates, summary.Unprocessed, summary.Selected, summary.Enqueued, summary.DroppedFull, summary.OtherErrors, summary.AlreadyProcessed)
		repo.OnBackfillComplete(summary)
	}()
}
