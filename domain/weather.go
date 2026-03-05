package domain

import "time"

// Forecast holds parsed daily weather data for a region from a single source fetch.
type Forecast struct {
	RegionID  string
	FetchedAt time.Time
	Source    string // "open_meteo", "nws"
	DailyData []DailyForecast
}

// DailyForecast holds the weather metrics for a single calendar day.
// The Day and Night half-day periods use OpenSnow's convention:
// Day = 6am–6pm local, Night = 6pm–6am local (evening into next morning).
type DailyForecast struct {
	Date            time.Time
	SnowfallCM      float64 // total for the full calendar day (used by detection)
	TemperatureMinC float64
	TemperatureMaxC float64
	PrecipitationMM float64
	FreezingLevelM  float64 // 0°C isotherm altitude; low values mean snow to base
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

// CMToInches converts centimeters to inches.
func CMToInches(cm float64) float64 { return cm / 2.54 }
