# Feature Specification: Forecast Accuracy Improvements

**Feature Branch**: `003-forecast-improvements`
**Created**: 2026-03-04
**Status**: Draft
**Input**: Improve forecast accuracy through multi-model aggregation, snow-to-liquid ratio calculations, NWS forecast discussions, elevation-aware predictions, and confidence-calibrated recommendations.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Confidence-Calibrated Recommendations (Priority: P1)

As a powder hunter receiving storm alerts, I want recommendations that account for forecast uncertainty at different time horizons so I can make appropriately cautious or committed travel decisions.

**Why this priority**: Without confidence calibration, users either over-commit to uncertain long-range forecasts (wasting money on busted storms) or under-react to high-confidence near-term events. This directly impacts user trust and the core value proposition.

**Independent Test**: Can be fully tested by generating evaluations at different lead times and verifying the LLM's advice reflects appropriate certainty levels — e.g., "book refundable flights" at 5 days vs. "commit now" at 1 day.

**Acceptance Scenarios**:

1. **Given** a storm detected 5+ days out, **When** the system generates an evaluation, **Then** the recommendation includes hedging language and suggests reversible commitments (refundable bookings, watching for updates).
2. **Given** a storm detected 1-2 days out, **When** the system generates an evaluation, **Then** the recommendation reflects high confidence and suggests decisive action.
3. **Given** a storm moves from 7-day to 2-day range across successive evaluations, **When** the user views the latest evaluation, **Then** the confidence level and recommendation urgency increase accordingly.

---

### User Story 2 - Accurate Mountain Snowfall Estimates (Priority: P1)

As a powder hunter, I want snowfall predictions that account for temperature-based snow density so the system doesn't underestimate cold-smoke destinations or overestimate warm coastal storms.

**Why this priority**: The current system significantly underestimates snowfall at cold-dry destinations (Utah, Montana) and overestimates wet-heavy snow at coastal resorts. This directly causes missed storms and false alerts — the two worst failure modes.

**Independent Test**: Can be tested by comparing system snowfall estimates against known SLR-adjusted values for storms at cold (< 20°F) and warm (28-32°F) destinations.

**Acceptance Scenarios**:

