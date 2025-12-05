// SPDX-License-Identifier: MIT
// cleanup_prompt.go builds the GPT-5.1 prompts/user payloads used for transcript cleanup.
package refine

import (
	"encoding/json"
	"strings"
	"time"

	"alert_framework/config"
)

type cleanupPayload struct {
	CleanTranscript string   `json:"clean_transcript"`
	Summary         string   `json:"summary"`
	IncidentType    string   `json:"incident_type"`
	PrimaryAddress  string   `json:"primary_address"`
	CrossStreets    []string `json:"cross_streets"`
	PatientDetails  string   `json:"patient_details"`
	RecognizedTowns []string `json:"recognized_towns"`
}

func buildCleanupPrompt(cfg config.NLPConfig) string {
	base := strings.TrimSpace(cfg.CleanupPrompt)
	if base == "" {
		base = config.DefaultNLPConfig().CleanupPrompt
	}
	style := cfg.CleanupStyle
	if style == "" {
		style = "succinct"
	}
	return base + "\nAlways produce polished, " + style + " copy and reply with JSON only."
}

func buildCleanupUserContent(req Request) string {
	payload := map[string]interface{}{
		"transcript": req.Transcript,
		"metadata": map[string]interface{}{
			"agency":       req.Metadata.AgencyDisplay,
			"call_type":    req.Metadata.CallType,
			"timestamp":    req.Metadata.DateTime.Format(time.RFC3339),
			"filename":     req.Metadata.RawFileName,
			"recognized":   req.RecognizedTowns,
			"municipality": req.Metadata.TownDisplay,
		},
	}
	data, _ := json.Marshal(payload)
	return string(data)
}
