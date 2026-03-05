package domain

import (
	"math"
	"time"
)

// Forecast holds parsed daily weather data from a single source fetch.
// Each forecast is tied to a specific resort's coordinates and elevation.
type Forecast struct {
	RegionID  string
	ResortID  string // resort this forecast was queried for; empty for legacy regional queries
	FetchedAt time.Time
	Source    string // "open_meteo", "nws"
	Model     string // weather model name (e.g., "gfs_seamless", "ecmwf_ifs025"); empty for single-model fetches
	DailyData []DailyForecast
}

// DailyForecast holds the weather metrics for a single calendar day.
// The Day and Night half-day periods use OpenSnow's convention:
// Day = 6am–6pm local, Night = 6pm–6am local (evening into next morning).
type DailyForecast struct {
	Date            time.Time
	SnowfallCM      float64 // SLR-adjusted total for the full calendar day (used by detection)
	TemperatureMinC float64
	TemperatureMaxC float64
	PrecipitationMM float64
	FreezingLevelM  float64 // 0°C isotherm altitude; low values mean snow to base
	SLRatio         float64 // effective (weighted average) SLR for the day's snowfall
	RainHours       int     // hours where temp > 1.67°C (35°F) during precipitation
	MixedHours      int     // hours in the 0–1.67°C (32–35°F) mixed zone during precipitation
	Day             HalfDay // 6am–6pm local
	Night           HalfDay // 6pm–6am local (snow here is what you ski next morning)
}

// HalfDay holds weather metrics for a 12-hour period (day or night).
type HalfDay struct {
	SnowfallCM      float64
	TemperatureC    float64 // high for day periods, low for night periods
	PrecipitationMM float64
	WindSpeedKmh    float64
	WindGustKmh     float64
	FreezingLevelMinM float64 // minimum freezing level altitude during period (meters)
	FreezingLevelMaxM float64 // maximum freezing level altitude during period (meters)
}

// SnowfallWindow summarizes accumulated snowfall over a date range. The near/extended
// distinction drives which threshold to compare against for storm detection.
// TotalIn is in inches — weather clients convert from API units (cm) at the parse boundary.
type SnowfallWindow struct {
	RegionID    string
	StartDate   time.Time
	EndDate     time.Time
	TotalIn     float64 // total snowfall in inches
	IsNearRange bool    // true if 1-7 days out, false if 8-16
}

// ModelConsensus aggregates multi-model forecasts for a region.
// Computed as pure domain logic (no I/O).
type ModelConsensus struct {
	RegionID       string
	Models         []string
	DailyConsensus []DayConsensus
}

// DayConsensus holds per-day consensus data across multiple weather models.
type DayConsensus struct {
	Date           time.Time
	SnowfallMinCM  float64
	SnowfallMaxCM  float64
	SnowfallMeanCM float64
	SpreadToMean   float64 // (Max - Min) / Mean; > 1.0 = low confidence
	Confidence     string  // "high" (spread < 0.5), "moderate" (0.5-1.0), "low" (> 1.0)
}

// ForecastDiscussion holds NWS Area Forecast Discussion text for a Weather Forecast Office.
type ForecastDiscussion struct {
	WFO       string    // Weather Forecast Office code (e.g., "SLC")
	IssuedAt  time.Time // when the AFD was published
	Text      string    // full discussion text
	FetchedAt time.Time // when we retrieved it
}

// CMToInches converts centimeters to inches.
func CMToInches(cm float64) float64 { return cm / 2.54 }

// AFDCoversSnowDays checks whether any day with significant snowfall (>=2")
// falls within the AFD's ~7-day coverage from issuance.
func AFDCoversSnowDays(d *ForecastDiscussion, forecasts []Forecast) bool {
	const afdHorizonDays = 7
	afdCoverage := d.IssuedAt.AddDate(0, 0, afdHorizonDays)
	for _, f := range forecasts {
		for _, day := range f.DailyData {
			if CMToInches(day.SnowfallCM) >= 2.0 && !day.Date.After(afdCoverage) {
				return true
			}
		}
	}
	return false
}

// SLR temperature thresholds in Celsius (exact conversions from Fahrenheit bands).
// Contiguous ranges: >1.67°C rain, [0, 1.67°C] mixed, [-3.89, 0) wet, [-9.44, -3.89) dry, <-9.44 cold smoke.
const (
	slrThresholdRainC      = 1.6667  // 35°F — above this is rain
	slrThresholdMixedC     = 0.0     // 32°F — above this (up to rain) is mixed
	slrThresholdWetC       = -3.8889 // 25°F — above this (up to mixed) is wet snow
	slrThresholdDryC       = -9.4444 // 15°F — above this (up to wet) is dry powder
)

