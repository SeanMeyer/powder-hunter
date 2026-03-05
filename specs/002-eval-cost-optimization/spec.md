# Feature Specification: Evaluation Cost Optimization

**Feature Branch**: `002-eval-cost-optimization`
**Created**: 2026-03-04
**Status**: Draft
**Blocked by**: `003-forecast-improvements` — must merge to main before 002 implementation begins (see Dependencies below)
**Input**: User description: "Keep Gemini evaluation costs at ~$3/month, never exceeding $20/month, by reducing unnecessary API calls through weather-change detection, tiered evaluation frequency, and smart region handling."

## Clarifications

### Session 2026-03-04

- Q: When both cooldown and weather-change checks apply, which takes precedence? → A: Weather change overrides cooldown. If weather changed materially, always re-evaluate regardless of cooldown. Cooldown only suppresses re-evaluation when weather is also stable.
- Q: Should the `trace` command be exempt from cost optimizations? → A: Yes, exempt. Trace always evaluates with no cooldown or budget checks — it is a debugging tool that should always work.
- Q: Should a failed Gemini API call count against the monthly budget? → A: No. Only successful calls count toward budget tracking.
- Q: Should weather-change detection compare all forecast fields or only detection-critical fields? → A: Compare only detection-critical fields (snowfall totals, temperature, precipitation). Enrichment data added by future forecast improvements (multi-model, SLR, NWS discussions, elevation gradients) should not trigger re-evaluation on its own.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Skip Evaluation When Weather Hasn't Changed (Priority: P1)

As the pipeline operator, I want the system to skip re-evaluating a storm when the underlying weather forecast hasn't materially changed since the last evaluation, so that I avoid paying for redundant Gemini API calls that would produce the same result.

**Why this priority**: This is the single highest-impact optimization. During active storm patterns, re-evaluation of unchanged forecasts is the dominant source of wasted API spend. Weather APIs are free; Gemini calls are not.

**Independent Test**: Run the pipeline twice in succession for a region with an active storm. If forecasts haven't changed between runs, the second run should skip the Gemini evaluation and log that it was skipped. Verify no API call is made.

**Acceptance Scenarios**:

1. **Given** a region has an active storm with a prior evaluation, **When** the pipeline runs and fetched forecasts are substantially identical to the weather snapshot stored with the last evaluation, **Then** the system skips the Gemini call for that region and logs the skip reason.
2. **Given** a region has an active storm with a prior evaluation, **When** the pipeline runs and fetched forecasts show meaningful changes (snowfall totals differ, temperature swings, new precipitation windows), **Then** the system proceeds with a full Gemini evaluation.
3. **Given** a storm has never been evaluated (new detection), **When** the pipeline runs, **Then** the system always performs a full Gemini evaluation regardless of any change detection logic.

---

### User Story 2 - Reduce Evaluation Frequency for Low-Tier Storms (Priority: P2)

As the pipeline operator, I want the system to re-evaluate low-tier storms (ON_THE_RADAR) less frequently than high-tier storms (DROP_EVERYTHING), so that I spend API budget on the storms that matter most.

**Why this priority**: Not all storms deserve the same attention cadence. A storm rated ON_THE_RADAR that hasn't changed much doesn't need re-evaluation every 12 hours. This reduces call volume during busy storm seasons when many regions are "on the radar" simultaneously.

**Independent Test**: Seed a storm with an ON_THE_RADAR evaluation from 13 hours ago. Run the pipeline. Verify the storm is skipped because it was evaluated too recently for its tier. Then seed a DROP_EVERYTHING storm from 13 hours ago and verify it IS re-evaluated.

**Acceptance Scenarios**:

1. **Given** a storm's current tier is ON_THE_RADAR and its last evaluation was less than 24 hours ago and weather has not materially changed, **When** the pipeline runs, **Then** the system skips re-evaluation for that storm.
2. **Given** a storm's current tier is WORTH_A_LOOK and its last evaluation was less than 12 hours ago and weather has not materially changed, **When** the pipeline runs, **Then** the system skips re-evaluation for that storm.
3. **Given** a storm's current tier is DROP_EVERYTHING, **When** the pipeline runs, **Then** the system always re-evaluates (subject to weather-change detection from User Story 1).
4. **Given** a storm is within its tier-based cooldown period but weather has materially changed, **When** the pipeline runs, **Then** the system proceeds with re-evaluation (weather change overrides cooldown).
5. **Given** a storm's tier-based cooldown has elapsed AND the weather has changed, **When** the pipeline runs, **Then** the system proceeds with re-evaluation.

