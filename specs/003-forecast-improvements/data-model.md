# Data Model: Forecast Accuracy Improvements

**Date**: 2026-03-04 | **Branch**: `003-forecast-improvements`

## Modified Types

### domain.HalfDay (extended)

Existing fields unchanged. New fields:

| Field | Type | Description |
|-------|------|-------------|
| FreezingLevelMinM | float64 | Minimum freezing level altitude during period (meters) |
| FreezingLevelMaxM | float64 | Maximum freezing level altitude during period (meters) |

### domain.DailyForecast (extended)

Existing fields unchanged. The `SnowfallCM` field changes semantics:
- **Before**: Raw API snowfall value (generic ~10:1 SLR)
- **After**: SLR-adjusted snowfall calculated per-hour from precipitation + temperature

New fields:

| Field | Type | Description |
|-------|------|-------------|
| SLRatio | float64 | Effective (weighted average) SLR for the day's snowfall |
| RainHours | int | Count of hours where temp > 35°F during precipitation |
| MixedHours | int | Count of hours in the 32-35°F mixed zone during precipitation |

### domain.Forecast (extended)

Existing fields unchanged. New field:

| Field | Type | Description |
|-------|------|-------------|
| Model | string | Weather model name (e.g., "gfs_seamless", "ecmwf_ifs025", "nws"). Empty string for backward compat with single-model fetches. |

Currently each `Forecast` has `Source` ("open_meteo" or "nws"). With multi-model queries, a single Open-Meteo API call returns data for multiple models. Each model's data becomes a separate `Forecast` with the same `Source` but different `Model` values.

## New Types

### domain.ModelConsensus

Aggregated view of multi-model forecasts for a region. Computed as pure domain logic (no I/O).

| Field | Type | Description |
|-------|------|-------------|
| RegionID | string | Region these models cover |
| Models | []string | Model names included in consensus |
| DailyConsensus | []DayConsensus | Per-day consensus data |

### domain.DayConsensus

| Field | Type | Description |
|-------|------|-------------|
| Date | time.Time | Calendar day |
| SnowfallMinCM | float64 | Lowest model's SLR-adjusted snowfall |
| SnowfallMaxCM | float64 | Highest model's SLR-adjusted snowfall |
| SnowfallMeanCM | float64 | Mean across models |
| SpreadToMean | float64 | (Max - Min) / Mean; > 1.0 = low confidence |
| Confidence | string | "high" (spread < 0.5), "moderate" (0.5-1.0), "low" (> 1.0) |

### domain.ForecastDiscussion

NWS Area Forecast Discussion text for a Weather Forecast Office.

| Field | Type | Description |
|-------|------|-------------|
| WFO | string | Weather Forecast Office code (e.g., "SLC") |
| IssuedAt | time.Time | When the AFD was published |
| Text | string | Full discussion text |
| FetchedAt | time.Time | When we retrieved it |

## New Pure Functions (domain package)

### SLR Calculation

```
CalculateSLR(tempC float64) float64
```
Returns the SLR multiplier for a given temperature. Pure lookup against the five temperature bands (converted to Celsius thresholds).

```
SnowfallFromPrecip(precipMM float64, tempC float64) float64
```
Returns snowfall in cm for a given hour's precipitation and temperature. Applies SLR and unit conversion: `precipMM / 10.0 * SLR` (mm→cm, then multiplied by ratio).

### Model Consensus

```
ComputeConsensus(forecasts []Forecast) ModelConsensus
```
Takes multiple `Forecast` values (same region, different models), aligns by date, computes spread/mean/confidence per day. Pure function, no I/O.

## Relationships

```
Region 1──* Forecast (one per source+model combination)
  │
  ├── Forecast[gfs_seamless]  ──┐
  ├── Forecast[ecmwf_ifs025]  ──┼── ComputeConsensus() → ModelConsensus
  ├── Forecast[gfs_hrrr]      ──┘
  └── Forecast[nws]           (separate, not in consensus — different data source)

Region 1──0..1 ForecastDiscussion (US regions only, via WFO)

ModelConsensus + ForecastDiscussion + Forecasts → Evaluation prompt context
```

## Storage Changes

The `weather_snapshot` JSON blob stored with each evaluation already captures the full `[]Forecast` data. With the `Model` field added to `Forecast`, multi-model data is automatically preserved. No schema migration needed.

The `ForecastDiscussion` text is included in the rendered prompt (already stored as `rendered_prompt` in evaluations table). No additional column needed.

## State Transitions

No new state machines. The existing Storm lifecycle (detected → evaluated → briefed → updated → expired) is unchanged. The only behavioral change is that storm detection now uses SLR-adjusted snowfall values, which may shift detection thresholds.

## Validation Rules

- SLR temperature thresholds are in Celsius with contiguous ranges (lower-inclusive, upper-exclusive): >1.67°C (rain, 0:1), [0°C, 1.67°C] (mixed 5:1), [-3.89°C, 0°C) (wet 10:1), [-9.44°C, -3.89°C) (dry 15:1), <-9.44°C (cold smoke 20:1). These are exact conversions from the Fahrenheit bands: >35°F, 32-35°F, 25-31°F, 15-24°F, <15°F.
- SpreadToMean is undefined when mean is 0 (no snowfall predicted by any model). In this case, confidence defaults to "high" (all models agree: no snow).
- ForecastDiscussion.Text may be empty if the NWS API fails; FR-005 requires graceful degradation.
