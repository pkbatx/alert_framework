package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type opsJob struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Status     string         `json:"status"`
	Payload    sql.NullString `json:"payload"`
	Accepted   int            `json:"accepted"`
	CreatedAt  time.Time      `json:"created_at"`
	StartedAt  sql.NullTime   `json:"started_at"`
	FinishedAt sql.NullTime   `json:"finished_at"`
	LastError  sql.NullString `json:"last_error"`
}

type opsJobLog struct {
	Timestamp time.Time `json:"ts"`
	Level     string    `json:"level"`
	Message   string    `json:"msg"`
}

type opsLogHub struct {
	mu          sync.Mutex
	ring        map[string][]opsJobLog
	subscribers map[string]map[chan opsJobLog]struct{}
	size        int
}

func newOpsLogHub(size int) *opsLogHub {
	return &opsLogHub{ring: make(map[string][]opsJobLog), subscribers: make(map[string]map[chan opsJobLog]struct{}), size: size}
}

func (h *opsLogHub) append(jobID string, evt opsJobLog) {
	h.mu.Lock()
	defer h.mu.Unlock()
	buf := append(h.ring[jobID], evt)
	if len(buf) > h.size {
		buf = buf[len(buf)-h.size:]
	}
	h.ring[jobID] = buf
	for ch := range h.subscribers[jobID] {
		select {
		case ch <- evt:
		default:
		}
	}
}

func (h *opsLogHub) subscribe(jobID string) (chan opsJobLog, []opsJobLog) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan opsJobLog, h.size)
	if h.subscribers[jobID] == nil {
		h.subscribers[jobID] = make(map[chan opsJobLog]struct{})
	}
	h.subscribers[jobID][ch] = struct{}{}
	snapshot := append([]opsJobLog(nil), h.ring[jobID]...)
	return ch, snapshot
}

func (h *opsLogHub) unsubscribe(jobID string, ch chan opsJobLog) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if subs := h.subscribers[jobID]; subs != nil {
		delete(subs, ch)
		close(ch)
	}
}

func (s *server) recordOpsJob(ctx context.Context, jobType string, payload interface{}) (*opsJob, error) {
	id := uuid.NewString()
	var payloadStr sql.NullString
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			payloadStr = sql.NullString{String: string(b), Valid: true}
		}
	}
	_, err := execWithRetry(s.db, `INSERT INTO ops_job (id, type, status, payload, created_at) VALUES (?, ?, 'running', ?, CURRENT_TIMESTAMP)`, id, jobType, payloadStr)
	if err != nil {
		return nil, err
	}
	return &opsJob{ID: id, Type: jobType, Status: "running", Payload: payloadStr, CreatedAt: time.Now()}, nil
}

func (s *server) completeOpsJob(id string, accepted int, errMsg string) {
	status := "succeeded"
	var lastErr sql.NullString
	if errMsg != "" {
		status = "failed"
		lastErr = sql.NullString{String: errMsg, Valid: true}
	}
	_, err := execWithRetry(s.db, `UPDATE ops_job SET status = ?, accepted = ?, last_error = ?, finished_at = CURRENT_TIMESTAMP WHERE id = ?`, status, accepted, lastErr, id)
	if err != nil {
		log.Printf("update ops job %s: %v", id, err)
	}
}

func (s *server) logOps(jobID, level, msg string) {
	evt := opsJobLog{Timestamp: time.Now(), Level: level, Message: msg}
	_, err := execWithRetry(s.db, `INSERT INTO ops_job_log (job_id, ts, level, message) VALUES (?, ?, ?, ?)`, jobID, evt.Timestamp, level, msg)
	if err != nil {
		log.Printf("persist ops log failed: %v", err)
	}
	if s.opsLogs != nil {
		s.opsLogs.append(jobID, evt)
	}
}

