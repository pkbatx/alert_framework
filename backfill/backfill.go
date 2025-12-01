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
	TotalCandidates     int `json:"total"`
	AlreadyProcessed    int `json:"already_processed"`
	Unprocessed         int `json:"unprocessed"`
	SelectedForBackfill int `json:"selected"`
	AttemptedEnqueue    int `json:"attempted_enqueue"`
	EnqueueSucceeded    int `json:"enqueued"`
	EnqueueDroppedFull  int `json:"dropped_full"`
}

// EnqueueResult captures queueing outcome for a record.
type EnqueueResult struct {
	Enqueued    bool
	DroppedFull bool
}

// Repository describes the data source needed for backfill.
type Repository interface {
	ListCandidates(ctx context.Context) ([]Record, error)
	QueueRecord(ctx context.Context, rec Record) EnqueueResult
	OnBackfillComplete(summary Summary)
}

// SelectPending returns up to limit records sorted by recency that are not fully processed.
// It also reports a summary of the candidate set.
func SelectPending(records []Record, limit int) ([]Record, Summary) {
	sort.Slice(records, func(i, j int) bool {
		return records[i].ModTime.After(records[j].ModTime)
	})

	summary := Summary{TotalCandidates: len(records)}
	unprocessed := make([]Record, 0, len(records))
	for _, r := range records {
		if r.Status == StatusDone {
			summary.AlreadyProcessed++
			continue
		}
		unprocessed = append(unprocessed, r)
	}

	summary.Unprocessed = len(unprocessed)
	if limit < summary.Unprocessed {
		unprocessed = unprocessed[:limit]
	}
	summary.SelectedForBackfill = len(unprocessed)
	return unprocessed, summary
}

// Run executes the backfill asynchronously.
func Run(ctx context.Context, repo Repository, limit int) {
	go func() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		records, err := repo.ListCandidates(ctx)
		if err != nil {
			log.Printf("backfill list failed: %v", err)
			return
		}

		selected, summary := SelectPending(records, limit)
		summary.AttemptedEnqueue = len(selected)

		for _, rec := range selected {
			result := repo.QueueRecord(ctx, rec)
			if result.Enqueued {
				summary.EnqueueSucceeded++
			}
			if result.DroppedFull {
				summary.EnqueueDroppedFull++
			}
		}

		log.Printf("backfill summary: total=%d unprocessed=%d selected=%d enqueued=%d dropped_full=%d already_processed=%d", summary.TotalCandidates, summary.Unprocessed, summary.SelectedForBackfill, summary.EnqueueSucceeded, summary.EnqueueDroppedFull, summary.AlreadyProcessed)
		repo.OnBackfillComplete(summary)
	}()
}
