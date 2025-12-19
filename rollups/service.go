package rollups

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"alert_framework/config"
)

const statusDone = "done"

type Service struct {
	db     *sql.DB
	cfg    config.RollupConfig
	client *http.Client
}

func NewService(db *sql.DB, client *http.Client, cfg config.RollupConfig) *Service {
	return &Service{db: db, cfg: cfg, client: client}
}

func (s *Service) Recompute(ctx context.Context) (RunResult, error) {
	runID, err := s.startRun(ctx)
	if err != nil {
		log.Printf("rollup run start failed: %v", err)
	}

	calls, err := s.loadCalls(ctx)
	if err != nil {
		s.finishRun(ctx, runID, "failed", err.Error(), 0)
		return RunResult{Status: "failed", Error: err.Error()}, err
	}

	clusters := groupCalls(calls, time.Duration(s.cfg.ChainWindowMin)*time.Minute, s.cfg.RadiusMeters, s.cfg.MaxCalls)
	count := 0
	for _, clusterCalls := range clusters {
		rollup, err := s.buildRollup(clusterCalls)
		if err != nil {
			log.Printf("rollup build failed: %v", err)
			continue
		}
		if err := s.upsertRollup(ctx, rollup, clusterCalls); err != nil {
			log.Printf("rollup upsert failed: %v", err)
			continue
		}
		count++
	}

	s.finishRun(ctx, runID, "ok", "", count)
	return RunResult{RollupCount: count, Status: "ok"}, nil
}

func (s *Service) loadCalls(ctx context.Context) ([]CallRecord, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(s.cfg.LookbackHours) * time.Hour)
	query := `SELECT id, filename, COALESCE(call_timestamp, created_at) as call_ts, call_type, clean_transcript_text, transcript_text, normalized_transcript, latitude, longitude, location_label, address_json, refined_metadata
FROM transcriptions
WHERE status = ?
  AND latitude IS NOT NULL
  AND longitude IS NOT NULL
  AND latitude != 0
  AND longitude != 0
  AND COALESCE(call_timestamp, created_at) >= ?
ORDER BY call_ts ASC`

	rows, err := s.db.QueryContext(ctx, query, statusDone, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CallRecord
	for rows.Next() {
		var rec CallRecord
		var clean, raw, normalized, locationLabel, addressJSON, refinedJSON sql.NullString
		if err := rows.Scan(&rec.ID, &rec.Filename, &rec.Timestamp, &rec.CallType, &clean, &raw, &normalized, &rec.Latitude, &rec.Longitude, &locationLabel, &addressJSON, &refinedJSON); err != nil {
			return nil, err
		}
		rec.CleanTranscript = clean.String
		rec.RawTranscript = raw.String
		rec.Normalized = normalized.String
		rec.LocationLabel = locationLabel.String
		rec.AddressJSON = addressJSON.String
		rec.RefinedJSON = refinedJSON.String
		out = append(out, rec)
	}
	return out, nil
}

func (s *Service) buildRollup(calls []CallRecord) (Rollup, error) {
	if len(calls) == 0 {
		return Rollup{}, fmt.Errorf("empty rollup")
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].Timestamp.Before(calls[j].Timestamp) })

	callIDs := make([]int64, 0, len(calls))
	for _, call := range calls {
		callIDs = append(callIDs, call.ID)
	}
	startAt := calls[0].Timestamp
	endAt := calls[len(calls)-1].Timestamp

	rollup := Rollup{
		Key:           rollupKey(callIDs),
		StartAt:       startAt,
		EndAt:         endAt,
		Latitude:      averageCoordinate(calls, true),
		Longitude:     averageCoordinate(calls, false),
		Municipality:  majorityString(calls, normalizeMunicipality),
		POI:           majorityString(calls, normalizePOI),
		Category:      deriveCategory(calls),
		Priority:      derivePriority(calls),
		Evidence:      []string{},
		CallIDs:       callIDs,
		CallCount:     len(callIDs),
		PromptVersion: s.cfg.PromptVersion,
		ModelName:     s.cfg.LLMModel,
		ModelBaseURL:  s.cfg.LLMBaseURL,
		Status:        StatusLLMSkipped,
	}
	return rollup, nil
}

