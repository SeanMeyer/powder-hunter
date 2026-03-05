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

// BriefingContext holds the inputs for a multi-region storm briefing.
type BriefingContext struct {
	MacroRegionName string
	FrictionTier    string
	Summaries       []RegionSummary
}

// RegionSummary is a condensed view of one region's evaluation for briefing.
type RegionSummary struct {
	RegionID       string
	RegionName     string
	Tier           string
	Snowfall       string
	SnowQuality    string
	CrowdEstimate  string
	Strategy       string
	Recommendation string
	BestDay        string
	BestDayReason  string
	LodgingCost    string
	FlightCost     string
	CarRental      string
}

// BriefingResult is the LLM's synthesized storm briefing for one or more regions.
// Used as the opening Discord notification for all storms (singletons and groups).
type BriefingResult struct {
	Briefing      string // 2-4 sentence storm briefing (the notification text)
	BestDay       string // YYYY-MM-DD
	BestDayReason string
	Action        string // "go_now", "book_flexibly", "keep_watching"
	RawResponse   string
}

// Briefer synthesizes per-region evaluations into a storm briefing.
type Briefer interface {
	BriefStorm(ctx context.Context, bc BriefingContext) (BriefingResult, error)
}
