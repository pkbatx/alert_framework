package formatting

import (
	"strings"
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

func TestNormalizeCallCategory(t *testing.T) {
	if got := NormalizeCallCategory("EMS Alarm"); got != "ems" {
		t.Fatalf("expected ems, got %s", got)
	}
	if got := NormalizeCallCategory("FIRE"); got != "fire" {
		t.Fatalf("expected fire, got %s", got)
	}
	if got := NormalizeCallCategory("Other"); got != "other" {
		t.Fatalf("expected other, got %s", got)
	}
}

func TestBuildListenURL(t *testing.T) {
	t.Setenv("HTTP_PORT", ":9000")
	t.Setenv("EXTERNAL_LISTEN_BASE_URL", "")
	t.Setenv("PUBLIC_BASE_URL", "")
	url := BuildListenURL("audio/test.mp3")
	if !strings.HasPrefix(url, "http://localhost:9000/") {
		t.Fatalf("unexpected listen URL %s", url)
	}

	t.Setenv("PUBLIC_BASE_URL", "https://alerts.example.com/media/")
	t.Setenv("EXTERNAL_LISTEN_BASE_URL", "")
	url = BuildListenURL("audio/test.mp3")
	if url != "https://alerts.example.com/media/audio/test.mp3" {
		t.Fatalf("unexpected public base listen URL %s", url)
	}

	t.Setenv("EXTERNAL_LISTEN_BASE_URL", "https://example.com/audio")
	url = BuildListenURL("/audio/test.mp3")
	if url != "https://example.com/audio/audio/test.mp3" {
		t.Fatalf("unexpected external listen URL %s", url)
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
