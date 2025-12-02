package formatting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type GeocoderConfig struct {
	BaseURL string
	Token   string
	BBox    []float64
}

func (cfg GeocoderConfig) endpoint() string {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		return "https://api.mapbox.com/geocoding/v5/mapbox.places/"
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base
}

func GeocodeParsedLocation(ctx context.Context, client *http.Client, cfg GeocoderConfig, loc *ParsedLocation) (float64, float64, string, error) {
	if loc == nil {
		return 0, 0, "", errors.New("missing location")
	}
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return 0, 0, "", errors.New("mapbox token missing")
	}
	query, precision := buildGeocodeQuery(loc)
	if query == "" {
		return 0, 0, precision, errors.New("no geocode query built")
	}

	encoded := url.PathEscape(query)
	endpoint := fmt.Sprintf("%s%s.json?access_token=%s&limit=1&country=US&language=en", cfg.endpoint(), encoded, token)
	if len(cfg.BBox) == 4 {
		endpoint += fmt.Sprintf("&bbox=%f,%f,%f,%f", cfg.BBox[0], cfg.BBox[1], cfg.BBox[2], cfg.BBox[3])
	}
	if precision == "intersection" {
		endpoint += "&types=intersection"
	} else if precision == "address" {
		endpoint += "&types=address"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, 0, precision, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, precision, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return 0, 0, precision, fmt.Errorf("mapbox status %d", resp.StatusCode)
	}

	var data struct {
		Features []struct {
			Center []float64 `json:"center"`
		} `json:"features"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, 0, precision, err
	}
	if len(data.Features) == 0 || len(data.Features[0].Center) < 2 {
		return 0, 0, precision, errors.New("no mapbox features")
	}
	lng := data.Features[0].Center[0]
	lat := data.Features[0].Center[1]
	return lat, lng, precision, nil
}

func buildGeocodeQuery(loc *ParsedLocation) (string, string) {
	var queryParts []string
	precision := "municipality"

	switch {
	case loc.HouseNumber != "" && loc.Street != "":
		queryParts = append(queryParts, strings.TrimSpace(loc.HouseNumber+" "+loc.Street))
		precision = "address"
	case loc.Street != "" && loc.CrossStreet != "":
		queryParts = append(queryParts, strings.TrimSpace(loc.Street+" & "+loc.CrossStreet))
		precision = "intersection"
	case loc.Street != "":
		queryParts = append(queryParts, loc.Street)
		precision = "street"
	}
	if loc.Municipality != "" {
		queryParts = append(queryParts, loc.Municipality)
	}
	queryParts = append(queryParts, "NJ")

	query := strings.Join(queryParts, ", ")
	return strings.TrimSpace(query), precision
}
