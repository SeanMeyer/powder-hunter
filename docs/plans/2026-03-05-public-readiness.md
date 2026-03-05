# Public Readiness Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the powder-hunter codebase ready for public open-source release on GitHub.

**Architecture:** The work is organized into 7 phases, ordered from quick-wins and blockers (legal, security, dead code) through architecture changes (package rename, friction tier auto-calculation, genericizing defaults) to quality improvements (error handling, tests, deduplication) and finally distribution (CI/CD, versioning, Docker publishing). Each phase is independently committable.

**Tech Stack:** Go 1.24, SQLite, Gemini API, Discord webhooks, Docker, GitHub Actions

---

## Phase 1: Legal & Security Blockers

### Task 1.1: Add LICENSE file

**Files:**
- Create: `LICENSE`

**Step 1: Create MIT license**

```
MIT License

Copyright (c) 2026 Sean Meyer

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

**Step 2: Commit**

```bash
git add LICENSE
git commit -m "chore: add MIT license"
```

---

### Task 1.2: Scrub CLAUDE.md of private infrastructure details

**Files:**
- Modify: `CLAUDE.md:31-72` (the "Debugging Production Data" and "Deploying to Unraid" sections)

**Step 1: Remove the "Debugging Production Data" section (lines 33-56)**

This contains `root@192.168.1.124`, SSH commands, and internal file paths. Replace with:

```markdown
## Debugging Production Data

Copy the production database locally for inspection:

```bash
# Copy from your deployment server
scp <user>@<server>:/path/to/powder-hunter.db ~/projects/powder-hunter/debug.db
```

Then query with sqlite3 (see the schema in `storage/sqlite.go` for table definitions).
```

**Step 2: Remove the "Deploying to Unraid" section (lines 58-72)**

Replace with a generic note:

```markdown
## Deploying

See `README.md` for Docker deployment instructions. For Unraid, see `unraid/powder-hunter.xml`.
```

**Step 3: Fix Go version — change "Go 1.23+" to "Go 1.24+" on lines 4 and 7**

`CLAUDE.md:4` says "Go 1.23+" but `go.mod:3` requires `go 1.24.1`.

**Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "chore: scrub private infra details from CLAUDE.md, fix Go version"
```

---

## Phase 2: Remove Dead Code & Unimplemented Features

### Task 2.1: Remove unused `NotificationLevel` system

These types are defined but never used anywhere in the codebase.

**Files:**
- Modify: `domain/tier.go:30-50` — delete `NotificationLevel`, `NotifyPing`, `NotifySilentPost`, `NotifyThreadOnly`, and `NotificationFor()`

**Step 1: Delete lines 30-50 from `domain/tier.go`**

The entire block from `// NotificationLevel determines...` through `NotificationFor()` is dead code.

**Step 2: Run tests**

```bash
go test ./...
```

Expected: all pass (nothing references these symbols).

**Step 3: Commit**

```bash
git add domain/tier.go
git commit -m "chore: remove unused NotificationLevel types"
```

---

### Task 2.2: Remove unused `QuietHours` and `MinTierForPing` fields

These are stored in the database but never respected by any pipeline or Discord posting logic.

**Files:**
- Modify: `domain/profile.go:18-20` — remove `MinTierForPing`, `QuietHoursStart`, `QuietHoursEnd`
- Modify: `seed/prompts.go:23-25` — remove corresponding defaults
- Modify: `storage/profiles.go` — remove these columns from SQL queries and scan
- Modify: `cmd/powder-hunter/main.go:542` — remove `MinTierForPing` from profile display

**Step 1: Remove the three fields from `domain/profile.go`**

Delete lines 18-20:
```go
MinTierForPing    Tier   // alerts below this tier are delivered silently
QuietHoursStart   string // "22:00" local time
QuietHoursEnd     string // "07:00" local time
```

**Step 2: Remove from `seed/prompts.go` default profile**

Delete lines 23-25:
```go
MinTierForPing:    domain.TierDropEverything,
QuietHoursStart:   "22:00",
QuietHoursEnd:     "07:00",
```

**Step 3: Remove from storage layer**

In `storage/profiles.go`, remove the columns from INSERT/UPDATE/SELECT queries and the corresponding `Scan` calls. The columns can stay in the DB schema (SQLite ignores extra columns gracefully), but don't read/write them.

**Step 4: Remove from CLI display**

In `cmd/powder-hunter/main.go:542`, remove:
```go
fmt.Printf("Min tier for ping: %s\n", profile.MinTierForPing)
```

**Step 5: Run tests**

```bash
go test ./...
```

**Step 6: Commit**

```bash
git add domain/profile.go seed/prompts.go storage/profiles.go cmd/powder-hunter/main.go
git commit -m "chore: remove unimplemented QuietHours and MinTierForPing"
```

---

### Task 2.3: Remove remaining dead code

