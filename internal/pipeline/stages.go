package pipeline

import (
    "context"
    "fmt"
    "os"
    "path/filepath"

    "alert_framework/internal/config"
    "alert_framework/internal/jobs"
    "alert_framework/internal/store"
)

// BuildRegistry wires deterministic stage functions.
func BuildRegistry(cfg config.Config, st *store.Store) jobs.Registry {
    return jobs.Registry{
        jobs.StageIngest:       ingestStage(cfg, st),
        jobs.StagePreprocess:   passthroughStage(cfg, st, "preprocess"),
        jobs.StageAlertInitial: alertStage(cfg, st),
        jobs.StageTranscribe:   passthroughStage(cfg, st, "transcribe"),
        jobs.StageNormalize:    passthroughStage(cfg, st, "normalize"),
        jobs.StageEnrich:       passthroughStage(cfg, st, "enrich"),
        jobs.StagePublish:      passthroughStage(cfg, st, "publish"),
    }
}

func ingestStage(cfg config.Config, st *store.Store) jobs.StageFunc {
    return func(ctx context.Context, exec jobs.ExecutionContext, callID string, params map[string]any) error {
        src := filepath.Join(cfg.CallsDir, callID)
        dstDir := filepath.Join(cfg.WorkDir, callID)
        if err := os.MkdirAll(dstDir, 0o755); err != nil {
            return err
        }
        dst := filepath.Join(dstDir, filepath.Base(src))
        if _, err := copyFile(src, dst); err != nil {
            return err
        }
        if err := st.UpdateCallStage(ctx, callID, string(jobs.StageIngest), jobs.StatusSucceeded, nil, config.Now()); err != nil {
            return err
        }
        exec.Logf(paramsInt64(params, "job_id"), fmt.Sprintf("ingest copied %s", dst))
        return nil
    }
}

func alertStage(cfg config.Config, st *store.Store) jobs.StageFunc {
    return func(ctx context.Context, exec jobs.ExecutionContext, callID string, params map[string]any) error {
        msg := fmt.Sprintf("call %s ready", callID)
        exec.Logf(paramsInt64(params, "job_id"), msg)
        return st.UpdateCallStage(ctx, callID, string(jobs.StageAlertInitial), jobs.StatusSucceeded, nil, config.Now())
    }
}

func passthroughStage(cfg config.Config, st *store.Store, name string) jobs.StageFunc {
    stageName := name
    return func(ctx context.Context, exec jobs.ExecutionContext, callID string, params map[string]any) error {
        exec.Logf(paramsInt64(params, "job_id"), fmt.Sprintf("stage %s for %s", stageName, callID))
        return st.UpdateCallStage(ctx, callID, stageName, jobs.StatusSucceeded, nil, config.Now())
    }
}

func copyFile(src, dst string) (int64, error) {
    in, err := os.Open(src)
    if err != nil {
        return 0, err
    }
    defer in.Close()
    out, err := os.Create(dst)
    if err != nil {
        return 0, err
    }
    defer out.Close()
    n, err := out.ReadFrom(in)
    return n, err
}

func paramsInt64(m map[string]any, key string) int64 {
    if v, ok := m[key]; ok {
        switch t := v.(type) {
        case float64:
            return int64(t)
        case int64:
            return t
        case int:
            return int64(t)
        }
    }
    return 0
}
