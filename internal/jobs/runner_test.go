package jobs

import (
    "context"
    "path/filepath"
    "testing"

    "alert_framework/internal/config"
    "alert_framework/internal/store"
)

func TestIdempotentEnqueue(t *testing.T) {
    cfg := config.Load()
    dbPath := filepath.Join(t.TempDir(), "test.db")
    cfg.DBPath = dbPath
    cfg.QueueSize = 2
    cfg.WorkerCount = 0
    st, err := store.Open(cfg.DBPath)
    if err != nil {
        t.Fatal(err)
    }
    runner := NewRunner(cfg, st, Registry{})
    ctx := context.Background()
    j1, err := runner.Enqueue(ctx, "file1.mp3", StageIngest, map[string]any{"foo": "bar"})
    if err != nil {
        t.Fatalf("enqueue1: %v", err)
    }
    j2, err := runner.Enqueue(ctx, "file1.mp3", StageIngest, map[string]any{"foo": "bar"})
    if err != nil {
        t.Fatalf("enqueue2: %v", err)
    }
    if j1.ID != j2.ID {
        t.Fatalf("expected idempotent job, got %d vs %d", j1.ID, j2.ID)
    }
}
