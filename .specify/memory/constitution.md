<!--
Sync Impact Report
==================
Version change: 0.0.0 → 1.0.0
Bump rationale: Initial constitution ratification (MAJOR)

Modified principles: N/A (initial version)

Added sections:
  - Core Principles (7 principles)
  - Technology & Deployment Constraints
  - Development Workflow
  - Governance

Removed sections: N/A (initial version)

Templates requiring updates:
  - .specify/templates/plan-template.md — ⚠ pending (Constitution Check
    section references generic gates; update after first /speckit.plan run)
  - .specify/templates/spec-template.md — ✅ no changes needed
  - .specify/templates/tasks-template.md — ✅ no changes needed

Follow-up TODOs: None
-->

# Powder Hunter Constitution

## Core Principles

### I. Parse, Don't Validate

Types encode validity. Once data crosses a system boundary and is
parsed into a domain type, downstream code MUST NOT re-check it.
A `ValidForecast` is a different type from raw API JSON. Internal
structs are supersets — they carry metadata, reasons, and sources
so nothing needs to be re-derived.

- Raw external data (API responses, config files) MUST be parsed
  into domain types at the boundary.
- Domain functions MUST accept parsed types, never raw input.
- If a value can be invalid, the type system MUST make the invalid
  state unrepresentable.

### II. I/O Sandwich

Business logic (storm evaluation, scoring, grouping) MUST be pure.
No `context.Context`, no database handles, no HTTP clients in the
core. The shell handles all I/O: fetching weather, calling Gemini,
writing to SQLite, posting to Discord.

- `domain/` package MUST have zero I/O imports.
- Concurrency (parallel weather fetches, etc.) lives in the shell
  via `errgroup`, never in domain code.
- Pure functions take data in, return data out. Side effects are
  the caller's responsibility.

### III. Decisions Are Data

Every evaluation MUST produce a rich result struct that captures
not just the outcome (tier, recommendation) but *why* — which
factors contributed, what the inputs were, what the LLM considered.

- Evaluation results MUST be persisted to SQLite with full context.
- You MUST be able to reconstruct exactly what happened on any run
  by inspecting the database alone.
- No "fire and forget" operations — every action that changes state
  produces a record.

### IV. Observability Is Not Optional

Structured logging on every operation in the shell. The database is
the primary audit log.

- All I/O operations MUST use `log/slog` with structured fields.
- Every storm evaluation, weather fetch, and Discord post MUST be
  recorded with enough context to debug "why didn't I get an alert?"
  or "why was this rated WORTH A LOOK instead of DROP EVERYTHING?"
- Silent failures are bugs. If an operation fails, it MUST be logged
  with enough context to diagnose without reproducing.

### V. Production Quality

This is not a script. Code MUST read like a well-maintained
production system even though it runs on a home server.

- Clean module boundaries with small, focused packages.
- Proper error handling — no ignored errors, no bare `log.Fatal`
  in library code.
- No "TODO: fix later" shortcuts that compromise correctness.
- Dependencies MUST be intentional and justified.

### VI. Domain-Organized

Packages organized by what they do (weather, evaluation, discord,
storage), not by type (models, controllers, utils).

- Small interfaces at package boundaries.
- Concrete return types — return structs, accept interfaces.
- No unnecessary abstractions. If an interface has one
  implementation and no testing need, use the concrete type.

### VII. Test Discipline

Testing strategy prioritizes confidence over coverage. Sociable
tests at the system boundary are the primary tool; isolated unit
tests are reserved for genuinely complex subsystems.

- **Sociable tests at the boundary**: Test through the system's
  entry point with fakes for external systems (weather APIs,
  Gemini, Discord, SQLite). A small number of these tests
  exercising the full pipeline provides more confidence than
  hundreds of isolated unit tests.
- **Bug fix TDD is NON-NEGOTIABLE**: Every bug fix MUST start
  with a failing test that reproduces the bug. Fix the test,
  then fix the code.
- **Edge-case unit tests**: Only when a subsystem has genuinely
  complex logic with many cases (e.g., storm detection thresholds,
  tier assignment rules). These supplement sociable tests, not
  replace them.
- **No coverage targets**: Confidence is the goal, not a
  percentage.
- **No redundant tests**: If a sociable test exercises a code
  path, do not write a unit test for the same thing.
- **TDD for domain code is RECOMMENDED**: Writing tests first
  for pure domain logic forces clean boundaries, well-defined
  input/output types, and locks in the spec before coding.

## Technology & Deployment Constraints

- **Language**: Go — single binary, clean deployment, strong
  HTTP/JSON support.
- **Database**: SQLite via `modernc.org/sqlite` — single file,
  zero-ops, no CGO dependency.
- **Weather APIs**: Open-Meteo (16-day, US + Canada) and NWS
  `api.weather.gov` (7-day, US only).
- **LLM**: Gemini via `google.golang.org/genai` with Google
  Search grounding for live web research.
- **Output**: Discord webhooks with rich embeds and threaded
  updates. No bot framework.
- **Logging**: `log/slog` from the standard library. No third-party
  logging frameworks.
- **Runtime**: Docker container on Unraid, triggered by cron
  (2x daily).
- **No CGO**: All dependencies MUST be pure Go or provide a
  pure-Go alternative.

Technology changes (adding a database, switching LLM provider,
adding a new output channel) MUST be documented as a constitution
amendment with rationale.

## Development Workflow

- **Commit discipline**: Atomic commits. Each commit MUST compile
  and pass tests. Commit messages describe *why*, not *what*.
- **Error handling**: Errors MUST be decorated with context using
  `fmt.Errorf("operation: %w", err)`. No swallowed errors.
- **Code review**: All changes MUST be reviewed before merge,
  either by automated tooling or human review.
- **Fakes over mocks**: External system boundaries use hand-written
  fakes, not mock frameworks. Fakes implement the same interface
  as the real client.
- **No dead code**: Unused code MUST be deleted, not commented out.
- **Simplicity first**: Start with the simplest implementation that
  works. Add complexity only when a concrete need arises (YAGNI).

## Governance

This constitution is the highest-authority document for the
Powder Hunter project. All code, reviews, and architectural
decisions MUST comply with these principles.

- **Amendments** require: (1) written rationale, (2) explicit
  approval, and (3) a version bump to this document.
- **Versioning** follows semantic versioning:
  - MAJOR: Principle removal or backward-incompatible redefinition.
  - MINOR: New principle or materially expanded guidance.
  - PATCH: Clarification, wording, or typo fix.
- **Compliance review**: Any PR or code change that appears to
  violate a principle MUST either comply or propose an amendment.
  "Move fast and break rules" is not acceptable.
- **Conflict resolution**: When principles conflict (e.g.,
  simplicity vs. observability), the higher-numbered principle
  yields to the lower-numbered one unless explicitly justified.

**Version**: 1.0.0 | **Ratified**: 2026-03-04 | **Last Amended**: 2026-03-04
