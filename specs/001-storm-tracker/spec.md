# Feature Specification: Automated Storm Tracker

**Feature Branch**: `001-storm-tracker`
**Created**: 2026-03-04
**Status**: Draft
**Input**: Design doc and implementation plan for powder-hunter

## User Scenarios & Testing *(mandatory)*

### User Story 1 — Weather Scanning & Storm Detection (Priority: P1)

As a skier, I want the system to automatically scan weather
forecasts across all US and Canadian ski regions so that promising
storms are detected without me checking OpenSnow manually.

**Why this priority**: Without weather scanning and detection,
nothing else in the pipeline works. This is the foundational
data-gathering step that feeds every downstream feature.

**Independent Test**: Run the scanner against a set of known
forecast data (faked). Verify that regions exceeding snowfall
thresholds are flagged, and regions below thresholds are not.

**Acceptance Scenarios**:

1. **Given** a local-tier region with 16-day forecast showing 14"
   in the 8-16 day window (exceeds 12" extended threshold),
   **When** the scanner runs, **Then** the region is flagged for
   evaluation with state `detected`.
2. **Given** a flight-tier region with forecast showing 18" in
   the 8-16 day window (below 36" extended threshold), **When**
   the scanner runs, **Then** the region is NOT flagged.
3. **Given** a regional-driveable region with 3-day forecast
   showing 16" (exceeds 14" near threshold), **When** the scanner
   runs, **Then** the region is flagged.
4. **Given** a storm already being tracked in a region, **When**
   the scanner runs, **Then** the region is always re-fetched
   regardless of snowfall thresholds.
5. **Given** a US region, **When** the scanner runs, **Then** both
   extended-range and near-range forecast sources are consulted.
6. **Given** a Canadian region, **When** the scanner runs, **Then**
   only the extended-range forecast source is consulted (NWS does
   not cover Canada).

---

### User Story 2 — LLM Storm Evaluation (Priority: P2)

As a skier, I want each detected storm to be evaluated by an LLM
that researches live conditions (lodging, flights, road closures)
and assigns a tier (DROP EVERYTHING / WORTH A LOOK / ON THE RADAR)
with a plain-language recommendation, so I get actionable
intelligence, not just raw snowfall numbers.

**Why this priority**: Detection without evaluation is just a
weather alert. The LLM evaluation transforms raw data into a
decision-support briefing — the core value proposition.

**Independent Test**: Feed the evaluator a faked weather snapshot
and region metadata. Verify it produces a structured result with
tier, recommendation, day-by-day breakdown, key factors, and
logistics summary.

**Acceptance Scenarios**:

1. **Given** a flagged region with weather data and user profile,
   **When** evaluation runs, **Then** a structured result is
   produced containing: tier, recommendation, day-by-day breakdown,
   key factors (pros/cons), logistics summary, recommended strategy,
   snow quality assessment, crowd estimate, and closure risk.
2. **Given** a storm being re-evaluated, **When** evaluation runs,
   **Then** prior evaluation history is included in the prompt so
   the LLM can identify what changed.
3. **Given** the user holds an Ikon pass, **When** a storm hits an
   Ikon-covered resort, **Then** the evaluation reflects that
   lift tickets are covered (reducing cost friction).
4. **Given** evaluation completes, **When** the result is persisted,
   **Then** the full evaluation (inputs, outputs, raw LLM response)
   is stored for later inspection.

---

### User Story 3 — Storm Lifecycle & Change Detection (Priority: P3)

As a skier, I want storms to be tracked over time as forecasts
evolve, with the system detecting material changes (tier upgrades,
significant snowfall revisions) versus minor adjustments, so I'm
notified about important developments without being spammed.

**Why this priority**: Without lifecycle tracking, every scan
produces duplicate alerts. This story turns raw detections into
a coherent conversation about a developing storm.

**Independent Test**: Simulate a storm through multiple evaluation
cycles with changing forecasts. Verify lifecycle state transitions
and correct change classification (new / material change / minor
update / downgrade).

**Acceptance Scenarios**:

1. **Given** a storm evaluated for the first time, **When** the
   comparison step runs, **Then** it is classified as "new storm."
2. **Given** a storm previously rated WORTH A LOOK now rated DROP
   EVERYTHING, **When** comparison runs, **Then** it is classified
   as "material change" (tier upgrade).
3. **Given** a storm with snowfall revised from 14" to 15", **When**
   comparison runs, **Then** it is classified as "minor update."
4. **Given** a storm whose forecast has degraded below thresholds,
   **When** comparison runs, **Then** the storm transitions to
   `expired` state.
5. **Given** a storm in `detected` state that moves into the 7-day
   window, **When** the scanner runs, **Then** it gets re-evaluated
   with higher-accuracy near-range data and transitions to
   `evaluated`.

---

### User Story 4 — Discord Briefings & Threaded Updates (Priority: P4)

As a skier, I want storm briefings delivered to a Discord channel
with rich formatting, and subsequent updates threaded under the
original alert, so a developing storm is a single conversation.

**Why this priority**: This is the delivery mechanism. Without it
the system produces evaluations but nobody sees them. It's P4
because the pipeline must work end-to-end before the output
channel matters.

**Independent Test**: Feed the Discord poster a faked evaluation
result. Verify it produces correctly formatted embed content and
that updates reference the correct thread.

**Acceptance Scenarios**:

1. **Given** a new storm classified as DROP EVERYTHING, **When**
   the Discord poster runs, **Then** a rich embed is posted to
   the channel with an `@here` ping, and a thread is created for
   updates.
2. **Given** a new storm classified as WORTH A LOOK, **When**
   posted, **Then** a rich embed is posted without `@here` ping.
3. **Given** an existing storm with a stored thread ID, **When**
   an update is posted, **Then** it appears in the existing thread
   (not as a new channel message).
4. **Given** a storm upgraded to DROP EVERYTHING, **When** the
   update posts, **Then** the thread update includes an `@here`
   ping.
5. **Given** a storm downgrading or expiring, **When** posted,
   **Then** a note appears in the thread with no ping.

---

### User Story 5 — Cron-Driven Pipeline Orchestration (Priority: P5)

As a skier, I want the entire pipeline (scan → evaluate → compare
→ post) to run automatically twice daily via cron inside a Docker
container on my home server, with no manual intervention required.

**Why this priority**: Orchestration ties the other stories
together into an automated system. It's last because each stage
must work independently before wiring them into a pipeline.

**Independent Test**: Run the full pipeline with all external
systems faked. Verify that stages execute in order, errors in one
region don't prevent processing of other regions, and all results
are persisted.

**Acceptance Scenarios**:

1. **Given** the container starts via cron, **When** the pipeline
   runs, **Then** all four stages execute in sequence (scan →
   evaluate → compare → post).
2. **Given** a weather API error for one region, **When** the
   pipeline runs, **Then** other regions continue processing and
   the error is logged with full context.
3. **Given** the pipeline completes, **When** results are
   inspected, **Then** every operation (fetch, evaluation, post)
   is recorded in the database with timestamps and context.
4. **Given** no regions exceed snowfall thresholds, **When** the
   pipeline runs, **Then** it completes with only scan results
   persisted and no evaluations or posts.

---

### Edge Cases

- What happens when the weather API returns empty or malformed
  data for a region? System MUST log the error with region
  context and skip that region without crashing.
- What happens when the LLM returns an unparseable response?
  System MUST log the raw response, skip evaluation for that
  storm, and continue processing other storms.
- What happens when Discord is unreachable? System MUST retry
  delivery a limited number of times within the same run (to
  handle transient failures). If still unreachable, persist the
  evaluation result, mark it as undelivered in the database, and
  log the failure. The next run produces a fresh evaluation
  naturally — stale briefings are NOT re-sent.
- What happens when two distinct weather systems hit the same
  region back-to-back (e.g., one ending Monday, another starting
  Wednesday)? They MUST be tracked as separate storms with
  non-overlapping date windows. (Note: if date windows DO
  overlap, they merge per the storm identity rule.)
- What happens when the database file doesn't exist on first
  run? System MUST create the schema automatically.
- What happens when a storm spans more than 16 days? The storm's
  date window expands as new overlapping detections arrive.
- What happens when two detections in the same region have
  overlapping date windows? They are merged into a single storm
  with the union of both date ranges.

## Clarifications

### Session 2026-03-04

- Q: How is the same storm recognized across runs when forecast windows shift? → A: Overlapping date range matching — if a new detection's window overlaps an existing storm's window in the same region, they merge (window expands to the union).
- Q: What are the default snowfall thresholds? → A: Tiered by region friction level. Local: 6-8" near / 12" extended. Regional driveable: 14" near / 18-20" extended. Flight mission: 24" near / 36" extended. Rationale: higher friction requires a bigger storm to justify the cost and logistics.
- Q: When Discord is unreachable, should undelivered briefings be queued for next run? → A: No. Retry within the same run for transient failures, but do not re-send stale briefings. Mark undelivered in DB. Next run produces fresh data naturally.
- Q: How many regions should be seeded? → A: Comprehensive (~40-50) covering every meaningful ski region in US and Canada. Weather API cost is per-region (cheap), LLM cost only hits regions that cross thresholds.
- Q: Should LLM evaluations run sequentially or in parallel? → A: Parallel with concurrency cap (3-5 simultaneous). Balances speed against rate limits and cost predictability.
- Q: What resort metadata do we store and how flexible is it? → A: Fixed core columns (name, coords, elevation, pass, vertical drop, lifts) that code uses programmatically + structured JSON metadata blob for extensible LLM-facing context (reputation, terrain, etc.). New fields added without schema migration. Both serialized into LLM prompts.
- Q: How are tiers assigned — LLM judgment or deterministic scoring? → A: Pure LLM judgment. No numeric scores. The prompt serves as the scoring rubric, enumerating all factors the LLM must consider (snow quality, timing, logistics, cost, crowds, terrain, user flexibility). Tier definitions are explicit in the prompt for consistency. Raw reasoning persisted per Decisions Are Data.
- Q: How are LLM prompts managed and iterated over time? → A: Versioned templates + replay. Prompts stored as named/versioned templates, each evaluation records prompt version used, CLI replay command re-runs past storm data through a new prompt version for comparison. No full A/B testing for MVP.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST scan weather forecasts for all
  predefined US and Canadian ski regions on each run.
- **FR-002**: System MUST flag regions where forecasted snowfall
  exceeds thresholds that vary by both forecast range AND the
  region's friction tier (local / regional-driveable / flight).
  Default thresholds:

  | Friction Tier        | Near-range (1-7 day) | Extended-range (8-16 day) |
  | -------------------- | -------------------- | ------------------------- |
  | Local                | 8"                   | 12"                       |
  | Regional driveable   | 14"                  | 20"                       |
  | Flight mission       | 24"                  | 36"                       |
- **FR-003**: System MUST always re-fetch weather for regions
  with actively tracked storms, regardless of thresholds.
- **FR-004**: System MUST evaluate each flagged region using an
  LLM with live web search grounding, producing a structured
  result with tier, recommendation, and supporting analysis.
  Tier assignment is pure LLM judgment (no numeric scoring) guided
  by a detailed prompt that MUST enumerate specific evaluation
  factors including at minimum: snowfall quantity and quality
  (temperature, density), timing (weekday vs weekend, clearing
  day availability), logistics (drive time or flight cost, lodging
  availability and price, car rental needs, road closure risk),
  cost (pass coverage, lift ticket price if off-pass), crowd
  expectations, user's work flexibility (PTO vs remote), terrain
  suitability for powder (tree skiing, steeps, bowls), and
  historical resort reputation. The prompt MUST define what each
  tier means so assignments are consistent across evaluations.
  Evaluations MUST run in parallel with a concurrency cap
  (default 3-5 simultaneous) to balance speed against rate
  limits and cost.
- **FR-005**: System MUST include the user's profile (home base,
  passes held, work flexibility) in every evaluation prompt.
