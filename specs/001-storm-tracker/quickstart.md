# Quickstart: Powder Hunter Development

**Feature Branch**: `001-storm-tracker`
**Date**: 2026-03-04

## Prerequisites

- Go 1.23+
- A Google API key with Gemini API access
- A Discord server with a Forum Channel and webhook URL
- SQLite (for DB inspection — not needed for building)

## Setup

```bash
# Clone and enter the repo
git clone <repo-url> && cd powder-hunter
git checkout 001-storm-tracker

# Install dependencies
go mod download

# Set required environment variables
export GOOGLE_API_KEY="your-gemini-api-key"
export DISCORD_WEBHOOK_URL="https://discord.com/api/webhooks/..."
```

## Initialize the Database

```bash
# Create DB and seed region/resort data
go run ./cmd/powder-hunter seed --db ./powder-hunter.db
```

This creates the SQLite database, runs schema migrations, and
inserts all predefined regions and resorts.

## Configure Your Profile

```bash
go run ./cmd/powder-hunter profile \
  --db ./powder-hunter.db \
  --home "Denver, CO" \
  --passes "ikon" \
  --remote true
```

## Run the Pipeline

```bash
# Full run (scan → evaluate → compare → post to Discord)
go run ./cmd/powder-hunter run --db ./powder-hunter.db

# Dry run (skip Discord posting)
go run ./cmd/powder-hunter run --db ./powder-hunter.db --dry-run

# Single region (for debugging)
go run ./cmd/powder-hunter run --db ./powder-hunter.db \
  --region summit-county --verbose
```

## Run Tests

```bash
go test ./...
```

Tests use fakes for all external systems (weather APIs, Gemini,
Discord, SQLite). No API keys or network access needed.

## Replay an Evaluation

After the system has run and persisted evaluations:

```bash
# Replay storm #42 with a different prompt version
go run ./cmd/powder-hunter replay \
  --db ./powder-hunter.db \
  --storm-id 42 \
  --prompt-version "1.1.0" \
  --output json
```

## Build Docker Image

```bash
docker build -t powder-hunter .

docker run --rm \
  -e GOOGLE_API_KEY="..." \
  -e DISCORD_WEBHOOK_URL="..." \
  -v /path/to/data:/data \
  powder-hunter run --db /data/powder-hunter.db
```

## Inspect the Database

```bash
sqlite3 powder-hunter.db

# Recent storms
SELECT id, region_id, state, current_tier, window_start, window_end
FROM storms ORDER BY detected_at DESC LIMIT 10;

# Evaluation history for a storm
SELECT evaluated_at, tier, recommendation, change_class, delivered
FROM evaluations WHERE storm_id = 42 ORDER BY evaluated_at;

# Weather fetch audit log
SELECT region_id, source, fetched_at, snowfall_cm, success
FROM weather_fetches ORDER BY fetched_at DESC LIMIT 20;
```

## Project Structure

```
cmd/powder-hunter/main.go   # Entry point
domain/                      # Pure core (no I/O)
weather/                     # Weather API clients
evaluation/                  # Gemini LLM client
discord/                     # Discord webhook client
storage/                     # SQLite persistence
pipeline/                    # Orchestration
seed/                        # Predefined region data
```

See [plan.md](plan.md) for full architecture details.
