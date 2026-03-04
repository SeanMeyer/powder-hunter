# Implementation Plan: Automated Storm Tracker

**Branch**: `001-storm-tracker` | **Date**: 2026-03-04 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/001-storm-tracker/spec.md`

## Summary

Build an automated ski storm tracker that scans weather forecasts
across ~40-50 US and Canadian ski regions, evaluates promising
storms via Gemini LLM with Google Search grounding for live
logistics research, and pushes rich briefings to a Discord forum
channel with threaded updates per storm. Storms are tracked over
time as forecasts evolve, with tier-appropriate notifications
(DROP EVERYTHING / WORTH A LOOK / ON THE RADAR).

Architecture: I/O Sandwich — pure domain types and business logic
with no side effects; impure shell for weather APIs, Gemini,
SQLite, and Discord. Domain-organized packages. All decisions
persisted as data for full traceability.

## Technical Context

**Language/Version**: Go 1.23+
**Primary Dependencies**:
- `google.golang.org/genai` — Gemini SDK (grounding + structured output)
- `modernc.org/sqlite` — pure-Go SQLite driver
- `log/slog` — structured logging (stdlib)
- `net/http` — weather APIs + Discord webhooks (stdlib)
- `encoding/json` — JSON parsing (stdlib)
- `golang.org/x/sync/errgroup` — concurrent weather fetches

**Storage**: SQLite (single file, WAL mode, auto-schema on first run)
**Testing**: `go test` with sociable tests at the pipeline boundary using fakes
**Target Platform**: Linux/amd64 Docker container on Unraid (also macOS for dev)
**Project Type**: CLI tool (run-once per cron invocation)
**Performance Goals**: Full pipeline completes in <10 minutes for ~50 regions
**Constraints**: No CGO, single binary, <100MB memory, runs 2x daily via cron
**Scale/Scope**: ~40-50 regions, single user, ~5-10 LLM evaluations per run typical

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
| --- | --- | --- |
| I. Parse, Don't Validate | PASS | Raw API JSON parsed into domain types at boundary (weather/, gemini/ packages). Domain functions accept parsed types only. |
| II. I/O Sandwich | PASS | `domain/` package has zero I/O imports. All HTTP, DB, and LLM calls in shell packages. Concurrency via errgroup in shell. |
| III. Decisions Are Data | PASS | Every evaluation persists full inputs, outputs, raw LLM response, and prompt version to SQLite. |
| IV. Observability | PASS | All I/O operations use slog with structured fields. DB is primary audit log. |
| V. Production Quality | PASS | Clean package boundaries, proper error decoration, no ignored errors. |
| VI. Domain-Organized | PASS | Packages by domain: weather/, evaluation/, discord/, storage/, domain/. No utils/. |
| VII. Test Discipline | PASS | Sociable tests at pipeline boundary with fakes. TDD for domain logic recommended. Bug-fix TDD non-negotiable. |

No violations. No complexity tracking entries needed.

## Project Structure

### Documentation (this feature)

```text
specs/001-storm-tracker/
├── plan.md              # This file
├── research.md          # Phase 0: API research findings
├── data-model.md        # Phase 1: entity definitions
├── quickstart.md        # Phase 1: dev setup guide
├── contracts/           # Phase 1: CLI interface contract
│   └── cli.md
└── tasks.md             # Phase 2: task list (via /speckit.tasks)
```

### Source Code (repository root)

```text
powder-hunter/
├── cmd/
│   └── powder-hunter/
│       └── main.go              # Entry point, run() pattern
├── domain/                      # Pure core — no I/O, no context.Context
│   ├── resort.go                # Resort, Region, FrictionTier types
│   ├── storm.go                 # Storm, StormState, lifecycle logic
│   ├── evaluation.go            # Evaluation result types
│   ├── weather.go               # Forecast, SnowfallWindow types
│   ├── tier.go                  # Tier type, notification rules
│   ├── profile.go               # UserProfile, AlertPreferences
│   ├── detection.go             # Pure storm detection (threshold filtering)
│   └── comparison.go            # Pure change classification logic
├── weather/                     # I/O shell — weather API clients
│   ├── openmeteo.go             # Open-Meteo 16-day forecast client
│   ├── nws.go                   # NWS api.weather.gov client
│   └── weather.go               # WeatherService interface + aggregation
├── evaluation/                  # I/O shell — LLM evaluation
│   ├── gemini.go                # Gemini 3 Flash client (grounded + structured)
│   ├── prompt.go                # Versioned prompt templates
│   └── evaluation.go            # Evaluator interface
├── discord/                     # I/O shell — Discord webhook posting
│   ├── webhook.go               # Webhook client (forum threads, embeds)
│   └── formatter.go             # Evaluation → embed formatting (pure)
├── storage/                     # I/O shell — SQLite persistence
│   ├── sqlite.go                # DB connection, schema migration
│   ├── storms.go                # Storm CRUD operations
│   ├── evaluations.go           # Evaluation persistence
│   ├── regions.go               # Region/resort seed data
│   └── profiles.go              # User profile storage
├── pipeline/                    # Orchestration — wires shell components
│   └── pipeline.go              # Scan → evaluate → compare → post
├── seed/                        # Static data
│   └── regions.go               # ~40-50 predefined regions with resorts
├── Dockerfile
├── go.mod
└── go.sum
```

**Structure Decision**: Domain-organized single project following
I/O Sandwich. `domain/` is the pure functional core with zero
external imports. Shell packages (`weather/`, `evaluation/`,
`discord/`, `storage/`) handle all I/O. `pipeline/` orchestrates
the stages. `discord/formatter.go` is pure (data → embed struct)
despite living in the discord package.

## Key Architecture Decisions

### Gemini 3 Flash — Single-Call Grounded + Structured

Using `gemini-3-flash-preview` which supports Google Search
grounding and structured JSON output in a single API call. This
is the simplest approach — one call per evaluation, structured
response with grounding metadata (source URLs, citations).

The model is in preview (no GA date announced). If Google
deprecates or breaks the preview, we fix it then — either by
migrating to GA when available or falling back to a two-phase
approach on 2.5 Flash (grounded text call → structured extraction
call). The Evaluator interface abstracts this so the pipeline
doesn't care which implementation runs.

Pricing: $0.50/1M input, $3/1M output. At ~50 evaluations/day
this is negligible.

### Discord Forum Channel

Using a Discord Forum Channel (not a regular text channel) enables
pure webhook-only operation with no bot token:
- New storm: `POST ?wait=true` with `thread_name` creates thread
- Updates: `POST ?thread_id={id}` into existing thread
- `@here`: in `content` field with `allowed_mentions`

### NWS for Near-Range Only (Days 1-3)

NWS `snowfallAmount` is machine-readable but only available 2-3
days out. Rather than parsing natural language text for days 4-7
(brittle, added complexity), we use NWS only for the near-range
window where it provides machine-readable data. Open-Meteo covers
the full 1-16 day range for all regions already, so the days 4-7
gap from NWS is not a loss.

### Prompt Versioning

Prompt templates stored in the database as versioned rows. Each
evaluation records which prompt version was used. New prompt
versions can be added without redeploying the binary. CLI replay
command re-runs historical data through a different prompt version
for comparison.
