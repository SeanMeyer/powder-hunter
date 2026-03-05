package weather

import (
	"context"

	"github.com/seanmeyer/powder-hunter/domain"
)

// FakeFetcher returns preconfigured forecast data for testing.
// Supports both single-forecast and multi-model responses.
type FakeFetcher struct {
	// Forecasts maps region ID to a single forecast to return.
	Forecasts map[string]domain.Forecast
	// MultiForecasts maps region ID to multiple forecasts (multi-model).
	// Takes precedence over Forecasts when present.
	MultiForecasts map[string][]domain.Forecast
	// Errors maps region ID to the error to return.
	Errors map[string]error
	// FetchCalls records which regions were fetched (for assertions).
	FetchCalls []string
}

func (f *FakeFetcher) Fetch(ctx context.Context, region domain.Region) (domain.Forecast, error) {
	f.FetchCalls = append(f.FetchCalls, region.ID)
	if err, ok := f.Errors[region.ID]; ok {
		return domain.Forecast{}, err
	}
	if multi, ok := f.MultiForecasts[region.ID]; ok && len(multi) > 0 {
		return multi[0], nil
	}
	if forecast, ok := f.Forecasts[region.ID]; ok {
		return forecast, nil
	}
	return domain.Forecast{RegionID: region.ID}, nil
}

// FetchMulti returns multiple forecasts for a region (multi-model support).
func (f *FakeFetcher) FetchMulti(ctx context.Context, region domain.Region) ([]domain.Forecast, error) {
	f.FetchCalls = append(f.FetchCalls, region.ID)
	if err, ok := f.Errors[region.ID]; ok {
		return nil, err
	}
	if multi, ok := f.MultiForecasts[region.ID]; ok {
		return multi, nil
	}
	if forecast, ok := f.Forecasts[region.ID]; ok {
		return []domain.Forecast{forecast}, nil
	}
	return []domain.Forecast{{RegionID: region.ID}}, nil
}
