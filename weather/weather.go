package weather

import (
	"context"
	"log/slog"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/seanmeyer/powder-hunter/domain"
)

// Fetcher retrieves a weather forecast for a region from an external source.
type Fetcher interface {
	Fetch(ctx context.Context, region domain.Region) (domain.Forecast, error)
}

// FetchResult holds all weather data fetched for a region in a single pass.
type FetchResult struct {
	Forecasts  []domain.Forecast
	Discussion *domain.ForecastDiscussion // nil for non-US or fetch failure
}

// Service fetches weather for a region from all applicable sources.
// Queries are made per-resort at mid-mountain elevation for temperature accuracy.
// US regions get both Open-Meteo (16-day, multi-model) and NWS (~7 day gridpoint data).
// Canadian regions get only Open-Meteo.
type Service struct {
	openMeteo *OpenMeteoClient
	nws       *NWSClient
}

func NewService(openMeteo *OpenMeteoClient, nws *NWSClient) *Service {
	return &Service{openMeteo: openMeteo, nws: nws}
}

// FetchAll fetches forecasts for each resort in a region from all applicable sources.
// Each resort is queried at its own coordinates and mid-mountain elevation.
// Open-Meteo returns one Forecast per model per resort. NWS provides an additional
// forecast per resort. AFD is fetched once per unique WFO.
// Errors for individual resorts/sources are logged but not fatal.
func (s *Service) FetchAll(ctx context.Context, region domain.Region, resorts []domain.Resort) (FetchResult, error) {
	if len(resorts) == 0 {
		return FetchResult{}, nil
	}

	var (
		mu         sync.Mutex
		forecasts  []domain.Forecast
		discussion *domain.ForecastDiscussion
	)

	g, gctx := errgroup.WithContext(ctx)

	// Fetch Open-Meteo per resort (all in parallel).
	for _, resort := range resorts {
		resort := resort
		g.Go(func() error {
			q := openMeteoQuery{
				RegionID:   region.ID,
				ResortID:   resort.ID,
				Lat:        resort.Latitude,
				Lon:        resort.Longitude,
				ElevationM: resort.MidMountainElevationM(),
				Country:    region.Country,
				Timezone:   region.Timezone,
			}
			result, err := s.openMeteo.FetchForResort(gctx, q)
			if err != nil {
				slog.WarnContext(gctx, "open-meteo fetch failed for resort",
					"region_id", region.ID, "resort_id", resort.ID, "error", err)
				return nil // non-fatal per-resort
			}
			mu.Lock()
			forecasts = append(forecasts, result...)
			mu.Unlock()
			return nil
		})
	}

	// For US regions: fetch NWS per resort + AFD per unique WFO.
	if region.Country == "US" {
		for _, resort := range resorts {
			resort := resort
			g.Go(func() error {
				f, err := s.nws.Fetch(gctx, domain.Region{
					ID:        region.ID,
					Latitude:  resort.Latitude,
					Longitude: resort.Longitude,
					Country:   region.Country,
					Timezone:  region.Timezone,
				})
				if err != nil {
					slog.WarnContext(gctx, "nws fetch failed for resort",
						"region_id", region.ID, "resort_id", resort.ID, "error", err)
					return nil
				}
				f.ResortID = resort.ID
				mu.Lock()
				forecasts = append(forecasts, f)
				mu.Unlock()
				return nil
			})
		}

		// AFD: resolve WFO for first resort (resorts in a region typically share a WFO).
		// If they don't, we get the most relevant one.
		g.Go(func() error {
			wfo := s.nws.WFO(gctx, resorts[0].Latitude, resorts[0].Longitude)
			if wfo == "" {
				return nil
			}
			afd, err := s.nws.FetchAFD(gctx, wfo)
			if err != nil {
				slog.WarnContext(gctx, "nws afd fetch failed",
					"region_id", region.ID, "wfo", wfo, "error", err)
				return nil
			}
			if afd.Text != "" {
				mu.Lock()
				discussion = &afd
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return FetchResult{}, err
	}

	if len(forecasts) == 0 {
		return FetchResult{}, nil
	}

	return FetchResult{
		Forecasts:  forecasts,
		Discussion: discussion,
	}, nil
}