**Files to modify:**
- `evaluation/prompt.go:455-477` — delete `FormatConsensusForPrompt` (never called; only `FormatResortConsensusForPrompt` is used)
- `weather/nws.go:25` — delete `nwsSnowfallSanityLimitCM` (unused constant)
- `weather/nws.go:220-225` — delete the empty `walkHourly` callback (no-op dead code)
- `weather/weather.go:16-19` — delete `Fetcher` interface (defined but never used as a dependency)
- `weather/fake.go` — delete entire file (`FakeFetcher` is unused by any test)
- `domain/weather_compare.go:102` — delete `_ = dateKey` (no-op leftover)
- `domain/detection_test.go:19-21` — remove `extDay` alias (identical to `nearDay`)

**Step 1: Make each deletion**

Remove each item listed above.

**Step 2: Update any test references**

In `domain/detection_test.go`, replace any uses of `extDay(...)` with `nearDay(...)`.

**Step 3: Run tests**

```bash
go test ./...
```

**Step 4: Commit**

```bash
git add -A
git commit -m "chore: remove dead code (unused interfaces, constants, functions)"
```

---

### Task 2.4: Fix `--force` flag on seed command (dead branch)

**Files:**
- Modify: `cmd/powder-hunter/main.go:434-458`

**Step 1: Remove the dead branch**

Both `if *force` and `else` execute identical code. Remove the branching:

```go
regions := seed.Regions()
for _, r := range regions {
    if err := db.UpsertRegion(ctx, r.Region); err != nil {
        slog.Error("upsert region", "region", r.Region.ID, "error", err)
        return 1
    }
    for _, resort := range r.Resorts {
        if err := db.UpsertResort(ctx, resort); err != nil {
            slog.Error("upsert resort", "resort", resort.ID, "error", err)
            return 1
        }
    }
}
```

Also remove the `force` flag declaration on line 422 since it no longer does anything.

**Step 2: Run tests**

```bash
go test ./...
```

**Step 3: Commit**

```bash
git add cmd/powder-hunter/main.go
git commit -m "fix: remove dead --force flag from seed command"
```

---

## Phase 3: Rename & Restructure

### Task 3.1: Rename `seed` package to `catalog`

The `seed` package contains the core region/resort dataset — not temporary bootstrap data. Rename it to `catalog` to accurately describe its purpose.

**Files:**
- Rename: `seed/` → `catalog/`
- Rename: `seed/data/` → `catalog/data/`
- Modify: every Go file that imports `"github.com/seanmeyer/powder-hunter/seed"`

**Step 1: Rename the directory**

```bash
mv seed catalog
```

**Step 2: Update the package declaration in all files**

In `catalog/regions.go` and `catalog/prompts.go`, change:
```go
package seed
```
to:
```go
package catalog
```

**Step 3: Update all import paths**

Search for `"github.com/seanmeyer/powder-hunter/seed"` and replace with `"github.com/seanmeyer/powder-hunter/catalog"`:
- `cmd/powder-hunter/main.go`
- `pipeline/pipeline_test.go`

**Step 4: Update all references from `seed.` to `catalog.`**

In `cmd/powder-hunter/main.go`, replace:
- `seed.Regions()` → `catalog.Regions()`
- `seed.DefaultProfile()` → `catalog.DefaultProfile()`
- `seed.InitialPromptTemplate()` → `catalog.InitialPromptTemplate()`
- `seed.RegionWithResorts` → `catalog.RegionWithResorts`

Same in `pipeline/pipeline_test.go`.

**Step 5: Run tests**

```bash
go test ./...
```

**Step 6: Commit**

```bash
git add -A
git commit -m "refactor: rename seed package to catalog"
```

---

### Task 3.2: Rename `MacroRegion` to `StormGroup`

**Files:**
- Modify: `domain/resort.go:44` — rename field
- Modify: `catalog/regions.go` — update field assignment
- Modify: `storage/regions.go` — update SQL column alias and scan
- Modify: any other references (use grep)

**Step 1: Find all references**

```bash
grep -rn "MacroRegion" --include="*.go"
```

**Step 2: Rename the field in `domain/resort.go:44`**

```go
StormGroup string // geographic grouping for storm correlation (e.g. "pnw_cascades")
```

**Step 3: Update all references found in step 1**

**Step 4: Run tests**

```bash
go test ./...
```

**Step 5: Commit**

```bash
git add -A
git commit -m "refactor: rename MacroRegion to StormGroup for clarity"
```

---

## Phase 4: Genericize Denver-Centric Defaults

### Task 4.1: Genericize default profile

**Files:**
- Modify: `catalog/prompts.go:7-27`

**Step 1: Replace the hardcoded Denver profile with a generic one**

```go
func DefaultProfile() domain.UserProfile {
    return domain.UserProfile{
        ID:                1,
        HomeBase:          "",
        HomeLatitude:      0,
        HomeLongitude:     0,
        PassesHeld:        nil,
        SkillLevel:        "intermediate",
        Preferences:       "",
        RemoteWorkCapable: false,
        TypicalPTODays:    10,
    }
}
```

