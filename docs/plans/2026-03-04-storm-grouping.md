# Storm Grouping by Macro-Region Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Group storm alerts by macro-region + friction tier so the user gets one consolidated Discord thread per group with an LLM comparison picking the best play, instead of N separate threads for regions hit by the same storm system.

**Architecture:** Define static macro-region groupings in seed data. Add a `Group()` pure function in `domain/` that buckets `CompareResult` entries by macro-region + friction tier + overlapping storm window. Add a new pipeline stage between Compare and Post that runs a lightweight LLM comparison per group. Post one Discord thread per group with the comparison up top and individual evaluations below.

**Tech Stack:** Go 1.23+, existing Gemini client (no grounding needed for comparison — just synthesis), existing Discord webhook client, SQLite for persisting group thread IDs.

---

### Task 1: Add MacroRegion to Region and seed data

**Files:**
- Modify: `domain/resort.go` (add MacroRegion field to Region)
- Modify: `seed/data/regions.json` (add macro_region to each region)
- Modify: `seed/regions.go` (parse macro_region from JSON)

**Step 1: Add MacroRegion field to Region struct**

In `domain/resort.go`, add a `MacroRegion` field to `Region`:

```go
type Region struct {
	ID                  string
	Name                string
	Latitude            float64
	Longitude           float64
	FrictionTier        FrictionTier
	NearThresholdIn     float64
	ExtendedThresholdIn float64
	Country             string
	Timezone            string
	Logistics           RegionLogistics
	MacroRegion         string // static grouping for storm correlation (e.g. "pnw_cascades")
}
```

**Step 2: Add macro_region to every region in `seed/data/regions.json`**

Add `"macro_region": "<value>"` to each region object. The full mapping:

| macro_region | region IDs |
|---|---|
| `co_front_range` | co_i70_corridor, co_front_range |
| `co_roaring_fork` | co_roaring_fork |
| `co_steamboat` | co_steamboat |
| `co_southern` | co_san_juans, co_wolf_creek |
| `co_western_slope` | co_powderhorn, co_route_50 |
| `wasatch` | ut_little_cottonwood, ut_big_cottonwood, ut_park_city, ut_northern |
| `ut_southern` | ut_southern |
| `sierra_nevada` | ca_tahoe_north, ca_tahoe_south, ca_central_sierra, ca_eastern_sierra |
| `pnw_cascades` | wa_cascades_central, wa_cascades_south, wa_cascades_north, or_mt_hood |
| `pnw_interior` | wa_eastern, or_central, or_eastern |
| `northern_rockies` | mt_western, mt_southwest, id_panhandle |
| `snake_river_tetons` | wy_tetons, id_central_southern |
| `bc_coast` | bc_coast |
| `bc_interior` | bc_interior_powder_highway, bc_okanagan |
| `alberta_rockies` | ab_banff, ab_jasper |
| `northeast` | vt_northern, vt_nh_southern, nh_white_mountains, me_ny_rugged |
| `nm_northern` | nm_northern |
| `nm_southern` | nm_southern |
| `az_flagstaff` | az_flagstaff |
| `ca_socal` | ca_socal |
| `ca_norcal` | ca_norcal_volcano |
| `ak_chugach` | ak_chugach |
| `mi_upper_peninsula` | mi_upper_peninsula |

Standalone regions use their own region ID as the macro_region value.

**Step 3: Parse macro_region in seed/regions.go**

In `seed/regions.go`, add `MacroRegion string` to the JSON struct and assign it to `region.MacroRegion`.

**Step 4: Add macro_region column to regions table**

In `storage/schema.sql`, add `macro_region TEXT NOT NULL DEFAULT ''` to the regions table.

In `storage/sqlite.go` `runMigrations()`, add:
```go
`ALTER TABLE regions ADD COLUMN macro_region TEXT NOT NULL DEFAULT ''`,
```

**Step 5: Wire macro_region through storage UpsertRegion**

In `storage/regions.go`, add `macro_region` to the INSERT and UPDATE in `UpsertRegion`, and to the scan in region-reading queries.

**Step 6: Verify**

Run: `go build ./... && go test ./... -count=1`

**Step 7: Commit**

```
feat: add macro_region field to regions for storm grouping
```

---

### Task 2: Domain grouping logic

