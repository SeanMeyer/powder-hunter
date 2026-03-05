package domain

import (
	"math"
	"time"
)

// SnowQuality holds per-day ride quality signals computed from weather data.
// RideQualityNotes contains pre-interpreted human-readable assessments.
type SnowQuality struct {
	CrystalQuality    string  // "intact" / "partially_broken" / "wind_broken"
	WindDuringSnowMph float64 // avg wind speed during hours with precipitation
	DensityCategory   string  // "cold_smoke" / "dry_powder" / "standard" / "heavy" / "wet_cement"
	AvgDensityKgM3    float64 // average Vionnet density during precip hours
	Bluebird          bool    // clear skies + recent fresh snow
	CloudCoverPct     float64 // daytime average cloud cover

	BaseRisk       string // "low" / "moderate" / "high"
	BaseRiskReason string // human-readable explanation

	RideQualityNotes []string // pre-interpreted assessments for LLM and trace output
}

// DensityCategory returns a human-readable density classification from kg/m3.
// Returns empty string for zero density (rain).
func DensityCategory(densityKgM3 float64) string {
	switch {
	case densityKgM3 <= 0:
		return ""
	case densityKgM3 < 60:
		return "cold_smoke"
	case densityKgM3 < 90:
		return "dry_powder"
	case densityKgM3 < 130:
		return "standard"
	case densityKgM3 < 180:
		return "heavy"
	default:
		return "wet_cement"
	}
}

// CrystalQuality classifies crystal integrity based on average wind during snowfall (mph).
func CrystalQuality(avgWindMph float64) string {
	switch {
	case avgWindMph < 15:
		return "intact"
	case avgWindMph < 25:
		return "partially_broken"
	default:
		return "wind_broken"
	}
}

// SolarElevationAtNoon returns the sun's elevation angle in degrees at solar noon
// for a given latitude and date. Uses standard astronomical formula.
func SolarElevationAtNoon(latitudeDeg float64, date time.Time) float64 {
	doy := float64(date.YearDay())
	declination := 23.45 * math.Sin(2*math.Pi*(284+doy)/365.0)
	elevation := 90.0 - math.Abs(latitudeDeg-declination)
	return elevation
}
