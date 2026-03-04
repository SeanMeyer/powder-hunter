package evaluation

import (
	"context"

	"github.com/seanmeyer/powder-hunter/domain"
)

// Evaluator scores a storm opportunity using forecast data, resort context, and
// the subscriber's profile. History is provided so the evaluator can classify
// whether changes are material, minor, or a downgrade relative to prior scores.
type Evaluator interface {
	Evaluate(ctx context.Context, forecasts []domain.Forecast, region domain.Region, resorts []domain.Resort, profile domain.UserProfile, history []domain.Evaluation) (domain.Evaluation, error)
}
