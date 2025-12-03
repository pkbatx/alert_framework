package formatting

import (
	"fmt"
	"strings"
	"time"
)

// IncidentDetails represents the human-facing view of an incident used across UI and webhooks.
type IncidentDetails struct {
	ID            string
	PrettyTitle   string
	Agency        string
	CallType      string
	CallCategory  string
	AddressLine   string
	CrossStreet   string
	CityOrTown    string
	County        string
	State         string
	Summary       string
	Tags          []string
	Timestamp     time.Time
	ListenURL     string
	AudioPath     string
	AudioFilename string
}

// FormatIncidentHeader renders a concise incident header.
func FormatIncidentHeader(incident IncidentDetails) string {
	prefix := formatCategoryPrefix(incident.CallCategory)
	agency := strings.TrimSpace(incident.Agency)
	callType := strings.TrimSpace(incident.CallType)
	headerParts := []string{prefix}
	if agency != "" {
		headerParts = append(headerParts, agency)
	}
	if callType != "" {
		headerParts = append(headerParts, callType)
	}
	return strings.Join(headerParts, " – ")
}

// FormatIncidentLocation renders a consistent, human-friendly location string.
func FormatIncidentLocation(incident IncidentDetails) string {
	parts := []string{}

	address := strings.TrimSpace(incident.AddressLine)
	if address != "" {
		if cross := strings.TrimSpace(incident.CrossStreet); cross != "" {
			address = fmt.Sprintf("%s (x-street %s)", address, cross)
		}
		parts = append(parts, address)
	}

	if town := strings.TrimSpace(incident.CityOrTown); town != "" {
		parts = append(parts, town)
	}
	if county := strings.TrimSpace(incident.County); county != "" {
		parts = append(parts, county+" County")
	}
	if state := strings.TrimSpace(incident.State); state != "" {
		parts = append(parts, state)
	}

	if len(parts) == 0 {
		return "Location unavailable"
	}
	return strings.Join(parts, ", ")
}

// BuildIncidentAlert constructs a GroupMe-friendly alert body.
func BuildIncidentAlert(incident IncidentDetails) string {
	var lines []string
	header := FormatIncidentHeader(incident)
	if header != "" {
		lines = append(lines, header)
	}

	location := FormatIncidentLocation(incident)
	if location != "" {
		lines = append(lines, location)
	}

	summary := strings.TrimSpace(incident.Summary)
	if summary != "" {
		lines = append(lines, summary)
	}

	listen := strings.TrimSpace(incident.ListenURL)
	if listen != "" {
		lines = append(lines, fmt.Sprintf("Listen: %s", listen))
	}

	return strings.Join(lines, "\n")
}

func formatCategoryPrefix(callCategory string) string {
	switch NormalizeCallCategory(callCategory) {
	case "ems":
		return "🚑 EMS"
	case "fire":
		return "🚒 FIRE"
	default:
		return "🚨 INCIDENT"
	}
}
