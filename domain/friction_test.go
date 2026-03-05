package domain

import (
	"math"
	"testing"
)

func TestHaversineDistanceKM(t *testing.T) {
	tests := []struct {
		name         string
		lat1, lon1   float64
		lat2, lon2   float64
		wantApproxKM float64
		toleranceKM  float64
	}{
		{"Denver to Vail", 39.7392, -104.9903, 39.6403, -106.3742, 120, 30},
		{"Denver to Park City", 39.7392, -104.9903, 40.6461, -111.498, 580, 60},
		{"SLC to Park City", 40.7608, -111.8910, 40.6461, -111.498, 40, 20},
		{"same point", 39.0, -105.0, 39.0, -105.0, 0, 0.1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HaversineDistanceKM(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			if math.Abs(got-tt.wantApproxKM) > tt.toleranceKM {
				t.Errorf("got %.0f km, want ~%.0f km (tolerance %.0f)", got, tt.wantApproxKM, tt.toleranceKM)
			}
		})
	}
}

func TestFrictionTierFromDistance(t *testing.T) {
	tests := []struct {
		name   string
		distKM float64
		want   FrictionTier
	}{
		{"very close (30 min drive)", 40, FrictionLocalDrive},
		{"2 hour drive", 150, FrictionLocalDrive},
		{"4 hour drive", 350, FrictionRegionalDrive},
		{"6 hour drive", 500, FrictionRegionalDrive},
		{"10 hour drive", 800, FrictionHighFrictionDrive},
		{"cross country", 2500, FrictionFlight},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FrictionTierFromDistance(tt.distKM)
			if got != tt.want {
				t.Errorf("distance %.0f km: got %s, want %s", tt.distKM, got, tt.want)
			}
		})
	}
}
