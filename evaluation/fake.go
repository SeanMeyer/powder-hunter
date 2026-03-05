package evaluation

import (
	"context"

	"github.com/seanmeyer/powder-hunter/domain"
)

// FakeEvaluator returns preconfigured evaluation results for testing.
type FakeEvaluator struct {
	// Results maps region ID to the evaluation to return.
	Results map[string]domain.Evaluation
	// Errors maps region ID to the error to return.
	Errors map[string]error
	// EvaluateCalls records calls for assertions.
	EvaluateCalls []EvaluateCall
}

// EvaluateCall captures the arguments of a single Evaluate invocation.
type EvaluateCall struct {
	RegionID        string
	Forecasts       []domain.Forecast
	ResortConsensus map[string]domain.ModelConsensus
}

func (f *FakeEvaluator) Evaluate(ctx context.Context, ec EvalContext) (domain.Evaluation, error) {
	f.EvaluateCalls = append(f.EvaluateCalls, EvaluateCall{
		RegionID:        ec.Region.ID,
		Forecasts:       ec.Forecasts,
		ResortConsensus: ec.ResortConsensus,
	})
	if err, ok := f.Errors[ec.Region.ID]; ok {
		return domain.Evaluation{}, err
	}
	if result, ok := f.Results[ec.Region.ID]; ok {
		return result, nil
	}
	return domain.Evaluation{Tier: domain.TierOnTheRadar}, nil
}