The profile is always overridden by `config.ProfileFromEnv()` on startup (see `autoSeed` in `main.go:243`), so the default only matters for the `trace` command when no DB exists. Setting empty values forces users to configure via `.env`.

**Step 2: Remove the Denver-specific prompt example**

In `catalog/prompts.go:78`, the prompt contains:
```
If the subscriber lives in Denver and both the I-70 corridor and Alaska are getting storms...
```

Replace with a generic example:
```
If both a local region and a distant flight destination are getting storms, the local storm
is more interesting unless the distant storm is truly exceptional — the subscriber can get
a similar experience for a fraction of the cost and hassle.
```

**Step 3: Fix the stale comment on line 32**

Change `// stormEvalPromptTemplate is the v1.0.0 template` to just:
```go
// stormEvalPromptTemplate is the LLM prompt for storm evaluation.
```

**Step 4: Run tests**

```bash
go test ./...
```

Fix any tests that relied on specific Denver defaults (likely in `config/profile_test.go` and `pipeline/pipeline_test.go`).

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: genericize default profile, remove Denver-specific prompt example"
```

---

### Task 4.2: Auto-calculate friction tiers from user location

This is the key change that makes the system location-independent. Instead of relying on hardcoded friction tiers in `regions.json` (which assume Denver), calculate them at startup from the user's `HOME_LATITUDE`/`HOME_LONGITUDE`.

**Files:**
- Create: `domain/friction.go`
- Create: `domain/friction_test.go`
- Modify: `catalog/regions.go` — apply auto-calculated tiers at load time
- Modify: `cmd/powder-hunter/main.go` — pass user coords to region loading

**Step 1: Write the failing test**

Create `domain/friction_test.go`:

```go
package domain

import (
    "math"
    "testing"
)

func TestHaversineDistanceKM(t *testing.T) {
    // Denver to Vail (~155 km)
    d := HaversineDistanceKM(39.7392, -104.9903, 39.6403, -106.3742)
    if math.Abs(d-155) > 20 {
        t.Errorf("Denver→Vail: got %.0f km, want ~155 km", d)
    }

    // Denver to Park City (~595 km)
    d = HaversineDistanceKM(39.7392, -104.9903, 40.6461, -111.498)
    if math.Abs(d-595) > 50 {
        t.Errorf("Denver→Park City: got %.0f km, want ~595 km", d)
    }
}

func TestFrictionTierFromDistance(t *testing.T) {
    tests := []struct {
        name    string
        distKM  float64
        want    FrictionTier
    }{
        {"very close", 50, FrictionLocalDrive},
        {"2 hour drive", 200, FrictionLocalDrive},
        {"4 hour drive", 400, FrictionRegionalDrive},
        {"8 hour drive", 700, FrictionHighFrictionDrive},
        {"cross country", 2000, FrictionFlight},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := FrictionTierFromDistance(tt.distKM)
            if got != tt.want {
                t.Errorf("distance %.0f km: got %s, want %s", tt.distKM, got, tt.want)
            }
        })
    }
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./domain/ -run TestHaversine -v
go test ./domain/ -run TestFrictionTier -v
```

Expected: FAIL (functions don't exist yet).

**Step 3: Write minimal implementation**

Create `domain/friction.go`:

```go
package domain

import "math"

