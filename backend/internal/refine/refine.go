// Package refine hosts the Sussex-focused metadata and transcript refinement pipeline.
// It orchestrates GPT-5.1 cleanup/metadata prompts, Mapbox verification, Sussex-
// boundary heuristics, and produces the structured metadata object persisted by
// the Alert Framework runtime.
package refine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"alert_framework/config"
	"alert_framework/formatting"
)

const (
	cleanupModel = "gpt-5.1"
)

// Service coordinates refinement passes for a single transcript.
type Service struct {
	client      *http.Client
	openAIKey   string
	mapboxToken string
	templates   *TemplateManager
}

// Request contains the context needed to refine a transcript.
type Request struct {
	Transcript      string
	Metadata        formatting.CallMetadata
	RecognizedTowns []string
}

// Result bundles every structured artifact produced by the refinement pipeline.
type Result struct {
	CleanTranscript   string
	Summary           string
	NormalizedSummary string
	Metadata          Metadata
	Address           Address
	RecognizedTowns   []string
	NeedsManualReview bool
}

// Metadata matches the JSON object stored in SQLite and served to the UI.
type Metadata struct {
	Agency             string   `json:"agency"`
	IncidentType       string   `json:"incident_type"`
	Timestamp          string   `json:"timestamp"`
	LocationString     string   `json:"location_string"`
	LocationConfidence float64  `json:"location_confidence"`
	CrossStreets       []string `json:"cross_streets"`
	Notes              string   `json:"notes"`
	Summary            string   `json:"summary"`
	RecognizedTowns    []string `json:"recognized_towns"`
	NeedsManualReview  bool     `json:"needs_manual_review"`
}

// Address represents the merged GPT + Mapbox address payload.
type Address struct {
	Street            string   `json:"street"`
	City              string   `json:"city"`
	Zip               string   `json:"zip"`
	County            string   `json:"county"`
	State             string   `json:"state"`
	Lat               float64  `json:"lat"`
	Lon               float64  `json:"lon"`
	MapboxConfidence  float64  `json:"mapbox_confidence"`
	CrossStreets      []string `json:"cross_streets"`
	LocationConfScore float64  `json:"confidence"`
}

// String renders a human readable label.
func (a Address) String() string {
	var parts []string
	if strings.TrimSpace(a.Street) != "" {
		parts = append(parts, strings.TrimSpace(a.Street))
	}
	if strings.TrimSpace(a.City) != "" {
		parts = append(parts, strings.TrimSpace(a.City))
	}
	if strings.TrimSpace(a.County) != "" {
		parts = append(parts, strings.TrimSpace(a.County))
	}
	if strings.TrimSpace(a.State) != "" {
		parts = append(parts, strings.TrimSpace(a.State))
	}
	return strings.Join(parts, ", ")
}

// NewService wires up a new refinement service instance.
func NewService(client *http.Client, cfg config.Config) (*Service, error) {
	tm, err := NewTemplateManager(cfg.NLPConfigPath, cfg.NLP)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if key == "" {
		return nil, errors.New("OPENAI_API_KEY missing for refinement")
	}
	return &Service{
		client:      client,
		openAIKey:   key,
		mapboxToken: strings.TrimSpace(cfg.MapboxToken),
		templates:   tm,
	}, nil
}

// Close releases any resources a service holds.
func (s *Service) Close() error {
	if s.templates != nil {
		return s.templates.Close()
	}
	return nil
}

// Refine executes the full GPT + Mapbox workflow.
func (s *Service) Refine(ctx context.Context, req Request) (Result, error) {
	cfg := s.templates.Current()
	if strings.TrimSpace(req.Transcript) == "" {
		return Result{}, errors.New("empty transcript for refinement")
	}

	cleanResp, err := s.runCleanup(ctx, cfg, req)
	if err != nil {
		return Result{}, err
	}

	metaResp, err := s.runMetadata(ctx, cfg, req, cleanResp)
	if err != nil {
		return Result{}, err
	}

	addressResp, err := s.runAddress(ctx, cfg, req, cleanResp)
	if err != nil {
		return Result{}, err
	}

	var recognized []string
	recognized = append(recognized, cleanResp.RecognizedTowns...)
	recognized = append(recognized, metaResp.RecognizedTowns...)
	recognized = append(recognized, addressResp.RecognizedTowns...)
	recognized = dedupeStrings(recognized)

	mergedAddress, needsManual, locConf := s.mergeAddressData(ctx, cfg, req, addressResp)
	finalLocConf := averageNonZero(metaResp.LocationConfidence, locConf)
	metadata := Metadata{
		Agency:             fallbackString(metaResp.Agency, req.Metadata.AgencyDisplay),
		IncidentType:       fallbackString(metaResp.IncidentType, cleanResp.IncidentType),
		Timestamp:          metaResp.Timestamp,
		LocationString:     fallbackString(metaResp.LocationString, cleanResp.PrimaryAddress),
		LocationConfidence: finalLocConf,
		CrossStreets:       dedupeStrings(append(metaResp.CrossStreets, cleanResp.CrossStreets...)),
		Notes:              metaResp.Notes,
		Summary:            fallbackString(metaResp.Summary, cleanResp.Summary),
		RecognizedTowns:    recognized,
		NeedsManualReview:  needsManual || metaResp.NeedsManualReview,
	}

	return Result{
		CleanTranscript:   cleanResp.CleanTranscript,
		Summary:           metadata.Summary,
		NormalizedSummary: cleanResp.Summary,
		Metadata:          metadata,
		Address:           mergedAddress,
		RecognizedTowns:   recognized,
		NeedsManualReview: metadata.NeedsManualReview,
	}, nil
}

