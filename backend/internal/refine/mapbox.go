// SPDX-License-Identifier: MIT
// mapbox.go provides the Mapbox Search v6 helpers for address verification.
package refine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"alert_framework/config"
)

type mapboxFeature struct {
	Street     string
	City       string
	County     string
	State      string
	Confidence float64
	Lat        float64
	Lon        float64
}

func forwardGeocode(ctx context.Context, client *http.Client, token, query string, cfg config.NLPConfig) (*mapboxFeature, error) {
	query = strings.TrimSpace(query)
	if query == "" || token == "" {
		return nil, errors.New("missing query or token")
	}
	form := url.Values{}
	form.Set("q", query)
	form.Set("limit", "1")
	form.Set("country", "US")
	form.Set("autocomplete", "false")
	form.Set("types", "address,place,locality")
	if len(cfg.MapboxBoundingBox) == 4 {
		form.Set("bbox", fmt.Sprintf("%f,%f,%f,%f", cfg.MapboxBoundingBox[0], cfg.MapboxBoundingBox[1], cfg.MapboxBoundingBox[2], cfg.MapboxBoundingBox[3]))
	}

	endpoint := url.URL{
		Scheme:   "https",
		Host:     "api.mapbox.com",
		Path:     "/search/geocode/v6/forward",
		RawQuery: form.Encode(),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	qp := req.URL.Query()
	qp.Set("access_token", token)
	req.URL.RawQuery = qp.Encode()

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("mapbox status %d", resp.StatusCode)
	}

	var decoded struct {
		Features []struct {
			PlaceName  string  `json:"place_name"`
			Relevance  float64 `json:"relevance"`
			Properties struct {
				Name       string  `json:"name"`
				Address    string  `json:"address"`
				Accuracy   string  `json:"accuracy"`
				Confidence float64 `json:"confidence"`
			} `json:"properties"`
			Geometry struct {
				Coordinates []float64 `json:"coordinates"`
			} `json:"geometry"`
			Context []struct {
				ID   string `json:"id"`
				Text string `json:"text"`
			} `json:"context"`
		} `json:"features"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	if len(decoded.Features) == 0 {
		return nil, nil
	}
	feat := decoded.Features[0]
	if len(feat.Geometry.Coordinates) < 2 {
		return nil, nil
	}

	result := &mapboxFeature{
		Confidence: feat.Properties.Confidence,
		Lon:        feat.Geometry.Coordinates[0],
		Lat:        feat.Geometry.Coordinates[1],
	}
	result.Street = strings.TrimSpace(strings.Join([]string{feat.Properties.Address, feat.Properties.Name}, " "))
	result.Street = strings.TrimSpace(result.Street)

	for _, ctxEntry := range feat.Context {
		switch {
		case strings.HasPrefix(ctxEntry.ID, "place"):
			result.City = ctxEntry.Text
		case strings.HasPrefix(ctxEntry.ID, "district"):
			result.County = ctxEntry.Text
		case strings.HasPrefix(ctxEntry.ID, "region"):
			result.State = ctxEntry.Text
		case strings.HasPrefix(ctxEntry.ID, "postcode") && strings.TrimSpace(result.City) == "":
			result.City = ctxEntry.Text
		}
	}

	if result.State == "" {
		result.State = "NJ"
	}
	return result, nil
}
