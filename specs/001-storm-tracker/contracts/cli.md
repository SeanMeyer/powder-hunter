# CLI Contract: powder-hunter

**Feature Branch**: `001-storm-tracker`
**Date**: 2026-03-04

## Commands

### `powder-hunter run`

Execute the full pipeline: scan → evaluate → compare → post.
This is the primary command invoked by cron.

```
powder-hunter run [flags]

Flags:
  --db PATH        SQLite database path (default: ./powder-hunter.db)
  --dry-run        Run pipeline but skip Discord posting
  --region ID      Evaluate only this region (for debugging)
  --verbose        Enable debug-level logging

Environment:
  GOOGLE_API_KEY       Gemini API key (required)
  DISCORD_WEBHOOK_URL  Discord forum channel webhook (required)
```

**Exit codes**:
- 0: Pipeline completed successfully (some regions may have
  had non-fatal errors)
- 1: Fatal error (DB inaccessible, missing env vars)

**stdout**: Structured log output (JSON lines via slog)
**stderr**: Fatal errors only

### `powder-hunter replay`

Re-run a past storm evaluation through a different prompt version.
Does not post to Discord.

```
powder-hunter replay [flags]

Flags:
  --db PATH             SQLite database path (default: ./powder-hunter.db)
  --storm-id ID         Storm ID to replay (required)
  --evaluation-id ID    Specific evaluation to replay (optional,
                        defaults to latest)
  --prompt-version VER  Prompt version to use (required)
  --output FORMAT       Output format: json | text (default: text)

Environment:
  GOOGLE_API_KEY       Gemini API key (required)
```

**Behavior**: Loads the WeatherSnapshot and context from the
specified evaluation, runs it through the specified prompt
version, and outputs the result. The original evaluation is
unchanged. The replay result is NOT persisted (ephemeral).

**Exit codes**:
- 0: Replay completed
- 1: Storm/evaluation not found, or API error

### `powder-hunter seed`

Initialize or update the region/resort database from embedded
seed data.

```
powder-hunter seed [flags]

Flags:
  --db PATH        SQLite database path (default: ./powder-hunter.db)
  --force          Overwrite existing region/resort data
```

**Behavior**: Creates the database schema if it doesn't exist,
then inserts/updates all predefined regions and resorts. Without
`--force`, only inserts missing records. With `--force`,
overwrites all region/resort data (preserving storms and
evaluations).

### `powder-hunter profile`

View or update the user profile.

```
powder-hunter profile [flags]

Flags:
  --db PATH        SQLite database path (default: ./powder-hunter.db)
  --home CITY      Set home base (e.g., "Denver, CO")
  --passes LIST    Comma-separated pass list (e.g., "ikon,epic")
  --remote BOOL    Set remote work capability
  --show           Display current profile (default if no flags)
```

## Interface Boundaries

### Weather Client Interface

```
Fetch(region) → (Forecast, error)
```

Accepts a Region, returns a parsed Forecast domain type. Two
implementations: OpenMeteo and NWS. The pipeline calls both
for US regions, only OpenMeteo for Canadian regions.

### Evaluator Interface

```
Evaluate(forecast, region, profile, history) → (Evaluation, error)
```

Accepts parsed domain types, returns a structured Evaluation.
Single implementation: Gemini 3 Flash (grounded + structured).
Fakes substitute for testing.

### Poster Interface

```
PostNew(evaluation) → (threadID, error)
PostUpdate(evaluation, threadID) → error
```

Posts to Discord. Returns thread ID for new storms. Fakes
substitute for testing.

### Store Interface

```
SaveStorm(storm) → error
SaveEvaluation(evaluation) → error
GetActiveStorms(regionID) → ([]Storm, error)
GetLatestEvaluation(stormID) → (Evaluation, error)
// ... additional CRUD as needed
```

SQLite implementation. Fakes substitute for testing.