---

### User Story 3 - Cost Tracking and Budget Guardrails (Priority: P3)

As the pipeline operator, I want the system to track how many Gemini API calls it makes and estimate costs, so that I can monitor spend and the system can halt evaluations if a monthly budget limit is approached.

**Why this priority**: Even with the above optimizations, unexpected patterns (45 simultaneous storms with rapidly shifting forecasts) could cause cost spikes. A budget guardrail provides a hard ceiling so spending never silently grows.

**Independent Test**: Run the pipeline with a configured monthly budget of $5. Simulate enough evaluations to approach the limit. Verify the system logs a warning at 80% and stops making new Gemini calls at 100%.

**Acceptance Scenarios**:

1. **Given** a monthly budget limit is configured, **When** the system estimates that cumulative calls this month have reached 80% of budget, **Then** the system logs a warning.
2. **Given** cumulative estimated cost has reached the budget limit, **When** the pipeline attempts to evaluate a storm, **Then** the system skips the evaluation and logs that the budget limit was reached.
3. **Given** a new calendar month begins, **When** the pipeline runs, **Then** the cost counter resets.
4. **Given** no budget limit is configured, **When** the pipeline runs, **Then** all evaluations proceed normally with no budget enforcement.

---

### User Story 4 - Pipeline Run Observability (Priority: P3)

As the pipeline operator, I want to see a summary of how many evaluations were skipped (and why) at the end of each pipeline run, so that I can verify cost optimizations are working and tune thresholds.

**Why this priority**: Without observability, the operator can't tell if optimizations are working or if they're being too aggressive (skipping evaluations that should happen).

**Independent Test**: Run the pipeline with a mix of changed/unchanged weather and varied storm tiers. Check the run summary includes skip counts and reasons.

**Acceptance Scenarios**:

1. **Given** the pipeline completes a run, **When** results are summarized, **Then** the summary includes counts for: evaluated, skipped (unchanged weather), skipped (cooldown), skipped (budget).
2. **Given** a storm evaluation is skipped, **When** the skip is logged, **Then** the log entry includes the storm ID, region ID, skip reason, and time since last evaluation.

---

### Edge Cases

- What happens when the weather API returns partial data (fewer forecast days than expected)? The system should treat partial data as "changed" and proceed with evaluation to avoid stale assessments.
- What happens when a storm is first detected but the budget limit has already been reached? New storm detections should still be evaluated (first evaluation is always allowed) to avoid missing a DROP_EVERYTHING event entirely.
- What happens when the pipeline interval changes from 12h to 6h mid-month? Cooldown periods are based on absolute time since last evaluation, not pipeline run count, so they adapt automatically.
- How does the system handle a storm that upgrades from ON_THE_RADAR to DROP_EVERYTHING? The tier-based cooldown uses the current tier at evaluation time, so an upgrade would immediately make the storm eligible for more frequent evaluation on the next run.
- What happens when spec 003 (Forecast Improvements) adds new fields to the forecast structure (multi-model, SLR, NWS discussions, elevation gradients)? Weather-change detection compares only detection-critical fields (snowfall, temperature, precipitation), so new enrichment fields do not trigger re-evaluation. The comparison logic is forward-compatible with forecast schema changes.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST compare current forecasts against the weather snapshot stored with the most recent evaluation for each storm before calling the Gemini API.
- **FR-002**: System MUST define a "material change" threshold for weather comparison based on detection-critical fields only: snowfall totals, temperature ranges, and precipitation timing. Enrichment data (multi-model comparisons, SLR calculations, NWS forecast discussions, elevation gradient data) MUST NOT be included in the change comparison, so that forecast schema evolution does not trigger unnecessary re-evaluations.
- **FR-003**: System MUST skip Gemini evaluation when forecasts have not materially changed and log the skip with a structured reason.
- **FR-004**: System MUST enforce tier-based evaluation cooldown periods: DROP_EVERYTHING storms have no cooldown, WORTH_A_LOOK storms have a 12-hour cooldown, ON_THE_RADAR storms have a 24-hour cooldown. Cooldown only applies when weather has not materially changed; a material weather change overrides the cooldown for any tier.
- **FR-005**: System MUST always perform a Gemini evaluation for newly detected storms (first evaluation), regardless of cooldown or budget limits.
- **FR-006**: System MUST track the count of Gemini API calls made per calendar month.
- **FR-007**: System MUST support a configurable monthly budget limit (in USD) and halt non-essential evaluations when the limit is reached.
- **FR-008**: System MUST emit a warning when estimated monthly spend reaches 80% of the configured budget.
- **FR-009**: System MUST include skip counts and reasons in the pipeline run summary.
- **FR-010**: System MUST preserve all existing pipeline behavior (scan, detect, compare, post, expire) when no optimizations are triggered.
- **FR-011**: The `trace` and `replay` commands MUST be exempt from all cost optimizations (cooldown, weather-change gating, and budget limits). These debugging tools always perform a full evaluation.