func (s *Service) runCleanup(ctx context.Context, cfg config.NLPConfig, req Request) (cleanupPayload, error) {
	system := buildCleanupPrompt(cfg)
	user := buildCleanupUserContent(req)
	var parsed cleanupPayload
	if err := s.callJSON(ctx, system, user, &parsed, cfg); err != nil {
		return cleanupPayload{}, err
	}
	return parsed, nil
}

func (s *Service) runMetadata(ctx context.Context, cfg config.NLPConfig, req Request, cleanup cleanupPayload) (metadataPayload, error) {
	system := buildMetadataPrompt(cfg)
	user := buildMetadataUserContent(req, cleanup)
	var parsed metadataPayload
	if err := s.callJSON(ctx, system, user, &parsed, cfg); err != nil {
		return metadataPayload{}, err
	}
	return parsed, nil
}

func (s *Service) runAddress(ctx context.Context, cfg config.NLPConfig, req Request, cleanup cleanupPayload) (addressPayload, error) {
	system := buildAddressPrompt(cfg)
	user := buildAddressUserContent(req, cleanup)
	var parsed addressPayload
	if err := s.callJSON(ctx, system, user, &parsed, cfg); err != nil {
		return addressPayload{}, err
	}
	return parsed, nil
}

func (s *Service) callJSON(ctx context.Context, system, user string, target interface{}, cfg config.NLPConfig) error {
	payload := map[string]interface{}{
		"model":           cleanupModel,
		"temperature":     cfg.RefinementTemperature,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.openAIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gpt-5.1 status %d: %s", resp.StatusCode, string(body))
	}
	var wrapper struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return err
	}
	if len(wrapper.Choices) == 0 {
		return errors.New("empty GPT-5.1 response")
	}
	content := strings.TrimSpace(wrapper.Choices[0].Message.Content)
	if content == "" {
		return errors.New("GPT-5.1 returned empty content")
	}
	return json.Unmarshal([]byte(content), target)
}

func (s *Service) mergeAddressData(ctx context.Context, cfg config.NLPConfig, req Request, llm addressPayload) (Address, bool, float64) {
	address := Address{
		Street:       llm.Street,
		City:         llm.City,
		Zip:          llm.Zip,
		County:       llm.County,
		State:        fallbackString(llm.State, "NJ"),
		CrossStreets: dedupeStrings(llm.CrossStreets),
	}

	var combinedConf = clamp(llm.Confidence, 0, 1)
	var needsReview bool

	if s.mapboxToken != "" && shouldGeocode(llm) {
		query := llm.String()
		if query == "" {
			query = req.Metadata.AgencyDisplay
		}
		feature, err := forwardGeocode(ctx, s.client, s.mapboxToken, query, cfg)
		if err == nil && feature != nil {
			address.Lat = feature.Lat
			address.Lon = feature.Lon
			address.County = fallbackString(feature.County, address.County)
			address.City = fallbackString(feature.City, address.City)
			address.Street = fallbackString(feature.Street, address.Street)
			address.MapboxConfidence = feature.Confidence

			combinedConf = averageNonZero(combinedConf, feature.Confidence)
			if feature.County != "" && !strings.Contains(strings.ToLower(feature.County), "sussex") {
				needsReview = true
				combinedConf = minFloat(combinedConf, 0.4)
			}
		}
	}

	sussexOK, reason := IsLikelySussexCounty(address.String())
	if !sussexOK && reason == "uncertain" {
		combinedConf = minFloat(combinedConf, 0.4)
		needsReview = true
	}

	address.LocationConfScore = combinedConf
	return address, needsReview || llm.NeedsManualReview, combinedConf
}

// TemplateManager hot-reloads NLP config templates without requiring process restarts.
type TemplateManager struct {
	path     string
	mu       sync.RWMutex
	cfg      config.NLPConfig
	lastLoad time.Time
}

// NewTemplateManager seeds a new manager.
func NewTemplateManager(path string, initial config.NLPConfig) (*TemplateManager, error) {
	tm := &TemplateManager{path: path, cfg: initial, lastLoad: time.Now()}
	_ = tm.reload()
	return tm, nil
}

// Current returns the latest config, reloading from disk when the file has changed.
func (tm *TemplateManager) Current() config.NLPConfig {
	_ = tm.reload()
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.cfg
}

// Close is a placeholder for future watchers.
func (tm *TemplateManager) Close() error { return nil }

func (tm *TemplateManager) reload() error {
	info, err := os.Stat(tm.path)
	if err != nil {
		return nil
	}
	if !info.ModTime().After(tm.lastLoad) {
		return nil
	}
	data, err := os.ReadFile(tm.path)
	if err != nil || len(data) == 0 {
		return err
	}
	cfg, err := config.LoadNLPConfig(tm.path)
	if err != nil {
		return err
	}
	tm.mu.Lock()
	tm.cfg = cfg
	tm.lastLoad = info.ModTime()
	tm.mu.Unlock()
	return nil
}

func dedupeStrings(items []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func fallbackString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func clamp(val, min, max float64) float64 {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

func averageNonZero(a, b float64) float64 {
	a = clamp(a, 0, 1)
	b = clamp(b, 0, 1)
	switch {
	case a == 0:
		return b
	case b == 0:
		return a
	default:
		return (a + b) / 2
	}
}

func minFloat(a, b float64) float64 {
	if a == 0 {
		return b
	}
	if b == 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}
