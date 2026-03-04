# Tasks: Automated Storm Tracker

**Input**: Design documents from `/specs/001-storm-tracker/`
**Prerequisites**: plan.md, spec.md, data-model.md, contracts/cli.md, research.md

**Tests**: Sociable tests at the pipeline boundary per constitution
(Test Discipline principle). No isolated unit tests unless a
subsystem has genuinely complex edge-case logic.

**Organization**: Tasks grouped by user story for independent
implementation and testing.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1-US5)
- All paths relative to repository root

---

## Phase 1: Setup

**Purpose**: Project initialization and Go module structure

- [x] T001 Initialize Go module and install dependencies (`go mod init`, add `google.golang.org/genai`, `modernc.org/sqlite`, `golang.org/x/sync`) in go.mod
- [x] T002 Create directory structure per plan: cmd/powder-hunter/, domain/, weather/, evaluation/, discord/, storage/, pipeline/, seed/
- [x] T003 Implement CLI entry point with run() pattern and subcommand routing (run, replay, seed, profile) in cmd/powder-hunter/main.go

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Domain types, storage layer, seed data, and interface
definitions that ALL user stories depend on

**CRITICAL**: No user story work can begin until this phase is
complete

### Domain Types (pure, no I/O)

- [x] T004 [P] Define Region and Resort types with FrictionTier enum in domain/resort.go
- [x] T005 [P] Define Forecast and SnowfallWindow types (parsed weather data) in domain/weather.go
- [x] T006 [P] Define Tier enum (DROP_EVERYTHING, WORTH_A_LOOK, ON_THE_RADAR) and notification rules in domain/tier.go
- [x] T007 [P] Define Storm type with StormState enum and lifecycle state machine in domain/storm.go
- [x] T008 [P] Define Evaluation result type with all structured fields in domain/evaluation.go
- [x] T009 [P] Define UserProfile and AlertPreferences types in domain/profile.go

### Storage Layer

- [x] T010 Implement SQLite connection with WAL mode, schema auto-creation from embedded SQL in storage/sqlite.go
- [x] T011 [P] Implement Region and Resort CRUD and seed upsert in storage/regions.go
- [x] T012 [P] Implement Storm CRUD with overlapping-window merge logic in storage/storms.go
- [x] T013 [P] Implement Evaluation persistence (save, get latest, get history) in storage/evaluations.go
- [x] T014 [P] Implement UserProfile read/write in storage/profiles.go
- [x] T015 [P] Implement PromptTemplate versioned storage (save, get active, list versions) in storage/prompts.go

### Interface Definitions

- [x] T016 [P] Define WeatherFetcher interface in weather/weather.go
- [x] T017 [P] Define Evaluator interface in evaluation/evaluation.go
- [x] T018 [P] Define Poster interface (PostNew, PostUpdate) in discord/webhook.go
- [x] T019 [P] Define Store interface aggregating all storage operations in storage/sqlite.go

### Seed Data

- [x] T020 Build comprehensive region and resort seed data (~40-50 regions with resorts, friction tiers, coordinates, metadata JSON) in seed/regions.go
- [x] T021 Implement `powder-hunter seed` CLI command wiring (calls storage seed upsert, creates default user profile if none exists) in cmd/powder-hunter/main.go
- [x] T022 Implement `powder-hunter profile` CLI command (view/update user profile) in cmd/powder-hunter/main.go

### Fakes for Testing

- [x] T023 [P] Implement fake WeatherFetcher (returns preconfigured Forecast data) in weather/fake.go
- [x] T024 [P] Implement fake Evaluator (returns preconfigured Evaluation results) in evaluation/fake.go
- [x] T025 [P] Implement fake Poster (records posted embeds and thread IDs) in discord/fake.go

**Checkpoint**: All domain types defined, storage works, seed data
loadable, fakes ready. User story implementation can begin.

---

## Phase 3: User Story 1 — Weather Scanning & Storm Detection (Priority: P1) MVP

**Goal**: Scan weather forecasts for all regions, flag regions
exceeding friction-tiered snowfall thresholds, create/merge storms

