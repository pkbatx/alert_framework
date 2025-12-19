package rollups

import (
	"encoding/json"
	"strings"
)

type addressPayload struct {
	Street string `json:"street"`
	City   string `json:"city"`
}

type refinedPayload struct {
	LocationString string `json:"location_string"`
}

func extractCity(addressJSON string, refinedJSON string) string {
	addressJSON = strings.TrimSpace(addressJSON)
	if addressJSON != "" {
		var addr addressPayload
		if err := json.Unmarshal([]byte(addressJSON), &addr); err == nil {
			if strings.TrimSpace(addr.City) != "" {
				return strings.TrimSpace(addr.City)
			}
		}
	}
	refinedJSON = strings.TrimSpace(refinedJSON)
	if refinedJSON != "" {
		var refined refinedPayload
		if err := json.Unmarshal([]byte(refinedJSON), &refined); err == nil {
			if strings.TrimSpace(refined.LocationString) != "" {
				return strings.TrimSpace(refined.LocationString)
			}
		}
	}
	return ""
}

func extractStreet(addressJSON string) string {
	addressJSON = strings.TrimSpace(addressJSON)
	if addressJSON == "" {
		return ""
	}
	var addr addressPayload
	if err := json.Unmarshal([]byte(addressJSON), &addr); err != nil {
		return ""
	}
	return strings.TrimSpace(addr.Street)
}
