package pipeline

import (
    "context"
    "os"
    "path/filepath"
    "testing"

    "alert_framework/internal/config"
    "alert_framework/internal/jobs"
    "alert_framework/internal/store"
)

func TestIngestStageCopiesFile(t *testing.T) {
    cfg := config.Load()
    cfg.CallsDir = t.TempDir()
    cfg.WorkDir = t.TempDir()
    cfg.DBPath = filepath.Join(t.TempDir(), "test.db")

    src := filepath.Join(cfg.CallsDir, "call1.mp3")
    if err := os.WriteFile(src, []byte("audio"), 0o644); err != nil {
        t.Fatal(err)
    }

    st, err := store.Open(cfg.DBPath)
    if err != nil {
        t.Fatal(err)
    }
    reg := BuildRegistry(cfg, st)
    fn := reg[jobs.StageIngest]
    if fn == nil {
        t.Fatal("missing ingest handler")
    }
    if err := fn(context.Background(), jobs.ExecutionContext{Cfg: cfg, Store: st, Logf: func(int64, string) {}}, "call1.mp3", map[string]any{}); err != nil {
        t.Fatalf("ingest err: %v", err)
    }

    dst := filepath.Join(cfg.WorkDir, "call1.mp3", "call1.mp3")
    if _, err := os.Stat(dst); err != nil {
        t.Fatalf("expected dst file: %v", err)
    }
}
