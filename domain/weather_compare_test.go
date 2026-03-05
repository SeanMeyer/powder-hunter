package domain

import (
	"testing"
	"time"
)

func TestForecastsChanged(t *testing.T) {
	base := time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)

	makeForecasts := func(days []DailyForecast) []Forecast {
		return []Forecast{{
			RegionID:  "test-region",
			FetchedAt: time.Now().UTC(),
			Source:    "open_meteo",
			DailyData: days,
		}}
	}

	baseDays := []DailyForecast{
		{Date: base, SnowfallCM: 10.0, TemperatureMinC: -5.0, TemperatureMaxC: 0.0},
		{Date: base.AddDate(0, 0, 1), SnowfallCM: 5.0, TemperatureMinC: -3.0, TemperatureMaxC: 2.0},
		{Date: base.AddDate(0, 0, 2), SnowfallCM: 2.0, TemperatureMinC: -1.0, TemperatureMaxC: 4.0},
	}

	tests := []struct {
		name        string
		previous    []Forecast
		current     []Forecast
		wantChanged bool
		wantReason  string
	}{
		{
			name:        "empty previous (first eval) returns changed",
			previous:    nil,
			current:     makeForecasts(baseDays),
			wantChanged: true,
			wantReason:  "first evaluation",
		},
		{
			name:        "empty current returns changed",
			previous:    makeForecasts(baseDays),
			current:     nil,
			wantChanged: true,
			wantReason:  "current forecast is empty",
		},
		{
			name:        "identical forecasts returns unchanged",
			previous:    makeForecasts(baseDays),
			current:     makeForecasts(baseDays),
			wantChanged: false,
		},
		{
			name:     "daily snowfall delta > 2 inches returns changed",
			previous: makeForecasts(baseDays),
			current: makeForecasts([]DailyForecast{
				{Date: base, SnowfallCM: 10.0 + 5.1*2.54, TemperatureMinC: -5.0, TemperatureMaxC: 0.0}, // +5.1"
				{Date: base.AddDate(0, 0, 1), SnowfallCM: 5.0, TemperatureMinC: -3.0, TemperatureMaxC: 2.0},
				{Date: base.AddDate(0, 0, 2), SnowfallCM: 2.0, TemperatureMinC: -1.0, TemperatureMaxC: 4.0},
			}),
			wantChanged: true,
			wantReason:  "daily snowfall delta",
		},
		{
			name:     "total snowfall delta > 3 inches returns changed",
			previous: makeForecasts(baseDays),
			current: makeForecasts([]DailyForecast{
				// Spread the delta across days so no single day exceeds 2"
				{Date: base, SnowfallCM: 10.0 + 1.5*2.54, TemperatureMinC: -5.0, TemperatureMaxC: 0.0},
				{Date: base.AddDate(0, 0, 1), SnowfallCM: 5.0 + 1.5*2.54, TemperatureMinC: -3.0, TemperatureMaxC: 2.0},
				{Date: base.AddDate(0, 0, 2), SnowfallCM: 2.0 + 1.0*2.54, TemperatureMinC: -1.0, TemperatureMaxC: 4.0},
			}),
			wantChanged: true,
			wantReason:  "total snowfall delta",
		},
		{
			name:     "temperature min delta > 4.4C returns changed",
			previous: makeForecasts(baseDays),
			current: makeForecasts([]DailyForecast{
				{Date: base, SnowfallCM: 10.0, TemperatureMinC: -5.0 - 5.0, TemperatureMaxC: 0.0}, // minC shifted 5°
				{Date: base.AddDate(0, 0, 1), SnowfallCM: 5.0, TemperatureMinC: -3.0, TemperatureMaxC: 2.0},
				{Date: base.AddDate(0, 0, 2), SnowfallCM: 2.0, TemperatureMinC: -1.0, TemperatureMaxC: 4.0},
			}),
			wantChanged: true,
			wantReason:  "temperature delta",
		},
		{
			name:     "temperature max delta > 4.4C returns changed",
			previous: makeForecasts(baseDays),
			current: makeForecasts([]DailyForecast{
				{Date: base, SnowfallCM: 10.0, TemperatureMinC: -5.0, TemperatureMaxC: 0.0 + 5.0}, // maxC shifted 5°
				{Date: base.AddDate(0, 0, 1), SnowfallCM: 5.0, TemperatureMinC: -3.0, TemperatureMaxC: 2.0},
				{Date: base.AddDate(0, 0, 2), SnowfallCM: 2.0, TemperatureMinC: -1.0, TemperatureMaxC: 4.0},
			}),
			wantChanged: true,
			wantReason:  "temperature delta",
		},
		{
			name:     "fewer days in current returns changed with mismatched count",
			previous: makeForecasts(baseDays),
			current: makeForecasts([]DailyForecast{
				{Date: base, SnowfallCM: 10.0, TemperatureMinC: -5.0, TemperatureMaxC: 0.0},
				// Only 1 day instead of 3
			}),
			wantChanged: true,
			wantReason:  "missing",
		},
		{
			name:     "more days in current compared only on overlapping range",
			previous: makeForecasts(baseDays),
			current: makeForecasts([]DailyForecast{
				{Date: base, SnowfallCM: 10.0, TemperatureMinC: -5.0, TemperatureMaxC: 0.0},
				{Date: base.AddDate(0, 0, 1), SnowfallCM: 5.0, TemperatureMinC: -3.0, TemperatureMaxC: 2.0},
				{Date: base.AddDate(0, 0, 2), SnowfallCM: 2.0, TemperatureMinC: -1.0, TemperatureMaxC: 4.0},
				{Date: base.AddDate(0, 0, 3), SnowfallCM: 20.0, TemperatureMinC: -15.0, TemperatureMaxC: -10.0}, // extra day, big values
			}),
			wantChanged: false,
		},
		{
			name:     "small changes below all thresholds returns unchanged",
			previous: makeForecasts(baseDays),
			current: makeForecasts([]DailyForecast{
				{Date: base, SnowfallCM: 10.0 + 1.0, TemperatureMinC: -5.0 + 1.0, TemperatureMaxC: 0.0 + 1.0}, // +0.4" snow, +1°C temp
				{Date: base.AddDate(0, 0, 1), SnowfallCM: 5.0, TemperatureMinC: -3.0, TemperatureMaxC: 2.0},
				{Date: base.AddDate(0, 0, 2), SnowfallCM: 2.0, TemperatureMinC: -1.0, TemperatureMaxC: 4.0},
			}),
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ForecastsChanged(tt.previous, tt.current)
			if result.Changed != tt.wantChanged {
				t.Errorf("Changed = %v, want %v (reason: %s)", result.Changed, tt.wantChanged, result.Reason)
			}
			if tt.wantReason != "" && result.Reason == "" {
				t.Errorf("expected non-empty reason containing %q", tt.wantReason)
			}
			if tt.wantReason != "" && !containsSubstring(result.Reason, tt.wantReason) {
				t.Errorf("reason %q does not contain %q", result.Reason, tt.wantReason)
			}
		})
	}
}

