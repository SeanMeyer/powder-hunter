# Powder-ETL Design Document

**Date**: 2026-03-04
**Status**: Approved
**Branch**: `001-powder-hunter`

## Problem

Chasing powder is a complex decision with many interacting variables: snowfall quantity and quality, timing (weekday vs weekend), logistics (drive vs fly, lodging, car rental), cost (pass coverage, lift tickets, lodging), crowd levels, road/resort closures, and work flexibility (PTO vs remote). Today this analysis is manual — check OpenSnow, mentally weigh a dozen factors, and hope you don't miss anything. The Powderhorn problem: a surprise 2-foot dump at an off-pass resort that almost got missed because it wasn't in the usual filter.

## Solution

An automated storm tracker that scans weather forecasts across all US and Canadian ski resorts, evaluates promising storms using an LLM with live web research, and pushes rich briefings to Discord. Storms are tracked over time as forecasts evolve, with updates threaded in Discord so a developing storm is a single conversation, not repeated spam.

## Project Principles

### 1. Parse, Don't Validate

Types encode validity. A `ValidForecast` is different from raw API JSON. Once data crosses the boundary and is parsed into a domain type, downstream code never re-checks. Internal structs are supersets of data — they carry metadata, reasons, and sources so nothing needs to be re-derived.

### 2. I/O Sandwich

Business logic (storm evaluation, scoring, grouping) is pure. No `context.Context`, no DB handles, no HTTP clients in the core. The shell handles all I/O: fetching weather, calling Gemini, writing to SQLite, posting to Discord. Concurrency (parallel weather fetches) lives in the shell via `errgroup`.

### 3. Decisions Are Data

Every evaluation produces a rich result struct that captures not just the outcome (tier, recommendation) but *why* — which factors contributed, what the inputs were, what the LLM considered. These get persisted to SQLite so you can inspect the database and reconstruct exactly what happened on any run.

### 4. Observability Is Not Optional

Structured logging on every operation in the shell. The database is the primary audit log — every storm evaluation, every weather fetch, every Discord post is recorded with enough context to debug "why didn't I get an alert?" or "why was this rated WORTH A LOOK instead of DROP EVERYTHING?"

### 5. Production Quality

This is not a script. Clean module boundaries, proper error handling, tests, domain-organized packages. Code should read like a well-maintained production system even though it runs on a home server.

### 6. Domain-Organized

Packages organized by what they do (weather, evaluation, discord, storage), not by type (models, controllers, utils). Small interfaces, concrete return types. No unnecessary abstractions.

## Data Model

### Resorts (static reference data)

- Name, coordinates (lat/lon), elevation
- Pass affiliation: Ikon, Epic, Indy, independent
- Region ID (foreign key to Regions)
- Metadata: vertical drop, number of lifts, proximity to nearest metro, reputation notes (e.g., "skis off fast", "excellent tree skiing", "back bowls hold powder well")

### Regions (predefined ski area clusters)

Resorts that are close enough to represent the same trip decision are grouped into a region. A-Basin and Winter Park are in the same region (Summit County / I-70 corridor). Winter Park and Aspen are different regions despite being in the same state and potentially the same weather system.

- Name (e.g., "Summit County", "Stevens Pass Area", "Wasatch Front")
- Resort IDs belonging to this region
- Representative coordinates (for weather API calls — one per region, not per resort)
- Logistics metadata: nearest airports, typical drive time from user's home base, general lodging notes

### Storms (lifecycle-tracked entities)

The central entity. A storm is identified by region + date window.

- Region ID + storm window (start/end dates)
- Lifecycle state: `detected` -> `evaluated` -> `briefed` -> `updated` -> `expired`
- Evaluation history: timestamped array of assessments, each containing:
  - Weather data snapshot (what the forecast said at evaluation time)
  - LLM evaluation result (tier, score factors, recommendation, logistics research)
  - Raw LLM response (for debugging)
