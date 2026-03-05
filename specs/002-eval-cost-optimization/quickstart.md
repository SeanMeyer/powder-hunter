# Quickstart: Evaluation Cost Optimization

## What This Feature Does

Adds three layers of cost control to the pipeline's Evaluate stage:
1. **Weather-change detection** — skips re-evaluation when forecasts haven't materially changed
2. **Tier-based cooldown** — throttles low-priority storm re-evaluation (24h for ON_THE_RADAR, 12h for WORTH_A_LOOK, always for DROP_EVERYTHING)
3. **Budget guardrail** — configurable monthly spend cap with 80% warning

## Key Design Decisions

- **Weather change overrides cooldown**: If forecast data shifts materially, the storm is re-evaluated even within the cooldown window.
- **Compare detection-critical fields only**: Snowfall, temperature, precipitation. Future forecast enrichments (multi-model, SLR, NWS discussions) don't trigger re-evaluation.
- **Only successful calls count toward budget**: Failed/errored Gemini calls don't consume budget.
- **Trace and replay are exempt**: They call the evaluator directly, not through the pipeline gating.
- **First evaluations always proceed**: New storm detections bypass all cost gates.

## How to Test

```bash
# Run tests
go test ./domain/... ./pipeline/... ./storage/...

# Manual test: run pipeline twice quickly, second run should skip most evals
powder-hunter run --dry-run --verbose
powder-hunter run --dry-run --verbose
# Check logs for "evaluation skipped" entries

# Test with budget
powder-hunter run --dry-run --budget 0.05 --verbose
# After a few calls, should see budget warning/skip logs
```

## Files Changed

| Package | File | Change |
|---------|------|--------|
| domain | weather_compare.go | NEW — ForecastsChanged(), WeatherChangeSummary |
| domain | weather_compare_test.go | NEW — threshold edge cases |
| domain | tier.go | MODIFIED — add CooldownFor() |
| pipeline | gating.go | NEW — EvalDecision, ShouldEvaluate() |
| pipeline | pipeline.go | MODIFIED — wire gating into Evaluate stage, extend RunSummary |
| pipeline | pipeline_test.go | MODIFIED — skip scenario tests |
| storage | schema.sql | MODIFIED — add eval_costs table |
| storage | sqlite.go | MODIFIED — add migration |
| storage | costs.go | NEW — cost tracking CRUD |
| cmd/powder-hunter | main.go | MODIFIED — add --budget flag |
