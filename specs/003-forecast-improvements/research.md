# Research: Forecast Accuracy Improvements

**Date**: 2026-03-04 | **Branch**: `003-forecast-improvements`

## R1: Open-Meteo Multi-Model API Format

**Decision**: Use `&models=model1,model2` parameter to query multiple models in a single call.

**Rationale**: Tested the API directly. When multiple models are specified, Open-Meteo returns separate keyed arrays per variable per model:

```
hourly: {
  time: [...],
  snowfall_gfs_seamless: [...],
  snowfall_ecmwf_ifs025: [...],
  temperature_2m_gfs_seamless: [...],
  temperature_2m_ecmwf_ifs025: [...],
  precipitation_gfs_seamless: [...],
  precipitation_ecmwf_ifs025: [...]
}
```

Key naming pattern: `{variable}_{model_id}`. The `time` array is shared. This means we can parse each model's data independently from the same response, then compute consensus.

**Alternatives considered**:
- Separate API calls per model: More requests, but simpler parsing. Rejected because a single call is more efficient and the response format is straightforward.

## R2: Model Selection for Multi-Model Consensus

**Decision**: Query three models for all forecasts:
- `gfs_seamless` — NOAA GFS, global, ~13km resolution
- `ecmwf_ifs025` — ECMWF IFS, global, ~25km resolution
- For near-range (days 1-3), also include `gfs_hrrr` — NOAA HRRR, US only, 3km resolution

**Rationale**:
- GFS and ECMWF are the two primary global models used by all professional forecast services. They have different biases (GFS tends to overforecast big storms, ECMWF is generally more conservative), making them ideal for consensus detection.
- HRRR provides the high-resolution terrain-aware capability needed for FR-006. It's US-only and only goes out ~18-48 hours, so it supplements rather than replaces global models.
- Three models give a meaningful spread-to-mean calculation without excessive API response size.

**Alternatives considered**:
- Adding ICON (DWD German model): Good model but adds complexity without significantly improving US/Canada mountain coverage. Can add later.
- Using `best_match` (Open-Meteo auto-selection): Opaque — we can't compute consensus if we don't know which model was selected.

## R3: NWS Area Forecast Discussion (AFD) API

**Decision**: Fetch latest AFD from `https://api.weather.gov/products/types/AFD/locations/{WFO}` where WFO is the 3-letter office code (e.g., SLC, SEW, BOU).

**Rationale**: Tested the API directly. The endpoint returns a list of recent AFD products as JSON-LD. Each entry has an `id` UUID. To get the full text, fetch `https://api.weather.gov/products/{id}`. The `issuingOffice` field uses the K-prefixed 4-letter ICAO code (e.g., KSLC), but the URL filter uses the 3-letter WFO code.

**API Flow**:
1. We already resolve WFO via `/points/{lat},{lon}` → `gridId` (e.g., "SLC")
2. List AFDs: `GET /products/types/AFD/locations/{gridId}` → get first `@graph` entry's `id`
3. Fetch text: `GET /products/{id}` → `productText` field contains the full discussion

**Caching**: AFDs are issued 2-4 times daily. Cache the text for 6 hours (our pipeline runs 2x daily).

**Alternatives considered**:
- Scraping NWS website: Unnecessary — the API provides structured access.
- Using the forecast endpoint's `detailedForecast` text: Less detailed than AFD; doesn't contain the expert model-uncertainty analysis we want.

## R4: SLR Calculation Approach

**Decision**: Calculate SLR per-hour using hourly precipitation (liquid) and temperature, replacing the API's raw snowfall field.

**Rationale**: The API's snowfall field assumes a fixed SLR (roughly 10:1). By computing from liquid precipitation + temperature, we get location-appropriate estimates. Five temperature bands per spec clarification:

| Temperature Range | SLR | Snow Type |
|-------------------|-----|-----------|
| > 35°F (> 1.7°C) | 0:1 | Rain (no snow) |
| 32-35°F (0-1.7°C) | 5:1 | Mixed/very wet |
| 25-31°F (-3.9 to -1.1°C) | 10:1 | Wet snow |
| 15-24°F (-9.4 to -4.4°C) | 15:1 | Dry powder |
| < 15°F (< -9.4°C) | 20:1 | Cold smoke |

The calculation uses Celsius internally (matching API data) and converts thresholds. Per-hour calculation means each hour's precipitation gets the SLR for that hour's temperature, then hourly snow totals are summed into half-day and daily periods.

**Alternatives considered**:
- Continuous SLR function (linear interpolation): More "correct" but harder to test and explain. Discrete bands are the industry standard and match how forecasters communicate.
- Using the API snowfall field as a cross-check: Rejected per clarification — SLR-adjusted value replaces the raw field entirely.

## R5: Freezing Level Height for Rain-Line Detection

**Decision**: Add `freezing_level_height` to the Open-Meteo hourly query. Compare against resort base/summit elevations.

**Rationale**: Open-Meteo provides `freezing_level_height` as an hourly variable — the altitude where temperature is 0°C. When the freezing level is below summit but above base, the evaluation should flag rain risk at lower elevations. This directly supports FR-007.

**Implementation**: Query the variable alongside existing hourly params. During aggregation, store min/max freezing level per half-day period. The prompt formatter surfaces this alongside base/summit elevations.

## R6: Confidence Calibration by Lead Time

**Decision**: Prompt-only change. Add a structured confidence guide to the evaluation prompt template.

**Rationale**: Per spec priority, this is the highest-impact, lowest-effort change. The LLM already receives the storm window dates. Adding explicit guidance about what confidence level maps to what lead time lets the LLM calibrate its recommendations without any code changes beyond the prompt template.

**Lead time bands** (from spec):
- 1-2 days: High confidence → decisive recommendations
- 3-5 days: Moderate confidence → refundable bookings, tentative plans
- 6-7 days: Lower confidence → worth watching, no commitments
- 8-16 days: Pattern-level only → awareness, no action items

**Implementation**: Add to the prompt template stored in the database (or seed data).
