// SPDX-License-Identifier: MIT
// sussex.go contains Sussex County heuristics and validators.
package refine

import (
	"strings"
)

var sussexTowns = []string{
	"Andover", "Andover Township", "Byram", "Frankford", "Franklin", "Green",
	"Hamburg", "Hardyston", "Hopatcong", "Lafayette", "Montague", "Newton",
	"Ogdensburg", "Sandyston", "Sparta", "Stanhope", "Stillwater", "Sussex",
	"Vernon", "Wantage", "Fredon", "Branchville",
}

var neighboringCounties = []string{"Warren County", "Morris County", "Passaic County"}

// IsLikelySussexCounty heuristically determines whether a free-form address
// refers to Sussex County, NJ. It returns (true, reason) when confident and
// (false, "uncertain") for ambiguous inputs.
func IsLikelySussexCounty(address string) (bool, string) {
	address = strings.ToLower(strings.TrimSpace(address))
	if address == "" {
		return false, "empty"
	}
	if strings.Contains(address, "sussex county") {
		return true, "explicit"
	}
	for _, town := range sussexTowns {
		if strings.Contains(address, strings.ToLower(town)) {
			return true, "municipality"
		}
	}
	for _, county := range neighboringCounties {
		if strings.Contains(address, strings.ToLower(county)) {
			return false, "neighbor_" + strings.ReplaceAll(strings.ToLower(county), " ", "_")
		}
	}
	if strings.Contains(address, "new jersey") {
		return false, "uncertain"
	}
	return false, "uncertain"
}
