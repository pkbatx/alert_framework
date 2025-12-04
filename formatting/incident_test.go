package formatting

import (
	"testing"
	"time"
)

func TestBuildIncidentAlert(t *testing.T) {
	incident := IncidentDetails{
		Agency:       "Lakeland EMS",
		CallCategory: "ems",
		CallType:     "medical",
		AddressLine:  "43 MS7 Waterloo Road",
		CityOrTown:   "Capital Care",
		Summary:      "35-year-old female experiencing difficulty breathing.",
		Timestamp:    time.Date(2025, time.December, 4, 10, 6, 13, 0, time.UTC),
		ListenURL:    "http://localhost:8000/Lakeland_EMS__Gen__2025_12_04_10_06_13.mp3",
	}

	got := BuildIncidentAlert(incident)
	want := "🚑 Lakeland EMS – EMS/Medical\n\n" +
		"📍 Location: 43 MS7 Waterloo Road, Capital Care\n" +
		"🏷️ Type: EMS – Medical\n" +
		"🕒 Time: 2025-12-04 10:06:13\n\n" +
		"Transcript:\n" +
		"35-year-old female experiencing difficulty breathing.\n\n" +
		"🎧 Audio: http://localhost:8000/Lakeland_EMS__Gen__2025_12_04_10_06_13.mp3"

	if got != want {
		t.Fatalf("BuildIncidentAlert mismatch.\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}