// HaversineDistanceKM returns the great-circle distance in km between two lat/lon points.
func HaversineDistanceKM(lat1, lon1, lat2, lon2 float64) float64 {
    const earthRadiusKM = 6371.0
    dLat := (lat2 - lat1) * math.Pi / 180
    dLon := (lon2 - lon1) * math.Pi / 180
    a := math.Sin(dLat/2)*math.Sin(dLat/2) +
        math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
            math.Sin(dLon/2)*math.Sin(dLon/2)
    return earthRadiusKM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// FrictionTierFromDistance assigns a friction tier based on straight-line distance.
// Drive times are estimated as ~1.3x the straight-line distance at 100 km/h average
// (accounting for mountain roads being slower and indirect).
func FrictionTierFromDistance(distKM float64) FrictionTier {
    estimatedDriveHours := (distKM * 1.3) / 100.0
    switch {
    case estimatedDriveHours <= 3:
        return FrictionLocalDrive
    case estimatedDriveHours <= 8:
        return FrictionRegionalDrive
    case estimatedDriveHours <= 14:
        return FrictionHighFrictionDrive
    default:
        return FrictionFlight
    }
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./domain/ -run "TestHaversine|TestFrictionTier" -v
```

**Step 5: Wire it into region loading**

Modify `catalog/regions.go` to accept optional home coordinates. Add a function:

```go
// RegionsForUser loads regions and calculates friction tiers based on the
// user's home coordinates. If lat/lon are zero, falls back to the static
// friction tiers in regions.json.
func RegionsForUser(homeLat, homeLon float64) []RegionWithResorts {
    regions := Regions()
    if homeLat == 0 && homeLon == 0 {
        return regions // use static tiers from JSON
    }
    for i := range regions {
        r := &regions[i].Region
        dist := domain.HaversineDistanceKM(homeLat, homeLon, r.Latitude, r.Longitude)
        r.FrictionTier = domain.FrictionTierFromDistance(dist)
        near, ext := r.FrictionTier.Thresholds()
        r.NearThresholdIn = near
        r.ExtendedThresholdIn = ext
        r.Logistics.DriveTimeHours = (dist * 1.3) / 100.0
    }
    return regions
}
```

**Step 6: Update `autoSeed` in `cmd/powder-hunter/main.go` to use `RegionsForUser`**

After loading the profile config, pass the home coordinates:

```go
profile := config.ProfileFromEnv(os.Getenv)
regions := catalog.RegionsForUser(profile.HomeLatitude, profile.HomeLongitude)
```

**Step 7: Run tests**

```bash
go test ./...
```

**Step 8: Commit**

```bash
git add -A
git commit -m "feat: auto-calculate friction tiers from user home coordinates"
```

---

### Task 4.3: Make NWS User-Agent configurable

**Files:**
- Modify: `weather/nws.go:22` — read from parameter instead of constant
- Modify: `weather/nws.go` — `NWSClient` struct and `NewNWSClient`

**Step 1: Add a `userAgent` field to `NWSClient`**

```go
type NWSClient struct {
    client    *http.Client
    baseURL   string
    userAgent string
}

func NewNWSClient(client *http.Client) *NWSClient {
    return &NWSClient{
        client:    client,
        baseURL:   "https://api.weather.gov",
        userAgent: "(powder-hunter, https://github.com/seanmeyer/powder-hunter)",
    }
}
```

This also fixes the mutable package-level `nwsBaseURL` variable by making it a field.

**Step 2: Delete the package-level vars**

Remove:
```go
var nwsBaseURL = "https://api.weather.gov"
const nwsUserAgent = "(powder-hunter, contact@example.com)"
```

**Step 3: Update all references to use the struct fields**

Replace `nwsBaseURL` with `c.baseURL` and `nwsUserAgent` with `c.userAgent` in all methods.

**Step 4: Update tests**

In `weather/nws_test.go`, remove `setNWSBaseURL()` and instead create clients with a test URL:

```go
client := &NWSClient{
    client:    httpClient,
    baseURL:   server.URL,
    userAgent: "(test)",
}
```

**Step 5: Optionally add `NWS_CONTACT_EMAIL` env var support**

In `cmd/powder-hunter/main.go`, if the env var is set, pass it to the client:

```go
nwsClient := weather.NewNWSClient(httpClient)
if email := os.Getenv("NWS_CONTACT_EMAIL"); email != "" {
    nwsClient.SetUserAgent(fmt.Sprintf("(powder-hunter, %s)", email))
}
```

**Step 6: Add `NWS_CONTACT_EMAIL` to `.env.example`**

```
# Optional: Email for NWS API User-Agent (recommended by NWS terms of service)
# NWS_CONTACT_EMAIL=your@email.com
```

**Step 7: Run tests**

```bash
go test ./...
```

**Step 8: Commit**

```bash
git add -A
git commit -m "feat: make NWS user-agent configurable, fix mutable package var"
```

---

### Task 4.4: Make Gemini model configurable

**Files:**
- Modify: `evaluation/gemini.go:23` — read model from parameter instead of constant

**Step 1: Accept model as parameter in `NewGeminiClient`**

Change the constructor to accept a model parameter with a sensible default:

```go
func NewGeminiClient(ctx context.Context, apiKey, model string) (*GeminiClient, error) {
    if model == "" {
        model = "gemini-3-flash-preview"
    }
    // ... existing code
}
```

**Step 2: Wire up in `cmd/powder-hunter/main.go`**

```go
geminiModel := os.Getenv("GEMINI_MODEL")
geminiClient, err := evaluation.NewGeminiClient(ctx, apiKey, geminiModel)
```

**Step 3: Add to `.env.example`**

```
# Gemini model to use for evaluations (default: gemini-3-flash-preview)
# GEMINI_MODEL=gemini-3-flash-preview
```

**Step 4: Run tests**

```bash
go test ./...
```

**Step 5: Commit**

```bash
git add -A
git commit -m "feat: make Gemini model configurable via GEMINI_MODEL env var"
```

---

## Phase 5: Error Handling & Type Safety

### Task 5.1: Add `ParseTier` and `ParseStormState` validators

**Files:**
- Create: `domain/parse.go`
- Create: `domain/parse_test.go`
- Modify: `storage/storms.go:187-188` — use validators
- Modify: `storage/evaluations.go:201-202` — use validators
- Modify: `evaluation/gemini.go:130` — use validator at LLM parse boundary

**Step 1: Write the failing tests**

Create `domain/parse_test.go`:

```go
package domain

import "testing"

func TestParseTier(t *testing.T) {
    tests := []struct {
        input string
        want  Tier
        ok    bool
    }{
        {"DROP_EVERYTHING", TierDropEverything, true},
        {"WORTH_A_LOOK", TierWorthALook, true},
        {"ON_THE_RADAR", TierOnTheRadar, true},
        {"INVALID", "", false},
        {"", "", false},
    }
    for _, tt := range tests {
        t.Run(tt.input, func(t *testing.T) {
            got, err := ParseTier(tt.input)
            if tt.ok && err != nil {
                t.Fatalf("unexpected error: %v", err)
            }
            if !tt.ok && err == nil {
                t.Fatal("expected error, got nil")
            }
            if got != tt.want {
                t.Errorf("got %q, want %q", got, tt.want)
            }
        })
    }
}

func TestParseStormState(t *testing.T) {
    tests := []struct {
        input string
        want  StormState
        ok    bool
    }{
        {"detected", StormDetected, true},
        {"evaluated", StormEvaluated, true},
        {"briefed", StormBriefed, true},
        {"updated", StormUpdated, true},
        {"expired", StormExpired, true},
        {"INVALID", "", false},
    }
    for _, tt := range tests {
        t.Run(tt.input, func(t *testing.T) {
            got, err := ParseStormState(tt.input)
            if tt.ok && err != nil {
                t.Fatalf("unexpected error: %v", err)
            }
            if !tt.ok && err == nil {
                t.Fatal("expected error, got nil")
            }
            if got != tt.want {
                t.Errorf("got %q, want %q", got, tt.want)
            }
        })
    }
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./domain/ -run "TestParse" -v
```

**Step 3: Write implementation**

Create `domain/parse.go`:

```go
package domain

import "fmt"

func ParseTier(s string) (Tier, error) {
    switch Tier(s) {
    case TierDropEverything, TierWorthALook, TierOnTheRadar:
        return Tier(s), nil
    default:
        return "", fmt.Errorf("invalid tier: %q", s)
    }
}

func ParseStormState(s string) (StormState, error) {
    switch StormState(s) {
    case StormDetected, StormEvaluated, StormBriefed, StormUpdated, StormExpired:
        return StormState(s), nil
    default:
        return "", fmt.Errorf("invalid storm state: %q", s)
    }
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./domain/ -run "TestParse" -v
```

**Step 5: Use validators at boundaries**

In `storage/storms.go:187-188`, replace:
```go
st.State = domain.StormState(state)
st.CurrentTier = domain.Tier(tier)
```
with:
```go
st.State, err = domain.ParseStormState(state)
if err != nil {
    return domain.Storm{}, fmt.Errorf("invalid storm state in DB: %w", err)
}
if tier != "" {
    st.CurrentTier, err = domain.ParseTier(tier)
    if err != nil {
        return domain.Storm{}, fmt.Errorf("invalid tier in DB: %w", err)
    }
}
```

Similarly in `storage/evaluations.go:202` and `evaluation/gemini.go` where tier is parsed from LLM response.

**Step 6: Run tests**

```bash
go test ./...
```

**Step 7: Commit**

```bash
git add -A
git commit -m "feat: add ParseTier and ParseStormState validators at data boundaries"
```

---

### Task 5.2: Fix silent `time.Parse` errors in storage layer

**Files:**
- Modify: `storage/storms.go:151-156, 189-193`
- Modify: `storage/evaluations.go:201`
- Modify: `storage/prompts.go:105`

**Step 1: Fix `parseOptionalTime` to return an error**

In `storage/storms.go:151-157`:

```go
func parseOptionalTime(s string) (time.Time, error) {
    if s == "" {
        return time.Time{}, nil
    }
    t, err := time.Parse(time.RFC3339, s)
    if err != nil {
        return time.Time{}, fmt.Errorf("parse time %q: %w", s, err)
    }
    return t, nil
}
```

**Step 2: Fix `scanStorm` to check all time.Parse errors**

In `storage/storms.go:189-193`, replace:

```go
st.WindowStart, _ = time.Parse(time.RFC3339, windowStart)
st.WindowEnd, _ = time.Parse(time.RFC3339, windowEnd)
st.DetectedAt, _ = time.Parse(time.RFC3339, detectedAt)
st.LastEvaluatedAt = parseOptionalTime(lastEval)
st.LastPostedAt = parseOptionalTime(lastPosted)
```

with:

```go
if st.WindowStart, err = time.Parse(time.RFC3339, windowStart); err != nil {
    return domain.Storm{}, fmt.Errorf("parse window_start: %w", err)
}
if st.WindowEnd, err = time.Parse(time.RFC3339, windowEnd); err != nil {
    return domain.Storm{}, fmt.Errorf("parse window_end: %w", err)
}
if st.DetectedAt, err = time.Parse(time.RFC3339, detectedAt); err != nil {
    return domain.Storm{}, fmt.Errorf("parse detected_at: %w", err)
}
if st.LastEvaluatedAt, err = parseOptionalTime(lastEval); err != nil {
    return domain.Storm{}, fmt.Errorf("parse last_evaluated_at: %w", err)
}
if st.LastPostedAt, err = parseOptionalTime(lastPosted); err != nil {
    return domain.Storm{}, fmt.Errorf("parse last_posted_at: %w", err)
}
```

**Step 3: Fix `scanEvaluation` similarly**

In `storage/evaluations.go:201`:
```go
if e.EvaluatedAt, err = time.Parse(time.RFC3339, evaluatedAt); err != nil {
    return domain.Evaluation{}, fmt.Errorf("parse evaluated_at: %w", err)
}
```

**Step 4: Fix `storage/prompts.go:105` similarly**

**Step 5: Run tests**

```bash
go test ./...
```

**Step 6: Commit**

```bash
git add storage/
git commit -m "fix: handle time.Parse errors in storage layer instead of silently discarding"
```

---

### Task 5.3: Fix other error handling issues

**Files:**
- Modify: `discord/webhook.go:149` — check `io.ReadAll` error
- Modify: `cmd/powder-hunter/main.go:37-61` — check `scanner.Err()`

**Step 1: Fix `discord/webhook.go:149`**

Replace:
```go
body, _ := io.ReadAll(resp.Body)
```
with:
```go
body, err := io.ReadAll(resp.Body)
if err != nil {
    return nil, true, fmt.Errorf("read discord response body: %w", err)
}
```

**Step 2: Fix `loadEnvFile` scanner error check**

After the `for scanner.Scan()` loop in `main.go`, add:
```go
if err := scanner.Err(); err != nil {
    slog.Warn("error reading .env file", "error", err)
}
```

**Step 3: Run tests**

```bash
go test ./...
```

**Step 4: Commit**

```bash
git add discord/webhook.go cmd/powder-hunter/main.go
git commit -m "fix: check io.ReadAll and scanner.Err() instead of discarding errors"
```

---

## Phase 6: Deduplicate & Simplify

### Task 6.1: Eliminate duplicated scan functions

**Files:**
- Modify: `storage/evaluations.go` — delete `scanEvaluationRow`, use `scanEvaluation` instead
- Modify: `storage/storms.go` — delete `scanStormRow`, use `scanStorm` instead
- Modify: `storage/regions.go` — delete `scanRegionRow` if it exists (it may already use `scanner`)

The `scanner` interface at `storage/regions.go:133-135` already abstracts over `*sql.Row` and `*sql.Rows`. Apply the same pattern to storms and evaluations.

**Step 1: Delete `scanStormRow` in `storage/storms.go`**

Find all callers of `scanStormRow(row)` and replace with `scanStorm(row)`.

**Step 2: Delete `scanEvaluationRow` in `storage/evaluations.go`**

Find all callers and replace with `scanEvaluation(row)`.

**Step 3: Run tests**

```bash
go test ./...
```

**Step 4: Commit**

```bash
git add storage/
git commit -m "refactor: eliminate duplicated scan functions using scanner interface"
```

---

### Task 6.2: Consolidate duplicated functions

**Files:**
- Modify: `domain/comparison.go:39-50` — export `TierRank`
- Modify: `discord/formatter.go:130-137` — use `domain.TierRank` instead of local `tierRank`
- Create: `domain/convert.go` — move `cToF` here
- Modify: `evaluation/prompt.go:138-140` — use `domain.CToF`
- Modify: `trace/formatter.go:389-391` — use `domain.CToF`
- Consolidate: `afdCoversSnowDays` — move to one package, import from the other

**Step 1: Export `tierRank` as `TierRank` in `domain/comparison.go`**

```go
// TierRank maps tiers to an ordinal for comparison. Higher rank = better storm.
func TierRank(t Tier) int {
```

Update the internal call in `Compare` to use `TierRank`.

**Step 2: Replace `tierRank` in `discord/formatter.go` with `domain.TierRank`**

Delete the local `tierRank` function and replace calls.

**Step 3: Create `domain/convert.go` with shared `CToF`**

```go
package domain

// CToF converts Celsius to Fahrenheit.
func CToF(c float64) float64 {
    return c*9.0/5.0 + 32.0
}
```

**Step 4: Replace local `cToF` in `evaluation/prompt.go` and `trace/formatter.go`**

**Step 5: Move `afdCoversSnowDays` to one location**

It exists in both `evaluation/prompt.go:502-518` and `trace/formatter.go:365-376`. Move to `evaluation/prompt.go` (or `domain/`) and import from `trace/`. Since `trace` already imports `domain`, `domain` is the cleanest home if the function only uses domain types.

**Step 6: Run tests**

```bash
go test ./...
```

**Step 7: Commit**

```bash
git add -A
git commit -m "refactor: consolidate duplicated tierRank, cToF, afdCoversSnowDays"
```

---

### Task 6.3: Extract hardcoded cost magic number

**Files:**
- Modify: `pipeline/pipeline.go:497` — replace `0.015` with a named constant or config field

**Step 1: Add to `BudgetConfig`**

```go
type BudgetConfig struct {
    MonthlyLimitUSD     float64
    WarningThreshold    float64
    EstimatedCostPerEval float64 // default: 0.015
}
```

**Step 2: Use it in the pipeline**

Replace:
```go
p.costTracker.RecordCost(gctx, scan.Storm.ID, scan.Region.ID, 0.015, true)
```
with:
```go
cost := p.budgetCfg.EstimatedCostPerEval
if cost == 0 {
    cost = 0.015
}
p.costTracker.RecordCost(gctx, scan.Storm.ID, scan.Region.ID, cost, true)
```

**Step 3: Run tests**

```bash
go test ./...
```

**Step 4: Commit**

```bash
git add pipeline/pipeline.go
git commit -m "refactor: extract hardcoded Gemini cost estimate to BudgetConfig"
```

---

## Phase 7: CLI UX & Documentation

### Task 7.1: Add `version` command with build-time injection

**Files:**
- Modify: `cmd/powder-hunter/main.go` — add version var and command

**Step 1: Add version variable**

At the top of `main.go`:
```go
var version = "dev"
```

**Step 2: Add version case in the switch**

```go
case "version":
    fmt.Println(version)
    return 0
```

**Step 3: Update `printUsage` to include version**

```go
fmt.Fprintln(os.Stderr, `Usage: powder-hunter <command> [flags]

Commands:
  run       Execute the full pipeline (scan -> evaluate -> compare -> post)
  trace     Run pipeline for one region with human-readable debug output
  regions   List all regions from seed data
  profile   View or update user profile
  seed      Initialize or update region/resort database
  replay    Re-run a past evaluation with a different prompt version
  reset     Delete all storms and evaluations (keeps regions, profiles, prompts)
  version   Print the version and exit`)
```

**Step 4: Update Dockerfile to inject version at build time**

```dockerfile
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-X main.version=${VERSION}" -o /powder-hunter ./cmd/powder-hunter/
```

**Step 5: Run tests**

```bash
go test ./...
```

**Step 6: Commit**

```bash
git add cmd/powder-hunter/main.go Dockerfile
git commit -m "feat: add version command with build-time injection"
```

---

### Task 7.2: Improve CLI error messages with guidance

**Files:**
- Modify: `cmd/powder-hunter/main.go:131-139`

**Step 1: Add guidance to missing API key errors**

Replace:
```go
slog.Error("GOOGLE_API_KEY environment variable is required")
```
with:
```go
slog.Error("GOOGLE_API_KEY is required. Get a free key at https://aistudio.google.com/apikey")
```

Replace:
```go
slog.Error("DISCORD_WEBHOOK_URL environment variable is required (or use --dry-run)")
```
with:
```go
slog.Error("DISCORD_WEBHOOK_URL is required (or use --dry-run). Create one in Discord: Server Settings > Integrations > Webhooks")
```

**Step 2: Commit**

```bash
git add cmd/powder-hunter/main.go
git commit -m "feat: add guidance URLs to missing API key error messages"
```

---

### Task 7.3: Rework README for public audience

**Files:**
- Modify: `README.md`

**Step 1: Restructure the README**

The README is already good but needs these changes:
- Remove the Unraid-specific section or move it to a separate `docs/unraid.md`
- Add a "How it works" section briefly explaining the pipeline (scan -> detect -> evaluate -> post)
- Change `git clone <repo-url>` to the actual public URL
- Add a "Limitations" section: US-focused weather (NWS is US-only), friction tiers auto-calculated from your location, Discord-only notifications
- Add a "Contributing" section (standard for open source)
- Remove the "TODO: Published Docker Images" section (will be replaced by actual CI/CD)
- Add badges (license, Go version)

**Step 2: Create `docs/unraid.md` for Unraid-specific instructions**

Move the Unraid content from README into its own doc.

**Step 3: Commit**

```bash
git add README.md docs/unraid.md
git commit -m "docs: rework README for public audience, move Unraid docs to docs/"
```

---

## Phase 8: Test Coverage

### Task 8.1: Add NWS parsing tests

**Files:**
- Create: `weather/nws_parse_test.go`

Write fixture-based tests for:
- `parseISO8601Duration` — test `PT1H`, `PT6H`, `P1D`, `P1DT12H`, `PT30M`
- `parseGridpointForecast` — test with a hand-crafted NWS JSON fixture verifying snowfall totals, day/night split, temperature min/max

These are the most critical untested paths — 230 lines of complex time-series aggregation with zero tests.

**Step 1: Write tests using golden fixtures**

Create a test JSON file in `weather/testdata/nws_gridpoint.json` with known values, then assert the parsed output matches expected daily forecasts.

**Step 2: Run tests**

```bash
go test ./weather/ -run TestParse -v
```

**Step 3: Commit**

```bash
git add weather/
git commit -m "test: add NWS parsing tests for ISO8601 duration and gridpoint forecast"
```

---

### Task 8.2: Add storage round-trip tests

**Files:**
- Create: `storage/storage_test.go`

Write tests for:
- Storm CRUD: create, find overlapping, update state
- Evaluation save/retrieve with full JSON round-trip (WeatherSnapshot, StructuredResponse, ResortInsights)
- Profile persistence

**Step 1: Write tests using temp SQLite databases**

```go
func setupTestDB(t *testing.T) *DB {
    t.Helper()
    tmpDir := t.TempDir()
    db, err := Open(filepath.Join(tmpDir, "test.db"))
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { db.Close() })
    return db
}
```

**Step 2: Run tests**

```bash
go test ./storage/ -v
```

**Step 3: Commit**

```bash
git add storage/
git commit -m "test: add storage round-trip tests for storms, evaluations, profiles"
```

---

### Task 8.3: Add prompt rendering tests

**Files:**
- Create: `evaluation/prompt_test.go`

Write tests for:
- `CToF` unit conversions (after moving to domain, test there)
- `FormatRainLineRisk` — freezing level exactly at base, at summit, below both, above both
- `afdCoversSnowDays` — snow inside and outside the 7-day horizon

**Step 1-4: Standard TDD cycle**

**Step 5: Commit**

```bash
git add evaluation/
git commit -m "test: add prompt rendering tests for unit conversions and rain-line risk"
```

---

## Phase 9: Distribution & CI/CD

### Task 9.1: Add Makefile

**Files:**
- Create: `Makefile`

```makefile
.PHONY: build test lint clean

VERSION ?= dev

build:
	CGO_ENABLED=0 go build -ldflags="-X main.version=$(VERSION)" -o powder-hunter ./cmd/powder-hunter/

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -f powder-hunter
```

**Step 1: Create the file and verify**

```bash
make build && ./powder-hunter version
make test
```

**Step 2: Commit**

```bash
git add Makefile
git commit -m "chore: add Makefile for build, test, lint"
```

---

### Task 9.2: Add GitHub Actions CI/CD

**Files:**
- Create: `.github/workflows/ci.yml` — test on push/PR
- Create: `.github/workflows/release.yml` — build + publish Docker on tag

**Step 1: Create CI workflow**

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      - run: go test ./...
      - run: go vet ./...
```

**Step 2: Create release workflow**

```yaml
name: Release

on:
  push:
    tags: ['v*']

permissions:
  contents: write
  packages: write

jobs:
  docker:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-buildx-action@v3
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - uses: docker/build-push-action@v5
        with:
          push: true
          build-args: VERSION=${{ github.ref_name }}
          platforms: linux/amd64,linux/arm64
          tags: |
            ghcr.io/${{ github.repository }}:${{ github.ref_name }}
            ghcr.io/${{ github.repository }}:latest
```

**Step 3: Commit**

```bash
git add .github/
git commit -m "ci: add GitHub Actions for CI tests and Docker release publishing"
```

---

### Task 9.3: Update docker-compose and Unraid template for published images

**Files:**
- Modify: `docker-compose.yml` — add commented-out `image:` line for published image
- Modify: `unraid/powder-hunter.xml` — point to ghcr.io image
- Create: `unraid/icon.png` — project icon (placeholder or real)

**Step 1: Update docker-compose.yml**

```yaml
services:
  powder-hunter:
    # To build from source: uncomment 'build' and comment 'image'
    # build: .
    image: ghcr.io/seanmeyer/powder-hunter:latest
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

**Step 2: Update Unraid template repository reference**

In `unraid/powder-hunter.xml`, change:
```xml
<Repository>ghcr.io/seanmeyer/powder-hunter</Repository>
```

**Step 3: Add icon**

Create or source a simple icon PNG (512x512). This can be a placeholder snowflake or mountain icon.

**Step 4: Commit**

```bash
git add docker-compose.yml unraid/
git commit -m "chore: update docker-compose and Unraid template for published images"
```

---

## Phase Summary

| Phase | Tasks | Focus |
|-------|-------|-------|
| 1 | 1.1-1.2 | Legal & security blockers |
| 2 | 2.1-2.4 | Remove dead code & unimplemented features |
| 3 | 3.1-3.2 | Rename & restructure packages |
| 4 | 4.1-4.4 | Genericize Denver defaults, auto-friction tiers |
| 5 | 5.1-5.3 | Error handling & type safety |
| 6 | 6.1-6.3 | Deduplicate & simplify |
| 7 | 7.1-7.3 | CLI UX & documentation |
| 8 | 8.1-8.3 | Test coverage |
| 9 | 9.1-9.3 | Distribution & CI/CD |

**Total: 22 tasks across 9 phases.**