- **FR-006**: System MUST persist every evaluation result with
  full inputs, outputs, raw LLM response, and the prompt template
  version used.
- **FR-017**: LLM prompts MUST be stored as named, versioned
  templates (not hardcoded strings). Each template has a version
  identifier recorded with every evaluation that uses it.
- **FR-018**: System MUST support a CLI replay command that
  re-runs a past storm's persisted weather data and context
  through a different prompt template version, producing a
  comparison result without posting to Discord. This enables
  prompt iteration by comparing old vs new prompt output on
  real historical data.
- **FR-007**: System MUST track storm lifecycle states:
  `detected` → `evaluated` → `briefed` → `updated` → `expired`.
- **FR-008**: System MUST compare new evaluations against prior
  evaluations for the same storm to classify changes as new,
  material, minor, or downgrade.
- **FR-009**: System MUST post new storm briefings to Discord
  as rich embeds with tier-appropriate notification levels
  (DROP EVERYTHING = `@here`, WORTH A LOOK = silent post,
  ON THE RADAR = thread-only).
- **FR-010**: System MUST thread all updates for a storm under
  the original Discord message.
- **FR-011**: System MUST store Discord thread IDs for ongoing
  storm conversations.
- **FR-012**: System MUST run as a single command invocation
  suitable for cron scheduling.
