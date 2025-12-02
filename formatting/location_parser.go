package formatting

import (
	"errors"
	"regexp"
	"sort"
	"strings"
)

type ParsedLocation struct {
	Municipality string
	Street       string
	CrossStreet  string
	HouseNumber  string
	RawText      string
}

var knownMunicipalities = []string{
	"Andover", "Byram", "Frankford", "Franklin", "Green", "Hamburg", "Hardyston", "Hopatcong", "Lafayette", "Montague", "Newton", "Ogdensburg", "Sandyston", "Sparta", "Stanhope", "Stillwater", "Sussex", "Vernon", "Wantage", "Fredon", "Branchville", "Allamuchy", "Alpha", "Belvidere", "Blairstown", "Frelinghuysen", "Greenwich", "Hackettstown", "Hardwick", "Harmony", "Hope", "Independence", "Knowlton", "Liberty", "Lopatcong", "Mansfield", "Oxford", "Phillipsburg", "Pohatcong", "Washington Boro", "Washington Township", "White",
}

var streetSuffixList = []string{
	"Road", "Street", "Avenue", "Highway", "Route", "Lane", "Drive", "Court", "Place", "Way", "Pike", "Circle", "Boulevard", "Parkway",
}

var (
	intersectionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)intersection of\s+([^,]+?)\s+(?:and|&)\s+([^,\.]+)`),
		regexp.MustCompile(`(?i)at\s+([^,]+?)\s+(?:and|&)\s+([^,\.]+)`),
	}
	addressPattern = regexp.MustCompile(`(?i)\b(\d{1,6})\s+([A-Za-z0-9'\.\s]+?(?:` + suffixAlternation() + `))\b`)
)

func ParseLocationFromTranscript(text string) (*ParsedLocation, error) {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return nil, errors.New("empty transcript")
	}

	municipality := findMunicipality(cleaned)

	for _, pattern := range intersectionPatterns {
		if matches := pattern.FindStringSubmatch(cleaned); len(matches) == 3 {
			streetA := normalizeStreet(matches[1])
			streetB := normalizeStreet(matches[2])
			if streetA != "" && streetB != "" {
				return &ParsedLocation{
					Municipality: municipality,
					Street:       streetA,
					CrossStreet:  streetB,
					RawText:      strings.TrimSpace(matches[0]),
				}, nil
			}
		}
	}

	if matches := addressPattern.FindStringSubmatch(cleaned); len(matches) == 3 {
		house := strings.TrimSpace(matches[1])
		street := normalizeStreet(matches[2])
		return &ParsedLocation{
			Municipality: municipality,
			Street:       street,
			HouseNumber:  house,
			RawText:      strings.TrimSpace(matches[0]),
		}, nil
	}

	atPattern := regexp.MustCompile(`(?i)at\s+([^,]+?)\s+in\s+([^,\.]+)`) // fallback single street
	if matches := atPattern.FindStringSubmatch(cleaned); len(matches) == 3 {
		street := normalizeStreet(matches[1])
		if municipality == "" {
			municipality = normalizeMunicipality(matches[2])
		}
		if street != "" || municipality != "" {
			return &ParsedLocation{Municipality: municipality, Street: street, RawText: strings.TrimSpace(matches[0])}, nil
		}
	}

	if municipality != "" {
		return &ParsedLocation{Municipality: municipality, RawText: municipality}, nil
	}

	return nil, errors.New("no location found")
}

func FormatLocationLabel(loc *ParsedLocation) string {
	if loc == nil {
		return ""
	}
	parts := []string{}
	if loc.HouseNumber != "" && loc.Street != "" {
		parts = append(parts, strings.TrimSpace(loc.HouseNumber+" "+loc.Street))
	} else if loc.Street != "" && loc.CrossStreet != "" {
		parts = append(parts, strings.TrimSpace(loc.Street+" & "+loc.CrossStreet))
	} else if loc.Street != "" {
		parts = append(parts, loc.Street)
	}
	if loc.Municipality != "" {
		parts = append(parts, loc.Municipality)
	}
	return strings.Join(parts, ", ")
}

func findMunicipality(text string) string {
	lower := strings.ToLower(text)
	for _, town := range knownMunicipalities {
		variants := []string{town, town + " Township", town + " Twp", town + " Boro", town + " Borough"}
		for _, variant := range variants {
			if strings.Contains(lower, strings.ToLower(variant)) {
				return normalizeMunicipality(variant)
			}
		}
	}

	townPattern := regexp.MustCompile(`(?i)(?:in|into|to)\s+([A-Za-z\s]{3,30})`)
	if matches := townPattern.FindStringSubmatch(text); len(matches) == 2 {
		guess := normalizeMunicipality(matches[1])
		return guess
	}
	return ""
}

func normalizeMunicipality(value string) string {
	cleaned := strings.Title(strings.ToLower(strings.TrimSpace(value)))
	cleaned = strings.ReplaceAll(cleaned, "Twp", "Township")
	cleaned = strings.ReplaceAll(cleaned, "Boro", "Borough")
	cleaned = strings.ReplaceAll(cleaned, "Borough Borough", "Borough")
	cleaned = strings.ReplaceAll(cleaned, "Township Township", "Township")
	return strings.TrimSpace(cleaned)
}

func normalizeStreet(value string) string {
	value = strings.TrimSpace(value)
	value = whitespacePattern.ReplaceAllString(value, " ")
	tokens := strings.Fields(value)
	if len(tokens) == 0 {
		return ""
	}
	for i, token := range tokens {
		lower := strings.ToLower(token)
		if repl, ok := streetSuffixes[lower]; ok {
			tokens[i] = repl
			continue
		}
		tokens[i] = strings.Title(lower)
	}
	return strings.Join(tokens, " ")
}

func suffixAlternation() string {
	parts := append([]string{}, streetSuffixList...)
	for key := range streetSuffixes {
		parts = append(parts, key)
	}
	sort.Strings(parts)
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return strings.Join(parts, "|")
}
