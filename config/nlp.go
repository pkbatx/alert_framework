package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// NLPConfig captures prompt + tuning parameters used by the refinement pipeline.
// The fields can be customized via config.yaml (JSON is also accepted because it
// is a subset of YAML 1.2).
type NLPConfig struct {
	RefinementTemperature float64   `json:"refinement_temperature" yaml:"refinement_temperature"`
	CleanupStyle          string    `json:"cleanup_style" yaml:"cleanup_style"`
	AddressMode           string    `json:"address_mode" yaml:"address_mode"`
	MapboxBoundingBox     []float64 `json:"mapbox_bounding_box" yaml:"mapbox_bounding_box"`
	CleanupPrompt         string    `json:"cleanup_prompt" yaml:"cleanup_prompt"`
	MetadataPrompt        string    `json:"metadata_prompt" yaml:"metadata_prompt"`
	AddressPrompt         string    `json:"address_prompt" yaml:"address_prompt"`
}

const (
	defaultCleanupStyle  = "succinct"
	defaultAddressMode   = "sussex-biased"
	defaultConfigRefTemp = 0.15
)

var defaultBoundingBox = []float64{-75.2, 40.9, -74.3, 41.4}

// DefaultNLPConfig returns the baked-in prompt + tuning defaults.
func DefaultNLPConfig() NLPConfig {
	return NLPConfig{
		RefinementTemperature: defaultConfigRefTemp,
		CleanupStyle:          defaultCleanupStyle,
		AddressMode:           defaultAddressMode,
		MapboxBoundingBox:     append([]float64{}, defaultBoundingBox...),
		CleanupPrompt: `You are a Sussex County NJ emergency communications analyst.
Clean and normalize the provided transcript. Requirements:
1. Remove radio noise, repetition, and filler words.
2. Preserve facts; never invent information.
3. Produce polished sentences with correct punctuation, capitalization, and units.
4. Extract incident type, primary address, cross streets, patient details (age, gender, condition), and time references.
5. Output JSON with fields:
{
  "clean_transcript": string,
  "summary": string (<=2 sentences),
  "incident_type": string,
  "primary_address": string,
  "cross_streets": [string],
  "patient_details": string,
  "recognized_towns": [string]
}.
Summary must stay under two sentences and retain the original meaning.`,
		MetadataPrompt: `You are the metadata-refinement agent for Lakeland EMS covering Sussex County, NJ.
Given the existing metadata and cleaned transcript, return JSON with:
{
  "agency": "",
  "incident_type": "",
  "timestamp": ISO8601 with offset,
  "location_string": "",
  "location_confidence": float 0-1,
  "notes": "",
  "summary": "",
  "recognized_towns": [string]
}.
Never hallucinate details. Prefer Sussex County municipalities unless evidence proves otherwise.`,
		AddressPrompt: `You extract structured addresses for Sussex County NJ incidents.
Given transcript context and prior metadata, output JSON:
{
  "street": "",
  "city": "",
  "zip": "",
  "county": "",
  "state": "",
  "cross_streets": [string],
  "confidence": float 0-1
}
Return empty strings when unknown. Bias toward Sussex County.`,
	}
}

// LoadNLPConfig reads YAML/JSON and merges it with defaults.
func LoadNLPConfig(path string) (NLPConfig, error) {
	cfg := DefaultNLPConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if len(data) == 0 {
		return cfg, errors.New("empty config file")
	}
	var parsed struct {
		NLP NLPConfig `json:"nlp" yaml:"nlp"`
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, &parsed); err != nil {
			return cfg, err
		}
	case ".yaml", ".yml", "":
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			return cfg, err
		}
	default:
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			return cfg, err
		}
	}
	return MergeNLPConfig(cfg, parsed.NLP), nil
}

func stringsTrim(v string) string {
	return strings.TrimSpace(v)
}

// MergeNLPConfig overlays non-empty fields onto the base config.
func MergeNLPConfig(base NLPConfig, override NLPConfig) NLPConfig {
	if override.RefinementTemperature > 0 {
		base.RefinementTemperature = override.RefinementTemperature
	}
	if stringsTrim(override.CleanupStyle) != "" {
		base.CleanupStyle = override.CleanupStyle
	}
	if stringsTrim(override.AddressMode) != "" {
		base.AddressMode = override.AddressMode
	}
	if len(override.MapboxBoundingBox) == 4 {
		base.MapboxBoundingBox = append([]float64{}, override.MapboxBoundingBox...)
	}
	if stringsTrim(override.CleanupPrompt) != "" {
		base.CleanupPrompt = override.CleanupPrompt
	}
	if stringsTrim(override.MetadataPrompt) != "" {
		base.MetadataPrompt = override.MetadataPrompt
	}
	if stringsTrim(override.AddressPrompt) != "" {
		base.AddressPrompt = override.AddressPrompt
	}
	return base
}
