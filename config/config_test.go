package config

import "testing"

func TestBackfillLimitClamped(t *testing.T) {
	t.Setenv("BACKFILL_LIMIT", "100")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BackfillLimit != maxBackfillLimit {
		t.Fatalf("expected backfill limit %d, got %d", maxBackfillLimit, cfg.BackfillLimit)
	}
}

func TestBackfillLimitInvalidDefaults(t *testing.T) {
	t.Setenv("BACKFILL_LIMIT", "not-a-number")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BackfillLimit != maxBackfillLimit {
		t.Fatalf("expected invalid value to clamp to %d, got %d", maxBackfillLimit, cfg.BackfillLimit)
	}
}

func TestWorkerCountFallsBack(t *testing.T) {
	t.Setenv("WORKER_COUNT", "0")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WorkerCount != defaultWorkerCount {
		t.Fatalf("expected worker count default %d, got %d", defaultWorkerCount, cfg.WorkerCount)
	}
}

func TestJobQueueSizeMinimum(t *testing.T) {
	t.Setenv("JOB_QUEUE_SIZE", "10")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.JobQueueSize != minQueueSize {
		t.Fatalf("expected queue size raised to %d, got %d", minQueueSize, cfg.JobQueueSize)
	}
}
