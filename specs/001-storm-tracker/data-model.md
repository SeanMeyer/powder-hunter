# Data Model: Automated Storm Tracker

**Feature Branch**: `001-storm-tracker`
**Date**: 2026-03-04

## Entities

### Region

A cluster of geographically close ski resorts representing a
single trip decision.

| Field | Type | Notes |
| --- | --- | --- |
| ID | string | Stable identifier (e.g., "summit-county") |
| Name | string | Display name (e.g., "Summit County") |
| Latitude | float64 | Representative coordinate for weather lookups |
| Longitude | float64 | Representative coordinate for weather lookups |
| FrictionTier | enum | `local` / `regional_driveable` / `flight` |
| NearThresholdIn | float64 | Near-range (1-7 day) snowfall threshold in inches |
| ExtendedThresholdIn | float64 | Extended-range (8-16 day) threshold in inches |
| Country | string | "US" or "CA" (determines NWS availability) |

**Relationships**: Has many Resorts.

**Validation**:
- Latitude: -90 to 90
- Longitude: -180 to 180
- FrictionTier MUST be one of the three enum values
- Thresholds MUST be positive

**Default thresholds** (derived from friction tier):

| Friction Tier | Near (inches) | Extended (inches) |
| --- | --- | --- |
| local | 8 | 12 |
| regional_driveable | 14 | 20 |
| flight | 24 | 36 |

### Resort

An individual ski area within a region.

| Field | Type | Notes |
| --- | --- | --- |
| ID | string | Stable identifier (e.g., "a-basin") |
| RegionID | string | Foreign key to Region |
| Name | string | Display name (e.g., "Arapahoe Basin") |
| Latitude | float64 | Resort coordinates |
| Longitude | float64 | Resort coordinates |
| ElevationM | int | Summit elevation in meters |
| PassAffiliation | string | "ikon" / "epic" / "indy" / "independent" |
| VerticalDropM | int | Vertical drop in meters |
| LiftCount | int | Number of lifts |
| Metadata | JSON | Extensible key-value blob (see below) |

**Metadata JSON schema** (extensible, not exhaustive):
```json
{
  "reputation": "Excellent tree skiing, back bowls hold powder",
  "terrain_character": "steep, above-treeline bowls + gladed trees",
  "tree_skiing_quality": "excellent",
  "access_notes": "I-70, 90 min from Denver, chain law applies",
  "crowd_tendency": "moderate weekdays, packed weekends",
  "powder_stash_notes": "Montezuma Bowl opens late after big storms"
}
```

**Validation**:
- ElevationM MUST be positive
- PassAffiliation MUST be a known value
- Metadata MUST be valid JSON (or null)

### Storm

The central lifecycle-tracked entity, identified by region +
date window.

| Field | Type | Notes |
| --- | --- | --- |
| ID | int64 | Auto-increment primary key |
| RegionID | string | Foreign key to Region |
| WindowStart | date | Start of storm date window |
| WindowEnd | date | End of storm date window |
| State | enum | Lifecycle state (see below) |
| CurrentTier | string | Latest tier or null if not yet evaluated |
| DiscordThreadID | string | Forum thread ID for updates (nullable) |
| DetectedAt | timestamp | When first detected |
| LastEvaluatedAt | timestamp | When last evaluated (nullable) |
| LastPostedAt | timestamp | When last posted to Discord (nullable) |

**Identity rule**: A new detection merges with an existing storm
if same RegionID AND date windows overlap. The merged storm's
window becomes the union of both ranges.

**Uniqueness**: No two active (non-expired) storms in the same
region may have overlapping date windows.

**State machine**:
```
detected → evaluated → briefed → updated ─┐
                                           ↓
     ┌─────────────────────────────────── updated
     │                                     ↓
     └──────────────────────────────────→ expired

Any state → expired (when forecast degrades below thresholds)
```

