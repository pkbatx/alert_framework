package formatting

import (
	"regexp"
	"strings"
)

var whitespacePattern = regexp.MustCompile(`\s+`)

var streetSuffixes = map[string]string{
	"rd":      "Road",
	"rd.":     "Road",
	"road":    "Road",
	"st":      "Street",
	"st.":     "Street",
	"street":  "Street",
	"ave":     "Avenue",
	"ave.":    "Avenue",
	"avenue":  "Avenue",
	"hwy":     "Highway",
	"hwy.":    "Highway",
	"highway": "Highway",
	"ln":      "Lane",
	"ln.":     "Lane",
	"lane":    "Lane",
	"dr":      "Drive",
	"dr.":     "Drive",
	"drive":   "Drive",
	"ct":      "Court",
	"ct.":     "Court",
	"pkwy":    "Parkway",
	"pkwy.":   "Parkway",
	"blvd":    "Boulevard",
	"blvd.":   "Boulevard",
	"rt":      "Route",
	"rt.":     "Route",
	"rte":     "Route",
}

var townshipVariants = map[string]string{
	"twp":      "Township",
	"twp.":     "Township",
	"twsp":     "Township",
	"twsp.":    "Township",
	"township": "Township",
	"borough":  "Borough",
	"boro":     "Borough",
	"boro.":    "Borough",
	"borough.": "Borough",
	"city":     "City",
	"city.":    "City",
	"village":  "Village",
	"village.": "Village",
}

var suffixPattern = regexp.MustCompile(`(?i)\b([A-Za-z]+)(?:\s+)(` + streetSuffixAlternation() + `)\b`)

// NormalizeTranscript cleans radio transcripts for easier parsing.
func NormalizeTranscript(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}

	text = whitespacePattern.ReplaceAllString(text, " ")
	text = normalizeSuffixes(text)
	text = normalizeTownshipTokens(text)
	text = strings.TrimSpace(text)

	if text != "" {
		if last := text[len(text)-1]; last != '.' && last != '!' && last != '?' {
			text += "."
		}
	}
	return text
}

func normalizeSuffixes(text string) string {
	return suffixPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := strings.Fields(match)
		if len(parts) < 2 {
			return match
		}
		suffix := strings.ToLower(parts[len(parts)-1])
		replacement, ok := streetSuffixes[suffix]
		if !ok {
			return match
		}
		parts[len(parts)-1] = replacement
		return strings.Join(parts, " ")
	})
}

func normalizeTownshipTokens(text string) string {
	parts := strings.Fields(text)
	for i, part := range parts {
		lower := strings.ToLower(strings.Trim(part, ",."))
		if replacement, ok := townshipVariants[lower]; ok {
			parts[i] = replacement
		}
	}
	return strings.Join(parts, " ")
}

func streetSuffixAlternation() string {
	keys := make([]string, 0, len(streetSuffixes))
	for k := range streetSuffixes {
		keys = append(keys, regexp.QuoteMeta(k))
	}
	return strings.Join(keys, "|")
}
