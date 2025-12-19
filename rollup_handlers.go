package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"alert_framework/queue"
)

type rollupResponse struct {
	RollupID        int64     `json:"rollup_id"`
	StartAt         time.Time `json:"start_at"`
	EndAt           time.Time `json:"end_at"`
	Latitude        float64   `json:"latitude"`
	Longitude       float64   `json:"longitude"`
	Municipality    string    `json:"municipality,omitempty"`
	POI             string    `json:"poi,omitempty"`
	Category        string    `json:"category"`
	Priority        string    `json:"priority"`
	Title           string    `json:"title,omitempty"`
	Summary         string    `json:"summary,omitempty"`
	Evidence        []string  `json:"evidence,omitempty"`
	Confidence      string    `json:"confidence,omitempty"`
	Status          string    `json:"status"`
	MergeSuggestion string    `json:"merge_suggestion,omitempty"`
	ModelName       string    `json:"model_name,omitempty"`
	ModelBaseURL    string    `json:"model_base_url,omitempty"`
	PromptVersion   string    `json:"prompt_version,omitempty"`
	CallCount       int       `json:"call_count"`
	LastError       *string   `json:"last_error,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type rollupDetailResponse struct {
	Rollup  rollupResponse `json:"rollup"`
	CallIDs []int64        `json:"call_ids"`
}

type rollupListResponse struct {
	Rollups []rollupResponse `json:"rollups"`
}

func (s *server) handleRollups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := parseIntDefault(r.URL.Query().Get("limit"), 200)
	if limit <= 0 {
		limit = 200
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	from, _ := parseTimeParam(r.URL.Query().Get("from"))
	to, _ := parseTimeParam(r.URL.Query().Get("to"))

	query := `SELECT id, start_at, end_at, latitude, longitude, municipality, poi, category, priority, title, summary, evidence_json, confidence, status, merge_suggestion, model_name, model_base_url, prompt_version, call_count, last_error, updated_at FROM rollups`
	clauses := []string{}
	args := []interface{}{}
	if !from.IsZero() {
		clauses = append(clauses, "start_at >= ?")
		args = append(args, from)
	}
	if !to.IsZero() {
		clauses = append(clauses, "end_at <= ?")
		args = append(args, to)
	}
	if status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, status)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY updated_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := queryWithRetry(s.db, query, args...)
	if err != nil {
		log.Printf("rollups query failed: %v", err)
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var rollups []rollupResponse
	for rows.Next() {
		var resp rollupResponse
		var evidenceJSON sql.NullString
		var municipality, poi, title, summary, confidence, mergeSuggestion, modelName, modelBaseURL, promptVersion sql.NullString
		var lastError sql.NullString
		if err := rows.Scan(
			&resp.RollupID,
			&resp.StartAt,
			&resp.EndAt,
			&resp.Latitude,
			&resp.Longitude,
			&municipality,
			&poi,
			&resp.Category,
			&resp.Priority,
			&title,
			&summary,
			&evidenceJSON,
			&confidence,
			&resp.Status,
			&mergeSuggestion,
			&modelName,
			&modelBaseURL,
			&promptVersion,
			&resp.CallCount,
			&lastError,
			&resp.UpdatedAt,
		); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		resp.Municipality = municipality.String
		resp.POI = poi.String
		resp.Title = title.String
		resp.Summary = summary.String
		resp.Confidence = confidence.String
		resp.MergeSuggestion = mergeSuggestion.String
		resp.ModelName = modelName.String
		resp.ModelBaseURL = modelBaseURL.String
		resp.PromptVersion = promptVersion.String
		if lastError.Valid {
			resp.LastError = &lastError.String
		}
		resp.Evidence = decodeEvidence(evidenceJSON.String)
		rollups = append(rollups, resp)
	}

	respondJSON(w, rollupListResponse{Rollups: rollups})
}

func (s *server) handleRollupDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/rollups/")
	path = strings.Trim(path, "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(path, "calls") {
		s.handleRollupCalls(w, r)
		return
	}
	id, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rollup, err := s.fetchRollup(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	callIDs, err := s.fetchRollupCallIDs(r.Context(), id)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	respondJSON(w, rollupDetailResponse{Rollup: rollup, CallIDs: callIDs})
}

func (s *server) handleRollupCalls(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/rollups/")
	path = strings.Trim(path, "/")
	if !strings.HasSuffix(path, "calls") {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	callIDs, err := s.fetchRollupCallIDs(r.Context(), id)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if len(callIDs) == 0 {
		respondJSON(w, map[string]interface{}{"calls": []transcriptionResponse{}})
		return
	}
	placeholders := strings.Repeat("?,", len(callIDs))
	placeholders = strings.TrimSuffix(placeholders, ",")
	query := fmt.Sprintf(`SELECT id, filename, source_path, processed_path, COALESCE(ingest_source,'') as ingest_source, transcript_text, raw_transcript_text, clean_transcript_text, translation_text, status, last_error, size_bytes, duration_seconds, hash, duplicate_of, requested_model, requested_mode, requested_format, actual_openai_model_used, diarized_json, recognized_towns, normalized_transcript, call_type, call_timestamp, tags, latitude, longitude, location_label, location_source, refined_metadata, address_json, needs_manual_review, created_at, updated_at FROM transcriptions WHERE id IN (%s)`, placeholders)

	args := make([]interface{}, 0, len(callIDs))
	for _, id := range callIDs {
		args = append(args, id)
	}
	rows, err := queryWithRetry(s.db, query, args...)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	baseURL := s.resolveBaseURL(r)
	var calls []transcriptionResponse
	for rows.Next() {
		var t transcription
		if err := scanTranscription(rows, &t); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		calls = append(calls, s.toResponse(t, baseURL))
	}

	respondJSON(w, map[string]interface{}{"calls": calls})
}

func (s *server) handleRollupRecompute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.queue == nil || s.rollups == nil {
		http.Error(w, "rollup workers disabled", http.StatusServiceUnavailable)
		return
	}
	enqueued := s.enqueueRollupJob("api")
	respondJSON(w, map[string]interface{}{"status": "queued", "enqueued": enqueued})
}

func (s *server) fetchRollup(ctx context.Context, id int64) (rollupResponse, error) {
	var resp rollupResponse
	var evidenceJSON sql.NullString
	var municipality, poi, title, summary, confidence, mergeSuggestion, modelName, modelBaseURL, promptVersion sql.NullString
	var lastError sql.NullString
	row := s.db.QueryRowContext(ctx, `SELECT id, start_at, end_at, latitude, longitude, municipality, poi, category, priority, title, summary, evidence_json, confidence, status, merge_suggestion, model_name, model_base_url, prompt_version, call_count, last_error, updated_at FROM rollups WHERE id = ?`, id)
	if err := row.Scan(
		&resp.RollupID,
		&resp.StartAt,
		&resp.EndAt,
		&resp.Latitude,
		&resp.Longitude,
		&municipality,
		&poi,
		&resp.Category,
		&resp.Priority,
		&title,
		&summary,
		&evidenceJSON,
		&confidence,
		&resp.Status,
		&mergeSuggestion,
		&modelName,
		&modelBaseURL,
		&promptVersion,
		&resp.CallCount,
		&lastError,
		&resp.UpdatedAt,
	); err != nil {
		return resp, err
	}
	resp.Municipality = municipality.String
	resp.POI = poi.String
	resp.Title = title.String
	resp.Summary = summary.String
	resp.Confidence = confidence.String
	resp.MergeSuggestion = mergeSuggestion.String
	resp.ModelName = modelName.String
	resp.ModelBaseURL = modelBaseURL.String
	resp.PromptVersion = promptVersion.String
	resp.Evidence = decodeEvidence(evidenceJSON.String)
	if lastError.Valid {
		resp.LastError = &lastError.String
	}
	return resp, nil
}

func (s *server) fetchRollupCallIDs(ctx context.Context, id int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT call_id FROM rollup_calls WHERE rollup_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var callID int64
		if err := rows.Scan(&callID); err != nil {
			return nil, err
		}
		ids = append(ids, callID)
	}
	return ids, nil
}

func decodeEvidence(raw string) []string {
	var out []string
	if strings.TrimSpace(raw) == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []string{}
	}
	return out
}

func parseTimeParam(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, nil
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts, nil
	}
	if unix, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(unix, 0).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid time")
}

func parseIntDefault(value string, fallback int) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func (s *server) startRollupScheduler(ctx context.Context) {
	interval := time.Duration(s.cfg.Rollup.RefreshIntervalSec) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	go func() {
		_ = s.enqueueRollupJob("startup")
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.shutdown:
				return
			case <-ticker.C:
				_ = s.enqueueRollupJob("interval")
			}
		}
	}()
}

func (s *server) enqueueRollupJob(source string) bool {
	if s.queue == nil || s.rollups == nil {
		return false
	}
	s.rollupMu.Lock()
	if s.rollupEnqueued {
		s.rollupMu.Unlock()
		return false
	}
	s.rollupEnqueued = true
	s.rollupMu.Unlock()

	job := queue.Job{
		ID:       "rollup-recompute",
		Source:   source,
		FileName: "rollup-recompute",
		Work: func(ctx context.Context) error {
			_, err := s.rollups.Recompute(ctx)
			return err
		},
		OnFinish: func(err error) {
			s.rollupMu.Lock()
			s.rollupEnqueued = false
			s.rollupMu.Unlock()
			if err != nil {
				log.Printf("rollup recompute failed: %v", err)
			}
		},
	}
	enqueued := s.queue.Enqueue(job)
	if !enqueued {
		s.rollupMu.Lock()
		s.rollupEnqueued = false
		s.rollupMu.Unlock()
	}
	return enqueued
}
