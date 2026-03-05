package domain

import (
	"testing"
	"time"
)

// inchesToCM converts inches to centimeters for building test forecast fixtures.
func inchesToCM(in float64) float64 { return in * 2.54 }

// nearDay returns a date at the given day offset from today UTC.
func nearDay(offset int) time.Time {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return today.AddDate(0, 0, offset)
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

func sourceForecast(regionID, source string, days []DailyForecast) Forecast {
	return Forecast{
		RegionID:  regionID,
		Source:    source,
		DailyData: days,
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name            string
		region          Region
		forecasts       []Forecast
		wantDetected    bool
		wantWindowCount int
		wantNearWindow  bool
		wantExtWindow   bool
	}{
		{
			name:   "local tier near-range just above threshold (6in) → detected",
			region: regionWithFriction("local-1", FrictionLocalDrive),
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
			forecasts: []Forecast{
				singleDayForecast("local-2", nearDay(3), inchesToCM(5.9)),
			},
			wantDetected:    false,
			wantWindowCount: 0,
		},
		{
			name:   "flight tier extended-range at 35in (below 36in threshold) → not detected",
			region: regionWithFriction("flight-1", FrictionFlight),
			forecasts: []Forecast{
				singleDayForecast("flight-1", nearDay(10), inchesToCM(35)),
			},
			wantDetected:    false,
			wantWindowCount: 0,
		},
		{
			name:   "flight tier extended-range at 37in (above 36in threshold) → detected",
			region: regionWithFriction("flight-2", FrictionFlight),
			forecasts: []Forecast{
				singleDayForecast("flight-2", nearDay(10), inchesToCM(37)),
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
				singleDayForecast("regional-1", nearDay(9), inchesToCM(21)),
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
			name:   "high-friction-drive tier exact thresholds trigger (>=)",
			region: regionWithFriction("hfd-1", FrictionHighFrictionDrive),
			// supply exactly 18" near and 24" extended — both should trigger (>=)
			forecasts: []Forecast{
				singleDayForecast("hfd-1", nearDay(4), inchesToCM(18)),
				singleDayForecast("hfd-1", nearDay(12), inchesToCM(24)),
			},
			wantDetected:    true,
			wantWindowCount: 2,
			wantNearWindow:  true,
			wantExtWindow:   true,
		},
		{
			name:   "only extended-range days → only extended window checked",
			region: regionWithFriction("flight-3", FrictionFlight),
			forecasts: []Forecast{
				singleDayForecast("flight-3", nearDay(11), inchesToCM(40)),
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
			if tc.wantNearWindow && !hasWindow(result.Windows, true) {
				t.Error("expected a near-range window but none found")
			}
			if tc.wantExtWindow && !hasWindow(result.Windows, false) {
				t.Error("expected an extended-range window but none found")
			}
		})
	}
}

func TestDetect_SourcePreference(t *testing.T) {
	tests := []struct {
		name         string
		region       Region
		forecasts    []Forecast
		wantDetected bool
		wantNearIn   float64 // approximate expected near-range total; 0 = don't check
	}{
		{
			name:   "max across sources used for detection (OM higher)",
			region: regionWithFriction("sp-1", FrictionLocalDrive), // 6" near threshold
			forecasts: []Forecast{
				sourceForecast("sp-1", "open_meteo", []DailyForecast{
					{Date: nearDay(2), SnowfallCM: inchesToCM(10)},
				}),
				sourceForecast("sp-1", "nws", []DailyForecast{
					{Date: nearDay(2), SnowfallCM: inchesToCM(7)},
				}),
			},
			wantDetected: true,
			wantNearIn:   10.0, // max across sources
		},
		{
			name:   "max across sources — both above threshold",
			region: regionWithFriction("sp-2", FrictionLocalDrive), // 6" near threshold
			forecasts: []Forecast{
				sourceForecast("sp-2", "open_meteo", []DailyForecast{
					{Date: nearDay(2), SnowfallCM: inchesToCM(8)},
				}),
				sourceForecast("sp-2", "nws", []DailyForecast{
					{Date: nearDay(2), SnowfallCM: inchesToCM(4)},
				}),
			},
			wantDetected: true, // OM's 8" exceeds 6" threshold
			wantNearIn:   8.0,
		},
		{
			name:   "no NWS data → falls back to Open-Meteo for near-range",
			region: regionWithFriction("sp-3", FrictionLocalDrive), // 6" near threshold
			forecasts: []Forecast{
				sourceForecast("sp-3", "open_meteo", []DailyForecast{
					{Date: nearDay(2), SnowfallCM: inchesToCM(8)},
				}),
			},
			wantDetected: true,
			wantNearIn:   8.0,
		},
		{
			name:   "NWS has zero snow, Open-Meteo has data → falls back to Open-Meteo",
			region: regionWithFriction("sp-4", FrictionLocalDrive), // 6" near threshold
			forecasts: []Forecast{
				sourceForecast("sp-4", "open_meteo", []DailyForecast{
					{Date: nearDay(2), SnowfallCM: inchesToCM(8)},
				}),
				sourceForecast("sp-4", "nws", []DailyForecast{
					{Date: nearDay(2), SnowfallCM: 0},
				}),
			},
			wantDetected: true,
			wantNearIn:   8.0,
		},
		{
			name:   "same day from two sources → NWS preferred, no double-counting",
			region: regionWithFriction("sp-5", FrictionLocalDrive), // 6" near threshold
			// Both sources report 4" on the SAME day — only NWS's 4" is used
			forecasts: []Forecast{
				sourceForecast("sp-5", "open_meteo", []DailyForecast{
					{Date: nearDay(2), SnowfallCM: inchesToCM(4)},
				}),
				sourceForecast("sp-5", "nws", []DailyForecast{
					{Date: nearDay(2), SnowfallCM: inchesToCM(4)},
				}),
			},
			wantDetected: false, // 4" < 6" threshold
		},
		{
			name:   "different days from different sources → complementary data used",
			region: regionWithFriction("sp-6", FrictionLocalDrive), // 6" near threshold
			// Open-Meteo has 4" on day 2 (NWS has nothing), NWS has 4" on day 3
			// Per-day preference picks the best source for each day: 4+4 = 8" > 6"
			forecasts: []Forecast{
				sourceForecast("sp-6", "open_meteo", []DailyForecast{
					{Date: nearDay(2), SnowfallCM: inchesToCM(4)},
				}),
				sourceForecast("sp-6", "nws", []DailyForecast{
					{Date: nearDay(3), SnowfallCM: inchesToCM(4)},
				}),
			},
			wantDetected: true,
			wantNearIn:   8.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := Detect(tc.region, tc.forecasts)

			if result.Detected != tc.wantDetected {
				t.Errorf("Detected = %v, want %v", result.Detected, tc.wantDetected)
			}
			if tc.wantNearIn > 0 {
				for _, w := range result.Windows {
					if w.IsNearRange {
						delta := w.TotalIn - tc.wantNearIn
						if delta < 0 {
							delta = -delta
						}
						if delta > 0.1 {
							t.Errorf("near-range TotalIn = %.1f, want ~%.1f", w.TotalIn, tc.wantNearIn)
						}
					}
				}
			}
		})
	}
}

func TestDetect_BridgeWindows(t *testing.T) {
	tests := []struct {
		name         string
		region       Region
		forecasts    []Forecast
		wantDetected bool
	}{
		{
			name:   "storm straddling boundary detected via bridge",
			region: regionWithFriction("bridge-1", FrictionFlight), // near=24", ext=36"
			// 15" in near (days 5-7), 15" in extended (days 8-10)
			// Near total: 15" < 24". Extended total: 15" < 36".
			// Bridge [4-10] captures all 30" > 24" → detected
			forecasts: []Forecast{
				singleDayForecast("bridge-1", nearDay(5), inchesToCM(5)),
				singleDayForecast("bridge-1", nearDay(6), inchesToCM(5)),
				singleDayForecast("bridge-1", nearDay(7), inchesToCM(5)),
				singleDayForecast("bridge-1", nearDay(8), inchesToCM(5)),
				singleDayForecast("bridge-1", nearDay(9), inchesToCM(5)),
				singleDayForecast("bridge-1", nearDay(10), inchesToCM(5)),
			},
			wantDetected: true,
		},
		{
			name:   "late boundary storm detected via second bridge",
			region: regionWithFriction("bridge-2", FrictionFlight), // near=24", ext=36"
			// Storm concentrated on days 7-13 with 4"/day = 28" total
			// Near [1-7]: only day 7 = 4" < 24". Extended [8-16]: days 8-13 = 24" < 36".
			// Bridge-early [4-10]: days 7-10 = 16" < 24". No.
			// Bridge-late [7-13]: days 7-13 = 28" > 24". Yes!
			forecasts: []Forecast{
				singleDayForecast("bridge-2", nearDay(7), inchesToCM(4)),
				singleDayForecast("bridge-2", nearDay(8), inchesToCM(4)),
				singleDayForecast("bridge-2", nearDay(9), inchesToCM(4)),
				singleDayForecast("bridge-2", nearDay(10), inchesToCM(4)),
				singleDayForecast("bridge-2", nearDay(11), inchesToCM(4)),
				singleDayForecast("bridge-2", nearDay(12), inchesToCM(4)),
				singleDayForecast("bridge-2", nearDay(13), inchesToCM(4)),
			},
			wantDetected: true,
		},
		{
			name:   "bridge not checked when near already triggers",
			region: regionWithFriction("bridge-3", FrictionLocalDrive), // near=6"
			// Near triggers with 8". Bridge would also trigger but shouldn't be checked.
			forecasts: []Forecast{
				singleDayForecast("bridge-3", nearDay(2), inchesToCM(8)),
			},
			wantDetected: true,
		},
		{
			name:   "separate storms far apart → bridge doesn't false-trigger",
			region: regionWithFriction("bridge-4", FrictionFlight), // near=24"
			// 10" on day 2 and 10" on day 14 — not a boundary storm.
			// Neither bridge window captures both.
			forecasts: []Forecast{
				singleDayForecast("bridge-4", nearDay(2), inchesToCM(10)),
				singleDayForecast("bridge-4", nearDay(14), inchesToCM(10)),
			},
			wantDetected: false,
		},
		{
			name:   "diffuse snow below bridge threshold → not detected",
			region: regionWithFriction("bridge-5", FrictionFlight), // near=24"
			// 3"/day across days 4-10 = 21" total, below 24" threshold
			forecasts: []Forecast{
				singleDayForecast("bridge-5", nearDay(4), inchesToCM(3)),
				singleDayForecast("bridge-5", nearDay(5), inchesToCM(3)),
				singleDayForecast("bridge-5", nearDay(6), inchesToCM(3)),
				singleDayForecast("bridge-5", nearDay(7), inchesToCM(3)),
				singleDayForecast("bridge-5", nearDay(8), inchesToCM(3)),
				singleDayForecast("bridge-5", nearDay(9), inchesToCM(3)),
				singleDayForecast("bridge-5", nearDay(10), inchesToCM(3)),
			},
			wantDetected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := Detect(tc.region, tc.forecasts)
			if result.Detected != tc.wantDetected {
				t.Errorf("Detected = %v, want %v", result.Detected, tc.wantDetected)
				for _, w := range result.Windows {
					t.Logf("  window: %s–%s, %.1f\", near=%v",
						w.StartDate.Format("day 2"), w.EndDate.Format("day 2"),
						w.TotalIn, w.IsNearRange)
				}
			}
		})
	}
}

func hasWindow(windows []SnowfallWindow, nearRange bool) bool {
	for _, w := range windows {
		if w.IsNearRange == nearRange {
			return true
		}
	}
	return false
}
