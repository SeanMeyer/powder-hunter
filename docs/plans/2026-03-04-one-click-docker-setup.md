# One-Click Docker Setup Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make powder-hunter deployable with `docker compose up -d` by reading all configuration from environment variables, auto-seeding on first run, and shipping `.env.example` + `docker-compose.yml`.

**Architecture:** Extract a pure function `ProfileFromEnv` that builds a `domain.UserProfile` from env vars (with defaults from `seed.DefaultProfile()`). The `runPipeline` function gains auto-seed logic and reads pipeline config from env vars as fallbacks when CLI flags aren't set. New files: `docker-compose.yml`, `.env.example`, `README.md`.

**Tech Stack:** Go 1.23+, Docker, Docker Compose

---

### Task 1: Pure function to build UserProfile from env vars

**Files:**
- Create: `config/profile.go`
- Test: `config/profile_test.go`

**Step 1: Write the failing test**

```go
// config/profile_test.go
package config

import (
	"testing"

	"github.com/seanmeyer/powder-hunter/domain"
)

func TestProfileFromEnv_Defaults(t *testing.T) {
	// No env vars set — should return seed defaults.
	got := ProfileFromEnv(func(string) string { return "" })

	if got.HomeBase != "Denver, CO" {
		t.Errorf("HomeBase = %q, want %q", got.HomeBase, "Denver, CO")
	}
	if got.SkillLevel != "expert" {
		t.Errorf("SkillLevel = %q, want %q", got.SkillLevel, "expert")
	}
}

func TestProfileFromEnv_Overrides(t *testing.T) {
	env := map[string]string{
		"HOME_BASE":      "Salt Lake City, UT",
		"HOME_LATITUDE":  "40.7608",
		"HOME_LONGITUDE": "-111.8910",
		"PASSES":         "ikon,epic",
		"SKILL_LEVEL":    "advanced",
		"PREFERENCES":    "Love steep chutes",
		"REMOTE_WORK":    "false",
		"PTO_DAYS":       "10",
		"MIN_TIER":       "WORTH_A_LOOK",
	}
	lookup := func(key string) string { return env[key] }

	got := ProfileFromEnv(lookup)

	if got.HomeBase != "Salt Lake City, UT" {
		t.Errorf("HomeBase = %q, want %q", got.HomeBase, "Salt Lake City, UT")
	}
	if got.HomeLatitude != 40.7608 {
		t.Errorf("HomeLatitude = %f, want %f", got.HomeLatitude, 40.7608)
	}
	if got.HomeLongitude != -111.8910 {
		t.Errorf("HomeLongitude = %f, want %f", got.HomeLongitude, -111.8910)
	}
	if len(got.PassesHeld) != 2 || got.PassesHeld[0] != "ikon" || got.PassesHeld[1] != "epic" {
		t.Errorf("PassesHeld = %v, want [ikon epic]", got.PassesHeld)
	}
	if got.SkillLevel != "advanced" {
		t.Errorf("SkillLevel = %q, want %q", got.SkillLevel, "advanced")
	}
	if got.Preferences != "Love steep chutes" {
		t.Errorf("Preferences = %q, want %q", got.Preferences, "Love steep chutes")
	}
	if got.RemoteWorkCapable != false {
		t.Errorf("RemoteWorkCapable = %v, want false", got.RemoteWorkCapable)
	}
	if got.TypicalPTODays != 10 {
		t.Errorf("TypicalPTODays = %d, want 10", got.TypicalPTODays)
	}
	if got.MinTierForPing != domain.TierWorthALook {
		t.Errorf("MinTierForPing = %q, want %q", got.MinTierForPing, domain.TierWorthALook)
	}
}

func TestProfileFromEnv_PartialOverride(t *testing.T) {
	// Only override home base — everything else should stay at defaults.
	env := map[string]string{"HOME_BASE": "Portland, OR"}
	lookup := func(key string) string { return env[key] }

	got := ProfileFromEnv(lookup)

	if got.HomeBase != "Portland, OR" {
		t.Errorf("HomeBase = %q, want %q", got.HomeBase, "Portland, OR")
	}
	// Defaults preserved.
	if got.SkillLevel != "expert" {
		t.Errorf("SkillLevel = %q, want %q (default)", got.SkillLevel, "expert")
	}
	if got.RemoteWorkCapable != true {
		t.Errorf("RemoteWorkCapable = %v, want true (default)", got.RemoteWorkCapable)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./config/ -v`