- **FR-013**: System MUST handle errors in one region without
  aborting processing of other regions.
- **FR-014**: System MUST log every I/O operation with structured
  fields sufficient to diagnose "why didn't I get an alert?"
- **FR-015**: System MUST store a predefined database of ski
  regions with resort metadata. Each resort has fixed core fields
  (name, coordinates, elevation, pass affiliation, vertical drop,
  number of lifts) plus a structured JSON metadata blob for
  extensible context (reputation, terrain notes, etc.). Both core
  fields and JSON metadata are included in LLM evaluation prompts.
- **FR-016**: System MUST support single-user configuration
  stored in the database (home base, passes, work flexibility,
  alert preferences).

### Key Entities

- **Region**: A cluster of geographically close ski resorts
  representing a single trip decision. Has a name, representative
  coordinates (for weather lookups), a list of resorts, and a
  friction tier (local / regional-driveable / flight) that
  determines snowfall detection thresholds.
- **Resort**: An individual ski area within a region. Has fixed
  core fields the system logic uses (name, coordinates, elevation,
  pass affiliation, vertical drop, number of lifts) plus a
  structured JSON metadata blob for extensible LLM-facing context
  (reputation, terrain character, tree skiing quality, access
  notes, etc.). New metadata fields can be added without schema
  migrations. Both core fields and JSON metadata are serialized
  into the LLM evaluation prompt.
