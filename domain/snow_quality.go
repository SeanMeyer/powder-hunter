package domain

import (
	"fmt"
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

// AssessBaseRisk evaluates the risk of a hard layer under new snow.
func AssessBaseRisk(preStormDays []DailyForecast, latitude float64, stormStartDate time.Time) (risk string, reason string) {
	if len(preStormDays) == 0 {
		return "low", ""
	}

	var warmHours int
	var solarRisk bool
	solarElevation := SolarElevationAtNoon(latitude, stormStartDate)

	for _, d := range preStormDays {
		if d.TemperatureMaxC > 0 {
			if d.TemperatureMaxC > 3 {
				warmHours += 6
			} else {
				warmHours += 2
			}
		}

		// Solar crust check — sun angle determines temperature threshold.
		tempThresholdC := -3.0
		if solarElevation > 55 {
			tempThresholdC = -8.0
		} else if solarElevation > 45 {
			tempThresholdC = -5.0
		} else if solarElevation < 35 {
			continue // Dec-Jan sun too weak
		}

		if d.Day.CloudCoverPct < 30 && d.Day.TemperatureC > tempThresholdC {
			solarRisk = true
		}
	}

	switch {
	case warmHours >= 6:
		return "high", fmt.Sprintf("above freezing for ~%d hours before storm — melt-freeze crust likely", warmHours)
	case warmHours >= 1 && solarRisk:
		return "high", "melt-freeze and sun crust both likely"
	case solarRisk:
		return "moderate", fmt.Sprintf("clear skies with solar elevation %.0f° — sun crust possible on south-facing terrain", solarElevation)
	case warmHours >= 1:
		return "moderate", "brief above-freezing period — possible melt-freeze layer"
	default:
		return "low", ""
	}
}

// AssessRideQuality computes snow quality signals for each day in the forecast.
// preStormDays are the days before snowfall begins (for base risk).
func AssessRideQuality(days []DailyForecast, preStormDays []DailyForecast, latitude float64, stormStartDate time.Time) []SnowQuality {
	if len(days) == 0 {
		return nil
	}

	baseRisk, baseRiskReason := AssessBaseRisk(preStormDays, latitude, stormStartDate)

	densityForDay := func(d DailyForecast) float64 {
		if d.SLRatio <= 0 {
			return 0
		}
		return 1000.0 / d.SLRatio
	}

	avgWindDuringSnow := func(d DailyForecast) float64 {
		var totalWind, totalPrecip float64
		if d.Day.PrecipitationMM > 0 {
			totalWind += d.Day.WindSpeedKmh * d.Day.PrecipitationMM
			totalPrecip += d.Day.PrecipitationMM
		}
		if d.Night.PrecipitationMM > 0 {
			totalWind += d.Night.WindSpeedKmh * d.Night.PrecipitationMM
			totalPrecip += d.Night.PrecipitationMM
		}
		if totalPrecip <= 0 {
			return 0
		}
		return totalWind / totalPrecip * 0.621371 // km/h → mph
	}

	qualities := make([]SnowQuality, len(days))

	for i, d := range days {
		snowIn := CMToInches(d.SnowfallCM)
		density := densityForDay(d)
		densCat := DensityCategory(density)
		windMph := avgWindDuringSnow(d)
		crystalQ := CrystalQuality(windMph)

		q := SnowQuality{
			DensityCategory:   densCat,
			AvgDensityKgM3:    density,
			CrystalQuality:    crystalQ,
			WindDuringSnowMph: windMph,
			CloudCoverPct:     d.Day.CloudCoverPct,
			BaseRisk:          baseRisk,
			BaseRiskReason:    baseRiskReason,
		}

		isSnowDay := snowIn >= 0.5
		var notes []string

		if isSnowDay {
			switch crystalQ {
			case "intact":
				notes = append(notes, "Fresh dendrites likely — expect true powder feel")
			case "partially_broken":
				notes = append(notes, "Moderate wind during snowfall — crystals partially broken, still good but not the lightest")
			case "wind_broken":
				notes = append(notes, "Heavy wind during snowfall — snow will feel chalky on exposed terrain, best quality in protected trees")
			}
		}

		// Layering.
		if isSnowDay && i > 0 {
			prevDensity := densityForDay(days[i-1])
			prevCat := DensityCategory(prevDensity)
			prevSnowIn := CMToInches(days[i-1].SnowfallCM)

			if prevSnowIn >= 0.5 && prevCat != "" && densCat != "" && prevCat != densCat {
				prevIsLight := prevCat == "cold_smoke" || prevCat == "dry_powder"
				curIsLight := densCat == "cold_smoke" || densCat == "dry_powder"

				if !prevIsLight && curIsLight {
					notes = append(notes, fmt.Sprintf("Favorable layering — light snow over supportive dense base from %s",
						days[i-1].Date.Format("Jan 02")))
				} else if prevIsLight && !curIsLight {
					notes = append(notes, "New heavy snow over lighter layer — may feel punchy and inconsistent")
				}
			}
		}

		// Base risk + punch-through (first snow day only).
		if isSnowDay && baseRisk != "low" {
			isFirstSnowDay := true
			for j := 0; j < i; j++ {
				if CMToInches(days[j].SnowfallCM) >= 0.5 {
					isFirstSnowDay = false
					break
				}
			}

			if isFirstSnowDay {
				notes = append(notes, baseRiskReason)

				if baseRisk == "high" {
					switch {
					case densCat == "cold_smoke" && snowIn < 12:
						notes = append(notes, "Likely punching through to hard layer underneath")
					case densCat == "cold_smoke" && snowIn >= 12:
						notes = append(notes, "Deep enough to float above the crust, but may hit it in thin spots")
					case densCat == "dry_powder" && snowIn < 8:
						notes = append(notes, "May punch through to crust in spots")
					case densCat == "dry_powder" && snowIn >= 8:
						notes = append(notes, "Should have enough depth over the crust")
					case snowIn >= 4:
						notes = append(notes, "Dense enough to ride without hitting the crust")
					}
				} else if baseRisk == "moderate" && densCat == "cold_smoke" && snowIn < 8 {
					notes = append(notes, "Thin — could feel inconsistent over variable base")
				}
			}
		}

		// Bluebird.
		if i > 0 {
			prevSnowIn := CMToInches(days[i-1].SnowfallCM)
			prevNightSnowIn := CMToInches(days[i-1].Night.SnowfallCM)
			if d.Day.CloudCoverPct < 20 && (prevNightSnowIn >= 4 || prevSnowIn >= 4) {
				q.Bluebird = true
				notes = append(notes, "Bluebird powder day — clear skies with fresh snow")
			}
		}

		q.RideQualityNotes = notes
		qualities[i] = q
	}

	return qualities
}