Expected: FAIL — package doesn't exist yet.

**Step 3: Write minimal implementation**

```go
// config/profile.go
package config

import (
	"strconv"
	"strings"

	"github.com/seanmeyer/powder-hunter/domain"
	"github.com/seanmeyer/powder-hunter/seed"
)

// ProfileFromEnv builds a UserProfile starting from seed defaults, overriding
// any field that has a corresponding non-empty env var. The lookup function
// is injected so callers can use os.Getenv in production and a map in tests.
func ProfileFromEnv(lookup func(string) string) domain.UserProfile {
	p := seed.DefaultProfile()

	if v := lookup("HOME_BASE"); v != "" {
		p.HomeBase = v
	}
	if v := lookup("HOME_LATITUDE"); v != "" {
		if lat, err := strconv.ParseFloat(v, 64); err == nil {
			p.HomeLatitude = lat
		}
	}
	if v := lookup("HOME_LONGITUDE"); v != "" {
		if lon, err := strconv.ParseFloat(v, 64); err == nil {
			p.HomeLongitude = lon
		}
	}
	if v := lookup("PASSES"); v != "" {
		p.PassesHeld = strings.Split(v, ",")
	}
	if v := lookup("SKILL_LEVEL"); v != "" {
		p.SkillLevel = v
	}
	if v := lookup("PREFERENCES"); v != "" {
		p.Preferences = v
	}
	if v := lookup("REMOTE_WORK"); v != "" {
		p.RemoteWorkCapable = v == "true"
	}
	if v := lookup("PTO_DAYS"); v != "" {
		if days, err := strconv.Atoi(v); err == nil {
			p.TypicalPTODays = days
		}
	}
	if v := lookup("MIN_TIER"); v != "" {
		p.MinTierForPing = domain.Tier(v)
	}

	return p
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./config/ -v`
Expected: PASS (all 3 tests)

**Step 5: Commit**

```bash
git add config/profile.go config/profile_test.go
git commit -m "feat: add ProfileFromEnv to build user profile from environment variables"
```

---

### Task 2: Pure function to build pipeline config from env vars

**Files:**
- Create: `config/pipeline.go`
- Test: `config/pipeline_test.go`

**Step 1: Write the failing test**

```go
// config/pipeline_test.go
package config

import (
	"testing"
	"time"
)

func TestPipelineConfigFromEnv_Defaults(t *testing.T) {
	got := PipelineConfigFromEnv(func(string) string { return "" })

	if got.DBPath != "/data/powder-hunter.db" {
		t.Errorf("DBPath = %q, want %q", got.DBPath, "/data/powder-hunter.db")
	}
	if got.LoopInterval != 12*time.Hour {
		t.Errorf("LoopInterval = %v, want %v", got.LoopInterval, 12*time.Hour)
	}
	if got.DryRun != false {
		t.Errorf("DryRun = %v, want false", got.DryRun)
	}
	if got.Budget != 20.0 {
		t.Errorf("Budget = %f, want 20.0", got.Budget)
	}
	if got.RegionFilter != "" {
		t.Errorf("RegionFilter = %q, want empty", got.RegionFilter)
	}
	if got.Verbose != false {
		t.Errorf("Verbose = %v, want false", got.Verbose)
	}
}

func TestPipelineConfigFromEnv_Overrides(t *testing.T) {
	env := map[string]string{
		"DB_PATH":       "/custom/path.db",
		"LOOP_INTERVAL": "6h",
		"DRY_RUN":       "true",
		"BUDGET":        "50.0",
		"REGION_FILTER": "co-front-range",
		"VERBOSE":       "true",
	}
	lookup := func(key string) string { return env[key] }

	got := PipelineConfigFromEnv(lookup)

	if got.DBPath != "/custom/path.db" {
		t.Errorf("DBPath = %q, want %q", got.DBPath, "/custom/path.db")
	}
	if got.LoopInterval != 6*time.Hour {
		t.Errorf("LoopInterval = %v, want %v", got.LoopInterval, 6*time.Hour)
	}
	if got.DryRun != true {
		t.Errorf("DryRun = %v, want true", got.DryRun)
	}
	if got.Budget != 50.0 {
		t.Errorf("Budget = %f, want 50.0", got.Budget)
	}
	if got.RegionFilter != "co-front-range" {
		t.Errorf("RegionFilter = %q, want %q", got.RegionFilter, "co-front-range")
	}
	if got.Verbose != true {
		t.Errorf("Verbose = %v, want true", got.Verbose)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./config/ -v -run PipelineConfig`