- Current tier and plain-language recommendation
- Discord thread ID (for threaded updates)
- Detection source: which forecast range (extended vs near-term)

### User Profile & Configuration (in SQLite, not config files)

Stored in the database for future extensibility (Discord bot commands, calendar integration, multiple users).

- Home base location (city, coordinates)
- Passes held (list of pass affiliations)
- Work flexibility: remote work capable, typical PTO availability, blackout dates
- Alert preferences: minimum tier for `@here` ping, quiet hours, excluded regions
- Thresholds: extended-range snowfall trigger, near-range snowfall trigger (configurable per-region if desired)

## Architecture

### Tech Stack

- **Language**: Go — single binary, clean deployment, good HTTP/JSON support, developer familiarity
- **Database**: SQLite — single file, zero-ops, perfect for Unraid Docker deployment
- **Weather APIs**: Open-Meteo (16-day forecast, US + Canada) + NWS (7-day precision, US only)
- **LLM**: Gemini with Google Search grounding — built-in web search for lodging, flights, road conditions without a separate search API
- **Output**: Discord webhook with rich embeds and threaded updates
- **Runtime**: Docker container on Unraid, triggered by cron (2x daily)

### Pipeline (per cron run)

```
┌─────────────────────────────────────────────────────┐
│                    CRON TRIGGER                      │
│                  (2x daily, e.g. 6am/6pm)           │
└──────────────────────┬──────────────────────────────┘
                       │
                       v
┌─────────────────────────────────────────────────────┐
│              STAGE 1: WEATHER SCAN                  │
│           (deterministic, no LLM)                   │
│                                                     │
│  For each region:                                   │
│    - Open-Meteo 16-day forecast (all regions)       │
│    - NWS 7-day forecast (US regions only)           │
│                                                     │
│  Flag regions where:                                │
│    - Extended (8-16 day): snowfall > high threshold  │
│    - Near-term (1-7 day): snowfall > lower threshold │
│    - Already tracked: always re-fetch               │
│                                                     │
│  Output: list of regions needing evaluation         │
└──────────────────────┬──────────────────────────────┘
                       │
                       v
┌─────────────────────────────────────────────────────┐
│            STAGE 2: LLM EVALUATION                  │
│        (Gemini + Google Search grounding)           │
│                                                     │
│  For each flagged region, prompt includes:          │
│    - Raw weather data (snowfall, temps, wind, viz)  │
│    - Region metadata (resorts, passes, elevation)   │
│    - User profile (home base, passes, flexibility)  │
│    - Prior evaluation history (if re-evaluation)    │
│                                                     │
│  Gemini researches live:                            │
│    - Lodging availability and prices                │
│    - Flight costs (if fly-to destination)           │
│    - Road conditions and closure risks              │
│    - Car rental logistics                           │
│                                                     │
│  Returns structured JSON:                           │
│    - Tier (DROP EVERYTHING / WORTH A LOOK /         │
│      ON THE RADAR)                                  │
│    - Plain-language recommendation                  │
│    - Day-by-day breakdown                           │
│    - Key factors (pros and cons)                    │
│    - Logistics summary                              │
│    - Recommended strategy (timing, travel, PTO mix) │
│    - Snow quality assessment                        │
│    - Crowd estimate                                 │
│    - Closure risk assessment                        │
└──────────────────────┬──────────────────────────────┘
                       │
                       v
┌─────────────────────────────────────────────────────┐
│          STAGE 3: COMPARE & DECIDE                  │
│                                                     │
│  For each evaluated storm:                          │
│    - Compare to prior evaluation (if exists)        │
│    - Determine: new storm? material change?         │
│      minor update? downgrade?                       │
│    - Decide notification level:                     │
│      @here ping / silent post / thread-only         │
└──────────────────────┬──────────────────────────────┘
                       │
                       v
┌─────────────────────────────────────────────────────┐
│          STAGE 4: DISCORD POST & PERSIST            │
│                                                     │
│  New storm:                                         │
│    - Post rich embed to channel                     │
│    - Create thread for updates                      │
│    - Store Discord thread ID in DB                  │
│                                                     │
│  Storm update:                                      │
│    - Post update in existing thread                 │
│    - Include what changed since last evaluation     │
│                                                     │
│  Persist to SQLite:                                 │
│    - Full evaluation result with all inputs/outputs │
│    - Updated storm lifecycle state                  │
│    - Weather data snapshot                          │
│    - Discord message IDs                            │
└─────────────────────────────────────────────────────┘
```