### Key Entities

- **Evaluation Cost Record**: Tracks per-call cost estimates and monthly aggregates. Key attributes: call timestamp, storm ID, region ID, estimated cost (fixed per-call estimate per research R3), whether the call succeeded (only successful calls count toward budget).
- **Weather Change Summary**: A comparison result between two forecast snapshots for the same region. Compares only detection-critical fields (snowfall totals, temperature, precipitation) and ignores enrichment data. Key attributes: changed (boolean), change magnitude, which detection-critical fields differed.

## Dependencies

### Blocked by: 003-forecast-improvements

This feature MUST be implemented after `003-forecast-improvements` merges to main. The 003 branch includes extensive changes to the forecast data model and pipeline that directly affect our weather-change comparison logic:

- **`domain/weather.go`**: 003 adds `ResortID`, `Model` fields to `Forecast`; `SLRatio`, `RainHours`, `MixedHours` to `DailyForecast`; `FreezingLevelMinM/MaxM` to `HalfDay`; and entirely new types (`ModelConsensus`, `DayConsensus`, `ForecastDiscussion`, SLR functions).
- **`pipeline/pipeline.go`**: 003 expands `ScanResult` with `ResortConsensus` and `Discussion` fields; refactors `Scan()` signature (regionFilter moved to `Run()`); adds per-resort multi-model fetching.
- **`evaluation/prompt.go`**: 003 adds +328 lines for multi-model and SLR prompt formatting.
- **`weather/` package**: 003 substantially reworks both `openmeteo.go` and `nws.go` for multi-model and SLR support.

Building 002 against the current (pre-003) code would require rewriting `weather_compare.go` after 003 merges. By waiting, we write the comparison logic once against the final `Forecast` shape and avoid merge conflicts in `pipeline.go`.

Our spec decision to compare only "detection-critical fields" (snowfall, temperature, precipitation) is even more important given 003's additions — the new enrichment fields (`SLRatio`, `RainHours`, `MixedHours`, `ModelConsensus`, `ForecastDiscussion`) should not trigger re-evaluation.

## Assumptions

- Gemini 3 Flash Preview pricing remains at $0.50/1M input tokens, $3.00/1M output tokens, with 5,000 free grounding searches per month on the paid tier.
- Average token usage per evaluation call is approximately 5,000 input tokens and 3,000 output tokens (~$0.01 per call).
- The pipeline's default 12-hour interval is the baseline; cost estimates assume 2 runs per day.
- Weather forecasts from OpenMeteo and NWS are free and do not contribute to cost.
- The existing `WeatherSnapshot` stored on each evaluation provides a sufficient baseline for forecast comparison — no additional storage is needed.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Monthly Gemini API cost stays at or below $5 during typical storm seasons (10-15 active regions), targeting ~$3/month.
- **SC-002**: Monthly cost never exceeds $20 under any conditions when a budget limit is configured.
- **SC-003**: At least 50% of re-evaluations are skipped during stable weather patterns (forecasts not changing between runs).
- **SC-004**: New storm detections are never skipped — 100% of first-time evaluations proceed regardless of optimizations.
- **SC-005**: Pipeline run summary clearly reports how many evaluations were performed vs. skipped, with categorized skip reasons, in every run.
