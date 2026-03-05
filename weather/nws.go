package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/seanmeyer/powder-hunter/domain"
)

const (
	nwsBaseURL   = "https://api.weather.gov"
	nwsUserAgent = "(powder-hunter, contact@example.com)"

	// NWS snowfall values are in cm; values above this are sensor/model artifacts.
	nwsSnowfallSanityLimitCM = 500.0
)

// NWSClient fetches near-range forecasts from the National Weather Service API.
// The NWS two-step API first resolves coordinates to a forecast grid (WFO/x/y),
// then fetches gridpoint data containing time-series values for each weather element.
type NWSClient struct {
	client    *http.Client
	gridCache map[string]nwsGrid // keyed by "lat,lon" rounded to 4dp
}

type nwsGrid struct {
	WFO   string
	GridX int
	GridY int
}

func NewNWSClient(client *http.Client) *NWSClient {
	return &NWSClient{
		client:    client,
		gridCache: make(map[string]nwsGrid),
	}
}

// Fetch retrieves a forecast from NWS for a US region. Returns an error for
// non-US coordinates (NWS 404) or unrecoverable API failures.
func (c *NWSClient) Fetch(ctx context.Context, region domain.Region) (domain.Forecast, error) {
	grid, err := c.resolveGrid(ctx, region.Latitude, region.Longitude)
	if err != nil {
		return domain.Forecast{}, fmt.Errorf("nws: resolve grid for region %s: %w", region.ID, err)
	}

	raw, err := c.fetchGridpoint(ctx, grid)
	if err != nil {
		return domain.Forecast{}, fmt.Errorf("nws: fetch gridpoint for region %s: %w", region.ID, err)
	}

	loc := time.UTC
	if region.Timezone != "" {
		if l, tzErr := time.LoadLocation(region.Timezone); tzErr == nil {
			loc = l
		}
	}

	daily, err := parseGridpointForecast(raw, loc)
	if err != nil {
		return domain.Forecast{}, fmt.Errorf("nws: parse gridpoint for region %s: %w", region.ID, err)
	}

	return domain.Forecast{
		RegionID:  region.ID,
		FetchedAt: time.Now().UTC(),
		Source:    "nws",
		DailyData: daily,
	}, nil
}

// resolveGrid returns the NWS forecast grid for the given coordinates, using
// the in-process cache to avoid repeated /points calls for the same location.
func (c *NWSClient) resolveGrid(ctx context.Context, lat, lon float64) (nwsGrid, error) {
	key := gridCacheKey(lat, lon)
	if grid, ok := c.gridCache[key]; ok {
		return grid, nil
	}

	lat4 := roundTo4(lat)
	lon4 := roundTo4(lon)
	url := fmt.Sprintf("%s/points/%.4f,%.4f", nwsBaseURL, lat4, lon4)

	body, statusCode, err := c.get(ctx, url)
	if err != nil {
		return nwsGrid{}, err
	}
	if statusCode == http.StatusNotFound {
		return nwsGrid{}, fmt.Errorf("coordinates %.4f,%.4f not covered by NWS (not a US location)", lat4, lon4)
	}
	if statusCode != http.StatusOK {
		return nwsGrid{}, fmt.Errorf("points endpoint returned HTTP %d", statusCode)
	}

	var resp nwsPointsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nwsGrid{}, fmt.Errorf("decode points response: %w", err)
	}

	grid := nwsGrid{
		WFO:   resp.Properties.GridID,
		GridX: resp.Properties.GridX,
		GridY: resp.Properties.GridY,
	}
	c.gridCache[key] = grid
	return grid, nil
}

func (c *NWSClient) fetchGridpoint(ctx context.Context, grid nwsGrid) ([]byte, error) {
	url := fmt.Sprintf("%s/gridpoints/%s/%d,%d", nwsBaseURL, grid.WFO, grid.GridX, grid.GridY)
	body, statusCode, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("gridpoints endpoint returned HTTP %d", statusCode)
	}
	return body, nil
}

