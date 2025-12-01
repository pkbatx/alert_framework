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
