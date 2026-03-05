package domain

import (
	"testing"
	"time"
)

func TestDensityCategory(t *testing.T) {
	tests := []struct {
		name    string
		density float64
		want    string
	}{
		{"cold smoke", 45.0, "cold_smoke"},
		{"cold smoke boundary", 59.9, "cold_smoke"},
		{"dry powder low", 60.0, "dry_powder"},
		{"dry powder mid", 75.0, "dry_powder"},
		{"standard low", 90.0, "standard"},
		{"standard mid", 110.0, "standard"},
		{"heavy low", 130.0, "heavy"},
		{"heavy mid", 150.0, "heavy"},
		{"wet cement", 180.0, "wet_cement"},
		{"wet cement high", 250.0, "wet_cement"},
		{"zero (rain)", 0.0, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DensityCategory(tc.density)
			if got != tc.want {
				t.Errorf("DensityCategory(%v) = %q, want %q", tc.density, got, tc.want)
			}
		})
	}
}

func TestCrystalQuality(t *testing.T) {
	tests := []struct {
		name    string
		windMph float64
		want    string
	}{
		{"calm", 5.0, "intact"},
		{"light breeze", 14.9, "intact"},
		{"moderate", 15.0, "partially_broken"},
		{"strong", 24.9, "partially_broken"},
		{"very strong", 25.0, "wind_broken"},
		{"gale", 40.0, "wind_broken"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CrystalQuality(tc.windMph)
			if got != tc.want {
				t.Errorf("CrystalQuality(%v) = %q, want %q", tc.windMph, got, tc.want)
			}
		})
	}
}

func TestAssessBaseRisk(t *testing.T) {
	t.Run("cold and cloudy — low risk", func(t *testing.T) {
		days := makePreStormDays(-10, -5, 80.0)
		risk, _ := AssessBaseRisk(days, 40.0, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))
		if risk != "low" {
			t.Errorf("risk = %q, want low", risk)
		}
	})

	t.Run("well above freezing — high risk", func(t *testing.T) {
		days := makePreStormDays(2, 5, 50.0)
		risk, reason := AssessBaseRisk(days, 40.0, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))
		if risk != "high" {
			t.Errorf("risk = %q, want high", risk)
		}
		if reason == "" {
			t.Error("expected reason for high risk")
		}
	})

	t.Run("March clear sky — solar crust risk", func(t *testing.T) {
		days := makePreStormDays(-5, -2, 15.0) // cold but clear
		risk, reason := AssessBaseRisk(days, 40.0, time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC))
		if risk != "moderate" {
			t.Errorf("risk = %q, want moderate (reason: %s)", risk, reason)
		}
	})

	t.Run("empty pre-storm — low risk", func(t *testing.T) {
		risk, _ := AssessBaseRisk(nil, 40.0, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))
		if risk != "low" {
			t.Errorf("risk = %q, want low", risk)
		}
	})
}

func makePreStormDays(tempMinC, tempMaxC, cloudPct float64) []DailyForecast {
	return []DailyForecast{
		{
			TemperatureMinC: tempMinC, TemperatureMaxC: tempMaxC,
			Day:   HalfDay{TemperatureC: tempMaxC, CloudCoverPct: cloudPct},
			Night: HalfDay{TemperatureC: tempMinC, CloudCoverPct: cloudPct},
		},
		{
			TemperatureMinC: tempMinC, TemperatureMaxC: tempMaxC,
			Day:   HalfDay{TemperatureC: tempMaxC, CloudCoverPct: cloudPct},
			Night: HalfDay{TemperatureC: tempMinC, CloudCoverPct: cloudPct},
		},
	}
}