1. **Given** a storm with 0.5" liquid equivalent at 12°F, **When** snowfall is calculated, **Then** the estimate uses 20:1 SLR (~10" cold smoke) rather than the default 10:1 (~5").
2. **Given** a storm with 1" liquid equivalent at 28°F, **When** snowfall is calculated, **Then** the estimate uses 10:1 SLR (~10" wet snow) and the system notes heavy/wet conditions.
3. **Given** hourly temperatures of 36°F for 4 hours then 22°F for 8 hours during steady 0.1"/hr precipitation, **When** snowfall is calculated, **Then** the first 4 hours contribute 0" (rain) and the last 8 hours use 15:1 SLR (~12" dry powder).
4. **Given** temperatures in the 32-35°F mixed zone, **When** the evaluation is generated, **Then** the system flags mixed precipitation risk and uses 5:1 SLR.

---

### User Story 3 - Multi-Model Consensus Detection (Priority: P2)

As a powder hunter, I want the system to compare multiple weather models so I can trust that a big storm alert reflects genuine model agreement rather than one model's outlier prediction.

**Why this priority**: Single-model reliance leads to both false positives (GFS hype storms) and false negatives (missing storms one model catches). Multi-model consensus is the industry standard for forecast confidence.

**Independent Test**: Can be tested by querying multiple models for the same location/timeframe and verifying the system identifies agreement vs. divergence.

**Acceptance Scenarios**:

1. **Given** GFS predicts 30" but ECMWF predicts 6" for the same resort, **When** a storm is detected, **Then** the system flags the storm as "low confidence" due to model disagreement.
2. **Given** GFS, ECMWF, and a high-resolution model all predict 12-18" for a resort, **When** a storm is detected, **Then** the system reports high confidence in the forecast.
3. **Given** model disagreement on storm totals, **When** the evaluation is generated, **Then** the LLM receives model spread context and adjusts its tier recommendation accordingly.

---

### User Story 4 - Expert Meteorological Context (Priority: P2)

As a powder hunter, I want the system to incorporate NWS forecaster analysis so the LLM has access to expert nuance that raw model data cannot provide.

**Why this priority**: NWS forecast discussions contain critical context like "models consistently underpredict for this area" or "unusual model uncertainty for this event." This expert signal is free and dramatically improves evaluation quality.

**Independent Test**: Can be tested by fetching NWS Area Forecast Discussions for a region and verifying the content reaches the LLM prompt.

**Acceptance Scenarios**:

1. **Given** a resort in a known NWS Weather Forecast Office coverage area, **When** a storm evaluation is generated, **Then** the relevant NWS forecast discussion text is included in the LLM prompt context.
2. **Given** a NWS discussion mentions "higher than usual model uncertainty," **When** the LLM generates an evaluation, **Then** the evaluation reflects increased caution.
3. **Given** the NWS API is unavailable, **When** a storm evaluation is generated, **Then** the system proceeds without the discussion context and notes its absence.

---

### User Story 5 - High-Resolution Near-Range Forecasts (Priority: P3)

As a powder hunter, I want the system to use high-resolution weather models (3km grid) for storms within 3 days so predictions account for terrain effects like canyon funneling and orographic lift.

**Why this priority**: Global models (10-25km grid) literally cannot resolve narrow canyons like Little Cottonwood or terrain-driven snowfall bands. High-resolution models are critical for near-range accuracy but less important for longer-range pattern detection.

**Independent Test**: Can be tested by comparing forecasts from global vs. high-resolution models for terrain-complex resorts and verifying the system selects the appropriate model based on lead time.

**Acceptance Scenarios**:

1. **Given** a storm is 1-3 days out, **When** the system fetches weather data, **Then** it uses high-resolution model data (HRRR/NAM equivalent) for the forecast.
2. **Given** a storm is 4+ days out, **When** the system fetches weather data, **Then** it uses global models (GFS/ECMWF) for trend detection.
3. **Given** high-resolution model data is unavailable for a region, **When** the system fetches weather data, **Then** it falls back to global model data and notes reduced terrain accuracy.

---

### Edge Cases

- What happens when all weather models disagree significantly (no consensus)?
- SLR is calculated per-hour using that hour's temperature. Hours above freezing contribute zero snowfall (rain). Hourly snow totals are aggregated into half-day and daily periods.
- Non-US resorts have no NWS coverage; the system skips forecast discussion context and notes this in the evaluation. If a US resort fails WFO lookup, same graceful degradation applies.
- How does the system handle the transition from global to high-resolution models as a storm moves from day 4 to day 3?
- What happens when Open-Meteo is rate-limited or returns partial model data?

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST apply temperature-based snow-to-liquid ratio (SLR) when calculating snowfall using five temperature bands: >35°F rain (0 snow), 32-35°F mixed 5:1, 25-31°F wet snow 10:1, 15-24°F dry powder 15:1, <15°F cold smoke 20:1. Calculated per-hour and aggregated. The SLR-adjusted value replaces the raw API snowfall field entirely.
- **FR-002**: System MUST include forecast lead time context in the LLM evaluation prompt so the model can calibrate recommendation confidence.
- **FR-003**: System MUST query multiple weather models and present model agreement/divergence context to the LLM.
- **FR-004**: System MUST fetch NWS Area Forecast Discussion text for the relevant Weather Forecast Office when generating evaluations for US-based resorts. Non-US resorts skip this context, and the evaluation notes its absence.
- **FR-005**: System MUST gracefully degrade when optional data sources (NWS discussions, specific models) are unavailable.
- **FR-006**: System MUST select weather model resolution based on forecast lead time — high-resolution for near-range (1-3 days), global models for extended range (4+ days).
- **FR-007**: System MUST surface rain-line risk when base elevation temperatures are near or above freezing during a precipitation event.

### Key Entities

- **WeatherModel**: Represents a single model's forecast (model name, grid resolution, forecast horizon, snowfall/temperature/precipitation data).
- **ModelConsensus**: Aggregated view across models for a location — includes spread, agreement level, and confidence classification.
- **ForecastDiscussion**: NWS meteorologist free-text analysis for a Weather Forecast Office, with timestamp and relevance to specific regions.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Snowfall estimates for cold-smoke destinations (Utah, Montana, interior BC) are within 30% of observed totals for storms where verification data is available.
- **SC-002**: 90% of storm alerts at 1-2 day lead time include decisive, high-confidence recommendations.
- **SC-003**: 90% of storm alerts at 5+ day lead time include appropriately hedged recommendations with reversible-action suggestions.
- **SC-004**: When weather models' spread-to-mean ratio exceeds 1.0 (i.e., the difference between highest and lowest model predictions exceeds their mean), the system flags the storm as low confidence in 100% of cases.
- **SC-005**: NWS forecast discussion context is successfully included in evaluations for 95% of US-based resorts.
- **SC-006**: No increase in false-positive storm alerts compared to current single-model baseline.

## Clarifications

### Session 2026-03-04

- Q: Should SLR-adjusted snowfall replace or supplement the raw API snowfall field? → A: Replace — SLR-adjusted snowfall becomes the primary snowfall value; raw API field is discarded.
- Q: At what time granularity should SLR be calculated when temperatures fluctuate? → A: Per-hour — use each hour's temperature to determine SLR for that hour's precipitation, then aggregate. This captures rain-to-snow transitions that averaging would mask.
- Q: What metric defines "model disagreement" for confidence flagging? → A: Spread-to-mean ratio — flag as low confidence when (max − min) / mean > 1.0.
- Q: Should NWS forecast discussions cover non-US resorts? → A: US-only for now. Non-US resorts skip expert discussion context, noted in output. Canadian coverage deferred to future work.
- Q: What SLR temperature bands should the system use? → A: Five bands — >35°F: rain (0 snow), 32-35°F: mixed/very wet 5:1, 25-31°F: wet snow 10:1, 15-24°F: dry powder 15:1, <15°F: cold smoke 20:1. Uses 35°F operational rain/snow threshold (not 32°F) to account for wet-bulb cooling in dry mountain air.

## Assumptions

- Open-Meteo's multi-model API continues to be free and supports the models needed (GFS, ECMWF, HRRR/NAM equivalents).
- NWS API (`api.weather.gov`) remains freely accessible without authentication.
- The existing NWS `/points` endpoint mapping (already implemented) correctly resolves resort coordinates to Weather Forecast Offices.
- Temperature-based SLR heuristics (5:1 through 20:1 across five bands) provide sufficient accuracy without needing wind speed or humidity corrections.
- High-resolution models available through Open-Meteo have sufficient coverage for the resorts in our dataset.
