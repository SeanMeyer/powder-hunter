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
	forecast, err := client.Fetch(context.Background(), region)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if forecast.RegionID != "test_breck" {
		t.Errorf("RegionID: got %q, want %q", forecast.RegionID, "test_breck")
	}
	if forecast.Source != "open_meteo" {
		t.Errorf("Source: got %q, want %q", forecast.Source, "open_meteo")
	}
	if forecast.FetchedAt.Before(before) || forecast.FetchedAt.After(time.Now().Add(time.Minute)) {
		t.Errorf("FetchedAt %v is not within the last minute", forecast.FetchedAt)
	}
	if len(forecast.DailyData) != 16 {
		t.Errorf("DailyData length: got %d, want 16", len(forecast.DailyData))
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	for i, d := range forecast.DailyData {
		if d.SnowfallCM < 0 {
			t.Errorf("day %d: SnowfallCM %.2f is negative", i, d.SnowfallCM)
		}
		if d.TemperatureMinC < -50 || d.TemperatureMinC > 50 {
			t.Errorf("day %d: TemperatureMinC %.1f out of sane range [-50, 50]", i, d.TemperatureMinC)
		}
		if d.TemperatureMaxC < -50 || d.TemperatureMaxC > 50 {
			t.Errorf("day %d: TemperatureMaxC %.1f out of sane range [-50, 50]", i, d.TemperatureMaxC)
		}
		// Dates must be sequential starting from today or tomorrow.
		expectedDate := today.AddDate(0, 0, i)
		tomorrowStart := today.AddDate(0, 0, 1)
		if d.Date.Equal(tomorrowStart.AddDate(0, 0, i-1)) {
			// Shifted by one day — also acceptable for timezone reasons.
		} else if !d.Date.Equal(expectedDate) {
			// Allow off-by-one for timezone boundary around midnight.
			alt := today.AddDate(0, 0, i+1)
			if !d.Date.Equal(alt) {
				t.Errorf("day %d: Date %s not sequential from today (%s)", i, d.Date.Format("2006-01-02"), today.Format("2006-01-02"))
			}
		}
	}

	t.Logf("Got %d days of forecast data for %s", len(forecast.DailyData), region.Name)
	limit := 3
	if len(forecast.DailyData) < limit {
		limit = len(forecast.DailyData)
	}
	for _, d := range forecast.DailyData[:limit] {
		t.Logf("  %s: %.1fcm snow, %.1f/%.1f°C", d.Date.Format("2006-01-02"), d.SnowfallCM, d.TemperatureMinC, d.TemperatureMaxC)
	}
}
