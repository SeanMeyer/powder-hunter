# Feature Specification: Storm Correlation & Alert Grouping

**Feature Branch**: `002-storm-correlation`
**Created**: 2026-03-04
**Status**: Draft (future feature — do not implement until 001-storm-tracker is running in production)
**Depends On**: 001-storm-tracker (full pipeline operational with real detection data)

## Problem Statement

When a major weather system crosses multiple regions (e.g., a Pacific trough dumping on all of Colorado, or a Nor'easter blanketing Vermont + New Hampshire), the current pipeline produces **independent alerts per region**. A single storm system hitting 5 Colorado regions generates 5 separate Discord threads, creating notification fatigue and fragmenting what is actually one trip-planning decision.

## User Story

As a skier, I want storm alerts to be grouped when the same weather system affects multiple nearby regions, so I receive **one consolidated briefing per weather event** with a breakdown of affected regions, rather than being spammed with separate alerts that all say the same thing.

## Proposed Approach: Geographic Clustering at Post Time

Insert a new `Correlate()` stage between `Compare()` and `Post()` in the pipeline:

```
Scan → Evaluate → Compare → **Correlate** → Post → Expire
```

### Correlation Logic

After all regions have been evaluated and change-classified, group `CompareResult` entries that likely represent the same physical weather event:

1. **Temporal overlap**: Storm windows overlap or are within N days of each other
2. **Spatial proximity**: Region centroids within a configurable radius (e.g., 200 miles / 320 km)
3. **Same run**: Only correlate results from the same pipeline run (don't retroactively group across runs)

### Grouped Alert Behavior

- **Primary region**: The highest-tier result in the group becomes the "primary" alert
- **Discord post**: One thread created for the primary, with all affected regions summarized in the embed
- **Sub-region details**: Each region's evaluation is included as sections within the single briefing
- **Updates**: Subsequent runs update the same thread, even if different regions within the group change

### What This Does NOT Change

- **Weather fetching**: Still per-region (correct — regions have distinct microclimates)
- **Detection thresholds**: Still per-region friction tier
- **LLM evaluation**: Still per-region (each resort set needs individual analysis)
- **Storm lifecycle**: Still per-region in the database
- **Comparison logic**: Still per-storm

The correlation is purely a **presentation layer** concern — it groups already-evaluated results for cleaner Discord delivery.

## Key Design Decisions to Make (After Real Data)

These questions can only be answered after running the basic pipeline for a few weeks:

1. **Proximity threshold**: How close (in miles) should two region centroids be to correlate? Colorado I-70 resorts span ~60 miles; Colorado I-70 + Steamboat span ~120 miles. What radius captures "same storm" without over-grouping?

2. **Temporal window**: How many days of overlap/gap still counts as "same event"? A trough might hit Utah on Tuesday and Colorado on Wednesday — is that one event or two?

3. **Cross-state correlation**: Should a massive storm hitting UT + CO + WY be one alert? Or is state-level grouping sufficient?

4. **Tier disagreement**: If one region evaluates as DROP_EVERYTHING and a nearby region as ON_THE_RADAR (same storm, different terrain/access), how should the grouped alert present this?

5. **Re-correlation on updates**: If a new region triggers on a subsequent run and correlates with an existing group, should it join the existing thread or start fresh?

## Integration Points (from codebase analysis)

| Component | File | Change |
|-----------|------|--------|
| New correlation stage | `pipeline/pipeline.go` | Add `Correlate()` between `Compare()` and `Post()` |
| Correlation domain logic | `domain/correlation.go` (new) | Pure function: `Correlate([]CompareResult) → []GroupedAlert` |
| Grouped alert type | `domain/storm.go` | New `GroupedAlert` struct wrapping multiple `CompareResult` |
| Discord formatter | `discord/formatter.go` | New `FormatGroupedStorm()` for multi-region embeds |
| Discord poster | `discord/webhook.go` | Update `Post()` to iterate grouped alerts |
| DB schema (optional) | `storage/schema.sql` | Optional `correlation_group_id` on storms table |

## Prerequisites

- [ ] 001-storm-tracker pipeline running in production for 2+ weeks
- [ ] Observed real multi-region detection patterns to calibrate thresholds
- [ ] Confirmed that notification fatigue is actually a problem in practice (it might not be — most storms don't hit 5 regions simultaneously)

## Constitution Compliance

- **I/O Sandwich**: Correlation logic is pure (takes data in, returns grouped data out) — lives in `domain/`
- **Parse, Don't Validate**: `GroupedAlert` is a new domain type, not a raw transformation
- **Decisions Are Data**: Correlation decisions (why these regions grouped) should be logged
- **Domain-Organized**: New file in `domain/`, formatting in `discord/`
