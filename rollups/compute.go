package rollups

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

const earthRadiusMeters = 6371000

func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const degToRad = math.Pi / 180
	lat1 *= degToRad
	lon1 *= degToRad
	lat2 *= degToRad
	lon2 *= degToRad
	dlat := lat2 - lat1
	dlon := lon2 - lon1
	a := math.Sin(dlat/2)*math.Sin(dlat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dlon/2)*math.Sin(dlon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusMeters * c
}

type cluster struct {
	calls        []CallRecord
	lastSeen     time.Time
	anchorLat    float64
	anchorLon    float64
	municipality string
	poi          string
}

func groupCalls(calls []CallRecord, chainWindow time.Duration, radiusMeters float64, maxCalls int) [][]CallRecord {
	if len(calls) == 0 {
		return nil
	}
	sort.Slice(calls, func(i, j int) bool {
		return calls[i].Timestamp.Before(calls[j].Timestamp)
	})
	var clusters []cluster
	for _, call := range calls {
		municipality := normalizeMunicipality(call)
		poi := normalizePOI(call)

		bestIdx := -1
		bestDistance := math.MaxFloat64
		for i := range clusters {
			c := &clusters[i]
			if maxCalls > 0 && len(c.calls) >= maxCalls {
				continue
			}
			if call.Timestamp.Sub(c.lastSeen) > chainWindow {
				continue
			}
			sameMunicipality := municipality != "" && c.municipality != "" && municipality == c.municipality
			distance := haversineMeters(call.Latitude, call.Longitude, c.anchorLat, c.anchorLon)
			if !sameMunicipality && distance > radiusMeters {
				continue
			}
			if distance < bestDistance {
				bestDistance = distance
				bestIdx = i
			}
		}
		if bestIdx == -1 {
			clusters = append(clusters, cluster{
				calls:        []CallRecord{call},
				lastSeen:     call.Timestamp,
				anchorLat:    call.Latitude,
				anchorLon:    call.Longitude,
				municipality: municipality,
				poi:          poi,
			})
			continue
		}
		c := &clusters[bestIdx]
		c.calls = append(c.calls, call)
		if call.Timestamp.After(c.lastSeen) {
			c.lastSeen = call.Timestamp
		}
		c.anchorLat = averageCoordinate(c.calls, true)
		c.anchorLon = averageCoordinate(c.calls, false)
		c.municipality = majorityString(c.calls, normalizeMunicipality)
		c.poi = majorityString(c.calls, normalizePOI)
	}

	out := make([][]CallRecord, 0, len(clusters))
	for _, c := range clusters {
		out = append(out, c.calls)
	}
	return out
}

func averageCoordinate(calls []CallRecord, useLat bool) float64 {
	if len(calls) == 0 {
		return 0
	}
	var sum float64
	for _, c := range calls {
		if useLat {
			sum += c.Latitude
		} else {
			sum += c.Longitude
		}
	}
	return sum / float64(len(calls))
}

func majorityString(calls []CallRecord, fn func(CallRecord) string) string {
	counts := make(map[string]int)
	for _, c := range calls {
		key := fn(c)
		if key == "" {
			continue
		}
		counts[key]++
	}
	var top string
	best := 0
	for key, count := range counts {
		if count > best {
			best = count
			top = key
		}
	}
	return top
}

func normalizeMunicipality(call CallRecord) string {
	city := strings.TrimSpace(extractCity(call.AddressJSON, call.RefinedJSON))
	if city != "" {
		return strings.ToLower(city)
	}
	label := strings.TrimSpace(call.LocationLabel)
	if label != "" {
		label = strings.ToLower(strings.TrimSpace(strings.Split(label, ",")[0]))
	}
	return label
}

func normalizePOI(call CallRecord) string {
	street := strings.TrimSpace(extractStreet(call.AddressJSON))
	return strings.ToLower(street)
}

func rollupKey(callIDs []int64) string {
	if len(callIDs) == 0 {
		return ""
	}
	ids := append([]int64(nil), callIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	builder := strings.Builder{}
	for i, id := range ids {
		if i > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(int64ToString(id))
	}
	hash := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(hash[:])
}

func int64ToString(v int64) string {
	return strconv.FormatInt(v, 10)
}

func deriveCategory(calls []CallRecord) string {
	counts := make(map[string]int)
	for _, call := range calls {
		cat := normalizeCategory(call.CallType)
		if cat == "" {
			cat = "other"
		}
		counts[cat]++
	}
	var top string
	best := 0
	for key, count := range counts {
		if count > best {
			best = count
			top = key
		}
	}
	if top == "" {
		return "other"
	}
	return top
}

func normalizeCategory(callType string) string {
	lower := strings.ToLower(callType)
	switch {
	case strings.Contains(lower, "ems"), strings.Contains(lower, "medic"), strings.Contains(lower, "medical"):
		return "ems"
	case strings.Contains(lower, "fire"), strings.Contains(lower, "burning"), strings.Contains(lower, "smoke"):
		return "fire"
	default:
		return "other"
	}
}

func derivePriority(calls []CallRecord) string {
	priority := "low"
	for _, call := range calls {
		p := priorityForCall(call)
		if p == "high" {
			return "high"
		}
		if p == "medium" {
			priority = "medium"
		}
	}
	return priority
}

func priorityForCall(call CallRecord) string {
	text := strings.ToLower(strings.TrimSpace(call.CleanTranscript))
	if text == "" {
		text = strings.ToLower(strings.TrimSpace(call.RawTranscript))
	}
	if text == "" {
		text = strings.ToLower(strings.TrimSpace(call.Normalized))
	}
	combined := strings.ToLower(call.CallType + " " + text)

	highKeywords := []string{"cardiac arrest", "unresponsive", "working fire", "structure fire", "multiple casualties", "entrapment"}
	for _, kw := range highKeywords {
		if strings.Contains(combined, kw) {
			return "high"
		}
	}
	mediumKeywords := []string{"overdose", "accident", "mva", "injury", "assault", "fire", "smoke", "collapse"}
	for _, kw := range mediumKeywords {
		if strings.Contains(combined, kw) {
			return "medium"
		}
	}
	return "low"
}
