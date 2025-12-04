package formatting

import (
	"fmt"
	"strings"
	"time"
	"unicode"
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

var callClassSeparator = strings.NewReplacer("_", " ", "-", " ", "/", " ")

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
	ts := incident.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	emoji := incidentCategoryEmoji(incident.CallCategory)
	primary := primaryServiceLabel(incident.CallCategory)
	callClass, hasSpecific := callClassLabel(incident.CallType)
	categoryShort := categoryShortLabel(primary, callClass, hasSpecific)

	agency := strings.TrimSpace(incident.Agency)
	if agency == "" {
		agency = "Unknown agency"
	}

	location := FormatIncidentLocation(incident)
	audio := strings.TrimSpace(incident.ListenURL)
	if audio == "" {
		audio = "Not available"
	}
	summary := strings.TrimSpace(incident.Summary)
	if summary == "" {
		summary = "Transcript pending."
	}

	lines := []string{
		fmt.Sprintf("%s %s – %s", emoji, agency, categoryShort),
		"",
		fmt.Sprintf("📍 Location: %s", location),
		fmt.Sprintf("🏷️ Type: %s – %s", primary, callClass),
		fmt.Sprintf("🕒 Time: %s", ts.Format("2006-01-02 15:04:05")),
		"",
		"Transcript:",
		summary,
		"",
		fmt.Sprintf("🎧 Audio: %s", audio),
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

func incidentCategoryEmoji(callCategory string) string {
	switch NormalizeCallCategory(callCategory) {
	case "ems":
		return "🚑"
	case "fire":
		return "🚒"
	default:
		return "🚨"
	}
}

func primaryServiceLabel(callCategory string) string {
	switch NormalizeCallCategory(callCategory) {
	case "ems":
		return "EMS"
	case "fire":
		return "Fire"
	default:
		return "Incident"
	}
}

func callClassLabel(callType string) (string, bool) {
	value := strings.TrimSpace(callType)
	if value == "" {
		return "General", false
	}
	value = callClassSeparator.Replace(value)
	words := strings.Fields(value)
	if len(words) == 0 {
		return "General", false
	}
	for i, w := range words {
		words[i] = capitalizeWord(w)
	}
	return strings.Join(words, " "), true
}

func categoryShortLabel(primary, callClass string, hasSpecific bool) string {
	if hasSpecific && callClass != "" {
		return fmt.Sprintf("%s/%s", primary, callClass)
	}
	return primary
}

func capitalizeWord(word string) string {
	lowered := strings.ToLower(word)
	runes := []rune(lowered)
	if len(runes) == 0 {
		return word
	}
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