Expected: FAIL — `PipelineConfigFromEnv` undefined.

**Step 3: Write minimal implementation**

```go
// config/pipeline.go
package config

import (
	"strconv"
	"time"
)

// PipelineConfig holds all settings for the run command that can come from
// environment variables (for Docker) or CLI flags (for local use).
type PipelineConfig struct {
	DBPath       string
	LoopInterval time.Duration
	DryRun       bool
	Budget       float64
	RegionFilter string
	Verbose      bool
}

// PipelineConfigFromEnv builds pipeline configuration from env vars with
// sensible defaults for Docker deployment.
func PipelineConfigFromEnv(lookup func(string) string) PipelineConfig {
	cfg := PipelineConfig{
		DBPath:       "/data/powder-hunter.db",
		LoopInterval: 12 * time.Hour,
		DryRun:       false,
		Budget:       20.0,
		RegionFilter: "",
		Verbose:      false,
	}

	if v := lookup("DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := lookup("LOOP_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.LoopInterval = d
		}
	}
	if v := lookup("DRY_RUN"); v != "" {
		cfg.DryRun = v == "true"
	}
	if v := lookup("BUDGET"); v != "" {
		if b, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Budget = b
		}
	}
	if v := lookup("REGION_FILTER"); v != "" {
		cfg.RegionFilter = v
	}
	if v := lookup("VERBOSE"); v != "" {
		cfg.Verbose = v == "true"
	}

	return cfg
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./config/ -v`
Expected: PASS (all 5 tests)

**Step 5: Commit**

```bash
git add config/pipeline.go config/pipeline_test.go
git commit -m "feat: add PipelineConfigFromEnv for Docker-friendly pipeline configuration"
```

---

### Task 3: Wire env var config into runPipeline with CLI flag overrides

CLI flags take precedence over env vars. Env vars provide defaults for Docker users who don't pass flags.

**Files:**
- Modify: `cmd/powder-hunter/main.go:108-203` (runPipeline function)

**Step 1: Update runPipeline to use env var defaults**

Replace the flag defaults with values from `PipelineConfigFromEnv`, then let CLI flags override. The key change: flag defaults come from the env-var config, so `--budget` defaults to `20` (from env) instead of `0`.

```go
// In runPipeline, before flag parsing:
import "github.com/seanmeyer/powder-hunter/config"

func runPipeline(ctx context.Context, args []string) int {
	envCfg := config.PipelineConfigFromEnv(os.Getenv)

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	dbPath := fs.String("db", envCfg.DBPath, "SQLite database path")
	dryRun := fs.Bool("dry-run", envCfg.DryRun, "Run pipeline but skip Discord posting")
	regionFilter := fs.String("region", envCfg.RegionFilter, "Evaluate only this region (for debugging)")
	verbose := fs.Bool("verbose", envCfg.Verbose, "Enable debug-level logging")
	loop := fs.Bool("loop", false, "Run pipeline repeatedly on an interval")
	interval := fs.Duration("interval", envCfg.LoopInterval, "Time between pipeline runs (requires --loop)")
	budget := fs.Float64("budget", envCfg.Budget, "Monthly budget limit in USD (0 = disabled)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	// ... rest stays the same, except budget check changes from > 0 to >= 0
	// (budget default is now 20, not 0)
```