func (c *NWSClient) get(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", nwsUserAgent)
	req.Header.Set("Accept", "application/geo+json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("http get %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response body: %w", err)
	}
	return body, resp.StatusCode, nil
}

// parseGridpointForecast extracts daily snowfall, temperature, precipitation, and
// wind from the NWS gridpoint response with day/night half-day breakdown.
// loc determines the local timezone for 6am/6pm boundaries.
func parseGridpointForecast(body []byte, loc *time.Location) ([]domain.DailyForecast, error) {
	var resp nwsGridpointResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode gridpoint response: %w", err)
	}

	type dayAccum struct {
		snowCM, precipMM                   float64
		tempMin, tempMax                   float64
		tempInit                           bool
		daySnowCM, dayPrecipMM             float64
		dayTempMax, dayWindMax, dayGustMax float64
		dayInit                            bool
		nightSnowCM, nightPrecipMM         float64
		nightTempMin, nightWindMax, nightGustMax float64
		nightInit                          bool
	}
	byDate := make(map[string]*dayAccum)
	getAcc := func(dateKey string) *dayAccum {
		if a, ok := byDate[dateKey]; ok {
			return a
		}
		a := &dayAccum{}
		byDate[dateKey] = a
		return a
	}

	// Helper: walk an interval in 1-hour steps, calling fn with (localDate, localHour) for each.
	walkHourly := func(values []nwsValue, fn func(acc *dayAccum, hour int, val float64)) {
		for _, v := range values {
			if v.Value == nil {
				continue
			}
			start, dur, err := parseISO8601Interval(v.ValidTime)
			if err != nil || dur <= 0 {
				continue
			}
			end := start.Add(dur)
			hourlyVal := *v.Value / dur.Hours() // prorate per hour
			for t := start; t.Before(end); t = t.Add(time.Hour) {
				local := t.In(loc)
				dateKey := local.Format("2006-01-02")
				fn(getAcc(dateKey), local.Hour(), hourlyVal)
			}
		}
	}

	// Snowfall (NWS reports mm, convert to cm).
	walkHourly(resp.Properties.SnowfallAmount.Values, func(acc *dayAccum, hour int, mmPerHour float64) {
		cm := mmPerHour / 10.0
		if cm > nwsSnowfallSanityLimitCM {
			return
		}
		acc.snowCM += cm
		if hour >= 6 && hour < 18 {
			acc.daySnowCM += cm
		} else {
			acc.nightSnowCM += cm
		}
	})

	// Temperature (°C).
	walkHourly(resp.Properties.Temperature.Values, func(acc *dayAccum, hour int, c float64) {
		if !acc.tempInit {
			acc.tempMin, acc.tempMax = c, c
			acc.tempInit = true
		} else {
			if c < acc.tempMin { acc.tempMin = c }
			if c > acc.tempMax { acc.tempMax = c }
		}
		if hour >= 6 && hour < 18 {
			if !acc.dayInit {
				acc.dayTempMax = c
				acc.dayInit = true
			} else if c > acc.dayTempMax {
				acc.dayTempMax = c
			}
		} else {
			if !acc.nightInit {
				acc.nightTempMin = c
				acc.nightInit = true
			} else if c < acc.nightTempMin {
				acc.nightTempMin = c
			}
		}
	})

	// Precipitation (mm).
	walkHourly(resp.Properties.QuantitativePrecipitation.Values, func(acc *dayAccum, hour int, mm float64) {
		acc.precipMM += mm
		if hour >= 6 && hour < 18 {
			acc.dayPrecipMM += mm
		} else {
			acc.nightPrecipMM += mm
		}
	})

	// Wind speed (km/h) — NWS provides in km/h.
	walkHourly(resp.Properties.WindSpeed.Values, func(acc *dayAccum, hour int, kmh float64) {
		if hour >= 6 && hour < 18 {
			if kmh > acc.dayWindMax { acc.dayWindMax = kmh }
		} else {
			if kmh > acc.nightWindMax { acc.nightWindMax = kmh }
		}
	})

	// Wind gusts (km/h).
	walkHourly(resp.Properties.WindGust.Values, func(acc *dayAccum, hour int, kmh float64) {
		if hour >= 6 && hour < 18 {
			if kmh > acc.dayGustMax { acc.dayGustMax = kmh }
		} else {
			if kmh > acc.nightGustMax { acc.nightGustMax = kmh }
		}
	})

	dates := make([]string, 0, len(byDate))
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	daily := make([]domain.DailyForecast, 0, len(dates))
	for _, d := range dates {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			continue
		}
		acc := byDate[d]
		daily = append(daily, domain.DailyForecast{
			Date:            t.UTC(),
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
	return daily, nil
}


// parseISO8601Interval parses the NWS validTime format "2006-01-02T15:04:05+07:00/PTxH".
func parseISO8601Interval(s string) (time.Time, time.Duration, error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return time.Time{}, 0, fmt.Errorf("invalid interval %q: missing /", s)
	}
	t, err := time.Parse(time.RFC3339, parts[0])
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("parse interval start %q: %w", parts[0], err)
	}
	dur, err := parseISO8601Duration(parts[1])
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("parse interval duration %q: %w", parts[1], err)
	}
	return t, dur, nil
}

