package formatting

import "strings"

// NormalizeCallCategory maps a free-form call type into a small, stable category set.
func NormalizeCallCategory(callType string) string {
	t := strings.ToLower(callType)
	switch {
	case strings.Contains(t, "ems"), strings.Contains(t, "medic"), strings.Contains(t, "medical"):
		return "ems"
	case strings.Contains(t, "fire"), strings.Contains(t, "burning"), strings.Contains(t, "smoke"):
		return "fire"
	default:
		return "other"
	}
}
