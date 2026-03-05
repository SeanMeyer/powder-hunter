# Powder Hunter

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go)](https://go.dev)

Automated powder day alerts. Monitors weather forecasts across ski regions, uses Gemini AI to evaluate storm opportunities, and sends personalized alerts to Discord.

## How It Works

1. **Scan** — Fetches multi-model weather forecasts for 170+ ski regions across North America
2. **Detect** — Identifies significant snowfall windows that cross alert thresholds based on your proximity to each region
3. **Evaluate** — Sends promising storms to Gemini AI for deep analysis (snow quality, crowd dynamics, travel logistics, resort operations)
4. **Post** — Delivers personalized storm briefings to your Discord channel with day-by-day strategy

## Quick Start

1. Clone and copy the example config:

   ```bash
   git clone https://github.com/seanmeyer/powder-hunter.git
   cd powder-hunter
   cp .env.example .env
   ```

2. Edit `.env` with your API keys and preferences:

   - `GOOGLE_API_KEY` -- get one free at https://aistudio.google.com/apikey
   - `DISCORD_WEBHOOK_URL` -- create one in your Discord server settings
   - Update your location, ski passes, and preferences

3. Start it:

   ```bash
   docker compose up -d --build
   ```

That's it. The database seeds automatically on first run. Check your Discord channel for storm alerts.

## Configuration

All settings are controlled via environment variables in `.env`. See `.env.example` for the full list with descriptions.

### Required

| Variable | Description |
|----------|-------------|
| `GOOGLE_API_KEY` | Gemini API key for storm evaluation |
| `DISCORD_WEBHOOK_URL` | Discord webhook for posting alerts |

### Your Profile

| Variable | Default | Description |
|----------|---------|-------------|
| `HOME_BASE` | _(required)_ | Your home city (for LLM travel context) |
| `HOME_LATITUDE` | _(required)_ | Home latitude (for friction tier calculation) |
| `HOME_LONGITUDE` | _(required)_ | Home longitude (for friction tier calculation) |
| `PASSES` | | Comma-separated ski passes (ikon, epic, indy) |
| `SKILL_LEVEL` | intermediate | beginner, intermediate, advanced, expert |
| `PREFERENCES` | | Freeform skiing preferences for AI personalization |
| `REMOTE_WORK` | false | Can you work remotely from a ski town? |
| `PTO_DAYS` | 10 | Annual PTO days for ski trips |

### Pipeline Settings

| Variable | Default | Description |
|----------|---------|-------------|
| `LOOP_INTERVAL` | 12h | How often to scan for storms |
| `BUDGET` | 20 | Monthly Gemini API spend cap in USD |
| `DRY_RUN` | false | Skip Discord posting (for testing) |
| `REGION_FILTER` | _(all)_ | Only check specific region ID |
| `VERBOSE` | false | Enable debug logging |
| `DB_PATH` | /data/powder-hunter.db | SQLite path inside container |

## CLI Tools

For power users, the binary includes additional commands:

```bash
# Debug a specific region's weather + evaluation
docker compose exec powder-hunter powder-hunter trace --region co-front-range

# List all available regions
docker compose exec powder-hunter powder-hunter regions

# View your profile
docker compose exec powder-hunter powder-hunter profile --show
```

## Logs

```bash
docker compose logs -f powder-hunter
```

## Unraid

An Unraid Docker template is included. See [docs/unraid.md](docs/unraid.md) for setup instructions.

## Updating

### Docker Compose

```bash
docker compose pull
docker compose up -d
```

### Unraid

Click the container icon in the Docker tab and select **Update**. Unraid will pull the latest image automatically.

## Limitations

- **US-focused weather**: NWS forecasts are US-only. Canadian and international regions use Open-Meteo only (fewer models, no forecast discussions).
- **Discord-only**: Notifications are delivered via Discord webhooks. Other channels (Slack, email) are not currently supported.
- **Detection thresholds**: Friction tiers are auto-calculated from your home coordinates using straight-line distance. Actual drive times may differ, but the LLM accounts for real travel logistics in its evaluation.

## Contributing

Contributions are welcome — just open a PR.
