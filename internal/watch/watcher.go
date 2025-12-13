package watch

import (
    "context"
    "log"
    "path/filepath"
    "strings"

    "alert_framework/internal/config"
    "alert_framework/internal/jobs"
    "github.com/fsnotify/fsnotify"
)

// Watcher monitors CALLS_DIR for new audio files and enqueues ingest jobs.
type Watcher struct {
    cfg    config.Config
    runner *jobs.Runner
}

func New(cfg config.Config, runner *jobs.Runner) *Watcher {
    return &Watcher{cfg: cfg, runner: runner}
}

func (w *Watcher) Start(ctx context.Context) error {
    if !w.cfg.EnableWatcher {
        log.Println("watcher disabled")
        return nil
    }
    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        return err
    }
    go func() {
        defer watcher.Close()
        for {
            select {
            case <-ctx.Done():
                return
            case evt := <-watcher.Events:
                if evt.Op&(fsnotify.Create|fsnotify.Rename) != 0 {
                    if w.isAudio(evt.Name) {
                        callID := filepath.Base(evt.Name)
                        _, _ = w.runner.Enqueue(ctx, callID, jobs.StageIngest, map[string]any{})
                    }
                }
            case err := <-watcher.Errors:
                log.Printf("watcher error: %v", err)
            }
        }
    }()
    return watcher.Add(w.cfg.CallsDir)
}

func (w *Watcher) isAudio(path string) bool {
    ext := strings.ToLower(filepath.Ext(path))
    switch ext {
    case ".mp3", ".wav", ".m4a", ".aac", ".flac", ".ogg":
        return true
    default:
        return false
    }
}

// Backfill enqueues ingest for existing files.
func (w *Watcher) Backfill(ctx context.Context) error {
    entries, err := filepath.Glob(filepath.Join(w.cfg.CallsDir, "*"))
    if err != nil {
        return err
    }
    for _, e := range entries {
        if w.isAudio(e) {
            _, _ = w.runner.Enqueue(ctx, filepath.Base(e), jobs.StageIngest, map[string]any{})
        }
    }
    return nil
}