// parseISO8601Duration parses the subset of ISO 8601 durations used by NWS:
// PTxH (hours) and P1D (one day). NWS does not use months or years in gridpoint data.
func parseISO8601Duration(s string) (time.Duration, error) {
	if !strings.HasPrefix(s, "P") {
		return 0, fmt.Errorf("duration %q must start with P", s)
	}
	s = s[1:] // strip leading P

	var totalDur time.Duration
	timeSection := false

	for len(s) > 0 {
		if s[0] == 'T' {
			timeSection = true
			s = s[1:]
			continue
		}
		// Find the next digit sequence and its unit.
		i := 0
		for i < len(s) && (s[i] >= '0' && s[i] <= '9') {
			i++
		}
		if i == 0 || i >= len(s) {
			return 0, fmt.Errorf("unexpected token in duration %q at %q", s, s)
		}
		n, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0, err
		}
		unit := s[i]
		s = s[i+1:]

		switch {
		case !timeSection && unit == 'D':
			totalDur += time.Duration(n) * 24 * time.Hour
		case !timeSection && unit == 'W':
			totalDur += time.Duration(n) * 7 * 24 * time.Hour
		case timeSection && unit == 'H':
			totalDur += time.Duration(n) * time.Hour
		case timeSection && unit == 'M':
			totalDur += time.Duration(n) * time.Minute
		case timeSection && unit == 'S':
			totalDur += time.Duration(n) * time.Second
		default:
			return 0, fmt.Errorf("unsupported duration unit %q in %q", string(unit), s)
		}
	}
	return totalDur, nil
}


func gridCacheKey(lat, lon float64) string {
	return fmt.Sprintf("%.4f,%.4f", roundTo4(lat), roundTo4(lon))
}

func roundTo4(v float64) float64 {
	return math.Round(v*10000) / 10000
}

// NWS API response types.

type nwsPointsResponse struct {
	Properties struct {
		GridID string `json:"gridId"`
		GridX  int    `json:"gridX"`
		GridY  int    `json:"gridY"`
	} `json:"properties"`
}

type nwsGridpointResponse struct {
	Properties struct {
		SnowfallAmount            nwsTimeSeries `json:"snowfallAmount"`
		Temperature               nwsTimeSeries `json:"temperature"`
		QuantitativePrecipitation nwsTimeSeries `json:"quantitativePrecipitation"`
		WindSpeed                 nwsTimeSeries `json:"windSpeed"`
		WindGust                  nwsTimeSeries `json:"windGust"`
	} `json:"properties"`
}

type nwsTimeSeries struct {
	Values []nwsValue `json:"values"`
}

// nwsValue holds a single time-series entry. Value is a pointer because the
// NWS API returns explicit JSON null for periods with no data.
type nwsValue struct {
	ValidTime string   `json:"validTime"`
	Value     *float64 `json:"value"`
}
