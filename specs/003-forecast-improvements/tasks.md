# Tasks: Forecast Accuracy Improvements

**Input**: Design documents from `/specs/003-forecast-improvements/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, quickstart.md

**Tests**: Included per plan.md test discipline — SLR: table-driven unit tests, consensus: unit tests, multi-model parsing: unit tests, AFD: sociable test with fake HTTP.

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Phase 1: Setup

**Purpose**: No new project initialization needed — existing codebase. This phase adds shared domain types and pure functions that multiple user stories depend on.

- [x] T001 Add `Model` field to `domain.Forecast` struct in `domain/weather.go`
- [x] T002 Add `SLRatio`, `RainHours`, `MixedHours` fields to `domain.DailyForecast` in `domain/weather.go`
- [x] T003 Add `FreezingLevelMinM`, `FreezingLevelMaxM` fields to `domain.HalfDay` in `domain/weather.go`
- [x] T004 [P] Add `ModelConsensus`, `DayConsensus` types in `domain/weather.go`
- [x] T005 [P] Add `ForecastDiscussion` type in `domain/weather.go`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Pure domain functions that US2 and US3 both depend on. Must complete before user story implementation.

- [x] T000 Implement `CalculateSLR(tempC float64) float64` pure function in `domain/weather.go` — five contiguous temperature bands: >1.67°C=0, [0, 1.67°C]=5:1, [-3.89, 0°C)=10:1, [-9.44, -3.89°C)=15:1, <-9.44°C=20:1 (exact conversions from Fahrenheit bands in spec)
- [x] T000 Implement `SnowfallFromPrecip(precipMM float64, tempC float64) float64` pure function in `domain/weather.go` — returns snowfall in cm using SLR
- [x] T000 [P] Write table-driven unit tests for `CalculateSLR` and `SnowfallFromPrecip` in `domain/weather_test.go` — cover all five bands, boundary temperatures, zero precip, edge cases
- [x] T000 Implement `ComputeConsensus(forecasts []Forecast) ModelConsensus` pure function in `domain/weather.go` — align by date, compute spread/mean/confidence per day, handle zero-mean edge case
- [x] T010 [P] Write unit tests for `ComputeConsensus` in `domain/weather_test.go` — cover agreement, divergence, single model, zero snowfall (all agree no snow = high confidence)

**Checkpoint**: Foundation ready — `go test ./domain/...` passes. User story implementation can begin.

---

## Phase 3: User Story 1 — Confidence-Calibrated Recommendations (Priority: P1)

**Goal**: Recommendations account for forecast uncertainty at different lead times so users make appropriately cautious or committed travel decisions.

**Independent Test**: Generate evaluations at different lead times and verify the LLM prompt includes confidence guidance matching lead time bands.

### Implementation for User Story 1

- [x] T011 [US1] Add lead-time confidence guidance section to the prompt template in `seed/prompts.go` — four bands per research R6: 1-2 days (high confidence, decisive), 3-5 days (moderate, refundable bookings), 6-7 days (lower, worth watching), 8-16 days (pattern-level only)
- [x] T012 [US1] Update `evaluation.PromptData` struct and `RenderPrompt` in `evaluation/prompt.go` to include `LeadTimeDays` context so the template can reference the storm's current lead time
- [x] T013 [US1] Update `FormatDetectionForPrompt` in `evaluation/prompt.go` to compute and include lead time (days from now to window start) in the detection context string
- [x] T014 [US1] Add `<!-- US1: confidence calibration -->` comment marker in prompt template in `seed/prompts.go` to delineate the new section (version bump deferred to T044 for single v2.0.0)

**Checkpoint**: Prompt now includes confidence calibration context. Verifiable via `--trace` dry-run showing lead time and guidance in rendered prompt.

---

## Phase 4: User Story 2 — Accurate Mountain Snowfall Estimates (Priority: P1)

**Goal**: Snowfall predictions account for temperature-based snow density using per-hour SLR calculation, replacing raw API snowfall.

**Independent Test**: Compare system snowfall estimates against known SLR-adjusted values for cold (<20F) and warm (28-32F) storms using unit tests on the parsing layer.

### Implementation for User Story 2

- [x] T015 [US2] Modify `parseOpenMeteoHourly` in `weather/openmeteo.go` to compute SLR-adjusted snowfall from hourly precipitation + temperature instead of using raw `snowfall` field — aggregate per-hour snow into half-day and daily totals, populate `SLRatio`, `RainHours`, `MixedHours` on each `DailyForecast`
- [x] T016 [US2] Add `freezing_level_height` to `openMeteoHourlyVars` query string and response struct in `weather/openmeteo.go` — parse into `FreezingLevelMinM`/`FreezingLevelMaxM` on `HalfDay` and `FreezingLevelM` on `DailyForecast`
- [x] T017 [P] [US2] Modify `parseGridpointForecast` in `weather/nws.go` to compute SLR-adjusted snowfall from NWS hourly precipitation + temperature — same SLR bands, populate `SLRatio`, `RainHours`, `MixedHours`
- [x] T018 [P] [US2] Write unit tests for SLR-adjusted parsing in `weather/openmeteo_test.go` — test cold smoke (12F/0.5" liquid = ~10"), wet snow (28F/1" liquid = ~10"), mixed rain-to-snow transition, all-rain hours
- [x] T019 [US2] Update `FormatWeatherForPrompt` in `evaluation/prompt.go` to display SLR ratio, rain/mixed hours, and freezing level alongside existing weather table columns
- [x] T020 [US2] Validate storm detection thresholds against SLR-adjusted `SnowfallCM` values in `domain/detection.go` — run existing detection tests with SLR-adjusted sample data, verify thresholds still trigger appropriately (cold smoke ~2x increase, rain ~0x), adjust `NearThresholdIn`/`ExtendedThresholdIn` in `seed/regions.go` if SLR-adjusted values shift detection behavior significantly

**Checkpoint**: `go test ./weather/... ./domain/...` passes. SLR-adjusted snowfall flows through parsing, detection, and prompt formatting. Verifiable via `--trace` dry-run.

---

## Phase 5: User Story 3 — Multi-Model Consensus Detection (Priority: P2)

**Goal**: System queries multiple weather models and presents model agreement/divergence context to the LLM for confidence assessment.

**Independent Test**: Query multiple models for the same location and verify the system identifies agreement vs. divergence with correct spread-to-mean calculations.

### Implementation for User Story 3

- [x] T021 [US3] Modify `openMeteoHourlyVars` and response structs in `weather/openmeteo.go` to support `&models=gfs_seamless,ecmwf_ifs025` parameter — parse `{variable}_{model}` keyed response arrays per research R1
- [x] T022 [US3] Modify `OpenMeteoClient.Fetch` in `weather/openmeteo.go` to return multiple `domain.Forecast` values (one per model) from a single API call, each with `Model` field set
- [x] T023 [US3] Update `weather.Service.FetchAll` in `weather/weather.go` to handle multiple forecasts from Open-Meteo (was 1, now N per model) — return all model forecasts plus NWS
- [x] T024 [P] [US3] Write unit tests for multi-model response parsing in `weather/openmeteo_test.go` — test two-model response, model with missing/empty arrays, single model fallback, partial API response (one model's arrays truncated or absent)
- [x] T024b [US3] Handle partial model failure in multi-model parsing in `weather/openmeteo.go` — if one model returns empty/missing data, skip it, proceed with remaining models, log the missing model via slog (FR-005 graceful degradation)
- [x] T025 [US3] Wire `ComputeConsensus` into `pipeline.Scan` in `pipeline/pipeline.go` — compute consensus from Open-Meteo model forecasts, attach to `ScanResult`
- [x] T026 [US3] Add `ModelConsensus` field to `pipeline.ScanResult` struct in `pipeline/pipeline.go`
- [x] T027 [US3] Add consensus summary formatting function in `evaluation/prompt.go` — format per-day spread, confidence level, model agreement for LLM context
- [x] T028 [US3] Add `{{.ModelConsensus}}` placeholder to prompt template in `seed/prompts.go` and wire through `PromptData` and `RenderPrompt`
- [x] T029 [US3] Introduce `EvalContext` struct in `evaluation/evaluation.go` bundling all evaluator inputs (forecasts, region, resorts, profile, history, consensus, discussion) and update `Evaluator` interface to accept `EvalContext` instead of individual parameters — update `GeminiEvaluator` and `FakeEvaluator` implementations in `evaluation/gemini.go` and `evaluation/fake.go`
- [x] T029b [US3] Update `pipeline.Evaluate` in `pipeline/pipeline.go` to build `EvalContext` with consensus from `ScanResult` and pass to evaluator
- [x] T030 [US3] Update `weather/fake.go` `FakeFetcher` to support multi-model responses (return forecasts with different `Model` values)

**Checkpoint**: `go test ./...` passes. Multiple models queried, consensus computed, and model agreement/divergence context appears in the LLM prompt. Verifiable via `--trace` dry-run.

---

## Phase 6: User Story 4 — Expert Meteorological Context (Priority: P2)

**Goal**: System incorporates NWS Area Forecast Discussion text so the LLM has access to expert meteorological nuance.

**Independent Test**: Fetch AFD for a known WFO and verify the content reaches the LLM prompt context.

### Implementation for User Story 4

- [x] T031 [US4] Implement `FetchAFD(ctx, wfo string) (domain.ForecastDiscussion, error)` method on `NWSClient` in `weather/nws.go` — two-step: list AFDs via `/products/types/AFD/locations/{wfo}`, then fetch latest text via `/products/{id}` per research R3
- [x] T032 [US4] Change `weather.Service.FetchAll` return type to `FetchResult` struct in `weather/weather.go` — struct contains `Forecasts []domain.Forecast` and `Discussion *domain.ForecastDiscussion` (nil for non-US or failure). Fetch AFD for US regions using the WFO from grid resolution. Update all callers (`pipeline.Scan`).
- [x] T033 [US4] Add `ForecastDiscussion` field to `pipeline.ScanResult` in `pipeline/pipeline.go` — populate from `FetchResult.Discussion` returned by `FetchAll`, wire through to `EvalContext.Discussion` in `pipeline.Evaluate`
- [x] T034 [US4] Add AFD formatting function in `evaluation/prompt.go` — format discussion text with WFO, issue time, and relevance note for LLM context
- [x] T035 [US4] Add `{{.ForecastDiscussion}}` placeholder to prompt template in `seed/prompts.go` and wire through `PromptData` and `RenderPrompt`
- [x] T036 [US4] Implement graceful degradation — when AFD fetch fails or region is non-US, set empty discussion and note absence in prompt context per FR-005
- [x] T037 [P] [US4] Write sociable test for AFD fetching in `weather/nws_test.go` — use `httptest.Server` fake to simulate NWS AFD list and product endpoints

**Checkpoint**: `go test ./...` passes. AFD text included in US region evaluation prompts, gracefully absent for non-US. Verifiable via `--trace` dry-run.

---

## Phase 7: User Story 5 — High-Resolution Near-Range Forecasts (Priority: P3)

**Goal**: System uses high-resolution weather models (3km grid) for storms within 3 days for terrain-aware predictions.

**Independent Test**: Verify model selection logic chooses HRRR for 1-3 day range and global models for 4+ days.

### Implementation for User Story 5

- [x] T038 [US5] Add `gfs_hrrr` to the models list in `weather/openmeteo.go` — conditionally include for US regions only per research R2
- [x] T039 [US5] Update multi-model parsing in `weather/openmeteo.go` to handle HRRR's shorter forecast horizon (18-48h) — HRRR arrays will be shorter or have nulls beyond its range
- [x] T040 [US5] Update `ComputeConsensus` in `domain/weather.go` to include HRRR in per-day consensus only for days where it has data (days 1-3), exclude it for days 4+ — consensus is computed per-day so storms straddling the 3/4 day boundary naturally transition: day 3 uses 3 models (GFS+ECMWF+HRRR), day 4 uses 2 models (GFS+ECMWF). Pass `today` as parameter so function can compute day offsets.
- [x] T041 [US5] Implement fallback in `weather/openmeteo.go` when HRRR data is unavailable — use global models only and log reduced terrain accuracy per FR-005
- [x] T042 [P] [US5] Write unit tests for HRRR model selection and fallback in `weather/openmeteo_test.go` — US region includes HRRR, non-US excludes it, missing HRRR degrades gracefully

**Checkpoint**: `go test ./...` passes. Near-range forecasts use high-resolution model when available. Verifiable via `--trace` dry-run comparing model data for day 2 vs day 7.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories.

- [x] T043 [P] Add `FR-007` rain-line risk flagging in `evaluation/prompt.go` — when freezing level (meters) is below resort summit but above resort base during precipitation, surface rain-line risk in prompt context. Convert resort `BaseElevationFt`/`SummitElevationFt` to meters (×0.3048) for comparison against `FreezingLevelMinM`/`FreezingLevelMaxM`.
- [x] T044 Update `seed/prompts.go` prompt version to `v2.0.0` reflecting all new prompt sections (confidence, consensus, AFD, rain-line)
- [x] T045 [P] Update `weather/fake.go` to support all new fields (`Model`, `SLRatio`, `RainHours`, `MixedHours`, `FreezingLevelMinM`, `FreezingLevelMaxM`) for downstream test compatibility
- [x] T045b [P] Write sociable pipeline test in `pipeline/pipeline_test.go` — use fake weather service (multi-model forecasts + AFD) and fake evaluator to validate full flow: multi-model fetch → SLR-adjusted snowfall → consensus computation → prompt includes confidence guidance, consensus summary, and AFD text
- [x] T046 Run `go test ./...` full suite and fix any integration issues across stories
- [x] T047 Run quickstart.md validation — execute `go run ./cmd/powder-hunter evaluate --region wasatch-cottonwoods --dry-run --trace` and verify SLR-adjusted values, model consensus, and AFD content appear in output

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 type definitions — BLOCKS all user stories
- **US1 (Phase 3)**: Depends on Phase 2 completion. No dependency on other stories.
- **US2 (Phase 4)**: Depends on Phase 2 (uses `CalculateSLR`, `SnowfallFromPrecip`). No dependency on other stories.
- **US3 (Phase 5)**: Depends on Phase 2 (`ComputeConsensus`, `ModelConsensus` types) AND Phase 4 (multi-model parsing uses SLR-adjusted snowfall from US2)
- **US4 (Phase 6)**: Depends on Phase 2 (`ForecastDiscussion` type). Independent of US2 and US3.
- **US5 (Phase 7)**: Depends on Phase 5 (extends multi-model infrastructure from US3)
- **Polish (Phase 8)**: Depends on all desired user stories being complete

### User Story Dependencies

- **US1 (P1)**: Can start after Phase 2 — fully independent
- **US2 (P1)**: Can start after Phase 2 — fully independent
- **US3 (P2)**: Must start after US2 (SLR-adjusted snowfall feeds consensus calculation). Introduces `EvalContext` (T029) which US4 also uses.
- **US4 (P2)**: Depends on US3 for `EvalContext` and `FetchResult` pattern. Can start US4-specific work (T031 AFD fetch, T037 tests) in parallel with US3, but T032-T033 wiring depends on T029 completing.
- **US5 (P3)**: Must start after US3 (extends multi-model query infrastructure)

### Within Each User Story

- Tests can be written alongside or after implementation (not strict TDD)
- Domain changes before I/O shell changes
- Weather fetching before pipeline wiring
- Pipeline wiring before prompt formatting
- Commit after each task or logical group

### Parallel Opportunities

- **Phase 1**: T004 and T005 can run in parallel (independent new types)
- **Phase 2**: T008 and T010 can run in parallel with each other (independent test files)
- **After Phase 2**: US1 and US2 can run in parallel (different files, no dependencies)
- **After Phase 2**: US4 can run in parallel with US1, US2, or US3
- **Within US2**: T017 (NWS SLR) and T018 (Open-Meteo SLR tests) can run in parallel
- **Within US3**: T024 (multi-model tests) can run in parallel with T021-T023

---

## Parallel Example: After Foundational Phase

```
# These can all run in parallel after Phase 2 completes:
Agent 1: US1 — T011, T012, T013, T014 (prompt-only changes in seed/prompts.go + evaluation/prompt.go)
Agent 2: US2 — T015, T016, T017, T018 (weather parsing changes in weather/*.go)
Agent 3: US4 — T031, T037 (NWS AFD fetching in weather/nws.go)
```

---

## Implementation Strategy

### MVP First (US1 + US2 Only)

1. Complete Phase 1: Setup (type definitions)
2. Complete Phase 2: Foundational (SLR + consensus pure functions)
3. Complete Phase 3: US1 — Confidence calibration (prompt changes only)
4. Complete Phase 4: US2 — SLR-adjusted snowfall
5. **STOP AND VALIDATE**: `go test ./...` + `--trace` dry-run
6. This delivers the two highest-impact improvements with minimal scope

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. US1 + US2 → Test independently → MVP with calibrated confidence + accurate snowfall
3. US3 → Test independently → Multi-model consensus adds confidence signals
4. US4 → Test independently → Expert meteorological context enriches evaluations
5. US5 → Test independently → Terrain-aware near-range forecasts
6. Polish → Full integration validation

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- SLR functions are pure domain math — no I/O, no context.Context
- Multi-model parsing reuses the existing `parseOpenMeteoHourly` pattern but with dynamic model-keyed field names
- AFD fetch follows the existing NWS two-step pattern (resolve → fetch)
- Prompt version should increment to v2.0.0 at the end to capture all changes
- The `weather_snapshot` JSON blob auto-captures the new `Model` field — no schema migration needed
