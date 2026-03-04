package weather

import (
	"context"
	"net/http"
	"testing"

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

	forecast, err := client.Fetch(context.Background(), region)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if forecast.Source != "nws" {
		t.Errorf("Source: got %q, want %q", forecast.Source, "nws")
	}
	if len(forecast.DailyData) == 0 {
		t.Error("DailyData is empty, expected at least 1 day")
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
