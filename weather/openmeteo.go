package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/seanmeyer/powder-hunter/domain"
)

const openMeteoEndpoint = "https://api.open-meteo.com/v1/forecast"

// openMeteoDailyVars are the daily variables requested from the API. Order must
// match the documented parallel-array response.
const openMeteoDailyVars = "snowfall_sum,temperature_2m_max,temperature_2m_min,precipitation_sum"

type openMeteoResponse struct {
	Daily openMeteoDailyData `json:"daily"`
}

type openMeteoDailyData struct {
	Time             []string  `json:"time"`
	SnowfallSum      []float64 `json:"snowfall_sum"`
	Temperature2mMax []float64 `json:"temperature_2m_max"`
	Temperature2mMin []float64 `json:"temperature_2m_min"`
	PrecipitationSum []float64 `json:"precipitation_sum"`
}

// OpenMeteoClient fetches weather forecasts from the Open-Meteo public API.
// No API key is required.
type OpenMeteoClient struct {
	client *http.Client
}

func NewOpenMeteoClient(client *http.Client) *OpenMeteoClient {
	return &OpenMeteoClient{client: client}
}

func (c *OpenMeteoClient) Fetch(ctx context.Context, region domain.Region) (domain.Forecast, error) {
	reqURL := buildOpenMeteoURL(region)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return domain.Forecast{}, fmt.Errorf("building open-meteo request for region %s: %w", region.ID, err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		slog.ErrorContext(ctx, "open-meteo request failed", "region_id", region.ID, "error", err)
		return domain.Forecast{}, fmt.Errorf("open-meteo request for region %s: %w", region.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.ErrorContext(ctx, "open-meteo returned non-200", "region_id", region.ID, "status", resp.StatusCode)
		return domain.Forecast{}, fmt.Errorf("open-meteo returned HTTP %d for region %s", resp.StatusCode, region.ID)
	}

	var raw openMeteoResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		slog.ErrorContext(ctx, "open-meteo response decode failed", "region_id", region.ID, "error", err)
		return domain.Forecast{}, fmt.Errorf("decoding open-meteo response for region %s: %w", region.ID, err)
	}

	daily, err := parseOpenMeteoDailyData(raw.Daily)
	if err != nil {
		slog.ErrorContext(ctx, "open-meteo response parse failed", "region_id", region.ID, "error", err)
		return domain.Forecast{}, fmt.Errorf("parsing open-meteo daily data for region %s: %w", region.ID, err)
	}

	return domain.Forecast{
		RegionID:  region.ID,
		FetchedAt: time.Now(),
		Source:    "open_meteo",
		DailyData: daily,
	}, nil
}

func buildOpenMeteoURL(region domain.Region) string {
	params := url.Values{}
	params.Set("latitude", strconv.FormatFloat(region.Latitude, 'f', -1, 64))
	params.Set("longitude", strconv.FormatFloat(region.Longitude, 'f', -1, 64))
	params.Set("daily", openMeteoDailyVars)
	params.Set("forecast_days", "16")
	params.Set("timezone", "auto")
	return openMeteoEndpoint + "?" + params.Encode()
}

func parseOpenMeteoDailyData(d openMeteoDailyData) ([]domain.DailyForecast, error) {
	n := len(d.Time)
	if n == 0 {
		return nil, fmt.Errorf("daily.time array is empty")
	}

	// Guard against truncated parallel arrays before indexing.
	if len(d.SnowfallSum) != n ||
		len(d.Temperature2mMax) != n ||
		len(d.Temperature2mMin) != n ||
		len(d.PrecipitationSum) != n {
		return nil, fmt.Errorf("daily arrays have inconsistent lengths (time=%d)", n)
	}

	forecasts := make([]domain.DailyForecast, 0, n)
	for i := range d.Time {
		date, err := time.Parse("2006-01-02", d.Time[i])
		if err != nil {
			return nil, fmt.Errorf("parsing date %q at index %d: %w", d.Time[i], i, err)
		}
		forecasts = append(forecasts, domain.DailyForecast{
			Date:            date,
			SnowfallCM:      d.SnowfallSum[i],
			TemperatureMaxC: d.Temperature2mMax[i],
			TemperatureMinC: d.Temperature2mMin[i],
			PrecipitationMM: d.PrecipitationSum[i],
		})
	}
	return forecasts, nil
}