func TestForecastsChanged_EdgeCases(t *testing.T) {
	base := time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)

	makeForecasts := func(days []DailyForecast) []Forecast {
		return []Forecast{{
			RegionID: "test", FetchedAt: time.Now().UTC(), Source: "open_meteo", DailyData: days,
		}}
	}

	t.Run("both empty slices returns changed", func(t *testing.T) {
		result := ForecastsChanged(nil, nil)
		if !result.Changed {
			t.Error("both nil should return Changed=true (first eval)")
		}
	})

	t.Run("empty daily data in forecasts returns unchanged", func(t *testing.T) {
		prev := []Forecast{{RegionID: "test", Source: "open_meteo"}}
		curr := []Forecast{{RegionID: "test", Source: "open_meteo"}}
		result := ForecastsChanged(prev, curr)
		if result.Changed {
			t.Error("empty daily data in both should return Changed=false")
		}
	})

	t.Run("single day identical", func(t *testing.T) {
		day := []DailyForecast{{Date: base, SnowfallCM: 15.0, TemperatureMinC: -5, TemperatureMaxC: 0}}
		result := ForecastsChanged(makeForecasts(day), makeForecasts(day))
		if result.Changed {
			t.Error("single identical day should be unchanged")
		}
	})

	t.Run("same snowfall different temperatures below threshold", func(t *testing.T) {
		prev := makeForecasts([]DailyForecast{{Date: base, SnowfallCM: 10, TemperatureMinC: -5, TemperatureMaxC: 0}})
		curr := makeForecasts([]DailyForecast{{Date: base, SnowfallCM: 10, TemperatureMinC: -3, TemperatureMaxC: 2}}) // 2°C shift
		result := ForecastsChanged(prev, curr)
		if result.Changed {
			t.Errorf("2°C shift (below 4.4°C threshold) should be unchanged, got reason: %s", result.Reason)
		}
	})

	t.Run("enrichment fields ignored", func(t *testing.T) {
		prev := []Forecast{{
			RegionID: "test", Source: "open_meteo",
			DailyData: []DailyForecast{{Date: base, SnowfallCM: 10, TemperatureMinC: -5, TemperatureMaxC: 0, SLRatio: 15, RainHours: 0, MixedHours: 0}},
		}}
		curr := []Forecast{{
			RegionID: "test", Source: "open_meteo",
			DailyData: []DailyForecast{{Date: base, SnowfallCM: 10, TemperatureMinC: -5, TemperatureMaxC: 0, SLRatio: 20, RainHours: 5, MixedHours: 3}},
		}}
		result := ForecastsChanged(prev, curr)
		if result.Changed {
			t.Error("enrichment field changes (SLRatio, RainHours, MixedHours) should be ignored")
		}
	})
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && contains(s, sub))
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
