package config

import "testing"

func TestBackfillLimitClamp(t *testing.T) {
	t.Setenv("BACKFILL_LIMIT", "200")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if cfg.BackfillLimit != maxBackfillLimit {
		t.Fatalf("expected backfill limit %d, got %d", maxBackfillLimit, cfg.BackfillLimit)
	}
}

func TestQueueSizeDefaultsRespectWorkers(t *testing.T) {
	t.Setenv("WORKER_COUNT", "8")
	t.Setenv("JOB_QUEUE_SIZE", "4")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if cfg.WorkerCount != 8 {
		t.Fatalf("expected worker count 8, got %d", cfg.WorkerCount)
	}
	if cfg.JobQueueSize < cfg.WorkerCount {
		t.Fatalf("queue size should be at least workers, got %d", cfg.JobQueueSize)
	}
}

func TestHTTPPortDefaultFormatting(t *testing.T) {
	t.Setenv("HTTP_PORT", "9000")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if cfg.HTTPPort != ":9000" {
		t.Fatalf("expected HTTP_PORT to include colon, got %s", cfg.HTTPPort)
	}
}
