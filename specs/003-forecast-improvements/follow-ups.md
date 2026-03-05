# Follow-ups: Forecast Accuracy Improvements

Identified during implementation review. These are post-implementation fixes
that improve accuracy and usability of the feature work completed in tasks.md.

## 1. Per-Resort Weather Queries + LLM Consolidation (accuracy + usability)

**Status**: Complete (combined #1 and #2 — shipped together)

**Problem**: Weather is queried at a single regional centroid coordinate, which
may be at valley elevation thousands of feet below resort summits. This produces
temperatures 10-15°F too warm, causing SLR to underestimate snowfall or classify
it as rain. Stevens Pass (5845') and Snoqualmie (3865') are 25 miles apart in
different drainages but share one query point. Additionally, sending 3 models ×
N resorts of raw forecast data to the LLM is too much — it needs consolidated data.

**Design**:

### Weather layer
- `FetchAll(ctx, region, resorts)` takes resorts as new parameter
- Open-Meteo: one multi-model call per resort with `&elevation=` set to
  mid-mountain ((base + summit) / 2, converted to meters)
- NWS gridpoint: one call per resort (resolves to correct gridpoint)
- NWS AFD: one call per unique WFO, deduplicated across resorts
- `Forecast` gets `ResortID` field

### Detection
- Aggregate per-resort forecasts to region level
- Use max snowfall per day across resorts (if any resort is hammered, detect it)

### Consensus
- Computed per-resort across models (not across resorts)
- Each resort gets its own ModelConsensus

### LLM prompt (consolidated)
- One table per resort showing consensus mean values + confidence column
- Model disagreement callouts only on low-confidence days
- SLR annotations only on notable snow days
- AFD text included as-is (already compact)

### Trace output (full detail)
- Per-resort, per-model forecast tables (for debugging)
- Per-resort consensus tables
- AFD text

### Pipeline
- `Scan` passes resorts to `FetchAll`
- `ScanResult` carries per-resort forecasts + per-resort consensus
- Consensus and discussion wired through `EvalContext` as before

## 3. AFD Date Relevance (accuracy)

**Status**: Complete

**Problem**: The AFD we fetch is the *latest* discussion from the WFO, which may
not cover the dates of the detected storm window. NWS issues AFDs 2-4x daily and
they discuss the next 5-7 days. If the storm is 10 days out, the current AFD may
not mention it at all.

**Fix**: Added relevance checking based on AFD issuance date vs storm window start.
- `FormatDiscussionForPrompt` now accepts `DetectionResult` and adds a caveat note
  when the storm window starts beyond the AFD's ~7-day coverage horizon, telling
  the LLM to weight numerical forecast data more heavily
- `FormatAFD` (trace output) shows "✓ AFD covers storm window dates" or
  "⚠ Storm window starts N days after AFD issuance — may not be covered"
- Detection is now computed before AFD rendering in the trace command