func (s *server) handleOpsStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	version := strings.TrimSpace(os.Getenv("GIT_SHA"))
	if version == "" {
		version = "dev"
	}

	qStats := s.queue.Stats()
	s.metrics.UpdateQueue(qStats.Length, qStats.Capacity, qStats.WorkerCount)
	mSnap := s.metrics.Snapshot()

	running := 0
	s.running.Range(func(_, _ interface{}) bool { running++; return true })

	dbStatus := map[string]interface{}{"db_ok": true, "db_path": s.cfg.DBPath}
	if err := s.db.Ping(); err != nil {
		dbStatus["db_ok"] = false
		dbStatus["last_db_error"] = err.Error()
	}

	filesSeen := countRows(s.db, "SELECT COUNT(1) FROM transcriptions")
	successCount := countRows(s.db, "SELECT COUNT(1) FROM transcriptions WHERE status = ?", statusDone)
	failCount := countRows(s.db, "SELECT COUNT(1) FROM transcriptions WHERE status = ?", statusError)
	lastErr := nullableStringFromDB(s.db, "SELECT last_error FROM transcriptions WHERE last_error IS NOT NULL AND last_error != '' ORDER BY updated_at DESC LIMIT 1")
	lastCall := nullableTimeFromDB(s.db, "SELECT COALESCE(MAX(call_timestamp), MAX(created_at)) FROM transcriptions")
	queuedOldest := nullableTimeFromDB(s.db, "SELECT MIN(created_at) FROM transcriptions WHERE status = ?", statusQueued)
	groupmeCount := successCount
	jobsTotal := mSnap.ProcessedJobs
	var oldestAge string
	if queuedOldest != nil {
		oldestAge = time.Since(*queuedOldest).String()
	}

	summary := map[string]interface{}{
		"version": version,
		"config": map[string]interface{}{
			"CALLS_DIR":      s.cfg.CallsDir,
			"WORK_DIR":       s.cfg.WorkDir,
			"DB_PATH":        s.cfg.DBPath,
			"WORKER_COUNT":   s.cfg.WorkerCount,
			"JOB_QUEUE_SIZE": s.cfg.JobQueueSize,
		},
		"queue": map[string]interface{}{
			"queued":       qStats.Length,
			"running":      running,
			"succeeded":    jobsTotal - mSnap.FailedJobs,
			"failed":       mSnap.FailedJobs,
			"worker_count": qStats.WorkerCount,
			"oldest_age":   oldestAge,
		},
		"pipeline": map[string]interface{}{
			"files_seen_total":             filesSeen,
			"jobs_enqueued_total":          jobsTotal,
			"transcriptions_success_total": successCount,
			"transcriptions_fail_total":    failCount,
			"last_transcription_error":     lastErr,
			"groupme_sent_total":           groupmeCount,
			"last_call_ts":                 lastCall,
		},
		"db": dbStatus,
	}
	respondJSON(w, summary)
}

func countRows(db *sql.DB, query string, args ...interface{}) int64 {
	var n sql.NullInt64
	if err := queryRowWithRetry(db, func(row *sql.Row) error { return row.Scan(&n) }, query, args...); err != nil {
		return 0
	}
	if n.Valid {
		return n.Int64
	}
	return 0
}

func nullableStringFromDB(db *sql.DB, query string, args ...interface{}) *string {
	var v sql.NullString
	if err := queryRowWithRetry(db, func(row *sql.Row) error { return row.Scan(&v) }, query, args...); err != nil {
		return nil
	}
	if v.Valid {
		val := v.String
		if len(val) > 160 {
			val = val[:157] + "…"
		}
		return &val
	}
	return nil
}

func nullableTimeFromDB(db *sql.DB, query string, args ...interface{}) *time.Time {
	var v sql.NullTime
	if err := queryRowWithRetry(db, func(row *sql.Row) error { return row.Scan(&v) }, query, args...); err != nil {
		return nil
	}
	if v.Valid {
		t := v.Time
		return &t
	}
	return nil
}

type opsRequest struct {
	SinceMinutes int      `json:"since_minutes"`
	Limit        int      `json:"limit"`
	CallIDs      []string `json:"call_ids"`
	Force        bool     `json:"force"`
	Destination  string   `json:"destination"`
	Stage        string   `json:"stage"`
	CallID       string   `json:"call_id"`
}

func (s *server) decodeOpsRequest(r *http.Request) (opsRequest, error) {
	var req opsRequest
	if r.Body != nil {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			return req, err
		}
	}
	return req, nil
}

func (s *server) handleOpsRun(w http.ResponseWriter, r *http.Request, jobType string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	req, err := s.decodeOpsRequest(r)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	job, err := s.recordOpsJob(r.Context(), jobType, req)
	if err != nil {
		http.Error(w, "failed to record job", http.StatusInternalServerError)
		return
	}
	opts, _ := s.defaultOptions()
	cutoff := time.Duration(req.SinceMinutes) * time.Minute
	callIDs := req.CallIDs
	if len(callIDs) == 0 {
		if cutoff == 0 {
			cutoff = 60 * time.Minute
		}
		callIDs = s.lookupCallIDs(cutoff, req.Limit)
	}
	destination := strings.ToLower(strings.TrimSpace(req.Destination))
	if destination == "" {
		destination = "both"
	}
	sendGroupMe := jobType == "publish" && (destination == "both" || destination == "groupme")
	accepted := 0
	for _, id := range callIDs {
		filename := strings.TrimSpace(id)
		if filename == "" {
			continue
		}
		enqueued := s.queueJob("ops:"+jobType, filename, sendGroupMe, req.Force, opts)
		if enqueued {
			accepted++
			s.logOps(job.ID, "info", fmt.Sprintf("enqueued %s", filename))
		}
	}
	s.completeOpsJob(job.ID, accepted, "")
	respondJSON(w, map[string]interface{}{"job_id": job.ID, "accepted": accepted})
}