**Files:**
- Create: `domain/grouping.go`
- Create: `domain/grouping_test.go`

**Step 1: Write the test file `domain/grouping_test.go`**

```go
package domain

import (
	"testing"
	"time"
)

func TestGroupByMacroRegion(t *testing.T) {
	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)
	nextWeek := now.AddDate(0, 0, 7)

	makeResult := func(regionID, macroRegion string, friction FrictionTier, windowStart, windowEnd time.Time, tier Tier) StormGroupInput {
		return StormGroupInput{
			RegionID:    regionID,
			MacroRegion: macroRegion,
			Friction:    friction,
			WindowStart: windowStart,
			WindowEnd:   windowEnd,
			Tier:        tier,
		}
	}

	tests := []struct {
		name       string
		inputs     []StormGroupInput
		wantGroups int
		wantSizes  map[string]int // group key → count
	}{
		{
			name: "same macro-region and friction groups together",
			inputs: []StormGroupInput{
				makeResult("wa_central", "pnw_cascades", FrictionFlight, tomorrow, nextWeek, TierWorthALook),
				makeResult("wa_north", "pnw_cascades", FrictionFlight, tomorrow, nextWeek, TierWorthALook),
				makeResult("wa_south", "pnw_cascades", FrictionFlight, tomorrow, nextWeek, TierOnTheRadar),
			},
			wantGroups: 1,
		},
		{
			name: "different friction tiers split groups",
			inputs: []StormGroupInput{
				makeResult("co_i70", "co_front_range", FrictionLocalDrive, tomorrow, nextWeek, TierWorthALook),
				makeResult("co_roaring", "co_roaring_fork", FrictionRegionalDrive, tomorrow, nextWeek, TierWorthALook),
			},
			wantGroups: 2,
		},
		{
			name: "non-overlapping windows split groups",
			inputs: []StormGroupInput{
				makeResult("wa_central", "pnw_cascades", FrictionFlight, tomorrow, tomorrow.AddDate(0, 0, 3), TierWorthALook),
				makeResult("wa_north", "pnw_cascades", FrictionFlight, tomorrow.AddDate(0, 0, 10), nextWeek.AddDate(0, 0, 10), TierOnTheRadar),
			},
			wantGroups: 2,
		},
		{
			name: "single region stays as singleton group",
			inputs: []StormGroupInput{
				makeResult("ak_chugach", "ak_chugach", FrictionFlight, tomorrow, nextWeek, TierDropEverything),
			},
			wantGroups: 1,
		},
		{
			name: "different macro-regions split even with same friction",
			inputs: []StormGroupInput{
				makeResult("wa_central", "pnw_cascades", FrictionFlight, tomorrow, nextWeek, TierWorthALook),
				makeResult("mt_western", "northern_rockies", FrictionFlight, tomorrow, nextWeek, TierWorthALook),
			},
			wantGroups: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups := GroupByMacroRegion(tt.inputs)
			if len(groups) != tt.wantGroups {
				t.Errorf("got %d groups, want %d", len(groups), tt.wantGroups)
				for i, g := range groups {
					t.Logf("  group %d: key=%s, %d members", i, g.Key, len(g.Members))
				}
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./domain/... -run TestGroupByMacroRegion -v`
Expected: FAIL — types don't exist yet

**Step 3: Write `domain/grouping.go`**

