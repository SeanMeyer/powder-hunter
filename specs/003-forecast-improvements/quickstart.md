# Quickstart: Forecast Accuracy Improvements

**Branch**: `003-forecast-improvements`

## Prerequisites

- Go 1.23+
- Environment variables: `GEMINI_API_KEY`, `DISCORD_WEBHOOK_URL` (optional for dry-run)

## Setup

```bash
cd .worktrees/003-forecast-improvements  # or main repo on this branch
go mod download
```

## Run Tests

```bash
go test ./...
```

## Key Files to Modify (in order)

### 1. SLR Calculation (domain, pure)
- `domain/weather.go` — Add `CalculateSLR()`, `SnowfallFromPrecip()` functions and new type fields
- Test: Table-driven unit tests covering all five temperature bands + edge cases

### 2. Weather Fetching (I/O shell)
- `weather/openmeteo.go` — Change API params to query multiple models + freezing level; parse per-model response keys; use SLR instead of raw snowfall
- `weather/nws.go` — Add AFD fetching; use SLR for NWS snowfall too
- `weather/weather.go` — Update `Service.FetchAll()` to return per-model forecasts and AFD text

### 3. Consensus Calculation (domain, pure)
- `domain/weather.go` — Add `ComputeConsensus()` function
- Test: Unit tests for spread-to-mean calculation, edge cases (zero mean, single model)

### 4. Prompt Updates (evaluation)
- `evaluation/prompt.go` — Add confidence guidance, model consensus summary, AFD text to prompt formatting
- `seed/prompts.go` — Update default prompt template with new sections

### 5. Pipeline Integration
- `pipeline/pipeline.go` — Wire consensus and AFD through scan → evaluate stages

## Testing the SLR Calculation

```bash
# Run just domain tests
go test ./domain/... -v -run TestSLR
```

## Testing Multi-Model Parsing

```bash
# Run weather package tests
go test ./weather/... -v -run TestMultiModel
```

## Manual Smoke Test

```bash
# Dry-run pipeline for a single region
go run ./cmd/powder-hunter evaluate --region wasatch-cottonwoods --dry-run --trace
```

The `--trace` flag outputs the full rendered prompt and weather data to verify SLR-adjusted values, model consensus, and AFD content appear correctly.
