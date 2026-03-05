# Powder Hunter

Automated powder day alerts. Monitors weather forecasts across ski regions, uses Gemini AI to evaluate storm opportunities, and sends personalized alerts to Discord.

## Quick Start

1. Clone and copy the example config:

   ```bash
   git clone <repo-url>
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
| `HOME_BASE` | Denver, CO | Your home city (for travel estimates) |
| `HOME_LATITUDE` | 39.7392 | Home latitude |
| `HOME_LONGITUDE` | -104.9903 | Home longitude |
| `PASSES` | ikon | Comma-separated ski passes (ikon, epic, indy) |
| `SKILL_LEVEL` | expert | beginner, intermediate, advanced, expert |
| `PREFERENCES` | _(see .env.example)_ | Freeform skiing preferences for AI personalization |
| `REMOTE_WORK` | true | Can you work remotely from a ski town? |
| `PTO_DAYS` | 15 | Annual PTO days for ski trips |
| `MIN_TIER` | DROP_EVERYTHING | Minimum tier for Discord ping |

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

An Unraid Docker template is included at `unraid/powder-hunter.xml`. To install:

1. Build the image: `docker build -t powder-hunter .` (from the repo directory)
2. Copy the template: `cp unraid/powder-hunter.xml /boot/config/plugins/dockerMan/templates-user/my-powder-hunter.xml`
3. In the Unraid Docker tab, click **Add Container** and select **powder-hunter**
4. Fill in your API keys and preferences in the form, then click **Apply**

## Updating

### Docker Compose

```bash
docker compose down
git pull
docker compose up -d --build
```

### Unraid

```bash
cd /mnt/user/appdata/powder-hunter
git pull
docker build -t powder-hunter .
```

Then restart the container from the Unraid Docker UI.

## TODO: Published Docker Images

Currently the image must be built from source. Once the repo is public, add a GitHub Actions workflow to publish images to `ghcr.io` on tagged releases. This would enable:

- Unraid auto-update support (no more SSH + rebuild)
- Users skip building entirely -- just pull and run
- Template points to `ghcr.io/seanmeyer/powder-hunter:latest` instead of a local build
