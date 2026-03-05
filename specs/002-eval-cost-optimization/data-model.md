# Data Model: Evaluation Cost Optimization

## New Entities

### WeatherChangeSummary (domain type, not persisted)

Pure value type returned by the forecast comparison function. Used by the pipeline to decide whether to evaluate.

| Field | Type | Description |
|-------|------|-------------|
| Changed | bool | True if any detection-critical field exceeds its threshold |
| TotalSnowfallDeltaIn | float64 | Absolute difference in total window snowfall (inches) |
| MaxDailySnowfallDeltaIn | float64 | Largest single-day snowfall difference (inches) |
| MaxTempDeltaC | float64 | Largest single-day temperature shift (Celsius) |
| DaysMismatched | int | Number of days present in one snapshot but not the other |
| Reason | string | Human-readable summary of the largest change (for logging) |

**Thresholds** (configurable constants):
- Daily snowfall: 2 inches (5.08 cm)
- Total snowfall: 3 inches (7.62 cm)
- Temperature: 4.4°C (8°F)
- Day count decrease: any → material change

### EvalCost (persisted to SQLite)

One row per successful Gemini API call. Used for monthly budget tracking.

| Field | Type | Description |
|-------|------|-------------|
| id | INTEGER (PK, auto) | Row identifier |
| storm_id | INTEGER (FK → storms) | Which storm was evaluated |
| region_id | TEXT | Region ID (denormalized for query convenience) |
| evaluated_at | TEXT (RFC3339) | When the call was made |
| estimated_cost_usd | REAL | Estimated cost of this call |
| success | INTEGER (0/1) | Whether the call succeeded |

**Index**: `idx_eval_costs_month` on `evaluated_at` for efficient monthly aggregation.

### EvalDecision (domain type, not persisted)

Result of the gating logic for a single storm. Drives skip/proceed decision and logging.

| Field | Type | Description |
|-------|------|-------------|
| ShouldEvaluate | bool | Whether to proceed with Gemini call |
| Reason | SkipReason | Why evaluation was skipped (or empty if proceeding) |
| TimeSinceLastEval | time.Duration | Time elapsed since last evaluation |
| WeatherChange | WeatherChangeSummary | The weather comparison result |

### SkipReason (domain enum)

| Value | Description |
|-------|-------------|
| "" (empty) | Not skipped — proceed with evaluation |
| "unchanged_weather" | Forecasts haven't materially changed |
| "cooldown" | Within tier-based cooldown period and weather unchanged |
| "budget_exceeded" | Monthly budget limit reached |

## Modified Entities

### RunSummary (pipeline type — extended)

Existing fields preserved. New fields added:

| Field | Type | Description |
|-------|------|-------------|
| SkippedUnchanged | int | Count of storms skipped due to unchanged weather |
| SkippedCooldown | int | Count of storms skipped due to tier cooldown |
| SkippedBudget | int | Count of storms skipped due to budget limit |

### Tier (domain type — new method)

Add `CooldownFor(tier Tier) time.Duration` function:

| Tier | Cooldown |
|------|----------|
| DROP_EVERYTHING | 0 (always re-evaluate) |
| WORTH_A_LOOK | 12 hours |
| ON_THE_RADAR | 24 hours |

## New SQLite Table

```sql
CREATE TABLE IF NOT EXISTS eval_costs (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    storm_id         INTEGER NOT NULL REFERENCES storms(id),
    region_id        TEXT NOT NULL,
    evaluated_at     TEXT NOT NULL,
    estimated_cost_usd REAL NOT NULL,
    success          INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_eval_costs_month
    ON eval_costs(evaluated_at);
```

## Schema Migration

Add to `runMigrations()` in `storage/sqlite.go`:
```sql
CREATE TABLE IF NOT EXISTS eval_costs (...)
```

Using `CREATE TABLE IF NOT EXISTS` makes the migration idempotent (same pattern as existing schema).

## Relationships

```
storms 1──N eval_costs    (one storm has many cost records)
storms 1──N evaluations   (existing, unchanged)
```

The `eval_costs` table is independent of `evaluations` — it tracks API call costs regardless of whether the evaluation was ultimately persisted (e.g., if a call succeeds but post-processing fails). However, in practice, a successful Gemini call always results in a persisted evaluation.