```go
package domain

import "time"

// StormGroupInput holds the fields needed for grouping decisions.
// Extracted from CompareResult to keep grouping logic decoupled from pipeline types.
type StormGroupInput struct {
	RegionID    string
	MacroRegion string
	Friction    FrictionTier
	WindowStart time.Time
	WindowEnd   time.Time
	Tier        Tier
	Index       int // position in the original CompareResult slice
}

// StormGroup is a set of storm results that should be presented together.
type StormGroup struct {
	Key     string            // "macro_region:friction_tier" (for logging/dedup)
	Members []StormGroupInput // ordered by tier (highest first)
}

// GroupByMacroRegion buckets inputs by macro-region + friction tier, splitting
// groups whose storm windows don't overlap. Pure function, no I/O.
func GroupByMacroRegion(inputs []StormGroupInput) []StormGroup {
	type bucketKey struct {
		macroRegion string
		friction    FrictionTier
	}

	buckets := make(map[bucketKey][]StormGroupInput)
	var keyOrder []bucketKey

	for _, in := range inputs {
		k := bucketKey{macroRegion: in.MacroRegion, friction: in.Friction}
		if _, exists := buckets[k]; !exists {
			keyOrder = append(keyOrder, k)
		}
		buckets[k] = append(buckets[k], in)
	}

	var groups []StormGroup
	for _, k := range keyOrder {
		members := buckets[k]
		// Split into sub-groups by overlapping windows.
		for _, subgroup := range splitByWindowOverlap(members) {
			sortByTierDesc(subgroup)
			groups = append(groups, StormGroup{
				Key:     k.macroRegion + ":" + string(k.friction),
				Members: subgroup,
			})
		}
	}
	return groups
}

// splitByWindowOverlap partitions members into groups where all members'
// windows overlap with at least one other member in the group.
// Uses a simple greedy merge: extend the group window as members are added.
func splitByWindowOverlap(members []StormGroupInput) [][]StormGroupInput {
	if len(members) <= 1 {
		return [][]StormGroupInput{members}
	}

	type cluster struct {
		start   time.Time
		end     time.Time
		members []StormGroupInput
	}

	var clusters []cluster
	for _, m := range members {
		merged := false
		for i := range clusters {
			if windowsOverlap(clusters[i].start, clusters[i].end, m.WindowStart, m.WindowEnd) {
				clusters[i].members = append(clusters[i].members, m)
				if m.WindowStart.Before(clusters[i].start) {
					clusters[i].start = m.WindowStart
				}
				if m.WindowEnd.After(clusters[i].end) {
					clusters[i].end = m.WindowEnd
				}
				merged = true
				break
			}
		}
		if !merged {
			clusters = append(clusters, cluster{
				start:   m.WindowStart,
				end:     m.WindowEnd,
				members: []StormGroupInput{m},
			})
		}
	}

	result := make([][]StormGroupInput, len(clusters))
	for i, c := range clusters {
		result[i] = c.members
	}
	return result
}

func windowsOverlap(s1, e1, s2, e2 time.Time) bool {
	return !e1.Before(s2) && !e2.Before(s1)
}

// sortByTierDesc sorts members so DROP_EVERYTHING comes first, then WORTH_A_LOOK, then ON_THE_RADAR.
func sortByTierDesc(members []StormGroupInput) {
	tierRank := map[Tier]int{
		TierDropEverything: 0,
		TierWorthALook:     1,
		TierOnTheRadar:     2,
	}
	for i := 1; i < len(members); i++ {
		for j := i; j > 0 && tierRank[members[j].Tier] < tierRank[members[j-1].Tier]; j-- {
			members[j], members[j-1] = members[j-1], members[j]
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./domain/... -run TestGroupByMacroRegion -v`
Expected: PASS

**Step 5: Commit**

```
feat: add GroupByMacroRegion pure function for storm grouping
```

---

### Task 3: LLM comparison prompt and call

**Files:**
- Create: `evaluation/comparison.go`
- Modify: `evaluation/gemini.go` (add CompareStorms method to GeminiClient)
- Modify: `evaluation/evaluation.go` (add Comparer interface)

**Step 1: Define the Comparer interface in `evaluation/evaluation.go`**

```go
// CompareContext holds the inputs for a multi-region storm comparison.
type CompareContext struct {
	MacroRegion string
	FrictionTier string
	Summaries   []RegionSummary
}

// RegionSummary is a condensed version of one region's evaluation for the comparison prompt.
type RegionSummary struct {
	RegionID       string
	RegionName     string
	Tier           string
	Snowfall       string // total snowfall summary
	SnowQuality    string
	TopPick        string // best resort name
	TopPickReason  string
	BestDay        string
	BestDayReason  string
	Recommendation string
	LodgingCost    string
	FlightCost     string
	CarRental      string
}

// CompareResult is the LLM's synthesis across multiple regions.
type ComparisonResult struct {
	TopPickRegion  string // region ID of the recommended region
	TopPickName    string // human-readable region name
	Reasoning      string // why this region wins
	RunnerUp       string // runner-up region name (empty if only 1 region)
	RunnerUpReason string // why the runner-up is worth considering
	RawResponse    string
}

// Comparer synthesizes multiple region evaluations into a grouped recommendation.
type Comparer interface {
	CompareRegions(ctx context.Context, cc CompareContext) (ComparisonResult, error)
}
```