### Weather Source Strategy

| Forecast Range | Source | Coverage | Purpose |
|---|---|---|---|
| Days 1-7 | NWS (`api.weather.gov`) | US only | High-accuracy actionable forecasts |
| Days 1-16 | Open-Meteo | US + Canada | Extended detection + Canada coverage |

- Extended range (8-16 days): Higher snowfall threshold to flag. Storms enter as `detected` with low confidence. Purpose is early awareness — "something is brewing."
- Near range (1-7 days): Lower threshold, full deep evaluation. Forecasts are actionable.
- As a storm moves from extended into near range, it gets re-evaluated with better data.

### Tiers

Three tiers, designed for instant gut-level readability:

| Tier | Meaning | Notification |
|---|---|---|
| **DROP EVERYTHING** | Perfect alignment — great snow, logistics work, timing is right. Act now. | `@here` ping |
| **WORTH A LOOK** | Something interesting — worth reading the briefing, but friction or uncertainty exists. | Silent channel post |
| **ON THE RADAR** | Storm happening but probably not worth chasing. Awareness only. | Thread update only |

No numeric scores. The LLM assigns a tier and writes a natural-language recommendation explaining its reasoning. The tier is the quick-glance signal; the recommendation is the substance.

### Discord Output Format

**New storm — channel message (rich embed):**

```
STORM ALERT: Summit County
Tier: WORTH A LOOK  |  Confidence: High (3-day forecast)

Mar 8-10  |  14-18" expected  |  Cold temps (light powder)
Resorts: A-Basin, Keystone, Breck, Copper
Pass: Ikon (covered)

Best day: Tuesday Mar 11 (clearing day, weekday)
Strategy: Remote work Mon, PTO Tue

Key factors:
  + Cold temps = light, dry powder
  + Weekday clearing day = low crowds
  + A-Basin back bowls hold powder well
  - I-70 will be messy Mon evening

Full briefing in thread below.
```

**Storm update — in existing thread:**

```
UPDATE (Mar 7, 6am) — Forecast firmed up.

Snowfall revised UP to 18-22". Clearing day still
Tuesday. Silverthorne Airbnb ~$120/night with wifi.
Confidence: High.

Tier change: WORTH A LOOK -> DROP EVERYTHING
```

**Notification rules:**
- `@here`: New storm at DROP EVERYTHING, or upgrade to DROP EVERYTHING
- Silent post: New storm at WORTH A LOOK, or any significant change
- Thread-only: Minor forecast adjustments, ON THE RADAR storms
- Downgrade/expiry: Note in thread, no ping

## Scope

### MVP (this build)

- Weather scanning for all US + Canada regions (Open-Meteo + NWS)
- Storm detection, tracking, and lifecycle management
- LLM evaluation with web-grounded research (Gemini)
- Discord webhook output with rich embeds and threaded updates
- SQLite persistence for all data, config, and evaluation history
- Predefined region database with resort metadata
- Single-user configuration
- Docker container for Unraid deployment

### Future (explicitly out of scope)

- Discord bot with interactive commands (slash commands for config, follow-up questions)
- Surprise dump detection (morning-after actual snowfall reports)
- Calendar integration (auto-check PTO availability, block dates)
- Multiple user profiles with independent preferences
- Web UI or mobile app
- Feedback mechanism ("this alert wasn't useful") for tuning
