package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"alert_framework/config"
	"alert_framework/metrics"
	"alert_framework/queue"
)

func newOpsTestServer(t *testing.T) *server {
	t.Helper()
	cfg := config.Config{
		HTTPPort:      ":0",
		CallsDir:      t.TempDir(),
		WorkDir:       t.TempDir(),
		DBPath:        t.TempDir() + "/test.db",
		JobQueueSize:  10,
		WorkerCount:   0,
		JobTimeoutSec: 1,
	}
	db, err := openDB(cfg.DBPath)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	s := &server{
		db:       db,
		queue:    queue.New(cfg.JobQueueSize, cfg.WorkerCount, time.Second, metrics.New()),
		metrics:  metrics.New(),
		client:   &http.Client{Timeout: time.Second},
		botID:    "",
		shutdown: make(chan struct{}),
		cfg:      cfg,
		tz:       time.UTC,
		ctx:      t.Context(),
		opsLogs:  newOpsLogHub(20),
	}
	s.queue.Start(t.Context())
	return s
}

func TestOpsStatusEndpoint(t *testing.T) {
	s := newOpsTestServer(t)
	_, _ = execWithRetry(s.db, `INSERT INTO transcriptions (filename, source_path, status, created_at) VALUES (?,?,?,CURRENT_TIMESTAMP)`, "a.mp3", "", statusQueued)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ops/status", nil)
	s.handleOpsStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code %d", rr.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v", err)
	}
	if _, ok := payload["queue"]; !ok {
		t.Fatalf("missing queue section")
	}
	if _, ok := payload["pipeline"]; !ok {
		t.Fatalf("missing pipeline section")
	}
}

func TestOpsTranscribeRun(t *testing.T) {
	s := newOpsTestServer(t)
	body := bytes.NewBufferString(`{"call_ids":["file1.mp3"]}`)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ops/transcribe/run", body)
	s.handleOpsRun(rr, req, "transcribe")
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rr.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["job_id"] == "" {
		t.Fatalf("missing job id")
	}
	if resp["accepted"].(float64) < 1 {
		t.Fatalf("expected accepted >0")
	}
}

func TestOpsJobPersistence(t *testing.T) {
	s := newOpsTestServer(t)
	job, err := s.recordOpsJob(t.Context(), "transcribe", map[string]string{"call_id": "persist.mp3"})
	if err != nil {
		t.Fatalf("record job: %v", err)
	}
	s.completeOpsJob(job.ID, 1, "")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ops/jobs", nil)
	s.handleOpsJobs(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestOpsLogsSSE(t *testing.T) {
	s := newOpsTestServer(t)
	job, _ := s.recordOpsJob(t.Context(), "transcribe", nil)
	s.logOps(job.ID, "info", "hello")

	mux := http.NewServeMux()
	mux.HandleFunc("/ops/jobs/"+job.ID+"/logs", s.handleOpsLogs)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := &http.Client{}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/ops/jobs/"+job.ID+"/logs", nil)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(data, []byte("hello")) {
		t.Fatalf("expected log line, got %s", string(data))
	}
}