- **Storm**: The central tracked entity, identified by region +
  date window. If a new detection's date window overlaps an
  existing storm's window in the same region, they are the same
  storm (the window expands to encompass both). Has a lifecycle
  state, evaluation history, current tier, and Discord thread
  reference.
- **Evaluation**: A timestamped assessment of a storm, containing
  weather data snapshot, LLM result (tier, recommendation, factors,
  logistics), and raw LLM response.
- **User Profile**: Configuration for the person receiving alerts —
  home base, passes held, work flexibility, alert preferences,
  snowfall thresholds.

### Assumptions

- Single user for MVP. Multi-user support is explicitly out of
  scope.
- Regions and resorts are predefined in the database (seeded on
  first run), not user-configurable in MVP. Comprehensive coverage:
  ~40-50 regions spanning all meaningful US and Canadian ski areas.
  Region seed data quality is important and worth investing time in.
- Cron scheduling is handled externally (Docker/Unraid cron);
  the system is a run-once command, not a long-running daemon.
- Snowfall thresholds have sensible defaults but are configurable
  per user profile.
- "Material change" in storm comparison means a tier change or
  snowfall revision exceeding a configurable delta (default: ±4"
  / 10cm). Minor updates are changes below this delta with no
  tier change.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: System detects storms that produce 12"+ snowfall at
  any tracked region within 24 hours of the forecast becoming
  available — zero missed storms above threshold.
- **SC-002**: Storm briefings reach the Discord channel within 10
  minutes of the pipeline starting.
- **SC-003**: Every storm briefing includes actionable logistics
  (best day to ski, travel strategy, cost factors) — not just
  snowfall totals.
- **SC-004**: Storm updates appear threaded under the original
  alert 100% of the time — no duplicate top-level messages for
  the same storm.
- **SC-005**: The system runs unattended for 30 consecutive days
  without manual intervention or crashes.
- **SC-006**: Any past storm evaluation can be fully reconstructed
  from the database alone — inputs, outputs, and reasoning.
- **SC-007**: A weather API failure for one region does not prevent
  processing of other regions in the same run.
