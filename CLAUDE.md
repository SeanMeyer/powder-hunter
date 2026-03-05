# powder-hunter Development Guidelines

Auto-generated from all feature plans. Last updated: 2026-03-04

## Active Technologies
- Go 1.23+ + `google.golang.org/genai` (Gemini), `modernc.org/sqlite`, `net/http` (weather APIs) (003-forecast-improvements)
- SQLite via `modernc.org/sqlite` — single file, pure Go (003-forecast-improvements)

- Go 1.23+ (001-storm-tracker)

## Project Structure

```text
src/
tests/
```

## Commands

# Add commands for Go 1.23+

## Code Style

Go 1.23+: Follow standard conventions

## Recent Changes
- 003-forecast-improvements: Added Go 1.23+ + `google.golang.org/genai` (Gemini), `modernc.org/sqlite`, `net/http` (weather APIs)

- 001-storm-tracker: Added Go 1.23+

<!-- MANUAL ADDITIONS START -->

## Debugging Production Data

The production DB runs on Unraid at `root@192.168.1.124`. To inspect it locally:

```bash
scp root@192.168.1.124:/mnt/user/appdata/powder-hunter/powder-hunter.db ~/projects/powder-hunter/debug.db
```

Then query with sqlite3:

```bash
# List recent evaluations
sqlite3 debug.db "SELECT id, storm_id, tier, substr(recommendation, 1, 80) FROM evaluations ORDER BY id DESC LIMIT 20"

# Get full raw LLM response for an evaluation
sqlite3 debug.db "SELECT raw_llm_response FROM evaluations WHERE storm_id=31"

# Get structured JSON response
sqlite3 debug.db "SELECT structured_response FROM evaluations WHERE id=1"

# See which regions detected storms
sqlite3 debug.db "SELECT id, region_id, state, window_start, window_end FROM storms ORDER BY id DESC LIMIT 20"

# Check cost tracking
sqlite3 debug.db "SELECT region_id, estimated_cost_usd, evaluated_at FROM eval_costs ORDER BY id DESC LIMIT 20"

# Get the rendered prompt sent to Gemini
sqlite3 debug.db "SELECT rendered_prompt FROM evaluations WHERE id=1"
```

**Important:** If the pipeline is actively running, the WAL file may contain uncommitted data. Wait for `pipeline complete` in the logs before copying, or the DB may be incomplete.

## Deploying to Unraid

```bash
# Standard deploy (no data reset):
cd /mnt/user/appdata/powder-hunter
git pull
docker build -t powder-hunter .
docker rm -f powder-hunter
docker run -d --name powder-hunter --restart unless-stopped --env-file .env -v powder-hunter-data:/data powder-hunter run --loop

# Deploy with data reset (re-evaluates everything fresh):
cd /mnt/user/appdata/powder-hunter
git pull
docker build -t powder-hunter .
docker rm -f powder-hunter
docker run --rm -v powder-hunter-data:/data powder-hunter reset --db /data/powder-hunter.db
docker run -d --name powder-hunter --restart unless-stopped --env-file .env -v powder-hunter-data:/data powder-hunter run --loop
```

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
