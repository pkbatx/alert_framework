package refine

import (
	"context"
	"testing"

	"alert_framework/config"
	"alert_framework/formatting"
)

func TestShouldGeocode(t *testing.T) {
	var cases = []struct {
		payload addressPayload
		want    bool
	}{
		{addressPayload{Street: "", City: "", Confidence: 0.5}, false},
		{addressPayload{Street: "10 Main St", City: "Newton", Confidence: 0.99}, false},
		{addressPayload{Street: "24 Ruth Dr", City: "Wantage", Confidence: 0.50}, true},
	}
	for _, tc := range cases {
		got := shouldGeocode(tc.payload)
		if got != tc.want {
			t.Fatalf("shouldGeocode(%+v)=%v want %v", tc.payload, got, tc.want)
		}
	}
}

func TestMergeAddressDataSussexBias(t *testing.T) {
	cfg := config.DefaultNLPConfig()
	svc := &Service{mapboxToken: ""}
	req := Request{Metadata: formatting.CallMetadata{AgencyDisplay: "Wantage Fire"}}
	addr, manual, conf := svc.mergeAddressData(context.Background(), cfg, req, addressPayload{
		Street:     "24 Ruth Drive",
		City:       "Wantage",
		State:      "NJ",
		Confidence: 0.8,
	})
	if manual {
		t.Fatalf("expected sussex address to skip manual review")
	}
	if conf == 0 {
		t.Fatalf("expected non-zero confidence")
	}
	if addr.City != "Wantage" {
		t.Fatalf("expected Wantage city, got %s", addr.City)
	}
}

func TestMergeAddressDataFlagsOutOfCounty(t *testing.T) {
	cfg := config.DefaultNLPConfig()
	svc := &Service{mapboxToken: ""}
	req := Request{Metadata: formatting.CallMetadata{}}
	_, manual, conf := svc.mergeAddressData(context.Background(), cfg, req, addressPayload{
		Street:     "123 Broad St",
		City:       "Newark",
		State:      "NJ",
		Confidence: 0.9,
	})
	if !manual {
		t.Fatalf("expected manual review for out-of-county address")
	}
	if conf > 0.4 {
		t.Fatalf("expected confidence capped for out-of-county, got %f", conf)
	}
}
