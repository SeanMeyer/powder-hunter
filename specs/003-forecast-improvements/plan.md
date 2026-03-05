# 003: Forecast Accuracy Improvements

## Status: Backlog

## Problem

Our current forecast sources (Open-Meteo default model + NWS gridpoint) significantly
underestimate mountain snowfall compared to specialized services like OpenSnow. Testing
with Mt. Baker showed Open-Meteo reporting 2.2" on a day OpenSnow predicted 10". The
root cause is coarse grid resolution (~11km) that can't model orographic lift, plus
reliance on a single weather model.

## Improvement Ideas

### 1. Multi-Model Aggregation via Open-Meteo

Open-Meteo supports querying multiple weather models in a single API call. We could
pull from several and compare:

- **ECMWF (Euro)**: Historically most accurate global model
- **GFS (American)**: Good for long-range trends, but prone to overforecasting big storms
- **HRRR / NAM**: High-resolution local models (3km grid), terrain-aware, but only
  18-60 hours out

**Detection improvement**: If GFS says 30" but ECMWF says 6", flag the storm as
"low confidence" and suppress hype alerts. Only trigger high-tier alerts when models
reach consensus. This directly addresses the false-positive risk.

**Feasibility**: High — Open-Meteo already supports this, just need to change the
API query parameters and add model-comparison logic.

### 2. High-Resolution Model Handover at 72 Hours

Global models (GFS/ECMWF) use 10-25km grids and literally cannot see narrow canyons
like Little Cottonwood. The idea:

- **Days 4-16 (monitoring)**: Use global models for trend detection only
- **Days 1-3 (trigger)**: Automatically switch to HRRR/NAM 3km models that can
  resolve canyons, lake-effect bands, and individual peaks

This is essentially what we intended with NWS near-range preference, but using
better models than the NWS gridpoint API provides.

**Feasibility**: Medium — requires time-based model selection logic and understanding
which Open-Meteo model parameters map to HRRR/NAM.

### 3. Elevation Gradient Checks (Rain Line Detection)

We already have `base_elevation_ft` and `summit_elevation_ft` in our resort data.
We could query Open-Meteo twice per resort — once at base, once at summit — and
calculate the freezing level.

- Base 35°F + summit 22°F → "Rain likely below mid-mountain"
- Both below 25°F → "Top-to-bottom powder"

Pass this context to the LLM so it can factor rain risk into tier decisions.

**Feasibility**: Medium — doubles our API calls per resort but Open-Meteo is free
and fast. Need to add freezing-level logic and surface it in the evaluation prompt.

### 4. Custom Snow-to-Liquid Ratio (SLR)

Generic APIs assume 10:1 SLR (1" rain = 10" snow). This is Sierra Cement. Utah
cold smoke is 15:1 or 20:1. Instead of trusting the API's snowfall field:

1. Query for **precipitation (liquid equivalent)** and **temperature**
2. Apply temperature-based SLR heuristic:
   - 28-32°F → 10:1 (heavy wet snow)
   - 20-27°F → 15:1 (standard dry powder)
   - <20°F → 20:1 (blower cold smoke)

This would give us much better snowfall estimates, especially for cold-smoke
destinations like Utah, Montana, and interior BC.

**Feasibility**: High — we already fetch temperature and precipitation. Just need
to add the SLR calculation and use it instead of (or alongside) the raw snowfall field.

## Priority Recommendation

1. **SLR math** (easiest win, highest accuracy impact per effort)
2. **Multi-model aggregation** (moderate effort, big confidence improvement)
3. **Elevation gradient** (moderate effort, fixes rain-line blind spot)
4. **High-res handover** (most complex, biggest accuracy improvement for near-range)

## Related

- NWS mm→cm unit fix: committed in 001-storm-tracker
- Detection now uses source-preference (NWS near-range, Open-Meteo extended): 001-storm-tracker