**Independent Test**: Run scanner with fake weather data. Verify
regions above threshold are flagged (local 12"+, regional 14"+,
flight 24"+), regions below are not, and tracked storms always
re-fetch.

### Implementation

- [ ] T026 [P] [US1] Implement Open-Meteo client: fetch 16-day forecast, parse snowfall_sum (cm) into domain Forecast type in weather/openmeteo.go
- [ ] T027 [P] [US1] Implement NWS client: two-step points→gridpoints, parse snowfallAmount for days 1-3, cache grid lookups in weather/nws.go
- [ ] T028 [US1] Implement WeatherService that aggregates Open-Meteo + NWS (both for US, Open-Meteo only for CA), parallel fetch via errgroup in weather/weather.go
- [ ] T029 [US1] Implement pure detection logic: compare forecast snowfall against region's friction-tier thresholds, return list of flagged regions in domain/detection.go
- [ ] T030 [US1] Implement storm creation and overlapping-window merge: check existing active storms, create new or expand window in storage/storms.go (extend T012)
- [ ] T031 [US1] Wire scanner stage: query active storms from DB and always include their regions in fetch list (FR-003), fetch all regions → detect → create/merge storms → persist, with per-region error isolation and slog logging in pipeline/pipeline.go

**Checkpoint**: `powder-hunter run --dry-run` scans weather for all
regions, detects storms, persists to DB. No evaluation or Discord
posting yet.

---

## Phase 4: User Story 2 — LLM Storm Evaluation (Priority: P2)

**Goal**: Evaluate detected storms via Gemini 3 Flash with
grounded web search, producing structured tier + recommendation

**Independent Test**: Feed evaluator faked weather snapshot and
region metadata. Verify structured result contains tier,
recommendation, day-by-day, key factors, logistics, and all
required fields.

### Implementation

- [ ] T032 [US2] Create initial storm evaluation prompt template v1.0.0 with tier definitions, evaluation factors (snow quality, timing, logistics, cost, crowds, terrain, work flexibility, resort reputation), and structured output schema. Store as seed PromptTemplate row in seed/prompts.go
- [ ] T033 [US2] Implement prompt template rendering: load active template, substitute weather data, region metadata, user profile, and evaluation history placeholders in evaluation/prompt.go
- [ ] T034 [US2] Implement Gemini 3 Flash client: single call with GoogleSearch tool + ResponseSchema, parse structured response + grounding metadata in evaluation/gemini.go
- [ ] T035 [US2] Implement evaluation orchestration: load profile, load prompt, call Gemini with concurrency cap (errgroup semaphore, 3-5 simultaneous), persist full result in pipeline/pipeline.go (extend T031)
- [ ] T036 [US2] Implement evaluation persistence: save structured result, weather snapshot, raw response, grounding sources, prompt version in storage/evaluations.go (extend T013)

**Checkpoint**: `powder-hunter run --dry-run` scans weather AND
evaluates flagged storms with Gemini. Results persisted to DB with
full audit trail. No Discord posting yet.

---

## Phase 5: User Story 3 — Storm Lifecycle & Change Detection (Priority: P3)

**Goal**: Track storms over time, classify changes between
evaluations (new / material / minor / downgrade), manage state
transitions

**Independent Test**: Simulate a storm through multiple evaluation
cycles with changing forecasts. Verify correct change
classification and state transitions.

### Implementation

- [ ] T037 [P] [US3] Implement pure comparison logic: given previous and current evaluation, classify as new/material/minor/downgrade based on tier change and snowfall delta in domain/comparison.go
- [ ] T038 [P] [US3] Implement storm state transition logic: enforce state machine (detected→evaluated→briefed→updated→expired), validate transitions in domain/storm.go (extend T007)
- [ ] T039 [US3] Integrate comparison into pipeline: after evaluation, load prior evaluation, run comparison, set ChangeClass on result, update storm state in pipeline/pipeline.go (extend T035)
- [ ] T040 [US3] Handle storm expiration: when forecast degrades below thresholds for a tracked storm, transition to expired state in pipeline/pipeline.go

**Checkpoint**: Pipeline tracks storms across runs, detects
material changes, manages lifecycle states. Full evaluation
history viewable in DB.

---

## Phase 6: User Story 4 — Discord Briefings & Threaded Updates (Priority: P4)

**Goal**: Post storm briefings as rich embeds to Discord forum
channel with threaded updates per storm

**Independent Test**: Feed poster a faked evaluation result.
Verify correct embed formatting, thread creation for new storms,
thread updates for existing storms, and tier-appropriate @here
pings.

### Implementation

- [ ] T041 [P] [US4] Implement pure embed formatter: evaluation → Discord embed struct with tier-colored fields, key factors, logistics summary, strategy in discord/formatter.go
- [ ] T042 [P] [US4] Implement notification level logic: tier + change class → @here ping / silent post / thread-only in discord/formatter.go
- [ ] T043 [US4] Implement webhook client: POST to forum channel with thread_name (new storm) or thread_id (update), handle @here via content + allowed_mentions, retry on transient failure in discord/webhook.go
- [ ] T044 [US4] Implement Discord thread ID persistence: save thread ID to storm after initial post, load for updates in storage/storms.go (extend T012)
- [ ] T045 [US4] Integrate Discord posting into pipeline: after comparison, post new storms or updates based on change class, mark delivered/undelivered in pipeline/pipeline.go (extend T039)

**Checkpoint**: Full pipeline runs end-to-end: scan → evaluate →
compare → post to Discord. Storm threads created and updated
correctly.

---

## Phase 7: User Story 5 — Pipeline Orchestration & Deployment (Priority: P5)

**Goal**: Wire the full pipeline as a single cron-ready command
in a Docker container with the replay CLI command

**Independent Test**: Run full pipeline with all external systems
faked. Verify stages execute in order, errors in one region don't
block others, all results persisted.

### Implementation

- [ ] T046 [US5] Implement `powder-hunter run` CLI command: parse flags (--db, --dry-run, --region, --verbose), configure slog, initialize all real clients, invoke pipeline in cmd/powder-hunter/main.go (extend T003)
- [ ] T047 [US5] Implement `powder-hunter replay` CLI command: load evaluation from DB, re-run through specified prompt version, output result without posting in cmd/powder-hunter/main.go
- [ ] T048 [US5] Implement pipeline-level error isolation: each region processed independently, errors collected and logged, partial results persisted in pipeline/pipeline.go (extend T045)
- [ ] T049 [US5] Write sociable tests at pipeline boundary: full pipeline with fake weather, fake evaluator, fake poster, real SQLite. Test happy path, threshold filtering, error isolation, storm lifecycle in pipeline/pipeline_test.go
- [ ] T050 [US5] Create Dockerfile: multi-stage build (Go build → scratch/alpine), expose --db volume mount, document env vars in Dockerfile

**Checkpoint**: System is fully functional and deployable. Docker
container runs via cron on Unraid.

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Quality, documentation, and operational readiness

- [ ] T051 Review and refine storm evaluation prompt template v1.0.0 content for completeness and consistency of tier assignments
- [ ] T052 Review region seed data quality: verify coordinates, friction tiers, resort metadata accuracy for all ~40-50 regions
- [ ] T053 Run quickstart.md validation: follow quickstart steps from scratch, verify all commands work
- [ ] T054 [P] Add edge-case unit tests for detection threshold logic (many friction tier × forecast range combinations) in domain/detection_test.go
- [ ] T055 [P] Add edge-case unit tests for storm comparison logic (tier changes, snowfall deltas, boundary cases) in domain/comparison_test.go

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 — BLOCKS all user stories
- **US1 (Phase 3)**: Depends on Phase 2 — MVP milestone
- **US2 (Phase 4)**: Depends on Phase 2 + US1 (needs storms to evaluate)
- **US3 (Phase 5)**: Depends on Phase 2 + US2 (needs evaluations to compare)
- **US4 (Phase 6)**: Depends on Phase 2 + US3 (needs change classification for notification levels)
- **US5 (Phase 7)**: Depends on all prior stories (wires everything together)
- **Polish (Phase 8)**: Depends on all desired stories being complete

### Within Each Phase

- Types/models before services
- Services before orchestration
- Pure logic before I/O wiring
- Fakes defined alongside interfaces

### Parallel Opportunities

- All domain types (T004-T009) can run in parallel
- All storage implementations (T011-T015) can run in parallel (after T010)
- All interface definitions (T016-T019) can run in parallel
- All fakes (T023-T025) can run in parallel
- Within US1: Open-Meteo and NWS clients (T026, T027) in parallel
- Within US3: comparison logic and state transitions (T037, T038) in parallel
- Within US4: formatter and notification logic (T041, T042) in parallel
- Polish tests (T054, T055) in parallel

---

## Parallel Example: Foundational Phase

```bash
# Launch all domain types in parallel:
T004: "Define Region/Resort types in domain/resort.go"
T005: "Define Forecast types in domain/weather.go"
T006: "Define Tier enum in domain/tier.go"
T007: "Define Storm types in domain/storm.go"
T008: "Define Evaluation types in domain/evaluation.go"
T009: "Define UserProfile types in domain/profile.go"

# Then launch all storage implementations in parallel (after T010):
T011: "Region/Resort CRUD in storage/regions.go"
T012: "Storm CRUD in storage/storms.go"
T013: "Evaluation persistence in storage/evaluations.go"
T014: "UserProfile storage in storage/profiles.go"
T015: "PromptTemplate storage in storage/prompts.go"
```

---

## Implementation Strategy

### MVP First (Phase 1 + 2 + 3)

1. Complete Setup → project compiles
2. Complete Foundational → DB works, seed data loaded
3. Complete US1 → weather scanning and storm detection works
4. **STOP and VALIDATE**: Run with `--dry-run`, verify storms
   detected correctly across friction tiers
5. This alone is a useful weather monitoring tool

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. + US1 → Storm detection (MVP!)
3. + US2 → LLM evaluation (core value add)
4. + US3 → Lifecycle tracking (no spam)
5. + US4 → Discord delivery (visible output)
6. + US5 → Automated deployment (hands-off)
7. Each story adds value without breaking previous stories

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Seed data (T020) is the largest single task — can start with
  10 regions and expand incrementally
- Prompt template (T032) is the most impactful quality task —
  invest time in clear tier definitions and factor enumeration
- User stories are sequential (not parallel) because each builds
  on the previous: detection → evaluation → comparison → posting
