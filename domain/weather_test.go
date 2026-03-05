package domain

import (
	"testing"
	"time"
)

func TestCalculateSLR(t *testing.T) {
	tests := []struct {
		name    string
		tempC   float64
		wantSLR float64
	}{
		// Rain band: > 1.67°C (> 35°F)
		{"hot rain (10°C)", 10.0, 0},
		{"just above rain threshold (2°C)", 2.0, 0},
		{"rain boundary (1.67°C + epsilon)", 1.67, 0},

		// Mixed band: [0°C, 1.67°C] (32-35°F)
		{"mixed at 1.66°C", 1.66, 5},
		{"mixed at 1.0°C", 1.0, 5},
		{"mixed at 0°C (lower boundary)", 0.0, 5},

		// Wet snow band: [-3.89°C, 0°C) (25-31°F)
		{"wet snow just below 0°C", -0.01, 10},
		{"wet snow at -2°C", -2.0, 10},
		{"wet snow at -3.88°C", -3.88, 10},

		// Dry powder band: [-9.44°C, -3.89°C) (15-24°F)
		{"dry powder at -3.89°C (boundary)", -3.89, 15},
		{"dry powder at -5°C", -5.0, 15},
		{"dry powder at -9.43°C", -9.43, 15},

		// Cold smoke band: < -9.44°C (< 15°F)
		{"cold smoke at -9.45°C", -9.45, 20},
		{"cold smoke at -15°C", -15.0, 20},
		{"cold smoke at -30°C", -30.0, 20},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CalculateSLR(tc.tempC)
			if got != tc.wantSLR {
				t.Errorf("CalculateSLR(%v) = %v, want %v", tc.tempC, got, tc.wantSLR)
			}
		})
	}
}

func TestSnowfallFromPrecip(t *testing.T) {
	tests := []struct {
		name     string
		precipMM float64
		tempC    float64
		wantCM   float64
	}{
		// Zero precip → zero snow regardless of temp.
		{"zero precip cold", 0.0, -15.0, 0.0},
		{"negative precip", -1.0, -5.0, 0.0},

		// Rain → zero snow.
		{"rain at 5°C", 10.0, 5.0, 0.0},

		// Cold smoke: 0.5" liquid (12.7mm) at -11°C ≈ 12°F → 20:1 SLR.
		// 12.7mm / 10 * 20 = 25.4 cm ≈ 10"
		{"cold smoke 0.5in liquid", 12.7, -11.0, 25.4},

		// Wet snow: 1" liquid (25.4mm) at -2°C ≈ 28°F → 10:1 SLR.
		// 25.4mm / 10 * 10 = 25.4 cm ≈ 10"
		{"wet snow 1in liquid", 25.4, -2.0, 25.4},

		// Dry powder: 1" liquid (25.4mm) at -5°C → 15:1 SLR.
		// 25.4mm / 10 * 15 = 38.1 cm ≈ 15"
		{"dry powder 1in liquid", 25.4, -5.0, 38.1},

		// Mixed: 1" liquid (25.4mm) at 1.0°C → 5:1 SLR.
		// 25.4mm / 10 * 5 = 12.7 cm ≈ 5"
		{"mixed 1in liquid", 25.4, 1.0, 12.7},

		// Small precip at cold temp.
		// 1mm at -15°C → 20:1 → 1/10 * 20 = 2.0 cm
		{"small precip cold smoke", 1.0, -15.0, 2.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SnowfallFromPrecip(tc.precipMM, tc.tempC)
			delta := got - tc.wantCM
			if delta < 0 {
				delta = -delta
			}
			if delta > 0.01 {
				t.Errorf("SnowfallFromPrecip(%v, %v) = %v, want %v", tc.precipMM, tc.tempC, got, tc.wantCM)
			}
		})
	}
}

