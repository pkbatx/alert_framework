package httpapi

import (
    "encoding/json"
    "log"
    "net/http"
    "strconv"
    "strings"

    "alert_framework/internal/config"
    "alert_framework/internal/jobs"
    "alert_framework/internal/store"
)

// Router builds HTTP handlers for /api and /ops.
type Router struct {
    cfg    config.Config
    store  *store.Store
    runner *jobs.Runner
}

func NewRouter(cfg config.Config, st *store.Store, runner *jobs.Runner) *Router {
    return &Router{cfg: cfg, store: st, runner: runner}
}

func (r *Router) Register(mux *http.ServeMux) {
    mux.HandleFunc("/ops/status", r.status)
    mux.HandleFunc("/ops/jobs", r.jobs)
    mux.HandleFunc("/ops/jobs/enqueue", r.enqueue)
    mux.HandleFunc("/ops/jobs/", r.jobDetail)
    mux.HandleFunc("/api/calls", r.calls)
    mux.HandleFunc("/ops/health", r.health)
    mux.HandleFunc("/ops/backfill", r.backfill)
    mux.HandleFunc("/ops/reprocess", r.reprocess)
    mux.HandleFunc("/ops/briefing-data", r.briefing)
    mux.HandleFunc("/ops/anomalies", r.anomalies)
}

func (r *Router) status(w http.ResponseWriter, req *http.Request) {
    ctx := req.Context()
    calls, _ := r.store.ListCalls(ctx, 5)
    jobs, _ := r.store.ListJobs(ctx, 10)
    respondJSON(w, map[string]any{"calls": calls, "jobs": jobs, "workers": r.cfg.WorkerCount})
}

func (r *Router) jobs(w http.ResponseWriter, req *http.Request) {
    list, err := r.store.ListJobs(req.Context(), 50)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    respondJSON(w, list)
}

func (r *Router) calls(w http.ResponseWriter, req *http.Request) {
    list, err := r.store.ListCalls(req.Context(), 100)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    respondJSON(w, list)
}

func (r *Router) enqueue(w http.ResponseWriter, req *http.Request) {
    if req.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    var body struct {
        CallID string      `json:"call_id"`
        Stage  jobs.Stage  `json:"stage"`
        Params interface{} `json:"params"`
    }
    if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    p, ok := body.Params.(map[string]any)
    if !ok {
        p = map[string]any{}
    }
    job, err := r.runner.Enqueue(req.Context(), body.CallID, body.Stage, p)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    respondJSON(w, job)
}

func (r *Router) jobDetail(w http.ResponseWriter, req *http.Request) {
    // /ops/jobs/{id}/logs or detail
    path := req.URL.Path
    if strings.HasSuffix(path, "/logs") {
        idStr := strings.TrimSuffix(strings.TrimPrefix(path, "/ops/jobs/"), "/logs")
        id, _ := strconv.ParseInt(idStr, 10, 64)
        logs := r.runner.Logs(id)
        respondJSON(w, logs)
        return
    }
    idStr := strings.TrimPrefix(path, "/ops/jobs/")
    id, _ := strconv.ParseInt(idStr, 10, 64)
    jobs, err := r.store.ListJobs(req.Context(), 200)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    for _, j := range jobs {
        if j.ID == id {
            respondJSON(w, j)
            return
        }
    }
    http.NotFound(w, req)
}

func (r *Router) backfill(w http.ResponseWriter, req *http.Request) {
    if req.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    respondJSON(w, map[string]any{"status": "queued"})
}

func (r *Router) reprocess(w http.ResponseWriter, req *http.Request) {
    if req.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    var body struct {
        CallID string     `json:"call_id"`
        Stage  jobs.Stage `json:"stage"`
        Force  bool       `json:"force"`
    }
    if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    job, err := r.runner.Enqueue(req.Context(), body.CallID, body.Stage, map[string]any{"force": body.Force})
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    respondJSON(w, job)
}

func (r *Router) briefing(w http.ResponseWriter, req *http.Request) {
    calls, _ := r.store.ListCalls(req.Context(), 100)
    respondJSON(w, map[string]any{"total_calls": len(calls), "calls": calls})
}

func (r *Router) anomalies(w http.ResponseWriter, req *http.Request) {
    respondJSON(w, map[string]any{"anomalies": []string{}})
}

func (r *Router) health(w http.ResponseWriter, req *http.Request) {
    if err := r.store.Health(req.Context()); err != nil {
        http.Error(w, err.Error(), http.StatusServiceUnavailable)
        return
    }
    w.WriteHeader(http.StatusNoContent)
}

func respondJSON(w http.ResponseWriter, payload interface{}) {
    w.Header().Set("Content-Type", "application/json")
    if err := json.NewEncoder(w).Encode(payload); err != nil {
        log.Printf("write json: %v", err)
    }
}
