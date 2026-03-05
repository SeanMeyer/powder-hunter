# Implementation Plan: Evaluation Cost Optimization

**Branch**: `002-eval-cost-optimization` | **Date**: 2026-03-04 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/002-eval-cost-optimization/spec.md`
**Blocked by**: `003-forecast-improvements` must merge to main first

## Summary

Reduce Gemini API evaluation costs from potentially $20/month during active storm seasons to a target of ~$3/month by adding three layers of gating to the pipeline's Evaluate stage: (1) weather-change detection that skips re-evaluation when forecasts haven't materially changed, (2) tier-based cooldown periods that throttle low-priority storm re-evaluation, and (3) a monthly budget guardrail that halts non-essential evaluations at a configurable spend ceiling. All gating is implemented as pure domain logic in the existing I/O sandwich architecture, with cost tracking persisted to SQLite.

## Technical Context

**Language/Version**: Go 1.23+
**Primary Dependencies**: `google.golang.org/genai`, `modernc.org/sqlite`, `golang.org/x/sync/errgroup`
**Storage**: SQLite via `modernc.org/sqlite` (pure Go, no CGO)
**Testing**: `go test` with sociable tests using real SQLite + FakeEvaluator/FakePoster
**Target Platform**: Linux Docker container on Unraid, triggered by cron (2x daily)
**Project Type**: CLI tool with pipeline orchestration
**Performance Goals**: Pipeline run completes in < 5 minutes for 45 regions
**Constraints**: No CGO, single binary deployment, SQLite single-file database
**Scale/Scope**: 45 regions, ~120 resorts, 2 pipeline runs per day, budget target ~$3/month

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Parse, Don't Validate | PASS | Weather change comparison uses parsed `domain.Forecast` types, not raw JSON. New `WeatherChangeSummary` is a domain type with clear semantics. |
| II. I/O Sandwich | PASS | All gating logic (weather comparison, cooldown check, budget check) is pure domain functions. Cost tracking reads/writes happen in the shell (pipeline + storage). |
| III. Decisions Are Data | PASS | Skip decisions are recorded with structured reasons. Cost records persisted to SQLite. `RunSummary` extended with skip counts. |
| IV. Observability Is Not Optional | PASS | Every skip is logged with `slog` including storm ID, region ID, skip reason, and time since last eval. Budget warnings logged at 80% threshold. |
| V. Production Quality | PASS | Proper error handling, no shortcuts. Budget tracking uses conservative estimates (only successful calls count). |
| VI. Domain-Organized | PASS | New code lives in existing packages: `domain/` for comparison logic, `pipeline/` for gating orchestration, `storage/` for cost tracking persistence. No new packages needed. |
| VII. Test Discipline | PASS | Sociable pipeline tests with real SQLite + fakes. TDD for the domain-level weather comparison and cooldown logic. Edge-case unit tests for threshold boundary conditions. |

No violations. No complexity tracking needed.

## Project Structure

### Documentation (this feature)

```text
specs/002-eval-cost-optimization/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
└── tasks.md             # Phase 2 output (created by /speckit.tasks)
```

### Source Code (repository root)

```text
domain/
├── weather.go           # existing — Forecast, DailyForecast, HalfDay types
├── weather_compare.go   # NEW — ForecastsChanged(), WeatherChangeSummary
├── weather_compare_test.go # NEW — threshold edge cases
├── tier.go              # existing — Tier constants, CooldownFor() added
├── storm.go             # existing — Storm type (no changes needed)
├── evaluation.go        # existing — Evaluation type (no changes needed)
└── comparison.go        # existing — Compare() (no changes needed)

pipeline/
├── pipeline.go          # MODIFIED — Evaluate stage adds gating logic
├── pipeline_test.go     # MODIFIED — new tests for skip scenarios
└── gating.go            # NEW — EvalDecision, ShouldEvaluate() orchestration

storage/
├── schema.sql           # MODIFIED — add eval_costs table
├── sqlite.go            # MODIFIED — add migration for eval_costs
├── evaluations.go       # existing (no changes)
└── costs.go             # NEW — cost tracking CRUD

cmd/powder-hunter/
└── main.go              # MODIFIED — add --budget flag, wire cost tracker
```

**Structure Decision**: All new code fits within the existing domain-organized package layout. No new packages are created. The `domain/` package gets weather comparison logic (pure functions), `pipeline/` gets the gating orchestration, and `storage/` gets cost tracking persistence. This maintains the I/O sandwich — domain logic has zero I/O imports.

## Dependency: 003-forecast-improvements

**Implementation MUST wait for 003 to merge to main.** The 003 branch restructures the forecast data model extensively:

- `Forecast` gains `ResortID` and `Model` fields (per-resort, multi-model fetching)
- `DailyForecast` gains `SLRatio`, `RainHours`, `MixedHours` (SLR-adjusted snowfall)
- `HalfDay` gains `FreezingLevelMinM/MaxM` (elevation gradient)
- New types: `ModelConsensus`, `DayConsensus`, `ForecastDiscussion`
- `ScanResult` gains `ResortConsensus` and `Discussion` fields
- `Scan()` signature changes (regionFilter moves to `Run()`)

**Impact on our work:**

| Our file | Why it matters |
|----------|---------------|
| `domain/weather_compare.go` | Must compare the final `Forecast` shape, including knowing which fields to ignore (SLRatio, RainHours, etc.) |
| `pipeline/gating.go` | Inserts into `Evaluate()` which sits downstream of the restructured `Scan()` |
| `pipeline/pipeline.go` | Must extend `RunSummary` — needs to be done against 003's version of the file |
| `pipeline/pipeline_test.go` | Test helpers (`aboveThresholdForecast`, `ScanResult` construction) must match 003's expanded types |

**When 003 merges**: Rebase this branch onto main, then proceed with `/speckit.tasks` and implementation. The spec, plan, research, and data model are all valid — only the code needs to target the post-003 codebase.