Also change the budget wiring (line 162) so the default $20 budget is always applied:

```go
	// Old: if *budget > 0 {
	// New: always configure budget (default is $20 safety valve)
	p.WithBudgetConfig(pipeline.BudgetConfig{
		MonthlyLimitUSD:  *budget,
		WarningThreshold: 0.8,
	})
```

**Step 2: Run existing tests**

Run: `go test ./... -v -count=1`
Expected: PASS — no behavior change for CLI users (they still pass flags as before).

**Step 3: Commit**

```bash
git add cmd/powder-hunter/main.go
git commit -m "feat: wire env var config into runPipeline with CLI flag overrides"
```

---

### Task 4: Auto-seed on first run

When `runPipeline` opens the DB, check if regions exist. If not, run the seed logic automatically. Also upsert profile from env vars on every startup (so changing env vars updates the profile).

**Files:**
- Modify: `cmd/powder-hunter/main.go:108-203` (runPipeline function)
- Modify: `storage/regions.go` — need to check: does a `CountRegions` or similar method exist?

**Step 1: Check if storage has a region count method**

Read `storage/regions.go` to see what's available. If no count method exists, add one.

```go
// storage/regions.go — add if not present:
func (d *DB) CountRegions(ctx context.Context) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM regions`).Scan(&count)
	return count, err
}
```

**Step 2: Add autoSeed function to main.go**

```go
// autoSeed seeds regions, resorts, prompt templates, and the user profile
// (from env vars) if the database is empty. Called on every startup of `run`.
func autoSeed(ctx context.Context, db *storage.DB) error {
	count, err := db.CountRegions(ctx)
	if err != nil {
		return fmt.Errorf("count regions: %w", err)
	}

	if count == 0 {
		slog.Info("first run detected, seeding database")
		regions := seed.Regions()
		for _, r := range regions {
			if err := db.UpsertRegion(ctx, r.Region); err != nil {
				return fmt.Errorf("upsert region %s: %w", r.Region.ID, err)
			}
			for _, resort := range r.Resorts {
				if err := db.UpsertResort(ctx, resort); err != nil {
					return fmt.Errorf("upsert resort %s: %w", resort.ID, err)
				}
			}
		}
		slog.Info("seeded regions", "count", len(regions))

		promptID, promptVersion, promptTemplate := seed.InitialPromptTemplate()
		if err := db.SavePromptTemplate(ctx, promptID, promptVersion, promptTemplate); err != nil {
			return fmt.Errorf("seed prompt template: %w", err)
		}
		slog.Info("seeded prompt template", "version", promptVersion)
	}

	// Always upsert profile from env vars so changes take effect on restart.
	profile := config.ProfileFromEnv(os.Getenv)
	if err := db.SaveProfile(ctx, profile); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}

	return nil
}
```

**Step 3: Call autoSeed in runPipeline after opening the DB**

Insert after `defer db.Close()` (line 144):

```go
	if err := autoSeed(ctx, db); err != nil {
		slog.Error("auto-seed failed", "error", err)
		return 1
	}
```

**Step 4: Run tests**

Run: `go test ./... -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/powder-hunter/main.go storage/regions.go
git commit -m "feat: auto-seed database and upsert profile from env vars on startup"
```

---

### Task 5: Create docker-compose.yml

**Files:**
- Create: `docker-compose.yml`

**Step 1: Write docker-compose.yml**

```yaml
services:
  powder-hunter:
    build: .
    container_name: powder-hunter
    restart: unless-stopped
    env_file:
      - .env
    volumes:
      - powder-hunter-data:/data
    command: ["run", "--loop"]

volumes:
  powder-hunter-data:
