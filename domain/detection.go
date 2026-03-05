package domain

import "time"

// DetectionResult holds the outcome of checking a region's forecasts against thresholds.
type DetectionResult struct {
	RegionID string
	Detected bool
	Windows  []SnowfallWindow // windows that exceeded thresholds
}

// Source preference order for near-range detection. NWS has higher spatial
// resolution for US locations; Open-Meteo is the fallback and sole source for
// extended-range and non-US regions.
var nearRangeSourcePreference = []string{"nws", "open_meteo"}

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
func Detect(region Region, forecasts []Forecast) DetectionResult {
	now := time.Now().UTC()
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
// source for each day. NWS is preferred for days 1-7 (higher resolution near-range),
// with Open-Meteo as fallback. Extended range uses whatever source has data.
func preferredDailySnowfall(today time.Time, forecasts []Forecast) map[string]float64 {
	// Aggregate per source per day.
	bySource := make(map[string]map[string]float64)
	for _, f := range forecasts {
		sd, ok := bySource[f.Source]
		if !ok {
			sd = make(map[string]float64)
			bySource[f.Source] = sd
		}
		for _, d := range f.DailyData {
			key := d.Date.UTC().Format("2006-01-02")
			sd[key] += d.SnowfallCM
		}
	}

	preferred := make(map[string]float64)
	for dayOffset := 1; dayOffset <= 16; dayOffset++ {
		key := today.AddDate(0, 0, dayOffset).Format("2006-01-02")

		if dayOffset <= 7 {
			// Near-range: try preferred sources in order.
			found := false
			for _, src := range nearRangeSourcePreference {
				if sd, ok := bySource[src]; ok {
					if v := sd[key]; v > 0 {
						preferred[key] = v
						found = true
						break
					}
				}
			}
			if found {
				continue
			}
		}

		// Extended range or near-range fallback: use best available.
		for _, sd := range bySource {
			if v := sd[key]; v > preferred[key] {
				preferred[key] = v
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