// CalculateSLR returns the snow-to-liquid ratio for a given temperature in Celsius.
// Five contiguous bands per spec: rain (0), mixed (5), wet (10), dry (15), cold smoke (20).
func CalculateSLR(tempC float64) float64 {
	switch {
	case tempC > slrThresholdRainC:
		return 0 // rain — no snow
	case tempC >= slrThresholdMixedC:
		return 5 // mixed/very wet
	case tempC >= slrThresholdWetC:
		return 10 // wet snow
	case tempC >= slrThresholdDryC:
		return 15 // dry powder
	default:
		return 20 // cold smoke
	}
}

const (
	// Vionnet density clamps prevent unrealistic values at temperature/wind extremes.
	densityFloor   = 40.0  // kg/m3 — coldest realistic snow (25:1 SLR)
	densityCeiling = 250.0 // kg/m3 — heaviest realistic wet snow (4:1 SLR)
)

// CalculateDensity returns fresh snow density in kg/m3 using the Vionnet et al. (2012)
// formula: density = 109 + 6*T + 26*sqrt(u), clamped to [40, 250] kg/m3.
// Returns 0 for rain (temp above 1.67°C / 35°F).
func CalculateDensity(tempC float64, windSpeedMs float64) float64 {
	if tempC > slrThresholdRainC {
		return 0
	}
	density := 109.0 + 6.0*tempC + 26.0*math.Sqrt(windSpeedMs)
	if density < densityFloor {
		return densityFloor
	}
	if density > densityCeiling {
		return densityCeiling
	}
	return density
}

// SLRFromDensity converts snow density (kg/m3) to snow-to-liquid ratio.
// Returns 0 if density is 0 (rain).
func SLRFromDensity(density float64) float64 {
	if density <= 0 {
		return 0
	}
	return 1000.0 / density
}

// SnowfallFromPrecip returns snowfall in cm for a given hour's precipitation (mm),
// temperature (°C), and wind speed (m/s). Uses Vionnet density model with
// unit conversion: precipMM / 10.0 * SLR (mm→cm × ratio).
func SnowfallFromPrecip(precipMM float64, tempC float64, windSpeedMs float64) float64 {
	if precipMM <= 0 {
		return 0
	}
	density := CalculateDensity(tempC, windSpeedMs)
	if density <= 0 {
		return 0 // rain
	}
	slr := SLRFromDensity(density)
	return precipMM / 10.0 * slr
}

// IsRain returns true if the temperature is above the rain threshold (35°F / 1.67°C).
func IsRain(tempC float64) bool {
	return tempC > slrThresholdRainC
}

// IsMixedPrecip returns true if the temperature is in the mixed precipitation zone
// (32-35°F / 0-1.67°C) — snow may be mixed with rain/sleet.
func IsMixedPrecip(tempC float64) bool {
	return tempC >= 0 && tempC <= slrThresholdRainC
}

// ComputeConsensus takes multiple Forecast values (same region, different models),
// aligns by date, and computes spread/mean/confidence per day. Pure function, no I/O.
func ComputeConsensus(forecasts []Forecast) ModelConsensus {
	if len(forecasts) == 0 {
		return ModelConsensus{}
	}

	regionID := forecasts[0].RegionID
	models := make([]string, 0, len(forecasts))
	for _, f := range forecasts {
		if f.Model != "" {
			models = append(models, f.Model)
		}
	}

	// Collect per-date snowfall values from each model.
	type dateSnow struct {
		values []float64
	}
	byDate := make(map[string]*dateSnow)
	var dateOrder []string

	for _, f := range forecasts {
		for _, d := range f.DailyData {
			key := d.Date.Format("2006-01-02")
			ds, ok := byDate[key]
			if !ok {
				ds = &dateSnow{}
				byDate[key] = ds
				dateOrder = append(dateOrder, key)
			}
			ds.values = append(ds.values, d.SnowfallCM)
		}
	}

	daily := make([]DayConsensus, 0, len(dateOrder))
	for _, key := range dateOrder {
		ds := byDate[key]
		t, _ := time.Parse("2006-01-02", key)

		if len(ds.values) == 0 {
			continue
		}

		minVal := ds.values[0]
		maxVal := ds.values[0]
		sum := 0.0
		for _, v := range ds.values {
			sum += v
			if v < minVal {
				minVal = v
			}
			if v > maxVal {
				maxVal = v
			}
		}
		mean := sum / float64(len(ds.values))

		var spreadToMean float64
		var confidence string
		if mean == 0 {
			// All models agree: no snow → high confidence.
			spreadToMean = 0
			confidence = "high"
		} else {
			spreadToMean = (maxVal - minVal) / mean
			switch {
			case spreadToMean < 0.5:
				confidence = "high"
			case spreadToMean <= 1.0:
				confidence = "moderate"
			default:
				confidence = "low"
			}
		}

		daily = append(daily, DayConsensus{
			Date:           t,
			SnowfallMinCM:  minVal,
			SnowfallMaxCM:  maxVal,
			SnowfallMeanCM: mean,
			SpreadToMean:   spreadToMean,
			Confidence:     confidence,
		})
	}

	return ModelConsensus{
		RegionID:       regionID,
		Models:         models,
		DailyConsensus: daily,
	}
}
