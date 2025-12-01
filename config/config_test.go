package config

import "testing"

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

func TestDBPathDefaultsToWorkDir(t *testing.T) {
	t.Setenv("WORK_DIR", "/tmp/custom")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	expected := "/tmp/custom/transcriptions.db"
	if cfg.DBPath != expected {
		t.Fatalf("expected DBPath %s, got %s", expected, cfg.DBPath)
	}
}
