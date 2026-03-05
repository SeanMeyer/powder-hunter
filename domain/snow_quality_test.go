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
