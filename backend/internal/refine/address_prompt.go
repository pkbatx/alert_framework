// SPDX-License-Identifier: MIT
// address_prompt.go hosts the metadata + address extraction prompt helpers.
package refine

import (
	"encoding/json"
	"strings"
	"time"

	"alert_framework/config"
)

type metadataPayload struct {
	Agency             string   `json:"agency"`
	IncidentType       string   `json:"incident_type"`
	Timestamp          string   `json:"timestamp"`
	LocationString     string   `json:"location_string"`
	LocationConfidence float64  `json:"location_confidence"`
	Notes              string   `json:"notes"`
	Summary            string   `json:"summary"`
	RecognizedTowns    []string `json:"recognized_towns"`
	CrossStreets       []string `json:"cross_streets"`
	NeedsManualReview  bool     `json:"needs_manual_review"`
}

type addressPayload struct {
	Street            string   `json:"street"`
	City              string   `json:"city"`
	Zip               string   `json:"zip"`
	County            string   `json:"county"`
	State             string   `json:"state"`
	CrossStreets      []string `json:"cross_streets"`
	Confidence        float64  `json:"confidence"`
	RecognizedTowns   []string `json:"recognized_towns"`
	NeedsManualReview bool     `json:"needs_manual_review"`
}

func (a addressPayload) String() string {
	parts := []string{}
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

func buildMetadataPrompt(cfg config.NLPConfig) string {
	base := strings.TrimSpace(cfg.MetadataPrompt)
	if base == "" {
		base = config.DefaultNLPConfig().MetadataPrompt
	}
	return base + "\nOutput JSON only."
}

func buildMetadataUserContent(req Request, cleanup cleanupPayload) string {
	ts := req.Metadata.DateTime
	if ts.IsZero() {
		ts = time.Now()
	}
	payload := map[string]interface{}{
		"transcript":      cleanup.CleanTranscript,
		"raw_transcript":  req.Transcript,
		"primary_address": cleanup.PrimaryAddress,
		"cross_streets":   cleanup.CrossStreets,
		"summary":         cleanup.Summary,
		"incident_type":   cleanup.IncidentType,
		"metadata": map[string]interface{}{
			"agency":       req.Metadata.AgencyDisplay,
			"call_type":    req.Metadata.CallType,
			"timestamp":    ts.Format(time.RFC3339),
			"filename":     req.Metadata.RawFileName,
			"municipality": req.Metadata.TownDisplay,
		},
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

func buildAddressPrompt(cfg config.NLPConfig) string {
	base := strings.TrimSpace(cfg.AddressPrompt)
	if base == "" {
		base = config.DefaultNLPConfig().AddressPrompt
	}
	mode := strings.TrimSpace(cfg.AddressMode)
	if mode == "" {
		mode = "sussex-biased"
	}
	return base + "\nAddress mode: " + mode + ". Reply with JSON only."
}

func buildAddressUserContent(req Request, cleanup cleanupPayload) string {
	payload := map[string]interface{}{
		"transcript":      cleanup.CleanTranscript,
		"raw_transcript":  req.Transcript,
		"primary_address": cleanup.PrimaryAddress,
		"recognized":      cleanup.RecognizedTowns,
		"metadata": map[string]interface{}{
			"agency":       req.Metadata.AgencyDisplay,
			"municipality": req.Metadata.TownDisplay,
		},
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

func shouldGeocode(llm addressPayload) bool {
	if strings.TrimSpace(llm.Street) == "" && strings.TrimSpace(llm.City) == "" && strings.TrimSpace(llm.County) == "" {
		return false
	}
	if llm.Confidence >= 0.95 && strings.TrimSpace(llm.Street) != "" && strings.TrimSpace(llm.City) != "" {
		return false
	}
	return true
}
