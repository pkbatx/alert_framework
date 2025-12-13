package jobs

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "sync"
    "time"

    "alert_framework/internal/config"
    "alert_framework/internal/store"
)

// Status values for jobs.
const (
    StatusQueued    = "queued"
    StatusRunning   = "running"
    StatusSucceeded = "succeeded"
    StatusFailed    = "failed"
    StatusCancelled = "cancelled"
)

// Stage represents pipeline phases.
type Stage string

const (
    StageIngest       Stage = "INGEST"
    StagePreprocess   Stage = "PREPROCESS"
    StageAlertInitial Stage = "ALERT_INITIAL"
    StageTranscribe   Stage = "TRANSCRIBE"
    StageNormalize    Stage = "NORMALIZE"
    StageEnrich       Stage = "ENRICH"
    StagePublish      Stage = "PUBLISH"
)

// ExecutionContext bundles dependencies for stage execution.
type ExecutionContext struct {
    Cfg   config.Config
    Store *store.Store
    Logf  func(jobID int64, msg string)
}

// StageFunc is a deterministic stage implementation.
type StageFunc func(ctx context.Context, execCtx ExecutionContext, callID string, params map[string]any) error

// Registry maps stages to implementations.
type Registry map[Stage]StageFunc

// Runner executes jobs using worker pool.
type Runner struct {
    cfg       config.Config
    store     *store.Store
    reg       Registry
    queue     chan *store.Job
    wg        sync.WaitGroup
    cancel    context.CancelFunc
    logMu     sync.Mutex
    logBuffer map[int64][]string
}

// NewRunner constructs a runner.
func NewRunner(cfg config.Config, st *store.Store, reg Registry) *Runner {
    r := &Runner{
        cfg:       cfg,
        store:     st,
        reg:       reg,
        queue:     make(chan *store.Job, cfg.QueueSize),
        logBuffer: make(map[int64][]string),
    }
    return r
}

// Start spins worker pool.
func (r *Runner) Start(ctx context.Context) {
    ctx, cancel := context.WithCancel(ctx)
    r.cancel = cancel
    for i := 0; i < r.cfg.WorkerCount; i++ {
        r.wg.Add(1)
        go r.worker(ctx)
    }
}

// Stop waits for workers to finish.
func (r *Runner) Stop() {
    if r.cancel != nil {
        r.cancel()
    }
    r.wg.Wait()
}

// Enqueue inserts a job respecting idempotency.
func (r *Runner) Enqueue(ctx context.Context, callID string, stage Stage, params map[string]any) (*store.Job, error) {
    idem := r.idempotencyKey(callID, stage, params)
    payload, _ := json.Marshal(params)
    job := &store.Job{
        CallID:         callID,
        Stage:          string(stage),
        Status:         StatusQueued,
        ParamsJSON:     string(payload),
        IdempotencyKey: idem,
        CreatedAt:      config.Now(),
        UpdatedAt:      config.Now(),
    }
    j, err := r.store.InsertJobIdempotent(ctx, job)
    if err == store.ErrConflict {
        return j, nil
    }
    if err != nil {
        return nil, err
    }
    select {
    case r.queue <- j:
        return j, nil
    default:
        return nil, fmt.Errorf("queue full")
    }
}

func (r *Runner) worker(ctx context.Context) {
    defer r.wg.Done()
    for {
        select {
        case <-ctx.Done():
            return
        case job := <-r.queue:
            r.execute(ctx, job)
        }
    }
}

func (r *Runner) execute(ctx context.Context, job *store.Job) {
    stage := Stage(job.Stage)
    fn, ok := r.reg[stage]
    if !ok {
        r.appendLog(job.ID, "no handler for stage")
        _ = r.store.MarkJobFinished(ctx, job.ID, StatusFailed, config.Now())
        return
    }
    _ = r.store.MarkJobStarted(ctx, job.ID, config.Now())
    execCtx := ExecutionContext{Cfg: r.cfg, Store: r.store, Logf: func(id int64, msg string) { r.appendLog(id, msg) }}
    params := map[string]any{}
    _ = json.Unmarshal([]byte(job.ParamsJSON), &params)
    if err := fn(ctx, execCtx, job.CallID, params); err != nil {
        r.appendLog(job.ID, "error: "+err.Error())
        _ = r.store.MarkJobFinished(ctx, job.ID, StatusFailed, config.Now())
        return
    }
    _ = r.store.MarkJobFinished(ctx, job.ID, StatusSucceeded, config.Now())
}

func (r *Runner) appendLog(jobID int64, msg string) {
    r.logMu.Lock()
    defer r.logMu.Unlock()
    ts := config.Now()
    _ = r.store.AppendJobLog(context.Background(), jobID, msg, ts)
    r.logBuffer[jobID] = append(r.logBuffer[jobID], fmt.Sprintf("%s %s", ts.Format(time.RFC3339), msg))
    if len(r.logBuffer[jobID]) > 200 {
        r.logBuffer[jobID] = r.logBuffer[jobID][len(r.logBuffer[jobID])-200:]
    }
}

// Logs returns in-memory log buffer for SSE streaming.
func (r *Runner) Logs(jobID int64) []string {
    r.logMu.Lock()
    defer r.logMu.Unlock()
    return append([]string(nil), r.logBuffer[jobID]...)
}

func (r *Runner) idempotencyKey(callID string, stage Stage, params map[string]any) string {
    payload, _ := json.Marshal(params)
    h := sha256.Sum256([]byte(callID + string(stage) + string(payload)))
    return hex.EncodeToString(h[:])
}
