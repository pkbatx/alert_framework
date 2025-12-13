package app

import (
    "context"
    "log"
    "net/http"

    "alert_framework/internal/config"
    "alert_framework/internal/httpapi"
    "alert_framework/internal/jobs"
    "alert_framework/internal/pipeline"
    "alert_framework/internal/store"
    "alert_framework/internal/watch"
)

// App wires the data plane components together.
type App struct {
    cfg     config.Config
    store   *store.Store
    runner  *jobs.Runner
    watcher *watch.Watcher
    mux     *http.ServeMux
}

func New(cfg config.Config) (*App, error) {
    st, err := store.Open(cfg.DBPath)
    if err != nil {
        return nil, err
    }
    registry := pipeline.BuildRegistry(cfg, st)
    runner := jobs.NewRunner(cfg, st, registry)
    watcher := watch.New(cfg, runner)
    mux := http.NewServeMux()
    router := httpapi.NewRouter(cfg, st, runner)
    router.Register(mux)
    return &App{cfg: cfg, store: st, runner: runner, watcher: watcher, mux: mux}, nil
}

// Run starts workers, watcher, and HTTP server.
func (a *App) Run(ctx context.Context) error {
    a.runner.Start(ctx)
    if err := a.watcher.Start(ctx); err != nil {
        return err
    }
    srv := &http.Server{Addr: ":" + a.cfg.HTTPPort, Handler: a.mux}
    go func() {
        <-ctx.Done()
        _ = srv.Shutdown(context.Background())
    }()
    log.Printf("http listening on %s", a.cfg.HTTPPort)
    return srv.ListenAndServe()
}

// EnqueueStage exposes pipeline stage for tests/control plane.
func (a *App) EnqueueStage(ctx context.Context, callID string, stage jobs.Stage, params map[string]any) (*store.Job, error) {
    return a.runner.Enqueue(ctx, callID, stage, params)
}

func (a *App) Runner() *jobs.Runner { return a.runner }
func (a *App) Store() *store.Store { return a.store }
func (a *App) Mux() *http.ServeMux { return a.mux }
