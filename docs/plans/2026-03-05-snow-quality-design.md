# Snow Quality Assessment System — Design

## Goal

Assess the likely riding experience of forecasted snow beyond just quantity. Produce pre-interpreted, human-readable quality signals that get fed to the LLM evaluation alongside weather data, enabling better recommendations about when and where to ride.

## Motivation

Snowfall inches alone don't tell you what it will feel like. Key factors that determine ride quality:

- **Crystal quality**: Wind breaks snowflake dendrites during transport. Even in trees, snow that fell through 30mph wind arrives partially broken. But trees do protect from surface transport, so tree skiing in windy storms is genuinely better.
- **Density character**: Cold smoke (very light) isn't automatically great — without a supportive base you punch through 2 feet of it. Warm heavy snow isn't automatically bad — it's tiring but rideable and self-supporting.
- **Layering**: A storm that starts dense and ends light builds a supportive base topped with fluff (ideal). The reverse — light snow buried under heavy — feels punchy and inconsistent.
- **Base conditions**: A melt-freeze crust or sun crust underneath new snow means you need enough depth (which varies by density) to avoid hitting it.
- **Bluebird powder**: Clear skies + fresh snow from a recent storm = the best possible day.

## Architecture

### Approach: Pre-Interpreted Assessments (Option B)

Rather than passing raw signals to the LLM and hoping it understands snow physics, we encode domain knowledge as deterministic logic in `domain/`. The LLM receives pre-interpreted English statements it can weave into recommendations.

**Why:** The LLM doesn't have riding experience. It doesn't know that 8" of cold smoke on a crust means punching through, while 8" of heavy snow on a crust rides fine. We bake that knowledge into our domain logic where it's testable and consistent.

### Data Flow

```
Hourly weather data (temp, wind, precip, cloud cover)
    ↓
Vionnet density model (per-hour density + SLR)
    ↓
Per-day aggregation (avg density, wind during snow, cloud cover)
    ↓
Snow quality assessment (domain pure function)
    ↓
Ride quality notes ([]string per day — human-readable)
    ↓
LLM prompt (notes appear alongside weather table)
    ↓
Trace output (notes shown in weather-only mode too)
```

## Data Model

### New: Cloud Cover

Added to both weather sources:

- **Open-Meteo**: Add `cloud_cover` to `openMeteoHourlyVars`. Percentage 0-100. Parsed into `openMeteoHourlyData.CloudCover []float64`.
- **NWS**: Add `SkyCover` to `nwsGridpointResponse`. Same time-series format as other fields.
- **Domain**: Add `CloudCoverPct float64` to `HalfDay` (daytime average).

### New: SnowQuality struct

Attached per-day to `DailyForecast` (or computed alongside it):

```go
type SnowQuality struct {
    // Per-day raw signals (used internally for assessment logic)
    CrystalQuality     string   // "intact" / "partially_broken" / "wind_broken"
    WindDuringSnowMph  float64  // avg wind speed during hours with precip
    DensityCategory    string   // "cold_smoke" / "dry_powder" / "standard" / "heavy" / "wet_cement"
    AvgDensityKgM3     float64  // average Vionnet density during precip hours
    Bluebird           bool     // clear skies + recent fresh snow

    // Per-storm context (computed from surrounding days)
    BaseRisk           string   // "low" / "moderate" / "high"
    BaseRiskReason     string   // human-readable explanation

    // Pre-interpreted assessments — what gets fed to the LLM
    RideQualityNotes   []string // human-readable statements about expected feel
}
```

### Density Category Thresholds

| Category | Density (kg/m3) | SLR | Description |
|---|---|---|---|
| cold_smoke | < 60 | > 16:1 | Extremely light; needs solid base support |
| dry_powder | 60-90 | 11-16:1 | Classic Colorado powder |
| standard | 90-130 | 8-11:1 | Decent but not remarkable |
| heavy | 130-180 | 6-8:1 | Noticeable weight, tiring in deep snow |
| wet_cement | > 180 | < 6:1 | Spring slop, sierra cement |

### Crystal Quality Thresholds

| Avg wind during snowfall | Category | Notes |
|---|---|---|
| < 15 mph | intact | Dendrites preserved — true powder feel |
| 15-25 mph | partially_broken | Still good but not the lightest |
| > 25 mph | wind_broken | Chalky/wind-packed on exposed terrain; best in trees |

## Assessment Logic

### Scoping

Ride quality notes are computed for:
- Any day with forecasted snow > 0.5"
- 1 day after the last snow day (bluebird check)

Base risk looks back 48 hours before the first day with snow > 0.5".

Days with no snow and no quality relevance get no notes.

### Assessment 1: Crystal Quality Impact

Computed from average wind speed during hours with precipitation > 0:

| Wind during snow | Note |
|---|---|
| < 15 mph | "Fresh dendrites likely — expect true powder feel" |
| 15-25 mph | "Moderate wind during snowfall — crystals partially broken, still good but not the lightest" |
| > 25 mph | "Heavy wind during snowfall — snow will feel chalky on exposed terrain, best quality in protected trees" |

### Assessment 2: Layering (per-day, relative to prior day)

For each snow day, compare today's density category to the prior day's:

