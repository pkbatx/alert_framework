package formatting

import (
	"testing"
	"time"
)

func TestFormatPrettyTitle(t *testing.T) {
	loc, err := time.LoadLocation("EST5EDT")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}
	now := time.Date(2025, time.November, 27, 19, 58, 13, 0, loc)
	got := FormatPrettyTitle("Glenwood-Pochuck_EMS_2025_11_27_19_58_13.mp3", now, loc)
	want := "Glenwood-Pochuck EMS at 19:58 on 11/27/2025"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestParseCallMetadataFromFilename(t *testing.T) {
	loc, err := time.LoadLocation("EST5EDT")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}
	meta, err := ParseCallMetadataFromFilename("Glenwood-Pochuck_EMS_2025_11_27_19_58_13.mp3", loc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.AgencyDisplay != "Glenwood-Pochuck" {
		t.Fatalf("unexpected agency display: %s", meta.AgencyDisplay)
	}
	if meta.CallType != "EMS" {
		t.Fatalf("unexpected call type: %s", meta.CallType)
	}
	expectedTime := time.Date(2025, time.November, 27, 19, 58, 13, 0, loc)
	if !meta.DateTime.Equal(expectedTime) {
		t.Fatalf("unexpected datetime: %v", meta.DateTime)
	}
}

func TestParseCallMetadataWithExtraTokens(t *testing.T) {
	loc, err := time.LoadLocation("EST5EDT")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}

	meta, err := ParseCallMetadataFromFilename("Sussex_County_FM_2025_11_27_20_02_27.mp3", loc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.AgencyDisplay != "Sussex County" {
		t.Fatalf("unexpected agency display: %s", meta.AgencyDisplay)
	}
	if meta.CallType != "FM" {
		t.Fatalf("unexpected call type: %s", meta.CallType)
	}

	expectedTime := time.Date(2025, time.November, 27, 20, 2, 27, 0, loc)
	if !meta.DateTime.Equal(expectedTime) {
		t.Fatalf("unexpected datetime: %v", meta.DateTime)
	}
}

func TestParseCallMetadataWithProcessedSuffix(t *testing.T) {
	loc, err := time.LoadLocation("EST5EDT")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}

	meta, err := ParseCallMetadataFromFilename("Stanhope_FD_2025_12_02_15_45_30_proc.mp3", loc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.AgencyDisplay != "Stanhope" {
		t.Fatalf("unexpected agency display: %s", meta.AgencyDisplay)
	}
	if meta.CallType != "FD" {
		t.Fatalf("unexpected call type: %s", meta.CallType)
	}

	expectedTime := time.Date(2025, time.December, 2, 15, 45, 30, 0, loc)
	if !meta.DateTime.Equal(expectedTime) {
		t.Fatalf("unexpected datetime: %v", meta.DateTime)
	}
}

func TestParseCallMetadataWithEmptySegments(t *testing.T) {
	loc, err := time.LoadLocation("EST5EDT")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}

	meta, err := ParseCallMetadataFromFilename("Newton_EMS__Duty__2025_11_27_20_02_59.mp3", loc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.AgencyDisplay != "Newton EMS" {
		t.Fatalf("unexpected agency display: %s", meta.AgencyDisplay)
	}
	if meta.CallType != "DUTY" {
		t.Fatalf("unexpected call type: %s", meta.CallType)
	}

	expectedTime := time.Date(2025, time.November, 27, 20, 2, 59, 0, loc)
	if !meta.DateTime.Equal(expectedTime) {
		t.Fatalf("unexpected datetime: %v", meta.DateTime)
	}
}
