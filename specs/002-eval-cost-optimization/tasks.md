# Tasks: Evaluation Cost Optimization

**Input**: Design documents from `/specs/002-eval-cost-optimization/`
**Prerequisites**: plan.md (required), spec.md (required), research.md, data-model.md, contracts/cli.md

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

---

## Phase 1: Setup

**Purpose**: Database schema and migration for cost tracking — required by US3 but cheap to add upfront since it doesn't affect existing behavior.

- [x] T001 Add `eval_costs` table to `storage/schema.sql` with columns: id (PK), storm_id (FK), region_id (TEXT), evaluated_at (TEXT), estimated_cost_usd (REAL), success (INTEGER) and index `idx_eval_costs_month` on evaluated_at

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Domain types and pure functions that multiple user stories depend on.

- [x] T002 Add `WeatherChangeSummary` struct to `domain/weather_compare.go` with fields: Changed (bool), TotalSnowfallDeltaIn (float64), MaxDailySnowfallDeltaIn (float64), MaxTempDeltaC (float64), DaysMismatched (int), Reason (string). Add threshold constants: DailySnowfallThresholdIn=2.0, TotalSnowfallThresholdIn=3.0, TempThresholdC=4.4
- [x] T003 Add `ForecastsChanged(previous, current []domain.Forecast) WeatherChangeSummary` pure function to `domain/weather_compare.go` — compare only detection-critical fields (snowfall totals, temperature, precipitation) across matching days. Treat fewer days in current as material change. Ignore enrichment fields (SLRatio, RainHours, MixedHours, ModelConsensus, ForecastDiscussion)
- [x] T004 Add `SkipReason` string type and constants (SkipNone, SkipUnchangedWeather, SkipCooldown, SkipBudgetExceeded) to `domain/weather_compare.go`
- [x] T005 [P] Add `EvalDecision` struct to `pipeline/gating.go` with fields: ShouldEvaluate (bool), Reason (SkipReason), TimeSinceLastEval (time.Duration), WeatherChange (WeatherChangeSummary)
- [x] T006 [P] Add `CooldownFor(tier Tier) time.Duration` function to `domain/tier.go` — DROP_EVERYTHING returns 0, WORTH_A_LOOK returns 12h, ON_THE_RADAR returns 24h
- [x] T007 [P] Extend `RunSummary` in `pipeline/pipeline.go` with fields: SkippedUnchanged (int), SkippedCooldown (int), SkippedBudget (int)

**Checkpoint**: All domain types and pure functions ready. User story implementation can begin.

---

## Phase 3: User Story 1 — Skip Evaluation When Weather Hasn't Changed (Priority: P1)

**Goal**: Skip re-evaluating a storm when forecasts haven't materially changed since the last evaluation, avoiding redundant Gemini API calls.

**Independent Test**: Run the pipeline twice in succession for a region with an active storm. If forecasts haven't changed between runs, the second run should skip the Gemini evaluation and log that it was skipped.

### Implementation

- [x] T008 Add `domain/weather_compare_test.go` with table-driven tests for `ForecastsChanged()`: identical forecasts return Changed=false; snowfall delta > 2" on any day returns Changed=true; total snowfall delta > 3" returns Changed=true; temperature delta > 4.4C returns Changed=true; fewer days in current returns Changed=true with DaysMismatched; more days in current compared only on overlapping range; empty previous (first eval) returns Changed=true
- [x] T009 Implement `ShouldEvaluate()` pure function in `pipeline/gating.go` — takes storm, currentTier, lastEvalTime, weatherChange (WeatherChangeSummary), budgetExceeded (bool), isFirstEval (bool) and returns EvalDecision. For US1: if isFirstEval return evaluate; if weatherChange.Changed return evaluate
- [x] T010 Wire gating into `pipeline.Evaluate()` in `pipeline/pipeline.go` — before calling `evaluator.Evaluate()`, load the last evaluation's WeatherSnapshot, call `ForecastsChanged()` with current forecasts, then call `ShouldEvaluate()`. If decision is skip, log with slog (storm_id, region_id, skip_reason, hours_since_last_eval) and increment RunSummary.SkippedUnchanged. If first evaluation (no prior eval exists), always proceed
- [x] T011 Add skip scenario tests to `pipeline/pipeline_test.go` — test that a storm with unchanged weather is skipped on second pipeline run (FakeEvaluator not called); test that a storm with changed weather proceeds; test that a new storm (no prior eval) always proceeds

