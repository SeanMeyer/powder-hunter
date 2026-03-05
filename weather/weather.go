package weather

import (
	"context"
	"log/slog"

	"golang.org/x/sync/errgroup"

	"github.com/seanmeyer/powder-hunter/domain"
)

// Fetcher retrieves a weather forecast for a region from an external source.
type Fetcher interface {
	Fetch(ctx context.Context, region domain.Region) (domain.Forecast, error)
}

// Service fetches weather for a region from all applicable sources.
// US regions get both Open-Meteo (16-day) and NWS (~7 day gridpoint data).
// Canadian regions get only Open-Meteo.
type Service struct {
	openMeteo *OpenMeteoClient
	nws       *NWSClient
}

func NewService(openMeteo *OpenMeteoClient, nws *NWSClient) *Service {
	return &Service{openMeteo: openMeteo, nws: nws}
}

// FetchAll fetches forecasts for a region from all applicable sources.
// Returns a slice of forecasts (1 for CA, 2 for US). Errors from NWS are
// logged but not fatal — Open-Meteo alone is sufficient.
func (s *Service) FetchAll(ctx context.Context, region domain.Region) ([]domain.Forecast, error) {
	if region.Country != "US" {
		f, err := s.openMeteo.Fetch(ctx, region)
		if err != nil {
			return nil, err
		}
		return []domain.Forecast{f}, nil
	}

	// Both sources fetched in parallel; NWS failure is non-fatal.
	var omForecast, nwsForecast domain.Forecast
	var omErr, nwsErr error

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		omForecast, omErr = s.openMeteo.Fetch(gctx, region)
		return nil
	})
	g.Go(func() error {
		nwsForecast, nwsErr = s.nws.Fetch(gctx, region)
		return nil // NWS errors are non-fatal
	})
	_ = g.Wait()

	if omErr != nil {
		return nil, omErr
	}

	if nwsErr != nil {
		slog.WarnContext(ctx, "nws fetch failed, using open-meteo only",
			"region_id", region.ID,
			"error", nwsErr,
		)
		return []domain.Forecast{omForecast}, nil
	}

	return []domain.Forecast{omForecast, nwsForecast}, nil
}
