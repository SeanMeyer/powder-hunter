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

// CompareContext holds the inputs for a multi-region storm comparison.
type CompareContext struct {
	MacroRegionName string
	FrictionTier    string
	Summaries       []RegionSummary
}

// RegionSummary is a condensed view of one region's evaluation for comparison.
type RegionSummary struct {
	RegionID       string
	RegionName     string
	Tier           string
	Snowfall       string
	SnowQuality    string
	TopPick        string
	TopPickReason  string
	BestDay        string
	BestDayReason  string
	Recommendation string
	LodgingCost    string
	FlightCost     string
	CarRental      string
}

// ComparisonResult is the LLM's synthesis across multiple regions.
type ComparisonResult struct {
	TopPickRegion  string
	TopPickName    string
	Reasoning      string
	RunnerUp       string
	RunnerUpReason string
	RawResponse    string
}

// Comparer synthesizes multiple region evaluations into a grouped recommendation.
type Comparer interface {
	CompareRegions(ctx context.Context, cc CompareContext) (ComparisonResult, error)
}
