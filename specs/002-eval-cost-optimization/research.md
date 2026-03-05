# Research: Evaluation Cost Optimization

## R1: Weather Change Detection — What Constitutes "Material Change"?

**Decision**: Compare detection-critical fields only (snowfall totals, temperature ranges, precipitation amounts) across matching forecast days. A change is "material" if any single day's snowfall differs by more than 2 inches (5 cm), or total window snowfall differs by more than 3 inches (7.6 cm), or temperature min/max shifts by more than 8°F (4.4°C) on any day.

**Rationale**: These thresholds balance sensitivity against noise. Weather APIs return slightly different values on each fetch due to model run timing — a 0.5" snowfall change or 2°F temperature shift doesn't change a tier decision. The existing `materialSnowfallDeltaIn` constant (4 inches) in `domain/comparison.go` provides a precedent for snowfall significance thresholds, but that's for evaluation-to-evaluation comparison. For raw forecast comparison, we use a tighter threshold (2" per day, 3" total) because we want to catch shifts before they become 4" evaluation-level changes.

**Alternatives considered**:
- Hash-based comparison (hash the JSON of detection-critical fields): Simpler but no visibility into *what* changed. Can't log "snowfall increased by 4 inches on Thursday." Also brittle to floating-point serialization differences.
- Percentage-based thresholds (>20% change): Poor behavior at low values — a 1" to 1.5" shift is 50% but meaningless.
- Per-field weighted scoring: Over-engineered for v1. The simple threshold approach can be tuned later.

## R2: Forecast Comparison — Handling Mismatched Day Counts

**Decision**: Compare only the overlapping date range between the two forecast snapshots. If the new forecast has fewer days than the previous snapshot, treat it as a material change (partial data = changed per spec edge case). If the new forecast has more days (extended range grew), only compare the days that existed in both snapshots.

**Rationale**: NWS provides 7-day forecasts, Open-Meteo provides 16-day. The day count can change between fetches if a source temporarily returns fewer days. Treating fewer days as "changed" ensures we don't skip evaluation when data quality dropped. Extra days in extended range don't invalidate the near-range comparison.

**Alternatives considered**:
- Require exact day count match: Too aggressive — would force re-evaluation every time a source adds/drops a day at the edge.
- Ignore day count entirely: Risky — could miss cases where a source returned empty data.

## R3: Cost Estimation — Token Counting Approach

**Decision**: Use a fixed per-call cost estimate based on the observed average (~$0.01 per call: ~5K input tokens at $0.50/1M + ~3K output tokens at $3.00/1M = $0.0025 + $0.0090 = ~$0.012). The Gemini API response does not reliably return token counts in all response modes, so we use a conservative fixed estimate rather than parsing response metadata. The estimate can be tuned via configuration.

**Rationale**: For budget guardrails, approximate accuracy is sufficient. The goal is to prevent $20 months, not to get exact billing. A fixed estimate of $0.015 per call (slightly above observed average) provides a conservative safety margin. At $0.015/call, the $5 budget allows ~333 calls/month, and the $20 hard cap allows ~1,333 calls/month.

**Alternatives considered**:
- Parse `usage_metadata` from Gemini response: The genai SDK may expose token counts, but structured output mode + grounding can make this unreliable. Not worth the complexity for an estimate.
- Track actual billing via Google Cloud Billing API: Over-engineered. Requires additional API credentials and adds a dependency.

## R4: Cost Tracking — Storage Design

**Decision**: Add an `eval_costs` table to SQLite that records one row per successful Gemini call. Monthly aggregation is done via SQL query (SUM WHERE month matches). The table stores: timestamp, storm_id, region_id, estimated_cost, and success flag. Monthly budget checks query this table at the start of each Evaluate call.

**Rationale**: SQLite is already the primary data store (Constitution: Decisions Are Data). A simple table with an index on timestamp makes monthly aggregation fast. No new dependencies needed.

**Alternatives considered**:
- In-memory counter (reset on restart): Loses state between pipeline runs. Since the pipeline runs as a cron job (not a long-lived process), in-memory tracking would reset every run.
- File-based JSON counter: Adds a second state store alongside SQLite for no benefit.

## R5: Gating Logic — Evaluation Order

**Decision**: The evaluation gating checks run in this order for each storm:
1. **First evaluation?** → Always evaluate (FR-005)
2. **Budget exceeded?** → Skip (FR-007), unless first evaluation
3. **Weather changed?** → If yes, evaluate regardless of cooldown (Clarification Q1)
4. **Cooldown elapsed?** → If no, skip (FR-004). If yes, evaluate.

This is a pure function: `ShouldEvaluate(storm, currentTier, lastEvalTime, weatherChanged, budgetExceeded) → (bool, SkipReason)`

**Rationale**: This ordering ensures first evaluations always happen (even at budget cap), budget is checked before expensive weather comparison (but weather comparison happens in Scan anyway so it's already available), and weather change overrides cooldown as clarified in the spec.

**Alternatives considered**:
- Check cooldown before weather change: Would miss re-evaluating storms where weather shifted significantly within the cooldown window. Rejected per spec clarification.

## R6: Trace and Replay Exemption

**Decision**: The `trace` and `replay` commands bypass all gating logic. They construct their own evaluator directly (not through the pipeline's Evaluate stage), so no changes are needed — they already don't go through the gating path. The pipeline gating only applies to `pipeline.Evaluate()`, which `trace` and `replay` don't call.

**Rationale**: Looking at the code, `trace` (`cmd/powder-hunter/main.go:runTrace`) calls `evaluator.Evaluate()` directly, not `pipeline.Evaluate()`. Same for `replay`. Since the gating logic will be added to `pipeline.Evaluate()`, trace and replay are inherently exempt. No code changes needed for FR-011.

**Alternatives considered**:
- Add an explicit bypass flag to the pipeline: Unnecessary since trace/replay don't use the pipeline's Evaluate stage.
