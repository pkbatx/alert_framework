package formatting

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CallMetadata captures details derived from the filename.
type CallMetadata struct {
	AgencyDisplay string
	TownDisplay   string
	CallType      string
	DateTime      time.Time
	RawFileName   string
}

var tokenSplitter = regexp.MustCompile(`(?P<lower>[a-z])(?P<upper>[A-Z])`)
var separatorTokens = []string{"TWP", "FD", "Gen", "Duty"}

// FormatPrettyTitle replicates the old pretty.sh logic in pure Go.
func FormatPrettyTitle(fileName string, now time.Time, loc *time.Location) string {
	base := filepath.Base(fileName)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	cleaned := removeDigitsAndUnderscores(base)
	cleaned = tokenSplitter.ReplaceAllString(cleaned, "${lower} ${upper}")
	cleaned = splitSpecialTokens(cleaned)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if cleaned == "" {
		cleaned = base
	}

	if loc == nil {
		loc = time.Local
	}
	ts := now.In(loc)
	timestamp := fmt.Sprintf("%02d:%02d on %d/%d/%d", ts.Hour(), ts.Minute(), ts.Month(), ts.Day(), ts.Year())
	return fmt.Sprintf("%s at %s", strings.TrimSpace(cleaned), timestamp)
}

// ParseCallMetadataFromFilename extracts structured details from the expected filename pattern.
func ParseCallMetadataFromFilename(fileName string, loc *time.Location) (CallMetadata, error) {
	base := filepath.Base(fileName)
	base = strings.TrimSuffix(base, filepath.Ext(base))

	// Split on underscores while ignoring empty segments so filenames with
	// doubled separators (e.g., "EMS__Duty") are handled gracefully.
	parts := strings.FieldsFunc(base, func(r rune) bool { return r == '_' })
	if len(parts) < 7 {
		return CallMetadata{RawFileName: fileName}, fmt.Errorf("filename does not contain enough segments")
	}

	numericParts := parts[len(parts)-6:]
	year, err := strconv.Atoi(numericParts[0])
	if err != nil {
		return CallMetadata{RawFileName: fileName}, err
	}
	month, err := strconv.Atoi(numericParts[1])
	if err != nil {
		return CallMetadata{RawFileName: fileName}, err
	}
	day, err := strconv.Atoi(numericParts[2])
	if err != nil {
		return CallMetadata{RawFileName: fileName}, err
	}
	hour, err := strconv.Atoi(numericParts[3])
	if err != nil {
		return CallMetadata{RawFileName: fileName}, err
	}
	minute, err := strconv.Atoi(numericParts[4])
	if err != nil {
		return CallMetadata{RawFileName: fileName}, err
	}
	second, err := strconv.Atoi(numericParts[5])
	if err != nil {
		return CallMetadata{RawFileName: fileName}, err
	}

	if loc == nil {
		loc = time.Local
	}

	agencyTown := ""
	callType := ""
	descriptive := parts[:len(parts)-6]
	if len(descriptive) > 0 {
		if len(descriptive) > 1 {
			agencyTown = normalizeDisplay(strings.Join(descriptive[:len(descriptive)-1], " "))
			callType = strings.ToUpper(descriptive[len(descriptive)-1])
		} else {
			agencyTown = normalizeDisplay(descriptive[0])
		}
	}

	dt := time.Date(year, time.Month(month), day, hour, minute, second, 0, loc)
	return CallMetadata{
		AgencyDisplay: agencyTown,
		TownDisplay:   agencyTown,
		CallType:      callType,
		DateTime:      dt,
		RawFileName:   fileName,
	}, nil
}

// BuildAlertMessage creates a short, human-friendly alert payload.
func BuildAlertMessage(meta CallMetadata, prettyTitle string, url string) string {
	lines := []string{prettyTitle}
	if meta.CallType != "" {
		lines = append(lines, fmt.Sprintf("Call type: %s", meta.CallType))
	}
	if meta.AgencyDisplay != "" {
		lines = append(lines, fmt.Sprintf("Agency/Town: %s", meta.AgencyDisplay))
	}
	if url != "" {
		lines = append(lines, fmt.Sprintf("Listen: %s", url))
	}
	return strings.Join(lines, "\n")
}

func removeDigitsAndUnderscores(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '_' || (r >= '0' && r <= '9') {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func splitSpecialTokens(s string) string {
	for _, token := range separatorTokens {
		s = strings.ReplaceAll(s, token, " "+token+" ")
	}
	return s
}

func normalizeDisplay(value string) string {
	value = strings.ReplaceAll(value, "_", " ")
	value = tokenSplitter.ReplaceAllString(value, "${lower} ${upper}")
	value = splitSpecialTokens(value)
	return strings.Join(strings.Fields(value), " ")
}