**Checkpoint**: Weather-change detection working. Storms with stable forecasts skip re-evaluation.

---

## Phase 4: User Story 2 — Reduce Evaluation Frequency for Low-Tier Storms (Priority: P2)

**Goal**: Re-evaluate low-tier storms less frequently than high-tier storms using tier-based cooldown periods.

**Independent Test**: Seed a storm with ON_THE_RADAR evaluation from 13 hours ago. Run the pipeline. Verify skipped. Then seed DROP_EVERYTHING from 13 hours ago and verify it IS re-evaluated.

### Implementation

- [x] T012 Add tests for `CooldownFor()` in `domain/tier_test.go` (or extend existing) — verify DROP_EVERYTHING returns 0, WORTH_A_LOOK returns 12h, ON_THE_RADAR returns 24h
- [x] T013 Extend `ShouldEvaluate()` in `pipeline/gating.go` with cooldown logic — after weather-change check: if weather unchanged AND time since last eval < CooldownFor(currentTier), return skip with SkipCooldown. Weather change overrides cooldown (per spec clarification)
- [x] T014 Wire cooldown skip counting into `pipeline/pipeline.go` — increment RunSummary.SkippedCooldown when ShouldEvaluate returns SkipCooldown
- [x] T015 Add cooldown tests to `pipeline/pipeline_test.go` — ON_THE_RADAR evaluated 13h ago with unchanged weather is skipped (cooldown); DROP_EVERYTHING evaluated 13h ago with unchanged weather IS skipped (US1 weather-change gate still applies); DROP_EVERYTHING evaluated 13h ago with changed weather is NOT skipped (no cooldown delay); WORTH_A_LOOK evaluated 13h ago with changed weather is NOT skipped (weather overrides cooldown); ON_THE_RADAR evaluated 25h ago with unchanged weather is NOT skipped (cooldown elapsed)

**Checkpoint**: Tier-based throttling working. Low-priority storms evaluated less frequently.

---

## Phase 5: User Story 3 — Cost Tracking and Budget Guardrails (Priority: P3)

**Goal**: Track Gemini API call costs and halt non-essential evaluations when a monthly budget limit is reached.

**Independent Test**: Run pipeline with --budget 0.05. Simulate enough evaluations to reach limit. Verify warning at 80% and stops at 100%.

### Implementation

- [x] T016 [P] Implement `CostTracker` in `storage/costs.go` — methods: `RecordCost(ctx, stormID, regionID, estimatedCost, success)`, `MonthlySpend(ctx) (float64, int, error)` (returns total spend and call count for current UTC month). Use fixed per-call estimate of $0.015
- [x] T017 [P] Add `--budget` float64 flag to `cmd/powder-hunter/main.go` run command (default 0 = disabled). Pass budget value through to pipeline configuration
- [x] T018 Add `BudgetConfig` to pipeline — fields: MonthlyLimitUSD (float64), WarningThreshold (float64, default 0.8). Add `CostTracker` interface to pipeline dependencies with methods matching storage/costs.go
- [x] T019 Extend `ShouldEvaluate()` in `pipeline/gating.go` with budget check — if budgetExceeded AND not isFirstEval, return skip with SkipBudgetExceeded. First evaluations always proceed even at budget cap (FR-005)
- [x] T020 Wire budget checking into `pipeline/pipeline.go` Evaluate stage — before each storm: query MonthlySpend(), check against BudgetConfig.MonthlyLimitUSD, log warning at 80% threshold, pass budgetExceeded to ShouldEvaluate(). After successful evaluation: call RecordCost(). Increment RunSummary.SkippedBudget when applicable
- [x] T021 Add budget tests to `pipeline/pipeline_test.go` — test evaluation proceeds when no budget set; test evaluation skipped when budget exceeded (not first eval); test first evaluation proceeds even when budget exceeded; test 80% warning is logged

