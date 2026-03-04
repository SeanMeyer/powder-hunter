package weather

import (
	"context"

	"github.com/seanmeyer/powder-hunter/domain"
)

// Fetcher retrieves a weather forecast for a region from an external source.
type Fetcher interface {
	Fetch(ctx context.Context, region domain.Region) (domain.Forecast, error)
}
