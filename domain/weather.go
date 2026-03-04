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
type DailyForecast struct {
	Date            time.Time
	SnowfallCM      float64
	TemperatureMinC float64
	TemperatureMaxC float64
	PrecipitationMM float64
	FreezingLevelM  float64 // 0°C isotherm altitude; low values mean snow to base
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