func (s *server) lookupCallIDs(since time.Duration, limit int) []string {
	if limit <= 0 {
		limit = 200
	}
	cutoff := time.Now().UTC().Add(-since)
	rows, err := queryWithRetry(s.db, `SELECT filename FROM transcriptions WHERE COALESCE(call_timestamp, created_at) >= ? ORDER BY COALESCE(call_timestamp, created_at) DESC LIMIT ?`, cutoff, limit)
	if err != nil {
		log.Printf("call lookup failed: %v", err)
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *server) handleOpsReprocess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	req, err := s.decodeOpsRequest(r)
	if err != nil || req.CallID == "" || req.Stage == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	job, err := s.recordOpsJob(r.Context(), "reprocess", req)
	if err != nil {
		http.Error(w, "failed to record job", http.StatusInternalServerError)
		return
	}
	opts, _ := s.defaultOptions()
	stage := strings.ToLower(req.Stage)
	sendGroupMe := stage == "publish"
	accepted := 0
	if s.queueJob("ops:"+stage, req.CallID, sendGroupMe, req.Force, opts) {
		accepted = 1
		s.logOps(job.ID, "info", fmt.Sprintf("reprocess %s at %s", req.CallID, stage))
	}
	s.completeOpsJob(job.ID, accepted, "")
	respondJSON(w, map[string]interface{}{"job_id": job.ID, "accepted": accepted})
}

func (s *server) handleOpsJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rows, err := queryWithRetry(s.db, `SELECT id, type, status, payload, accepted, created_at, started_at, finished_at, last_error FROM ops_job ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var jobs []opsJob
	for rows.Next() {
		var j opsJob
		if err := rows.Scan(&j.ID, &j.Type, &j.Status, &j.Payload, &j.Accepted, &j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.LastError); err == nil {
			jobs = append(jobs, j)
		}
	}
	respondJSON(w, map[string]interface{}{"jobs": jobs})
}

func (s *server) handleOpsJobDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jobID := strings.TrimPrefix(r.URL.Path, "/ops/jobs/")
	jobID = strings.TrimSuffix(jobID, "/logs")
	var j opsJob
	err := queryRowWithRetry(s.db, func(row *sql.Row) error {
		return row.Scan(&j.ID, &j.Type, &j.Status, &j.Payload, &j.Accepted, &j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.LastError)
	}, `SELECT id, type, status, payload, accepted, created_at, started_at, finished_at, last_error FROM ops_job WHERE id = ?`, jobID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	logs := s.fetchOpsLogs(jobID, 200)
	respondJSON(w, map[string]interface{}{"job": j, "logs": logs})
}

func (s *server) fetchOpsLogs(jobID string, limit int) []opsJobLog {
	rows, err := queryWithRetry(s.db, `SELECT ts, level, message FROM ops_job_log WHERE job_id = ? ORDER BY ts ASC LIMIT ?`, jobID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var logs []opsJobLog
	for rows.Next() {
		var evt opsJobLog
		if err := rows.Scan(&evt.Timestamp, &evt.Level, &evt.Message); err == nil {
			logs = append(logs, evt)
		}
	}
	return logs
}

func (s *server) handleOpsLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jobID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/ops/jobs/"), "/logs")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, snapshot := s.opsLogs.subscribe(jobID)
	defer s.opsLogs.unsubscribe(jobID, ch)
	send := func(evt opsJobLog) {
		data := fmt.Sprintf("data: {\"ts\":%q,\"level\":%q,\"msg\":%q}\n\n", evt.Timestamp.Format(time.RFC3339), evt.Level, strings.ReplaceAll(evt.Message, "\"", "'"))
		_, _ = w.Write([]byte(data))
		flusher.Flush()
	}
	for _, evt := range snapshot {
		send(evt)
	}
	notify := w.(http.CloseNotifier).CloseNotify()
	for {
		select {
		case evt := <-ch:
			send(evt)
		case <-notify:
			return
		case <-r.Context().Done():
			return
		}
	}
}

func (s *server) handleOpsReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if os.Getenv("ENABLE_DANGEROUS_OPS") != "1" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	job, err := s.recordOpsJob(r.Context(), "reset", nil)
	if err != nil {
		http.Error(w, "failed", http.StatusInternalServerError)
		return
	}
	s.queue.Reset()
	_, _ = execWithRetry(s.db, `UPDATE transcriptions SET status = ?, last_error = 'reset by operator' WHERE status IN (?, ?)`, statusError, statusQueued, statusProcessing)
	s.logOps(job.ID, "warn", "queue drained and in-flight jobs marked as error")
	s.completeOpsJob(job.ID, 0, "")
	respondJSON(w, map[string]interface{}{"job_id": job.ID, "status": "ok"})
}
