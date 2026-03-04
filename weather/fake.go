package weather

import (
	"context"

	"github.com/seanmeyer/powder-hunter/domain"
)

// FakeFetcher returns preconfigured forecast data for testing.
type FakeFetcher struct {
	// Forecasts maps region ID to the forecast to return.
	Forecasts map[string]domain.Forecast
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
	if forecast, ok := f.Forecasts[region.ID]; ok {
		return forecast, nil
	}
	return domain.Forecast{RegionID: region.ID}, nil
}
