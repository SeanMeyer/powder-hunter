package domain

import "time"

// DetectionResult holds the outcome of checking a region's forecasts against thresholds.
type DetectionResult struct {
	RegionID string
	Detected bool
	Windows  []SnowfallWindow // windows that exceeded thresholds
}

// Detect checks forecasts against the region's friction-tier thresholds.
// Returns which snowfall windows (if any) exceeded detection thresholds.
//
// Two windows are evaluated: near-range (days 1-7) and extended-range (days 8-16),
// both measured from the current UTC date. A detection is triggered when the
// aggregated snowfall from all forecast sources exceeds the region's threshold
// for at least one window.
func Detect(region Region, forecasts []Forecast) DetectionResult {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	nearStart := today.AddDate(0, 0, 1)
	nearEnd := today.AddDate(0, 0, 7)
	extStart := today.AddDate(0, 0, 8)
	extEnd := today.AddDate(0, 0, 16)

	var nearCM, extCM float64
	for _, f := range forecasts {
		for _, d := range f.DailyData {
			day := time.Date(d.Date.Year(), d.Date.Month(), d.Date.Day(), 0, 0, 0, 0, time.UTC)
			switch {
			case !day.Before(nearStart) && !day.After(nearEnd):
				nearCM += d.SnowfallCM
			case !day.Before(extStart) && !day.After(extEnd):
				extCM += d.SnowfallCM
			}
		}
	}

	nearIn := CMToInches(nearCM)
	extIn := CMToInches(extCM)

	var windows []SnowfallWindow
	if nearIn >= region.NearThresholdIn {
		windows = append(windows, SnowfallWindow{
			RegionID:    region.ID,
			StartDate:   nearStart,
			EndDate:     nearEnd,
			TotalIn:     nearIn,
			IsNearRange: true,
		})
	}
	if extIn >= region.ExtendedThresholdIn {
		windows = append(windows, SnowfallWindow{
			RegionID:    region.ID,
			StartDate:   extStart,
			EndDate:     extEnd,
			TotalIn:     extIn,
			IsNearRange: false,
		})
	}

	return DetectionResult{
		RegionID: region.ID,
		Detected: len(windows) > 0,
		Windows:  windows,
	}
}