**Checkpoint**: Budget guardrails working. Monthly spend capped at configured limit.

---

## Phase 6: User Story 4 — Pipeline Run Observability (Priority: P3)

**Goal**: Show skip counts and reasons in the pipeline run summary so the operator can verify optimizations are working.

**Independent Test**: Run pipeline with mixed changed/unchanged weather and varied tiers. Check run summary includes skip counts.

### Implementation

- [x] T022 Update run summary log line in `pipeline/pipeline.go` Run() — extend the existing `slog.Info("pipeline complete", ...)` to include skipped_unchanged, skipped_cooldown, skipped_budget fields from RunSummary
- [x] T023 Verify skip log entries in `pipeline/pipeline_test.go` — test that run summary after a mixed run includes correct counts for evaluated, skipped_unchanged, skipped_cooldown, skipped_budget

**Checkpoint**: Full observability. Operator can see optimization impact in every run.

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Edge cases, validation, and end-to-end verification.

- [x] T024 [P] Add edge-case tests in `domain/weather_compare_test.go` — partial forecast data (fewer days than expected) treated as changed; empty forecast slices; single-day forecasts; forecasts with identical snowfall but different temperatures
- [x] T025 [P] Verify trace and replay exemption — add test or manual check that `runTrace` and `runReplay` in `cmd/powder-hunter/main.go` call evaluator.Evaluate() directly (not through pipeline gating) and are unaffected by budget/cooldown
- [x] T026 Run quickstart.md validation — follow all quickstart steps from scratch to verify `go test ./domain/... ./pipeline/... ./storage/...` passes, `powder-hunter run --dry-run --verbose` works twice (second shows skips), and `powder-hunter run --dry-run --budget 0.05 --verbose` shows budget behavior

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **Foundational (Phase 2)**: Can run in parallel with Phase 1 (different files)
- **US1 (Phase 3)**: Depends on Phase 2 completion (needs WeatherChangeSummary, EvalDecision, ForecastsChanged)
- **US2 (Phase 4)**: Depends on Phase 3 (extends ShouldEvaluate with cooldown, builds on weather-change wiring)
- **US3 (Phase 5)**: Depends on Phase 3 (extends ShouldEvaluate with budget, builds on gating wiring). Can run in parallel with US2 for storage/costs.go and CLI flag work (T016, T017)
- **US4 (Phase 6)**: Depends on Phases 3-5 (needs all skip counters wired)
- **Polish (Phase 7)**: Depends on all user stories complete

### User Story Dependencies

- **US1 (P1)**: Foundation only — MVP milestone
- **US2 (P2)**: Extends US1's ShouldEvaluate() with cooldown path
- **US3 (P3)**: Extends US1's pipeline wiring with budget path. Storage work (T016) is independent
- **US4 (P3)**: Read-only — consumes RunSummary fields added by US1-US3

### Parallel Opportunities

**Phase 2** (all [P] tasks work on different files):
```
T002 → T003 → T004 (domain/weather_compare.go — sequential, same file)
T005 (pipeline/gating.go)
T006 (domain/tier.go)
T007 (pipeline/pipeline.go)
```

**Phase 5** (partial parallelism):
```
T016 (storage/costs.go) + T017 (cmd/powder-hunter/main.go) — independent files
```

**Phase 7** (all [P] tasks):
```
T024 (domain/weather_compare_test.go) + T025 (cmd/powder-hunter/main.go check)
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Schema (T001)
2. Complete Phase 2: Domain types + pure functions (T002-T007)
3. Complete Phase 3: Weather-change detection (T008-T011)
4. **STOP and VALIDATE**: `go test ./domain/... ./pipeline/...` — weather-change skipping works
5. This alone delivers the highest-impact optimization (majority of cost savings)

### Incremental Delivery

1. Setup + Foundational → types ready
2. US1 (weather-change detection) → biggest cost savings → validate
3. US2 (tier cooldown) → reduces low-priority churn → validate
4. US3 (budget guardrail) → hard spending cap → validate
5. US4 (observability) → operator visibility → validate
6. Polish → edge cases and quickstart validation
