package weather

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/seanmeyer/powder-hunter/domain"
)

func TestSmoke_NWS_FetchForecast(t *testing.T) {
	skipUnlessSmoke(t)

	client := NewNWSClient(http.DefaultClient)
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
	if forecast.Source != "nws" {
		t.Errorf("Source: got %q, want %q", forecast.Source, "nws")
	}
	if forecast.FetchedAt.Before(before) || forecast.FetchedAt.After(time.Now().Add(time.Minute)) {
		t.Errorf("FetchedAt %v is not within the last minute", forecast.FetchedAt)
	}

	// NWS gridpoint data typically covers 5-9 days.
	if len(forecast.DailyData) < 3 {
		t.Errorf("DailyData length: got %d, want at least 3", len(forecast.DailyData))
	}
	if len(forecast.DailyData) > 14 {
		t.Errorf("DailyData length: got %d, want at most 14", len(forecast.DailyData))
	}

	var prevDate time.Time
	for i, d := range forecast.DailyData {
		// Snowfall must be non-negative and sane (no single day >100cm after mm→cm conversion).
		if d.SnowfallCM < 0 {
			t.Errorf("day %d (%s): SnowfallCM %.2f is negative", i, d.Date.Format("2006-01-02"), d.SnowfallCM)
		}
		if d.SnowfallCM > 100 {
			t.Errorf("day %d (%s): SnowfallCM %.2f exceeds sanity limit of 100cm (~40in)", i, d.Date.Format("2006-01-02"), d.SnowfallCM)
		}
		// Temperature sanity: mountain temps should be within [-50, 50]°C.
		if d.TemperatureMinC < -50 || d.TemperatureMinC > 50 {
			t.Errorf("day %d (%s): TemperatureMinC %.1f out of sane range [-50, 50]", i, d.Date.Format("2006-01-02"), d.TemperatureMinC)
		}
		if d.TemperatureMaxC < -50 || d.TemperatureMaxC > 50 {
			t.Errorf("day %d (%s): TemperatureMaxC %.1f out of sane range [-50, 50]", i, d.Date.Format("2006-01-02"), d.TemperatureMaxC)
		}
		// Dates must be in chronological order.
		if i > 0 && !d.Date.After(prevDate) {
			t.Errorf("day %d: date %s not after previous %s", i, d.Date.Format("2006-01-02"), prevDate.Format("2006-01-02"))
		}
		prevDate = d.Date
	}

	t.Logf("Got %d days of NWS forecast data for %s", len(forecast.DailyData), region.Name)
	for _, d := range forecast.DailyData {
		t.Logf("  %s: %.1fcm snow, %.1f/%.1f°C", d.Date.Format("2006-01-02"), d.SnowfallCM, d.TemperatureMinC, d.TemperatureMaxC)
	}
}

func TestSmoke_NWS_CanadianCoordinate(t *testing.T) {
	skipUnlessSmoke(t)

	client := NewNWSClient(http.DefaultClient)
	region := domain.Region{
		ID:        "test_whistler",
		Name:      "Test Whistler",
		Latitude:  50.1163,
		Longitude: -122.9574,
		Country:   "CA",
	}

	_, err := client.Fetch(context.Background(), region)
	if err == nil {
		t.Fatal("expected error for non-US coordinate, got nil")
	}
	t.Logf("Got expected error for Canadian coordinate: %v", err)
}