**Step 2: Implement CompareRegions in `evaluation/comparison.go`**

Build a prompt listing each region's key stats side-by-side, ask Gemini to pick the best option and explain why. No grounding search needed — all data is already evaluated. Use structured output schema for clean parsing.

The prompt should be concise — each region gets ~3-4 lines of stats, then ask: "Which region is the best play for this storm window and why? Consider snow quality, snow totals, resort terrain, logistics, cost, and crowd levels."

Use the existing `GeminiClient.generateWithRetry` (already has retry logic from our earlier work).

**Step 3: Add CompareRegions to GeminiEvaluator**

Wire through the `GeminiEvaluator` so it can be called alongside Evaluate.

**Step 4: Verify**

Run: `go build ./... && go test ./... -count=1`

**Step 5: Commit**

```
feat: add LLM comparison for multi-region storm groups
```

---

### Task 4: Grouped Discord formatting

**Files:**
- Modify: `discord/formatter.go` (add FormatGroupedStorm)
- Modify: `discord/webhook.go` (add PostGrouped to Poster interface)
- Modify: `discord/fake.go` (add PostGrouped to FakePoster)

**Step 1: Add PostGrouped to Poster interface**

```go
type Poster interface {
	PostNew(ctx context.Context, eval domain.Evaluation, region domain.Region) (threadID string, err error)
	PostUpdate(ctx context.Context, eval domain.Evaluation, region domain.Region, threadID string) error
	PostGrouped(ctx context.Context, group GroupedPost) (threadID string, err error)
}
```

**Step 2: Define GroupedPost type in `discord/formatter.go`**

```go
// GroupedPost holds everything needed to post a grouped storm alert.
type GroupedPost struct {
	MacroRegionName string // human-readable name for the thread title
	FrictionTier    domain.FrictionTier
	Comparison      evaluation.ComparisonResult
	Evaluations     []EvalWithRegion // individual evaluations in tier-descending order
	IsNew           bool             // true = create thread, false = update existing
	ThreadID        string           // set for updates
}

type EvalWithRegion struct {
	Evaluation domain.Evaluation
	Region     domain.Region
}
```

**Step 3: Implement FormatGroupedStorm**

The Discord embed structure:
- **Thread title:** `"{emoji} {MacroRegionName} — {date range}"`
- **First embed (comparison):** The LLM's top pick, reasoning, and runner-up
- **Subsequent embeds:** One per region (reuse existing `buildEmbed` logic), each as a compact card with tier, snowfall, top resort pick, and best day

Discord allows up to 10 embeds per message — sufficient for any macro-region group.

**Step 4: Implement PostGrouped on WebhookClient**

Similar to PostNew — send the grouped payload, return thread ID.

**Step 5: Add PostGrouped to FakePoster**

Record calls for test assertions.

**Step 6: Verify**

Run: `go build ./... && go test ./... -count=1`

**Step 7: Commit**

```
feat: add grouped storm Discord formatting with comparison embed
```

---

### Task 5: Pipeline Group stage and Post integration

**Files:**
- Modify: `pipeline/pipeline.go` (add Group stage, update Run and Post)

**Step 1: Add Group stage between Compare and Post in Run()**

```go
func (p *Pipeline) Run(ctx context.Context, regionFilter string) (RunSummary, error) {
	scans, err := p.Scan(ctx, regionFilter)
	// ...
	evals, err := p.Evaluate(ctx, scans, &summary)
	// ...
	compared, err := p.Compare(ctx, evals)
	// ...
	grouped := p.Group(ctx, compared)  // NEW
	if err := p.Post(ctx, grouped); err != nil {  // CHANGED: takes grouped instead of compared
	// ...
}
```

**Step 2: Implement Group()**

- Convert `[]CompareResult` to `[]StormGroupInput` (extracting region, macro-region, friction, window, tier)
- Call `domain.GroupByMacroRegion()`
- For groups with 2+ members: call `p.comparer.CompareRegions()` to get the LLM synthesis
- For singleton groups: skip the comparison (no point comparing one region to itself)
- Return `[]GroupedResult` which wraps groups with their comparison

**Step 3: Update Post() to handle grouped results**

- Singleton groups: use existing `PostNew`/`PostUpdate` (unchanged behavior)
- Multi-member groups: use `PostGrouped` with the comparison embed