```

Note: No command-line flags needed — everything comes from env vars via `.env`. The `--loop` flag is the only thing that must be explicit (it's a mode switch, not a config value).

**Step 2: Commit**

```bash
git add docker-compose.yml
git commit -m "feat: add docker-compose.yml for one-click deployment"
```

---

### Task 6: Create .env.example

**Files:**
- Create: `.env.example`

**Step 1: Write .env.example**

```bash
# ─── Required ────────────────────────────────────────────────────────────────
# Get a Gemini API key at https://aistudio.google.com/apikey
GOOGLE_API_KEY=

# Discord webhook URL for storm alerts
# Create one in Discord: Server Settings > Integrations > Webhooks
DISCORD_WEBHOOK_URL=

# ─── Your Profile ────────────────────────────────────────────────────────────
# Where you live (used for travel time/cost estimates)
HOME_BASE=Denver, CO
HOME_LATITUDE=39.7392
HOME_LONGITUDE=-104.9903

# Comma-separated ski passes you hold: ikon, epic, indy, etc.
PASSES=ikon

# Your skiing ability: beginner, intermediate, advanced, expert
SKILL_LEVEL=expert

# Freeform description of what you like (fed to the LLM for personalization)
PREFERENCES=Love powder days in the trees and open bowls

# Can you work remotely from a ski town? (true/false)
REMOTE_WORK=true

# Annual PTO days available for ski trips
PTO_DAYS=15

# Minimum storm tier to get a Discord ping: DROP_EVERYTHING, WORTH_A_LOOK, ON_THE_RADAR
MIN_TIER=DROP_EVERYTHING

# ─── Pipeline Settings ───────────────────────────────────────────────────────
# How often to check for storms (Go duration: 12h, 6h, 1h, etc.)
LOOP_INTERVAL=12h

# Monthly spending cap for Gemini API calls in USD (safety valve)
BUDGET=20

# Set to true to run without posting to Discord (useful for testing)
# DRY_RUN=false

# Only check a specific region (leave empty for all regions)
# REGION_FILTER=

# Enable debug logging
# VERBOSE=false

# SQLite database path inside the container
# DB_PATH=/data/powder-hunter.db
```

**Step 2: Add `.env` to `.gitignore`**

Check if `.gitignore` exists and has `.env`. If not, add it.

**Step 3: Commit**

```bash
git add .env.example .gitignore
git commit -m "feat: add .env.example with documented configuration"
```

---

### Task 7: Create README.md

**Files:**
- Create: `README.md`

**Step 1: Write README.md**

```markdown
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

   - `GOOGLE_API_KEY` — get one free at https://aistudio.google.com/apikey
   - `DISCORD_WEBHOOK_URL` — create one in your Discord server settings
   - Update your location, ski passes, and preferences

3. Start it:

   ```bash
   docker compose up -d
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

## Updating

```bash
docker compose down
git pull
docker compose up -d --build
```
```

**Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add README with quick start and configuration reference"
```

---

### Task 8: Update Dockerfile default and verify end-to-end

The Dockerfile's `ENTRYPOINT` is already `["powder-hunter"]`, which is correct — docker-compose passes `command: ["run", "--loop"]`.

**Step 1: Verify the .env loading path works inside the container**

The current `loadEnvFile(".env")` in `main()` reads from the working directory. In the Docker container, the working dir isn't set, so it falls back to `/`. This doesn't matter because Docker Compose injects env vars directly via `env_file:` — the file is read by Compose, not the binary. No change needed.

**Step 2: Build and verify the image**

Run: `docker compose build`
Expected: Successful build.

**Step 3: Dry-run test**

Create a minimal `.env` for testing:
```bash
cp .env.example .env
# Set GOOGLE_API_KEY to a real key, set DRY_RUN=true
```

Run: `docker compose run --rm powder-hunter run --dry-run`
Expected: Auto-seeds, runs one pipeline pass, exits without posting to Discord.

**Step 4: Commit any final adjustments**

```bash
git add -A
git commit -m "chore: verify end-to-end Docker setup"
```