func TestComputeConsensus(t *testing.T) {
	day1 := time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 3, 6, 0, 0, 0, 0, time.UTC)

	t.Run("two models agree closely → high confidence", func(t *testing.T) {
		forecasts := []Forecast{
			{RegionID: "test", Model: "gfs", DailyData: []DailyForecast{
				{Date: day1, SnowfallCM: 30.0},
				{Date: day2, SnowfallCM: 10.0},
			}},
			{RegionID: "test", Model: "ecmwf", DailyData: []DailyForecast{
				{Date: day1, SnowfallCM: 35.0},
				{Date: day2, SnowfallCM: 12.0},
			}},
		}

		c := ComputeConsensus(forecasts)
		if len(c.Models) != 2 {
			t.Fatalf("expected 2 models, got %d", len(c.Models))
		}
		if len(c.DailyConsensus) != 2 {
			t.Fatalf("expected 2 days, got %d", len(c.DailyConsensus))
		}

		d1 := c.DailyConsensus[0]
		if d1.SnowfallMinCM != 30.0 || d1.SnowfallMaxCM != 35.0 {
			t.Errorf("day1 min/max = %.1f/%.1f, want 30.0/35.0", d1.SnowfallMinCM, d1.SnowfallMaxCM)
		}
		// spread = (35-30)/32.5 ≈ 0.154 → high
		if d1.Confidence != "high" {
			t.Errorf("day1 confidence = %q, want high (spread=%.3f)", d1.Confidence, d1.SpreadToMean)
		}
	})

	t.Run("models diverge significantly → low confidence", func(t *testing.T) {
		forecasts := []Forecast{
			{RegionID: "test", Model: "gfs", DailyData: []DailyForecast{
				{Date: day1, SnowfallCM: 76.2}, // ~30"
			}},
			{RegionID: "test", Model: "ecmwf", DailyData: []DailyForecast{
				{Date: day1, SnowfallCM: 15.24}, // ~6"
			}},
		}

		c := ComputeConsensus(forecasts)
		d1 := c.DailyConsensus[0]
		// spread = (76.2-15.24)/45.72 ≈ 1.33 → low (SC-004 threshold: > 1.0)
		if d1.Confidence != "low" {
			t.Errorf("confidence = %q, want low (spread=%.3f)", d1.Confidence, d1.SpreadToMean)
		}
		if d1.SpreadToMean <= 1.0 {
			t.Errorf("spread-to-mean = %.3f, want > 1.0 for low confidence", d1.SpreadToMean)
		}
	})

	t.Run("single model → high confidence (no disagreement possible)", func(t *testing.T) {
		forecasts := []Forecast{
			{RegionID: "test", Model: "gfs", DailyData: []DailyForecast{
				{Date: day1, SnowfallCM: 25.0},
			}},
		}

		c := ComputeConsensus(forecasts)
		d1 := c.DailyConsensus[0]
		if d1.Confidence != "high" {
			t.Errorf("confidence = %q, want high for single model", d1.Confidence)
		}
		if d1.SpreadToMean != 0 {
			t.Errorf("spread = %.3f, want 0 for single model", d1.SpreadToMean)
		}
	})

	t.Run("zero snowfall all models → high confidence", func(t *testing.T) {
		forecasts := []Forecast{
			{RegionID: "test", Model: "gfs", DailyData: []DailyForecast{
				{Date: day1, SnowfallCM: 0},
			}},
			{RegionID: "test", Model: "ecmwf", DailyData: []DailyForecast{
				{Date: day1, SnowfallCM: 0},
			}},
		}

		c := ComputeConsensus(forecasts)
		d1 := c.DailyConsensus[0]
		if d1.Confidence != "high" {
			t.Errorf("confidence = %q, want high when all models agree: no snow", d1.Confidence)
		}
	})

	t.Run("empty forecasts → empty consensus", func(t *testing.T) {
		c := ComputeConsensus(nil)
		if len(c.DailyConsensus) != 0 {
			t.Errorf("expected empty consensus, got %d days", len(c.DailyConsensus))
		}
	})

	t.Run("moderate confidence band", func(t *testing.T) {
		forecasts := []Forecast{
			{RegionID: "test", Model: "gfs", DailyData: []DailyForecast{
				{Date: day1, SnowfallCM: 30.0},
			}},
			{RegionID: "test", Model: "ecmwf", DailyData: []DailyForecast{
				{Date: day1, SnowfallCM: 10.0},
			}},
		}

		c := ComputeConsensus(forecasts)
		d1 := c.DailyConsensus[0]
		// spread = (30-10)/20 = 1.0 → moderate (boundary: <= 1.0)
		if d1.Confidence != "moderate" {
			t.Errorf("confidence = %q, want moderate (spread=%.3f)", d1.Confidence, d1.SpreadToMean)
		}
	})
}

func TestCalculateDensity(t *testing.T) {
	tests := []struct {
		name        string
		tempC       float64
		windSpeedMs float64
		wantDensity float64
	}{
		{"moderate calm", -8.0, 2.0, 97.8},
		{"cold windy", -15.0, 10.0, 101.2},
		{"warm wet", -2.0, 5.0, 155.1},
		{"cold calm powder", -15.0, 2.0, 55.8},
		{"cold very windy", -15.0, 15.0, 119.7},
		{"zero wind moderate cold", -10.0, 0.0, 49.0},
		{"very cold calm clamps to floor", -22.0, 1.0, 40.0},
		{"extreme cold clamps to floor", -30.0, 0.0, 40.0},
		{"warm high wind clamps to ceiling", 0.0, 30.0, 250.0},
		{"rain returns zero density", 2.0, 5.0, 0.0},
		{"rain at threshold", 1.67, 5.0, 0.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CalculateDensity(tc.tempC, tc.windSpeedMs)
			delta := got - tc.wantDensity
			if delta < 0 {
				delta = -delta
			}
			if delta > 0.5 {
				t.Errorf("CalculateDensity(%v, %v) = %.1f, want %.1f", tc.tempC, tc.windSpeedMs, got, tc.wantDensity)
			}
		})
	}
}

func TestSLRFromDensity(t *testing.T) {
	tests := []struct {
		name    string
		density float64
		wantSLR float64
	}{
		{"100 kg/m3 → 10:1", 100.0, 10.0},
		{"50 kg/m3 → 20:1", 50.0, 20.0},
		{"200 kg/m3 → 5:1", 200.0, 5.0},
		{"zero density → zero SLR", 0.0, 0.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SLRFromDensity(tc.density)
			delta := got - tc.wantSLR
			if delta < 0 {
				delta = -delta
			}
			if delta > 0.01 {
				t.Errorf("SLRFromDensity(%v) = %.2f, want %.2f", tc.density, got, tc.wantSLR)
			}
		})
	}
}
