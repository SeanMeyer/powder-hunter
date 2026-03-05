package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/seanmeyer/powder-hunter/domain"
)

const openMeteoEndpoint = "https://api.open-meteo.com/v1/forecast"

const openMeteoHourlyVars = "snowfall,temperature_2m,precipitation,wind_speed_10m,wind_gusts_10m"

type openMeteoResponse struct {
	Hourly openMeteoHourlyData `json:"hourly"`
}

type openMeteoHourlyData struct {
	Time          []string  `json:"time"`
	Snowfall      []float64 `json:"snowfall"`
	Temperature2m []float64 `json:"temperature_2m"`
	Precipitation []float64 `json:"precipitation"`
	WindSpeed10m  []float64 `json:"wind_speed_10m"`
	WindGusts10m  []float64 `json:"wind_gusts_10m"`
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

	daily, err := parseOpenMeteoHourly(raw.Hourly)
	if err != nil {
		slog.ErrorContext(ctx, "open-meteo response parse failed", "region_id", region.ID, "error", err)
		return domain.Forecast{}, fmt.Errorf("parsing open-meteo hourly data for region %s: %w", region.ID, err)
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
	params.Set("hourly", openMeteoHourlyVars)
	params.Set("forecast_days", "16")
	tz := region.Timezone
	if tz == "" {
		tz = "auto"
	}
	params.Set("timezone", tz)
	return openMeteoEndpoint + "?" + params.Encode()
}

// parseOpenMeteoHourly aggregates hourly data into daily forecasts with day/night
// half-day breakdown. Day = hours 6-17 (6am-6pm), Night = hours 18-5 (6pm-6am).
// Open-Meteo timestamps are in the requested timezone, so hour checks are local.
func parseOpenMeteoHourly(h openMeteoHourlyData) ([]domain.DailyForecast, error) {
	n := len(h.Time)
	if n == 0 {
		return nil, fmt.Errorf("hourly.time array is empty")
	}
	if len(h.Snowfall) != n || len(h.Temperature2m) != n ||
		len(h.Precipitation) != n || len(h.WindSpeed10m) != n ||
		len(h.WindGusts10m) != n {
		return nil, fmt.Errorf("hourly arrays have inconsistent lengths (time=%d)", n)
	}

	type dayAccum struct {
		date          string
		snowCM        float64
		tempMin       float64
		tempMax       float64
		precipMM      float64
		daySnowCM     float64
		dayTempMax    float64
		dayPrecipMM   float64
		dayWindMax    float64
		dayGustMax    float64
		nightSnowCM   float64
		nightTempMin  float64
		nightPrecipMM float64
		nightWindMax  float64
		nightGustMax  float64
		dayInit       bool
		nightInit     bool
		tempInit      bool
	}
	byDate := make(map[string]*dayAccum)

	for i, ts := range h.Time {
		t, err := time.Parse("2006-01-02T15:04", ts)
		if err != nil {
			continue
		}
		dateKey := t.Format("2006-01-02")
		hour := t.Hour()

		acc, ok := byDate[dateKey]
		if !ok {
			acc = &dayAccum{date: dateKey}
			byDate[dateKey] = acc
		}

		snow := h.Snowfall[i]
		temp := h.Temperature2m[i]
		precip := h.Precipitation[i]
		wind := h.WindSpeed10m[i]
		gust := h.WindGusts10m[i]

		acc.snowCM += snow
		acc.precipMM += precip
		if !acc.tempInit {
			acc.tempMin = temp
			acc.tempMax = temp
			acc.tempInit = true
		} else {
			acc.tempMin = math.Min(acc.tempMin, temp)
			acc.tempMax = math.Max(acc.tempMax, temp)
		}

		if hour >= 6 && hour < 18 { // day: 6am-6pm
			acc.daySnowCM += snow
			acc.dayPrecipMM += precip
			acc.dayWindMax = math.Max(acc.dayWindMax, wind)
			acc.dayGustMax = math.Max(acc.dayGustMax, gust)
			if !acc.dayInit {
				acc.dayTempMax = temp
				acc.dayInit = true
			} else {
				acc.dayTempMax = math.Max(acc.dayTempMax, temp)
			}
		} else { // night: 6pm-6am
			acc.nightSnowCM += snow
			acc.nightPrecipMM += precip
			acc.nightWindMax = math.Max(acc.nightWindMax, wind)
			acc.nightGustMax = math.Max(acc.nightGustMax, gust)
			if !acc.nightInit {
				acc.nightTempMin = temp
				acc.nightInit = true
			} else {
				acc.nightTempMin = math.Min(acc.nightTempMin, temp)
			}
		}
	}

	dates := make([]string, 0, len(byDate))
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	forecasts := make([]domain.DailyForecast, 0, len(dates))
	for _, d := range dates {
		acc := byDate[d]
		t, _ := time.Parse("2006-01-02", d)
		forecasts = append(forecasts, domain.DailyForecast{
			Date:            t,
			SnowfallCM:      acc.snowCM,
			TemperatureMinC: acc.tempMin,
			TemperatureMaxC: acc.tempMax,
			PrecipitationMM: acc.precipMM,
			Day: domain.HalfDay{
				SnowfallCM:      acc.daySnowCM,
				TemperatureC:    acc.dayTempMax,
				PrecipitationMM: acc.dayPrecipMM,
				WindSpeedKmh:    acc.dayWindMax,
				WindGustKmh:     acc.dayGustMax,
			},
			Night: domain.HalfDay{
				SnowfallCM:      acc.nightSnowCM,
				TemperatureC:    acc.nightTempMin,
				PrecipitationMM: acc.nightPrecipMM,
				WindSpeedKmh:    acc.nightWindMax,
				WindGustKmh:     acc.nightGustMax,
			},
		})
	}
	return forecasts, nil
}
