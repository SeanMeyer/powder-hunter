package weather

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/seanmeyer/powder-hunter/domain"
)

func skipUnlessSmoke(t *testing.T) {
	t.Helper()
	if os.Getenv("SMOKE_TEST") != "1" {
		t.Skip("set SMOKE_TEST=1 to run smoke tests")
	}
}

func TestSmoke_OpenMeteo_FetchForecast(t *testing.T) {
	skipUnlessSmoke(t)

	client := NewOpenMeteoClient(http.DefaultClient)
	region := domain.Region{
		ID:        "test_breck",
		Name:      "Test Breckenridge",
		Latitude:  39.4817,
		Longitude: -106.0384,
		Country:   "US",
	}

	before := time.Now()
	q := openMeteoQuery{
		RegionID: region.ID, ResortID: "test_resort",
		Lat: region.Latitude, Lon: region.Longitude,
		ElevationM: 1500, Country: region.Country,
	}
	forecasts, err := client.FetchForResort(context.Background(), q)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if len(forecasts) == 0 {
		t.Fatal("expected at least 1 forecast, got 0")
	}

	// With multi-model enabled, we expect one forecast per model.
	t.Logf("Got %d model forecast(s)", len(forecasts))

	for _, forecast := range forecasts {
		if forecast.RegionID != "test_breck" {
			t.Errorf("RegionID: got %q, want %q", forecast.RegionID, "test_breck")
		}
		if forecast.Source != "open_meteo" {
			t.Errorf("Source: got %q, want %q", forecast.Source, "open_meteo")
		}
		if forecast.FetchedAt.Before(before) || forecast.FetchedAt.After(time.Now().Add(time.Minute)) {
			t.Errorf("FetchedAt %v is not within the last minute", forecast.FetchedAt)
		}

		t.Logf("  Model: %s, %d days of data", forecast.Model, len(forecast.DailyData))

		today := time.Now().UTC().Truncate(24 * time.Hour)
		for i, d := range forecast.DailyData {
			if d.SnowfallCM < 0 {
				t.Errorf("day %d: SnowfallCM %.2f is negative", i, d.SnowfallCM)
			}
			if d.TemperatureMinC < -50 || d.TemperatureMinC > 50 {
				t.Errorf("day %d: TemperatureMinC %.1f out of sane range [-50, 50]", i, d.TemperatureMinC)
			}
			// Dates must be roughly sequential.
			expectedDate := today.AddDate(0, 0, i)
			if d.Date.Before(expectedDate.AddDate(0, 0, -1)) || d.Date.After(expectedDate.AddDate(0, 0, 1)) {
				t.Errorf("day %d: Date %s not sequential from today (%s)", i, d.Date.Format("2006-01-02"), today.Format("2006-01-02"))
			}
		}

		limit := 3
		if len(forecast.DailyData) < limit {
			limit = len(forecast.DailyData)
		}
		for _, d := range forecast.DailyData[:limit] {
			t.Logf("    %s: %.1fcm snow (SLR %.0f:1), %.1f/%.1f°C, %dh rain, %dh mixed",
				d.Date.Format("2006-01-02"), d.SnowfallCM, d.SLRatio,
				d.TemperatureMinC, d.TemperatureMaxC, d.RainHours, d.MixedHours)
		}
	}
}
