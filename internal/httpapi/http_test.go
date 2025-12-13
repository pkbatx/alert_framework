package httpapi

import (
    "bytes"
    "net/http"
    "net/http/httptest"
    "path/filepath"
    "testing"

    "alert_framework/internal/config"
    "alert_framework/internal/jobs"
    "alert_framework/internal/pipeline"
    "alert_framework/internal/store"
)

func setupTest(t *testing.T) (*Router, *store.Store, *jobs.Runner) {
    cfg := config.Load()
    cfg.DBPath = filepath.Join(t.TempDir(), "test.db")
    cfg.QueueSize = 8
    cfg.WorkerCount = 0
    st, err := store.Open(cfg.DBPath)
    if err != nil {
        t.Fatal(err)
    }
    reg := pipeline.BuildRegistry(cfg, st)
    runner := jobs.NewRunner(cfg, st, reg)
    router := NewRouter(cfg, st, runner)
    return router, st, runner
}

func TestOpsEnqueueEndpoint(t *testing.T) {
    router, _, runner := setupTest(t)
    mux := http.NewServeMux()
    router.Register(mux)
    body := bytes.NewBufferString(`{"call_id":"abc.mp3","stage":"INGEST","params":{}}`)
    req := httptest.NewRequest(http.MethodPost, "/ops/jobs/enqueue", body)
    rr := httptest.NewRecorder()
    mux.ServeHTTP(rr, req)
    if rr.Code != http.StatusOK {
        t.Fatalf("unexpected status %d", rr.Code)
    }
    if len(runner.Logs(0)) != 0 {
        t.Fatalf("expected no logs yet")
    }
}

func TestHealthEndpoint(t *testing.T) {
    router, _, _ := setupTest(t)
    mux := http.NewServeMux()
    router.Register(mux)
    req := httptest.NewRequest(http.MethodGet, "/ops/health", nil)
    rr := httptest.NewRecorder()
    mux.ServeHTTP(rr, req)
    if rr.Code != http.StatusNoContent {
        t.Fatalf("expected 204, got %d", rr.Code)
    }
}
