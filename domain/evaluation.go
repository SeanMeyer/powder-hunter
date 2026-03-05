package domain

import "time"

// Evaluation is the full output of one LLM scoring pass on a storm. Every field
// is preserved so the pipeline can detect material changes across re-evaluations
// without re-querying the LLM unnecessarily.
type Evaluation struct {
	ID                 int64
	StormID            int64
	EvaluatedAt        time.Time
	PromptVersion      string
	Tier               Tier
	Recommendation     string
	DayByDay           []DayEvaluation
	KeyFactors         KeyFactors
	LogisticsSummary   LogisticsSummary
	Summary            string
	TopResortPicks     []ResortPick
	Strategy           string
	SnowQuality        string
	CrowdEstimate      string
	ClosureRisk        string
	BestSkiDay         time.Time
	BestSkiDayReason   string
	WeatherSnapshot    []Forecast     // raw weather data captured at evaluation time
	RawLLMResponse     string
	StructuredResponse map[string]any
	GroundingSources   []string
	RenderedPrompt     string      // the full prompt sent (or that would be sent) to the LLM
	ChangeClass        ChangeClass // set by comparison against prior evaluation
	Delivered          bool
}

// ChangeClass categorizes how much an evaluation differs from the previous one,
// driving whether a Discord update is sent and at what notification level.
type ChangeClass string

const (
	ChangeNew       ChangeClass = "new"       // no prior evaluation exists
	ChangeMaterial  ChangeClass = "material"  // tier changed or conditions significantly shifted
	ChangeMinor     ChangeClass = "minor"     // details updated but same tier
	ChangeDowngrade ChangeClass = "downgrade" // tier dropped; requires explicit user notice
)

// DayEvaluation is the LLM's narrative assessment for a single day within the window.
type DayEvaluation struct {
	Date           time.Time
	Snowfall       string
	Conditions     string
	Recommendation string
}

// KeyFactors captures the LLM's bulleted pro/con breakdown for the storm opportunity.
type KeyFactors struct {
	Pros []string
	Cons []string
}

// ResortPick is a single resort recommendation with reasoning for why it suits
// this specific storm. The LLM ranks 2-3 picks per evaluation.
type ResortPick struct {
	Resort string
	Reason string
}

// LogisticsSummary holds the LLM's narrative on trip logistics. Fields are strings
// rather than structured data because the LLM produces natural-language summaries
// that vary in specificity depending on available grounding data.
type LogisticsSummary struct {
	Lodging            string
	Transportation     string
	RoadConditions     string
	FlightCost         string
	CarRental          string
	LodgingCost        string
	TotalEstimatedCost string
}
