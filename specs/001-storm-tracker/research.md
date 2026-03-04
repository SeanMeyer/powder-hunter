# Research: Automated Storm Tracker

**Feature Branch**: `001-storm-tracker`
**Date**: 2026-03-04

## Weather APIs

### Open-Meteo (Extended-Range, US + Canada)

**Decision**: Use Open-Meteo forecast API as the primary weather
source for all regions, providing 16-day forecasts.

**Rationale**: Free for non-commercial use, no API key required,
global coverage including Canada, 16-day forecast horizon. Uses
a seamless blend of HRRR (US, 3km), NAM (US, 3km), GFS (global,
13km), and CMC (Canada) models automatically.

**Alternatives considered**:
- Tomorrow.io: requires API key, rate-limited free tier
- WeatherAPI.com: 14-day max, less granular snowfall data
- Environment Canada Datamart: Canada-only, complex data format

**Key technical details**:
- Endpoint: `GET https://api.open-meteo.com/v1/forecast`
- Parameters: `latitude`, `longitude`, `daily=snowfall_sum`,
  `forecast_days=16`, `timezone`
- Snowfall in **centimeters** (divide by 2.54 for inches)
- `freezinglevel_height` available for snow-vs-rain determination
- Rate limits: 600/min, 5000/hr, 10000/day (free tier)
- Response: parallel arrays keyed by date, one request per region

### NWS api.weather.gov (Near-Range, US Only)

**Decision**: Use NWS as a supplementary high-accuracy source for
US regions in the 1-7 day window.

**Rationale**: Higher accuracy than global models for near-term US
forecasts. Free, no API key (just User-Agent header required).

**Alternatives considered**:
- Using Open-Meteo exclusively: simpler but loses NWS precision
  for actionable near-term forecasts
- NOAA NDFD: raw gridded data, harder to consume

**Key technical details**:
- Two-step process: `/points/{lat},{lon}` → resolve grid →
  `/gridpoints/{wfo}/{x},{y}` for raw data
- `snowfallAmount` (cm): reliable only 2-3 days out
- `detailedForecast` text: contains "X to Y inches" for 7 days
  but requires text parsing
- Cache `/points` results (grid mapping rarely changes)
- Coordinate precision: max 4 decimal places
- Rate limit: undocumented, recommend 1-second delay between
  requests. Use `If-Modified-Since` for conditional fetches.
- US only — Canadian coordinates return 404
- Occasional anomalous values (sanity-check all numbers)

**Approach**: Use NWS only for machine-readable `snowfallAmount`
(days 1-3). Skip text parsing of `detailedForecast` for days 4-7
— Open-Meteo already covers that range, and text parsing adds
brittle complexity for marginal gain. Always fetch
`probabilityOfPrecipitation` and temperature when available.

## LLM Integration

### Gemini 3 Flash with Google Search Grounding

**Decision**: Use Gemini 3 Flash Preview via
`google.golang.org/genai` SDK with single-call grounded +
structured output.

**Rationale**: Google Search grounding lets the LLM research live
conditions (lodging, flights, road closures) without a separate
search API. Gemini 3 Flash is the only model that supports
grounding + structured JSON output in a single call. The preview
risk is acceptable for a personal project — if it breaks, we fix
it (swap model ID or fall back to two-phase on 2.5 Flash).

**Alternatives considered**:
- Gemini 2.5 Flash: stable/GA but cannot combine grounding +
  structured output (requires two API calls per evaluation)
- Claude: no built-in web search grounding
- GPT-4o + Bing: more expensive, separate search API needed

**Key technical details**:
- SDK: `google.golang.org/genai` (GA, replaces legacy
  `github.com/google/generative-ai-go`)
- Model: `gemini-3-flash-preview`
- Auth: `GOOGLE_API_KEY` env var, `BackendGeminiAPI`
- Grounding: attach `GoogleSearch` tool in config; model
  autonomously decides when to search
- Structured output: use `ResponseMIMEType: "application/json"`
  + `ResponseSchema` with explicit `Required` fields
- Combined in one call: grounding + structured output work
  together on Gemini 3 (not on 2.5 Flash)
- Temperature: keep at default 1.0 (Google warns against tuning
  on Gemini 3 preview)
- Pricing: $0.50/1M input, $3/1M output
- At ~50 evaluations/day = negligible cost
- Grounding metadata available at
  `resp.Candidates[0].GroundingMetadata` — includes search
  queries used and source URLs

## Discord Output

### Forum Channel with Webhooks

**Decision**: Use a Discord Forum Channel with webhook-only
posting (no bot token required).

**Rationale**: Forum channels support `thread_name` in webhook
payloads, creating a new thread per storm. Updates go to existing
threads via `?thread_id=`. This eliminates the need for a bot
token or bot framework entirely.

**Alternatives considered**:
- Regular text channel + bot token: requires managing bot auth,
  more complex deployment
- Regular text channel + webhook only: cannot create threads
  (requires bot token for the thread creation API call)
- Discord bot framework (discordgo): overkill for one-way
  notifications

**Key technical details**:
- Webhook URL: single static URL from Server Settings
- New storm: `POST ?wait=true` with `thread_name` → response
  contains `channel_id` (= thread ID)
- Updates: `POST ?thread_id={stored_id}` with update embed
- `@here`: must be in `content` field (not embed) with
  `"allowed_mentions": {"parse": ["everyone"]}`
- Rich embeds: up to 25 fields, 4096 char description, color
  coding by tier
- Rate limit: 5 requests per 2 seconds per webhook
- Forum tags: if forum requires tags, must include
  `applied_tags` in payload or get 400
- Archived threads accept messages — Discord auto-unarchives

## Storage

### SQLite via modernc.org/sqlite

**Decision**: Use `modernc.org/sqlite` for pure-Go SQLite.

**Rationale**: Single-file database, zero operational overhead,
perfect for Docker/Unraid deployment. Pure Go means no CGO
dependency, simplifying cross-compilation and Docker builds.

**Alternatives considered**:
- mattn/go-sqlite3: requires CGO
- PostgreSQL: operational overhead for a single-user home server
- BoltDB/BadgerDB: key-value only, no relational queries

**Key technical details**:
- Import: `modernc.org/sqlite`
- Driver: registers as `"sqlite"` for `database/sql`
- WAL mode recommended for concurrent reads during writes
- Schema auto-creation on first run
- JSON functions available for querying resort metadata blob
