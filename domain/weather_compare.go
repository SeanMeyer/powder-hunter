package domain

import (
	"fmt"
	"math"
)

// Thresholds for determining whether a forecast change is "material" enough
// to warrant re-evaluation. Only detection-critical fields are compared;
// enrichment data (SLRatio, RainHours, MixedHours, consensus, AFD) is ignored.
const (
	DailySnowfallThresholdIn = 2.0  // per-day snowfall delta (inches)
	TotalSnowfallThresholdIn = 3.0  // total window snowfall delta (inches)
	TempThresholdC           = 4.4  // temperature delta (Celsius, ~8°F)
)

// SkipReason categorizes why an evaluation was skipped.
type SkipReason string

const (
	SkipNone              SkipReason = ""
	SkipUnchangedWeather  SkipReason = "unchanged_weather"
	SkipCooldown          SkipReason = "cooldown"
	SkipBudgetExceeded    SkipReason = "budget_exceeded"
)

// WeatherChangeSummary is the result of comparing two forecast snapshots.
// Used by the pipeline to decide whether to re-evaluate a storm.
type WeatherChangeSummary struct {
	Changed                bool
	TotalSnowfallDeltaIn   float64
	MaxDailySnowfallDeltaIn float64
	MaxTempDeltaC          float64
	DaysMismatched         int
	Reason                 string
}

// ForecastsChanged compares previous and current forecast slices, examining only
// detection-critical fields (snowfall, temperature, precipitation). Returns a
// summary indicating whether the change is material. Enrichment fields
// (SLRatio, RainHours, MixedHours, etc.) are intentionally ignored.
//
// Empty previous forecasts (first evaluation) always return Changed=true.
// Fewer days in current than previous is treated as material change.
// Extra days in current are ignored (only overlapping range is compared).
func ForecastsChanged(previous, current []Forecast) WeatherChangeSummary {
	if len(previous) == 0 {
		return WeatherChangeSummary{Changed: true, Reason: "first evaluation (no previous forecast)"}
	}
	if len(current) == 0 {
		return WeatherChangeSummary{Changed: true, Reason: "current forecast is empty"}
	}

	// Build date-keyed maps of daily data from each snapshot.
	// Aggregate across all forecasts in each slice (multiple sources/models).
	prevByDate := aggregateDailyByDate(previous)
	currByDate := aggregateDailyByDate(current)

	// Check for missing days: if current has fewer days, treat as changed.
	mismatched := 0
	for dateKey := range prevByDate {
		if _, ok := currByDate[dateKey]; !ok {
			mismatched++
		}
	}
	if mismatched > 0 {
		return WeatherChangeSummary{
			Changed:        true,
			DaysMismatched: mismatched,
			Reason:         fmt.Sprintf("current forecast missing %d day(s) present in previous", mismatched),
		}
	}

	// Compare overlapping days on detection-critical fields.
	var totalSnowPrev, totalSnowCurr float64
	var maxDailyDelta float64
	var maxTempDelta float64

	for dateKey, prev := range prevByDate {
		curr, ok := currByDate[dateKey]
		if !ok {
			continue // extra day in previous not in current — already handled above
		}

		prevSnowIn := CMToInches(prev.SnowfallCM)
		currSnowIn := CMToInches(curr.SnowfallCM)
		totalSnowPrev += prevSnowIn
		totalSnowCurr += currSnowIn

		dailyDelta := math.Abs(currSnowIn - prevSnowIn)
		if dailyDelta > maxDailyDelta {
			maxDailyDelta = dailyDelta
		}

		minDelta := math.Abs(curr.TemperatureMinC - prev.TemperatureMinC)
		maxDelta := math.Abs(curr.TemperatureMaxC - prev.TemperatureMaxC)
		tempDelta := max(minDelta, maxDelta)
		if tempDelta > maxTempDelta {
			maxTempDelta = tempDelta
		}
	}

	totalDelta := math.Abs(totalSnowCurr - totalSnowPrev)

	summary := WeatherChangeSummary{
		TotalSnowfallDeltaIn:    totalDelta,
		MaxDailySnowfallDeltaIn: maxDailyDelta,
		MaxTempDeltaC:           maxTempDelta,
	}

	if maxDailyDelta >= DailySnowfallThresholdIn {
		summary.Changed = true
		summary.Reason = fmt.Sprintf("daily snowfall delta %.1f\" exceeds %.1f\" threshold", maxDailyDelta, DailySnowfallThresholdIn)
		return summary
	}
	if totalDelta >= TotalSnowfallThresholdIn {
		summary.Changed = true
		summary.Reason = fmt.Sprintf("total snowfall delta %.1f\" exceeds %.1f\" threshold", totalDelta, TotalSnowfallThresholdIn)
		return summary
	}
	if maxTempDelta >= TempThresholdC {
		summary.Changed = true
		summary.Reason = fmt.Sprintf("temperature delta %.1f°C exceeds %.1f°C threshold", maxTempDelta, TempThresholdC)
		return summary
	}

	return summary
}

// dailySummary holds aggregated detection-critical fields for one calendar day.
type dailySummary struct {
	SnowfallCM      float64
	TemperatureMinC float64
	TemperatureMaxC float64
}

// aggregateDailyByDate builds a date-keyed map from a forecast slice.
// When multiple forecasts cover the same date (multi-model), the values are
// averaged to produce a single representative value for comparison.
func aggregateDailyByDate(forecasts []Forecast) map[string]dailySummary {
	type accumulator struct {
		snowSum  float64
		tempMinSum float64
		tempMaxSum float64
		count    int
	}

	accum := make(map[string]*accumulator)
	for _, f := range forecasts {
		for _, d := range f.DailyData {
			key := d.Date.Format("2006-01-02")
			a, ok := accum[key]
			if !ok {
				a = &accumulator{}
				accum[key] = a
			}
			a.snowSum += d.SnowfallCM
			a.tempMinSum += d.TemperatureMinC
			a.tempMaxSum += d.TemperatureMaxC
			a.count++
		}
	}

	result := make(map[string]dailySummary, len(accum))
	for key, a := range accum {
		result[key] = dailySummary{
			SnowfallCM:      a.snowSum / float64(a.count),
			TemperatureMinC: a.tempMinSum / float64(a.count),
			TemperatureMaxC: a.tempMaxSum / float64(a.count),
		}
	}
	return result
}