func TestAssessRideQuality(t *testing.T) {
	t.Run("bluebird after storm", func(t *testing.T) {
		days := []DailyForecast{
			{ // Storm day
				SnowfallCM: 25.0, SLRatio: 12.0,
				Day:   HalfDay{WindSpeedKmh: 15, CloudCoverPct: 90, PrecipitationMM: 5, SnowfallCM: 10},
				Night: HalfDay{WindSpeedKmh: 10, CloudCoverPct: 95, PrecipitationMM: 10, SnowfallCM: 15},
			},
			{ // Clear day after
				SnowfallCM: 0,
				Day:   HalfDay{WindSpeedKmh: 5, CloudCoverPct: 10},
				Night: HalfDay{CloudCoverPct: 15},
			},
		}

		qualities := AssessRideQuality(days, nil, 40.0, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))
		if len(qualities) < 2 {
			t.Fatalf("expected 2 qualities, got %d", len(qualities))
		}
		if !qualities[1].Bluebird {
			t.Error("expected bluebird on day after storm")
		}
	})

	t.Run("windy storm crystal quality", func(t *testing.T) {
		days := []DailyForecast{
			{
				SnowfallCM: 20.0, SLRatio: 10.0,
				Day:   HalfDay{WindSpeedKmh: 50, CloudCoverPct: 100, PrecipitationMM: 8, SnowfallCM: 10},
				Night: HalfDay{WindSpeedKmh: 40, CloudCoverPct: 100, PrecipitationMM: 6, SnowfallCM: 10},
			},
		}

		qualities := AssessRideQuality(days, nil, 40.0, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))
		if qualities[0].CrystalQuality != "wind_broken" {
			t.Errorf("crystal quality = %q, want wind_broken", qualities[0].CrystalQuality)
		}
	})

	t.Run("favorable layering dense then light", func(t *testing.T) {
		day1 := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
		day2 := time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC)
		days := []DailyForecast{
			{ // Dense snow
				Date: day1, SnowfallCM: 15.0, SLRatio: 8.0,
				Day:   HalfDay{WindSpeedKmh: 30, CloudCoverPct: 100, PrecipitationMM: 10, SnowfallCM: 8},
				Night: HalfDay{WindSpeedKmh: 20, CloudCoverPct: 100, PrecipitationMM: 5, SnowfallCM: 7},
			},
			{ // Light powder
				Date: day2, SnowfallCM: 20.0, SLRatio: 18.0,
				Day:   HalfDay{WindSpeedKmh: 8, CloudCoverPct: 80, PrecipitationMM: 5, SnowfallCM: 12},
				Night: HalfDay{WindSpeedKmh: 5, CloudCoverPct: 90, PrecipitationMM: 3, SnowfallCM: 8},
			},
		}

		qualities := AssessRideQuality(days, nil, 40.0, day1)
		hasLayeringNote := false
		for _, note := range qualities[1].RideQualityNotes {
			if len(note) > 10 && note[0] == 'F' { // "Favorable..."
				hasLayeringNote = true
			}
		}
		if !hasLayeringNote {
			t.Errorf("expected favorable layering note, got: %v", qualities[1].RideQualityNotes)
		}
	})

	t.Run("no snow days get no notes", func(t *testing.T) {
		days := []DailyForecast{
			{SnowfallCM: 0, Day: HalfDay{CloudCoverPct: 50}, Night: HalfDay{CloudCoverPct: 50}},
		}
		qualities := AssessRideQuality(days, nil, 40.0, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))
		if len(qualities[0].RideQualityNotes) != 0 {
			t.Errorf("expected no notes for non-snow day, got: %v", qualities[0].RideQualityNotes)
		}
	})
}

func TestSolarElevationAtNoon(t *testing.T) {
	tests := []struct {
		name    string
		lat     float64
		month   int
		day     int
		wantMin float64
		wantMax float64
	}{
		{"Denver Dec 21", 40.0, 12, 21, 25.0, 29.0},
		{"Denver Mar 21", 40.0, 3, 21, 48.0, 52.0},
		{"Denver Apr 15", 40.0, 4, 15, 56.0, 60.0},
		{"Whistler Dec 21", 50.0, 12, 21, 15.0, 19.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			date := time.Date(2026, time.Month(tc.month), tc.day, 0, 0, 0, 0, time.UTC)
			got := SolarElevationAtNoon(tc.lat, date)
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("SolarElevationAtNoon(%v, %v) = %.1f, want [%.1f, %.1f]",
					tc.lat, date.Format("Jan 02"), got, tc.wantMin, tc.wantMax)
			}
		})
	}
}
