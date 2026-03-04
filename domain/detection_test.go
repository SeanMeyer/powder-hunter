package domain

import (
	"testing"
	"time"
)

// inchesToCM converts inches to centimeters for building test forecast fixtures.
func inchesToCM(in float64) float64 { return in * 2.54 }

// nearDay returns a date that falls within the near-range window (days 1-7 from today UTC).
func nearDay(offset int) time.Time {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return today.AddDate(0, 0, offset)
}

// extDay returns a date that falls within the extended-range window (days 8-16 from today UTC).
func extDay(offset int) time.Time {
	return nearDay(offset)
}

func regionWithFriction(id string, ft FrictionTier) Region {
	near, ext := ft.Thresholds()
	return Region{
		ID:                  id,
		FrictionTier:        ft,
		NearThresholdIn:     near,
		ExtendedThresholdIn: ext,
	}
}

func singleDayForecast(regionID string, date time.Time, snowfallCM float64) Forecast {
	return Forecast{
		RegionID: regionID,
		Source:   "test",
		DailyData: []DailyForecast{
			{Date: date, SnowfallCM: snowfallCM},
		},
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name              string
		region            Region
		forecasts         []Forecast
		wantDetected      bool
		wantWindowCount   int
		wantNearWindow    bool // at least one near-range window expected
		wantExtWindow     bool // at least one extended-range window expected
	}{
		{
			name:   "local tier near-range just above threshold (6in) → detected",
			region: regionWithFriction("local-1", FrictionLocalDrive),
			// 6" threshold; supply 6.1" → should trigger
			forecasts: []Forecast{
				singleDayForecast("local-1", nearDay(3), inchesToCM(6.1)),
			},
			wantDetected:    true,
			wantWindowCount: 1,
			wantNearWindow:  true,
		},
		{
			name:   "local tier near-range just below threshold (5.9in) → not detected",
			region: regionWithFriction("local-2", FrictionLocalDrive),
			// 6" threshold; supply 5.9" → should not trigger
			forecasts: []Forecast{
				singleDayForecast("local-2", nearDay(3), inchesToCM(5.9)),
			},
			wantDetected:    false,
			wantWindowCount: 0,
		},
		{
			name:   "flight tier extended-range at 35in (below 36in threshold) → not detected",
			region: regionWithFriction("flight-1", FrictionFlight),
			// extended threshold = 36"; supply 35"
			forecasts: []Forecast{
				singleDayForecast("flight-1", extDay(10), inchesToCM(35)),
			},
			wantDetected:    false,
			wantWindowCount: 0,
		},
		{
			name:   "flight tier extended-range at 37in (above 36in threshold) → detected",
			region: regionWithFriction("flight-2", FrictionFlight),
			// extended threshold = 36"; supply 37"
			forecasts: []Forecast{
				singleDayForecast("flight-2", extDay(10), inchesToCM(37)),
			},
			wantDetected:    true,
			wantWindowCount: 1,
			wantExtWindow:   true,
		},
		{
			name:   "regional-drive with near AND extended both above threshold → two windows",
			region: regionWithFriction("regional-1", FrictionRegionalDrive),
			// near threshold = 14", ext threshold = 20"
			forecasts: []Forecast{
				singleDayForecast("regional-1", nearDay(2), inchesToCM(15)),
				singleDayForecast("regional-1", extDay(9), inchesToCM(21)),
			},
			wantDetected:    true,
			wantWindowCount: 2,
			wantNearWindow:  true,
			wantExtWindow:   true,
		},
		{
			name:   "zero snowfall → not detected",
			region: regionWithFriction("local-3", FrictionLocalDrive),
			forecasts: []Forecast{
				singleDayForecast("local-3", nearDay(1), 0),
			},
			wantDetected:    false,
			wantWindowCount: 0,
		},
		{
			name:   "mixed forecast sources aggregated correctly → detected",
			region: regionWithFriction("local-4", FrictionLocalDrive),
			// two separate Forecast objects (different sources) on the same near-range day;
			// each contributes 3.1" — aggregate 6.2" exceeds local 6" threshold
			forecasts: []Forecast{
				{
					RegionID: "local-4",
					Source:   "open_meteo",
					DailyData: []DailyForecast{
						{Date: nearDay(2), SnowfallCM: inchesToCM(3.1)},
					},
				},
				{
					RegionID: "local-4",
					Source:   "nws",
					DailyData: []DailyForecast{
						{Date: nearDay(3), SnowfallCM: inchesToCM(3.1)},
					},
				},
			},
			wantDetected:    true,
			wantWindowCount: 1,
			wantNearWindow:  true,
		},
		{
			name:   "high-friction-drive tier near threshold 18in and extended 24in → correct threshold applied",
			region: regionWithFriction("hfd-1", FrictionHighFrictionDrive),
			// supply exactly 18" near and 24" extended — both should trigger (>=)
			forecasts: []Forecast{
				singleDayForecast("hfd-1", nearDay(4), inchesToCM(18)),
				singleDayForecast("hfd-1", extDay(12), inchesToCM(24)),
			},
			wantDetected:    true,
			wantWindowCount: 2,
			wantNearWindow:  true,
			wantExtWindow:   true,
		},
		{
			name:   "only extended-range days (no near-range data) → only extended window checked",
			region: regionWithFriction("flight-3", FrictionFlight),
			// extended threshold = 36"; supply 40" in extended range only
			forecasts: []Forecast{
				singleDayForecast("flight-3", extDay(11), inchesToCM(40)),
			},
			wantDetected:    true,
			wantWindowCount: 1,
			wantExtWindow:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := Detect(tc.region, tc.forecasts)

			if result.Detected != tc.wantDetected {
				t.Errorf("Detected = %v, want %v", result.Detected, tc.wantDetected)
			}

			if len(result.Windows) != tc.wantWindowCount {
				t.Errorf("len(Windows) = %d, want %d", len(result.Windows), tc.wantWindowCount)
			}

			if result.RegionID != tc.region.ID {
				t.Errorf("RegionID = %q, want %q", result.RegionID, tc.region.ID)
			}

			if tc.wantNearWindow {
				found := false
				for _, w := range result.Windows {
					if w.IsNearRange {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected a near-range window but none found")
				}
			}

			if tc.wantExtWindow {
				found := false
				for _, w := range result.Windows {
					if !w.IsNearRange {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected an extended-range window but none found")
				}
			}
		})
	}
}
