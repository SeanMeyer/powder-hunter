# powder-hunter Development Guidelines

Auto-generated from all feature plans. Last updated: 2026-03-04

## Active Technologies
- Go 1.24+ + `google.golang.org/genai` (Gemini), `modernc.org/sqlite`, `net/http` (weather APIs)
- SQLite via `modernc.org/sqlite` — single file, pure Go

## Project Structure

```text
src/
tests/
```

## Commands

## Code Style

Go 1.24+: Follow standard conventions

<!-- MANUAL ADDITIONS START -->

## Debugging Production Data

Copy the production database locally for inspection:

```bash
scp <user>@<server>:/path/to/powder-hunter.db ~/projects/powder-hunter/debug.db
```

Then query with sqlite3 (see the schema in `storage/sqlite.go` for table definitions).

## Deploying

See `README.md` for Docker deployment instructions.

## Local Testing

```bash
# Trace a region (weather + LLM evaluation, no DB or Discord):
go run ./cmd/powder-hunter trace --region co_front_range

# Weather only (no Gemini API call):
go run ./cmd/powder-hunter trace --region co_front_range --weather-only

# Show the rendered prompt:
go run ./cmd/powder-hunter trace --region co_front_range --show-prompt --weather-only
```

<!-- MANUAL ADDITIONS END -->