func (s *Service) upsertRollup(ctx context.Context, rollup Rollup, calls []CallRecord) error {
	if rollup.Key == "" {
		return fmt.Errorf("missing rollup key")
	}

	if s.cfg.LLMEnabled {
		llmCalls := calls
		if s.cfg.MaxCalls > 0 && len(llmCalls) > s.cfg.MaxCalls {
			llmCalls = llmCalls[:s.cfg.MaxCalls]
		}
		llmResult, baseURL, err := s.tryLLM(ctx, rollup, llmCalls)
		if err != nil {
			rollup.Status = StatusLLMFailed
			rollup.LastError = truncateError(err.Error())
		} else {
			rollup.Title = llmResult.Title
			rollup.Summary = llmResult.Summary
			rollup.Evidence = llmResult.Evidence
			rollup.MergeSuggestion = llmResult.MergeSuggestion
			rollup.Confidence = llmResult.Confidence
			rollup.ModelBaseURL = baseURL
			rollup.Status = StatusLLMOK
		}
	} else {
		rollup.Status = StatusLLMSkipped
	}

	evidenceJSON, _ := json.Marshal(rollup.Evidence)
	query := `INSERT INTO rollups (
rollup_key, start_at, end_at, latitude, longitude, municipality, poi, category, priority, title, summary, evidence_json, confidence, status, merge_suggestion, model_name, model_base_url, prompt_version, call_count, last_error
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(rollup_key) DO UPDATE SET
start_at=excluded.start_at,
end_at=excluded.end_at,
latitude=excluded.latitude,
longitude=excluded.longitude,
municipality=excluded.municipality,
poi=excluded.poi,
category=excluded.category,
priority=excluded.priority,
title=excluded.title,
summary=excluded.summary,
evidence_json=excluded.evidence_json,
confidence=excluded.confidence,
status=excluded.status,
merge_suggestion=excluded.merge_suggestion,
model_name=excluded.model_name,
model_base_url=excluded.model_base_url,
prompt_version=excluded.prompt_version,
call_count=excluded.call_count,
last_error=excluded.last_error,
updated_at=CURRENT_TIMESTAMP`

	_, err := s.db.ExecContext(ctx, query,
		rollup.Key,
		rollup.StartAt,
		rollup.EndAt,
		rollup.Latitude,
		rollup.Longitude,
		nullableString(rollup.Municipality),
		nullableString(rollup.POI),
		rollup.Category,
		rollup.Priority,
		nullableString(rollup.Title),
		nullableString(rollup.Summary),
		string(evidenceJSON),
		nullableString(rollup.Confidence),
		rollup.Status,
		nullableString(rollup.MergeSuggestion),
		nullableString(rollup.ModelName),
		nullableString(rollup.ModelBaseURL),
		nullableString(rollup.PromptVersion),
		rollup.CallCount,
		nullableString(rollup.LastError),
	)
	if err != nil {
		return err
	}

	rollupID, err := s.lookupRollupID(ctx, rollup.Key)
	if err != nil {
		return err
	}
	if err := s.replaceRollupCalls(ctx, rollupID, rollup.CallIDs); err != nil {
		return err
	}
	return nil
}

func (s *Service) tryLLM(ctx context.Context, rollup Rollup, calls []CallRecord) (LLMOutput, string, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	return callRollupLLM(ctx, s.client, s.cfg.LLMModel, s.cfg.LLMBaseURL, apiKey, s.cfg.PromptVersion, rollup, calls)
}

func (s *Service) lookupRollupID(ctx context.Context, key string) (int64, error) {
	var id int64
	row := s.db.QueryRowContext(ctx, `SELECT id FROM rollups WHERE rollup_key = ?`, key)
	if err := row.Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Service) replaceRollupCalls(ctx context.Context, rollupID int64, callIDs []int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM rollup_calls WHERE rollup_id = ?`, rollupID)
	if err != nil {
		return err
	}
	for _, id := range callIDs {
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO rollup_calls (rollup_id, call_id) VALUES (?, ?)`, rollupID, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) startRun(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO rollup_runs (status) VALUES (?)`, "running")
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Service) finishRun(ctx context.Context, runID int64, status, errMsg string, rollupCount int) {
	if runID == 0 {
		return
	}
	_, err := s.db.ExecContext(ctx, `UPDATE rollup_runs SET status=?, error=?, rollup_count=?, finished_at=CURRENT_TIMESTAMP WHERE id=?`, status, truncateError(errMsg), rollupCount, runID)
	if err != nil {
		log.Printf("rollup run update failed: %v", err)
	}
}

func truncateError(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) > 240 {
		return msg[:240]
	}
	return msg
}

func nullableString(value string) interface{} {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
