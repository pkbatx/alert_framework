package refine

import (
	"strings"
	"testing"
	"time"

	"alert_framework/config"
	"alert_framework/formatting"
)

func TestBuildCleanupPromptIncludesStyle(t *testing.T) {
	cfg := config.DefaultNLPConfig()
	cfg.CleanupStyle = "polished"
	prompt := buildCleanupPrompt(cfg)
	if !strings.Contains(prompt, "polished") {
		t.Fatalf("expected cleanup style to appear in prompt")
	}
}

func TestCleanupUserContentCarriesMetadata(t *testing.T) {
	req := Request{
		Transcript: "Test transcript",
		Metadata: formatting.CallMetadata{
			AgencyDisplay: "Wantage Fire",
			CallType:      "Fire",
			DateTime:      time.Date(2025, 12, 4, 14, 0, 0, 0, time.UTC),
			RawFileName:   "Wantage_Fire_2025.mp3",
			TownDisplay:   "Wantage",
		},
		RecognizedTowns: []string{"Wantage"},
	}
	content := buildCleanupUserContent(req)
	if !strings.Contains(content, "Wantage Fire") || !strings.Contains(content, "Test transcript") {
		t.Fatalf("expected metadata + transcript in user content: %s", content)
	}
}