| Prior day | Today | Note |
|---|---|---|
| Dense (standard/heavy/wet_cement) | Light (cold_smoke/dry_powder) | "Favorable layering — light snow over supportive dense base from [prior day]" |
| Light (cold_smoke/dry_powder) | Dense (standard/heavy/wet_cement) | "New heavy snow over lighter layer — may feel punchy and inconsistent" |
| First storm day | Light | No layering note (base risk handles this) |
| First storm day | Dense | "Dense enough to be self-supporting" |
| Same category | Same category | No note |

### Assessment 3: Base Condition & Punch-Through Risk

Combines base risk severity with today's density and forecasted amount.

**Base risk determination** (from pre-storm 48-hour weather):

Melt-freeze check:
- Above freezing (> 0°C) for 1+ hours → melt-freeze risk detected

Solar crust check (graduated by season):
- Compute solar elevation at noon for the latitude and date (deterministic)
- Solar risk scales continuously — higher sun angle = less temperature/duration needed
- Approximate thresholds:

| Period | Crust conditions |
|---|---|
| Dec-Jan (elevation ~27-30°) | Needs above-freezing temps for several hours |
| Feb-early Mar (~33-40°) | Above freezing 1-2 hrs, OR clear sky + above -3°C for 4+ hrs |
| Mid Mar-Apr (~41-58°) | Clear sky + above -5°C for 1-2 hrs |
| Late Apr+ (~60°+) | Almost any clear day above -8°C |

"Clear sky" = cloud cover < 30% during daylight hours. Solar crust applies primarily to south-facing terrain above treeline.

**Base risk levels:**
- "high": above-freezing 6+ hours, OR both melt-freeze and solar triggers
- "moderate": above-freezing 1-5 hours, OR solar trigger alone
- "low": neither trigger

**Punch-through assessment** (combines base risk + new snow density + amount):

| Base risk | New snow density | Amount | Note |
|---|---|---|---|
| high | cold_smoke | < 12" | "Likely punching through to hard layer underneath" |
| high | cold_smoke | 12"+ | "Deep enough to float above the crust, but may hit it in thin spots" |
| high | dry_powder | < 8" | "May punch through to crust in spots" |
| high | dry_powder | 8"+ | "Should have enough depth over the crust" |
| high | standard or denser | 4"+ | "Dense enough to ride without hitting the crust" |
| moderate | cold_smoke | < 8" | "Thin — could feel inconsistent over variable base" |
| moderate | any denser | any | No note |
| low | any | any | No base concern noted |

These depth thresholds are initial estimates to be refined with real-world experience and eventual forecast verification data.

### Assessment 4: Bluebird Powder

| Condition | Note |
|---|---|
| Daytime cloud cover < 20% AND snow > 4" prior night | "Bluebird powder day — clear skies with fresh snow" |
| Daytime cloud cover < 20% AND last snow day was yesterday | "Bluebird powder day — storm clearing to sunshine" |
| Clear skies but no recent snow | Not flagged |

### Assessment 5: Solar Crust Warning (pre-storm)

When solar crust risk is detected (see base risk logic above):

| Severity | Note |
|---|---|
| Solar trigger + late season (Apr+) | "Strong sun crust likely on south-facing terrain — new snow may not bond to surface" |
| Solar trigger + mid-season (Mar) | "Sun crust possible on south-facing terrain above treeline" |
| Melt-freeze + any season | "Melt-freeze crust likely underneath — expect hard layer below new snow" |
| Both triggers | "Hard base conditions — melt-freeze and sun crust likely" |

## LLM Integration

### Prompt Format

Ride quality notes appear per-day in the weather context, after the snowfall/temp/wind data:

```
### Silverton — Mar 06
Snow: 10.2" | Density: dry_powder (78 kg/m3) | Wind during snow: 12 mph

Ride quality notes:
- Fresh dendrites likely — expect true powder feel
- Favorable layering — light snow over supportive dense base from Mar 05
- Bluebird powder day — storm clearing to sunshine

### Silverton — Mar 07
Snow: 2.1" | Density: standard (115 kg/m3) | Wind during snow: 28 mph

Ride quality notes:
- Heavy wind during snowfall — best quality in protected trees
- New heavier snow over lighter layer — may feel punchy and inconsistent
```

### What the LLM does with it

The notes are pre-interpreted English. The LLM synthesizes them into its recommendation narrative, tier decision, and best-ski-day selection. It does not need to understand snow physics — just read and incorporate the assessments.

## Trace Output

In `--weather-only` mode, ride quality notes appear after the forecast data per resort, providing value even without an LLM evaluation.

## Testing Strategy

- Pure domain functions: table-driven tests for each assessment rule
- Each threshold boundary tested explicitly
- Multi-day storm scenarios testing layering logic
- Solar elevation computation verified against known values
- Integration test: full forecast → quality notes pipeline

## Dependencies

- **Requires**: Vionnet density model (docs/plans/2026-03-05-vionnet-slr-model.md) — must be implemented first
- **Requires**: Cloud cover data from Open-Meteo and NWS — added as part of this work

## Future Enhancements (not in scope)

- Forecast verification system (comparing predictions to actual resort reports)
- Solar radiation data for more precise crust prediction
- Surface hoar detection (clear cold nights before storms)
- Wind direction → aspect loading