- `detected`: Storm flagged by weather scan, not yet evaluated
- `evaluated`: LLM evaluation complete, not yet posted to Discord
- `briefed`: Initial Discord post sent, thread created
- `updated`: Subsequent evaluation posted as thread update
- `expired`: Forecast degraded or storm window passed

**Validation**:
- WindowStart MUST be before WindowEnd
- State transitions MUST follow the state machine
- DiscordThreadID is set when state transitions to `briefed`

### Evaluation

A timestamped assessment of a storm. Multiple evaluations per
storm (one per pipeline run that evaluates the storm).

| Field | Type | Notes |
| --- | --- | --- |
| ID | int64 | Auto-increment primary key |
| StormID | int64 | Foreign key to Storm |
| EvaluatedAt | timestamp | When this evaluation was performed |
| PromptVersion | string | Which prompt template version was used |
| Tier | string | "DROP_EVERYTHING" / "WORTH_A_LOOK" / "ON_THE_RADAR" |
| Recommendation | string | Plain-language recommendation text |
| DayByDay | JSON | Day-by-day breakdown |
| KeyFactors | JSON | Pros and cons list |
| LogisticsSummary | JSON | Lodging, flights, road conditions |
| Strategy | string | Recommended timing/travel strategy |
| SnowQuality | string | Snow quality assessment |
| CrowdEstimate | string | Expected crowd level |
| ClosureRisk | string | Road/resort closure risk assessment |
| WeatherSnapshot | JSON | Raw weather data at evaluation time |
| RawLLMResponse | string | Full serialized API response for debugging (includes grounding metadata, finish reason, etc.) |
| StructuredResponse | JSON | Parsed evaluation fields extracted from the structured output |
| GroundingSources | JSON | URLs from grounding metadata |
| ChangeClass | string | "new" / "material" / "minor" / "downgrade" / null |
| Delivered | bool | Whether this was successfully posted to Discord |

**Validation**:
- Tier MUST be one of the three enum values
- WeatherSnapshot MUST contain the raw data used for evaluation
- RawLLMResponse MUST be non-empty
- PromptVersion MUST be non-empty

### UserProfile

Single-user configuration for the person receiving alerts.

| Field | Type | Notes |
| --- | --- | --- |
| ID | int64 | Always 1 for MVP (single user) |
| HomeBase | string | City name (e.g., "Denver, CO") |
| HomeLatitude | float64 | Home coordinates |
| HomeLongitude | float64 | Home coordinates |
| PassesHeld | JSON | Array of pass affiliations (e.g., ["ikon"]) |
| RemoteWorkCapable | bool | Can work remotely |
| TypicalPTODays | int | Available PTO days per year |
| BlackoutDates | JSON | Array of date ranges to avoid |
| MinTierForPing | string | Minimum tier for @here notification |
| QuietHoursStart | string | Time string (e.g., "22:00") |
| QuietHoursEnd | string | Time string (e.g., "07:00") |

### PromptTemplate

Versioned LLM prompt templates for evaluation.

| Field | Type | Notes |
| --- | --- | --- |
| ID | string | Template name (e.g., "storm_evaluation") |
| Version | string | Semantic version (e.g., "1.0.0") |
| Template | string | Prompt template text with placeholders |
| CreatedAt | timestamp | When this version was created |
| IsActive | bool | Whether this is the current active version |

**Validation**:
- Only one version per template name may have IsActive = true
- Templates contain placeholder tokens (e.g., `{{.WeatherData}}`)
  substituted at evaluation time

## SQLite Schema Notes

- All timestamps stored as ISO 8601 strings (UTC)
- JSON columns stored as TEXT with JSON validation
- WAL mode enabled for concurrent read access during writes
- Schema auto-created on first run via embedded migration SQL
- Indexes on: Storm(RegionID, State), Evaluation(StormID)
- Weather fetch logging handled by slog, not a DB table.
  Each evaluation's WeatherSnapshot captures the data used.
