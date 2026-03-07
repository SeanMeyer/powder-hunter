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
// Three window types are evaluated against the current UTC date:
//   - Near-range (days 1-7): uses near threshold
//   - Extended-range (days 8-16): uses extended threshold
//   - Bridge windows (days 4-10, 7-13): use near threshold, only checked when
//     neither fixed window triggers — catches storms straddling the boundary
//
// Source preference is applied per-day: NWS preferred for days 1-7 when it has
// data, Open-Meteo otherwise. This uses each source where it's strongest without
// double-counting the same snowfall.
func Detect(region Region, forecasts []Forecast, now time.Time) DetectionResult {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	nearStart := today.AddDate(0, 0, 1)
	nearEnd := today.AddDate(0, 0, 7)
	extStart := today.AddDate(0, 0, 8)
	extEnd := today.AddDate(0, 0, 16)

	daily := preferredDailySnowfall(today, forecasts)

	nearIn := CMToInches(sumDailyCM(daily, nearStart, nearEnd))
	extIn := CMToInches(sumDailyCM(daily, extStart, extEnd))

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

	// Bridge windows catch storms straddling the near/extended boundary.
	// Only checked when neither fixed window triggered AND there is snowfall
	// on both sides of the boundary — prevents pure extended-range snow from
	// triggering against the lower near-range threshold.
	if len(windows) == 0 {
		hasNearSnow := sumDailyCM(daily, nearStart, nearEnd) > 0
		hasExtSnow := sumDailyCM(daily, extStart, extEnd) > 0

		if hasNearSnow && hasExtSnow {
			type bridge struct{ startDay, endDay int }
			bridges := []bridge{{4, 10}, {7, 13}}
			for _, b := range bridges {
				bStart := today.AddDate(0, 0, b.startDay)
				bEnd := today.AddDate(0, 0, b.endDay)
				bIn := CMToInches(sumDailyCM(daily, bStart, bEnd))
				if bIn >= region.NearThresholdIn {
					windows = append(windows, SnowfallWindow{
						RegionID:    region.ID,
						StartDate:   bStart,
						EndDate:     bEnd,
						TotalIn:     bIn,
						IsNearRange: true,
					})
					break // one bridge detection is sufficient
				}
			}
		}
	}

	return DetectionResult{
		RegionID: region.ID,
		Detected: len(windows) > 0,
		Windows:  windows,
	}
}

// preferredDailySnowfall builds a per-day snowfall map using the best available
// forecast for each day. With per-resort multi-model forecasts, each forecast
// represents a unique (resort, source, model) combination. For detection we want
// the best signal: for each day, take the max snowfall across all forecasts.
// This ensures that if any resort in the region is getting significant snow
// from any model, it triggers detection.
func preferredDailySnowfall(today time.Time, forecasts []Forecast) map[string]float64 {
	preferred := make(map[string]float64)
	for _, f := range forecasts {
		for _, d := range f.DailyData {
			key := d.Date.UTC().Format("2006-01-02")
			if d.SnowfallCM > preferred[key] {
				preferred[key] = d.SnowfallCM
			}
		}
	}
	return preferred
}

// sumDailyCM sums the preferred daily snowfall (cm) for all days in [start, end] inclusive.
func sumDailyCM(daily map[string]float64, start, end time.Time) float64 {
	var total float64
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		total += daily[d.Format("2006-01-02")]
	}
	return total
}
