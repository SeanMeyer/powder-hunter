package evaluation

import (
	"context"

	"github.com/seanmeyer/powder-hunter/domain"
)

// EvalContext bundles all inputs needed for storm evaluation. This replaces the
// previous multi-parameter Evaluator.Evaluate signature so new data (consensus,
// forecast discussions) can be added without interface churn.
type EvalContext struct {
	Forecasts       []domain.Forecast
	Region          domain.Region
	Resorts         []domain.Resort
	Profile         domain.UserProfile
	History         []domain.Evaluation
	ResortConsensus map[string]domain.ModelConsensus // per-resort consensus keyed by resort ID
	Discussion      *domain.ForecastDiscussion       // nil when unavailable (non-US, fetch failure)
}

// Evaluator scores a storm opportunity using forecast data, resort context, and
// the subscriber's profile. History is provided so the evaluator can classify
// whether changes are material, minor, or a downgrade relative to prior scores.
type Evaluator interface {
	Evaluate(ctx context.Context, ec EvalContext) (domain.Evaluation, error)
}
