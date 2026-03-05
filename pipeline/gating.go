package pipeline

import (
	"time"

	"github.com/seanmeyer/powder-hunter/domain"
)

// EvalDecision is the result of the gating logic for a single storm.
type EvalDecision struct {
	ShouldEvaluate  bool
	Reason          domain.SkipReason
	TimeSinceLastEval time.Duration
	WeatherChange   domain.WeatherChangeSummary
}

// ShouldEvaluate determines whether a storm should be re-evaluated.
// Gating order (per research R5):
//  1. First evaluation → always evaluate
//  2. Budget exceeded → skip (unless first eval)
//  3. Weather changed → evaluate regardless of cooldown
//  4. Cooldown elapsed → skip if not elapsed, evaluate if elapsed
func ShouldEvaluate(isFirstEval bool, currentTier domain.Tier, timeSinceLastEval time.Duration, weatherChange domain.WeatherChangeSummary, budgetExceeded bool) EvalDecision {
	decision := EvalDecision{
		TimeSinceLastEval: timeSinceLastEval,
		WeatherChange:     weatherChange,
	}

	// First evaluation always proceeds (FR-005).
	if isFirstEval {
		decision.ShouldEvaluate = true
		return decision
	}

	// Budget check (FR-007). First evals already returned above.
	if budgetExceeded {
		decision.ShouldEvaluate = false
		decision.Reason = domain.SkipBudgetExceeded
		return decision
	}

	// Weather change overrides cooldown (spec clarification).
	if weatherChange.Changed {
		decision.ShouldEvaluate = true
		return decision
	}

	// Tier-based cooldown (FR-004). Only applies when weather is unchanged.
	cooldown := domain.CooldownFor(currentTier)
	if cooldown > 0 && timeSinceLastEval < cooldown {
		decision.ShouldEvaluate = false
		decision.Reason = domain.SkipCooldown
		return decision
	}

	// Weather unchanged but cooldown elapsed — re-evaluate to check for
	// changes in enrichment data, model consensus, etc.
	decision.ShouldEvaluate = true
	return decision
}