**Step 4: Add Comparer to Pipeline struct**

```go
type Pipeline struct {
	// ... existing fields
	comparer evaluation.Comparer
}

func (p *Pipeline) WithComparer(c evaluation.Comparer) *Pipeline {
	p.comparer = c
	return p
}
```

Wire in `cmd/powder-hunter/main.go` — the `GeminiEvaluator` implements both `Evaluator` and `Comparer`.

**Step 5: Verify**

Run: `go build ./... && go test ./... -count=1`

**Step 6: Commit**

```
feat: add Group pipeline stage with LLM comparison for multi-region groups
```

---

### Task 6: Pipeline tests for grouping

**Files:**
- Modify: `pipeline/pipeline_test.go`

**Step 1: Test singleton group passes through unchanged**

Seed one region, run through Group stage, verify it produces a singleton that posts via the existing PostNew path.

**Step 2: Test multi-region group produces one grouped post**

Seed 3 regions in `pnw_cascades` with overlapping windows, run through Group, verify:
- Only 1 Discord thread created (not 3)
- The comparison was called with all 3 regions
- All 3 individual evaluations appear in the grouped post

**Step 3: Test different friction tiers produce separate groups**

Seed 2 regions in the same macro-region but different friction tiers, verify 2 separate posts.

**Step 4: Test non-overlapping windows split**

Seed 2 regions in the same macro-region + friction but with non-overlapping storm windows, verify 2 separate posts.

**Step 5: Verify all tests pass**

Run: `go test ./pipeline/... -v -count=1`

**Step 6: Commit**

```
test: add pipeline grouping tests for singleton, multi-region, friction split, and window split
```

---

### Task 7: Wire into main.go and add macro-region name mapping

**Files:**
- Modify: `cmd/powder-hunter/main.go`
- Create: `domain/macro_region.go` (human-readable name mapping)

**Step 1: Add macro-region display names**

```go
// MacroRegionName returns a human-readable name for a macro-region key.
var MacroRegionNames = map[string]string{
	"pnw_cascades":       "PNW Cascades",
	"pnw_interior":       "PNW Interior",
	"northern_rockies":   "Northern Rockies",
	"sierra_nevada":      "Sierra Nevada",
	"bc_coast":           "BC Coast",
	"bc_interior":        "BC Interior",
	"alberta_rockies":    "Alberta Rockies",
	"wasatch":            "Wasatch",
	"co_front_range":     "CO Front Range",
	"co_roaring_fork":    "CO Roaring Fork",
	"co_steamboat":       "CO Steamboat",
	"co_southern":        "CO Southern Mountains",
	"co_western_slope":   "CO Western Slope",
	"snake_river_tetons": "Snake River / Tetons",
	"northeast":          "Northeast",
	// Standalone regions fall back to region.Name
}
```

**Step 2: Wire comparer into main.go runPipeline()**

The `GeminiEvaluator` already has the Gemini client — add `CompareRegions` to it and pass it to the pipeline via `WithComparer`.

**Step 3: Update RunSummary log line**

Add `"grouped"` count and `"comparisons"` count to the pipeline complete log.

**Step 4: Full build and test**

Run: `go build ./... && go test ./... -count=1 -race`

**Step 5: Commit**

```
feat: wire storm grouping into CLI with macro-region display names
```

---

## Execution Order

Tasks are sequential — each builds on the previous:

1. **Task 1** — Seed data + schema (foundation)
2. **Task 2** — Grouping logic (pure domain, no dependencies)
3. **Task 3** — LLM comparison (needs evaluation package)
4. **Task 4** — Discord formatting (needs comparison types)
5. **Task 5** — Pipeline integration (wires everything together)
6. **Task 6** — Pipeline tests (validates integration)
7. **Task 7** — CLI wiring + polish (final assembly)

## Notes

- **Singleton groups** (one region in the macro-region this run) skip the comparison LLM call and post via the existing path — no change in behavior or cost for standalone storms.
- **Comparison LLM call is cheap** — no grounding search, just synthesis of data we already have. Estimated ~$0.003/call.
- **Macro-region groupings are in seed data** — easy to adjust by editing `regions.json` and re-running `powder-hunter seed`.
- **Existing tests must keep passing** — the singleton path is backward-compatible.
