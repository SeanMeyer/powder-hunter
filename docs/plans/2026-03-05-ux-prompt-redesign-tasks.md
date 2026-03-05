# UX & Prompt Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Shift powder-hunter from resort-ranking output to storm-briefing output, with unified Discord thread format and powder-chaser-focused prompts.

**Architecture:** Three layers: (1) domain/schema changes to replace ResortPick with ResortInsight and add BriefingResult, (2) prompt rewrite for per-region eval + new storm briefing prompt replacing comparison, (3) Discord formatter unification so all storms follow briefing + detail post pattern.

**Tech Stack:** Go 1.23+, google.golang.org/genai (Gemini), modernc.org/sqlite, Discord webhooks

**Design doc:** `docs/plans/2026-03-05-ux-prompt-redesign.md`

---

### Task 1: Replace ResortPick with ResortInsight in domain types

**Files:**
- Modify: `domain/evaluation.go:19` (ResortPick type) and `:62-65` (ResortPick struct)
- Modify: `storage/evaluations.go` (column name references in marshal/unmarshal)
- Modify: `storage/sqlite.go` (schema migration if column name changes)

**Step 1: Add ResortInsight type alongside ResortPick**

In `domain/evaluation.go`, add:

```go
// ResortInsight captures a notable finding about a resort that affects the
// storm decision -- closures that create powder stashes, special access
// considerations, pass coverage notes. Not a ranking or recommendation.
type ResortInsight struct {
	Resort  string
	Insight string
}
```

Update the Evaluation struct field:

```go
// Change from:
TopResortPicks     []ResortPick
// To:
ResortInsights     []ResortInsight
```

**Step 2: Update storage layer**

In `storage/evaluations.go`, the column is `top_resort_picks` in SQLite. We'll keep the DB
column name to avoid a migration, but change the Go field name. Update all references:

- `SaveEvaluation`: change `e.TopResortPicks` -> `e.ResortInsights` in the marshal call,
  keep the SQL column as `top_resort_picks`
- `scanEvaluationRow` and `scanEvaluation`: change the unmarshal target to `e.ResortInsights`
  and update the JSON struct tags on `ResortInsight` to match `ResortPick`'s JSON keys
  (`resort` and `reason` -> `resort` and `insight`)

Note: Since the DB stores JSON, old rows with `{"resort":"X","reason":"Y"}` will fail to
unmarshal if we change the JSON key. Two options:
- (A) Keep JSON keys as `resort` and `reason` on ResortInsight (simplest, no migration)
- (B) Write a migration to rename keys in existing JSON

**Go with (A)** -- keep JSON keys `resort` and `reason` on ResortInsight for backwards compat
with existing DB rows. The Go field names change but the serialized form stays the same.

```go
type ResortInsight struct {
	Resort  string `json:"resort"`
	Insight string `json:"reason"` // JSON key kept as "reason" for DB backwards compat
}
```

**Step 3: Update all Go references to TopResortPicks/ResortPick**

Files to update (find with `grep -rn "TopResortPicks\|ResortPick" --include="*.go"`):
- `evaluation/gemini.go`: `parseResortPicks` -> `parseResortInsights`, field mappings
- `pipeline/pipeline.go:718-720`: comparison summary builder references TopResortPicks
- `discord/formatter.go:246-248`: `formatResortPicks` -> `formatResortInsights`
- `discord/formatter.go:369-378`: `formatResortPicks` function
- `discord/formatter.go:396-403`: `buildCompactEmbed` references TopResortPicks

**Step 4: Run tests**

Run: `go build ./... && go test ./...`
Expected: All existing tests pass (the rename is structural, not behavioral)

**Step 5: Commit**

```bash
git add -A && git commit -m "refactor: replace ResortPick with ResortInsight

Rename the domain type and all references. Keep JSON serialization keys
unchanged for backwards compatibility with existing DB rows. The semantic
shift from 'ranked picks' to 'notable insights' is reflected in field
names; prompt changes come in a later commit."
```

---

### Task 2: Add BriefingResult type and update comparison/briefing interfaces

**Files:**
- Modify: `evaluation/evaluation.go:29-65` (CompareContext, RegionSummary, ComparisonResult, Comparer)
- Modify: `evaluation/comparison.go` (schema, prompt, response parsing)

**Step 1: Replace ComparisonResult with BriefingResult**

In `evaluation/evaluation.go`, replace:

```go
// ComparisonResult is the LLM's synthesis across multiple regions.
type ComparisonResult struct {
	TopPickRegion  string
	TopPickName    string
	Reasoning      string
	RunnerUp       string
	RunnerUpReason string
	RawResponse    string
}
```

With:

```go
// BriefingResult is the LLM's synthesized storm briefing for one or more regions.
// Used as the opening Discord notification for all storms (singletons and groups).
type BriefingResult struct {
	Briefing      string // 2-4 sentence storm briefing (the notification text)
	BestDay       string // YYYY-MM-DD
	BestDayReason string
	Action        string // "go_now", "book_flexibly", "keep_watching"
	RawResponse   string
}
```

**Step 2: Update RegionSummary**

Remove `TopPick` and `TopPickReason` fields. Add `CrowdEstimate` and `Strategy` to give
the briefing call more context about powder longevity and what to do:

```go
type RegionSummary struct {
	RegionID       string
	RegionName     string
	Tier           string
	Snowfall       string
	SnowQuality    string
	CrowdEstimate  string
	Strategy       string
	Recommendation string
	BestDay        string
	BestDayReason  string
	LodgingCost    string
	FlightCost     string
	CarRental      string
}
```

**Step 3: Rename Comparer interface**

```go
// Briefer synthesizes per-region evaluations into a storm briefing.
type Briefer interface {
	BriefStorm(ctx context.Context, bc BriefingContext) (BriefingResult, error)
}
```

Rename `CompareContext` to `BriefingContext`:

```go
type BriefingContext struct {
	MacroRegionName string
	FrictionTier    string
	Summaries       []RegionSummary
}
```

**Step 4: Update all references**

Files referencing old types (find with grep):
- `pipeline/pipeline.go`: `ComparisonResult` -> `BriefingResult`, `Comparer` -> `Briefer`,
  `CompareRegions` -> `BriefStorm`, `CompareContext` -> `BriefingContext`
- `discord/formatter.go`: `GroupedPost.Comparison` field type changes
- `evaluation/gemini.go`: `CompareRegions` -> `BriefStorm`, rename method
- `pipeline/pipeline_test.go`: any mocks of Comparer interface

**Step 5: Run tests**

Run: `go build ./... && go test ./...`
Expected: PASS

**Step 6: Commit**

```bash
git add -A && git commit -m "refactor: replace ComparisonResult with BriefingResult

Rename comparison types to briefing types. Remove winner/runner-up fields,
add briefing text and action fields. Prompt and formatter changes follow."
```

---

### Task 3: Rewrite the storm briefing prompt (replaces comparison prompt)

**Files:**
- Modify: `evaluation/comparison.go` (entire file: schema, prompt builder, response parser)

**Step 1: Rewrite the Gemini schema**

Replace `comparisonSchema()` with `briefingSchema()`:

```go
func briefingSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"briefing": {
				Type:        genai.TypeString,
				Description: "2-4 sentence storm briefing: what's the powder situation, will there be untouched snow, what should the subscriber do. Do NOT rank resorts or pick winners.",
			},
			"best_day": {
				Type:        genai.TypeString,
				Description: "The single best date to ski in YYYY-MM-DD format",
			},
			"best_day_reason": {
				Type:        genai.TypeString,
				Description: "Why this is the best day",
			},
			"action": {
				Type:        genai.TypeString,
				Enum:        []string{"go_now", "book_flexibly", "keep_watching"},
				Description: "Recommended action: go_now (book immediately), book_flexibly (plan with refundable options), keep_watching (monitor but don't commit)",
			},
		},
		Required: []string{"briefing", "best_day", "best_day_reason", "action"},
	}
}
```

**Step 2: Rewrite the prompt builder**

Replace `buildComparisonPrompt` with `buildBriefingPrompt`:

```go
func buildBriefingPrompt(bc BriefingContext) string {
	var b strings.Builder

	fmt.Fprintf(&b, `You are synthesizing storm evaluation data into a concise briefing for a powder chaser.

Write a 2-4 sentence storm briefing for the %s area that answers:
- What's the powder situation? (totals, quality, how long is the window)
- Will there be untouched powder? (crowds, timing, how long do stashes last)
- What should the subscriber do? (go now, keep watching, take PTO on a specific day, etc.)

Do NOT rank resorts or pick winners. The briefing is about the storm opportunity, not which
specific resort to visit. Resort-level detail belongs in the individual region briefings below.

If one zone has a notably different picture (e.g., much more snow, or a closure that creates
an opportunity), mention it naturally -- but don't frame it as a competition.

`, bc.MacroRegionName)

	for i, s := range bc.Summaries {
		fmt.Fprintf(&b, "## Region %d: %s (%s) [ID: %s]\n", i+1, s.RegionName, s.Tier, s.RegionID)
		fmt.Fprintf(&b, "- Snowfall: %s\n", s.Snowfall)
		fmt.Fprintf(&b, "- Snow Quality: %s\n", s.SnowQuality)
		fmt.Fprintf(&b, "- Crowds/Powder Longevity: %s\n", s.CrowdEstimate)
		fmt.Fprintf(&b, "- Best Day: %s — %s\n", s.BestDay, s.BestDayReason)
		fmt.Fprintf(&b, "- Recommendation: %s\n", s.Recommendation)
		fmt.Fprintf(&b, "- Strategy: %s\n\n", s.Strategy)
	}

	b.WriteString("Write a concise storm briefing and recommend an action.\n")
	return b.String()
}
```

**Step 3: Update the LLM call and response parsing**

Rename `CompareRegions` to `BriefStorm` in `evaluation/comparison.go`:

```go
func (g *GeminiClient) BriefStorm(ctx context.Context, bc BriefingContext) (BriefingResult, error) {
	prompt := buildBriefingPrompt(bc)

	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema:   briefingSchema(),
	}

	contents := []*genai.Content{
		genai.NewContentFromText(prompt, genai.RoleUser),
	}

	resp, err := g.generateWithRetry(ctx, contents, config, "briefing")
	if err != nil {
		return BriefingResult{}, fmt.Errorf("gemini briefing: %w", err)
	}

	rawText := resp.Text()

	var structured map[string]any
	if err := json.Unmarshal([]byte(rawText), &structured); err != nil {
		return BriefingResult{}, fmt.Errorf("parse gemini briefing response: %w", err)
	}

	return BriefingResult{
		Briefing:      stringField(structured, "briefing"),
		BestDay:       stringField(structured, "best_day"),
		BestDayReason: stringField(structured, "best_day_reason"),
		Action:        stringField(structured, "action"),
		RawResponse:   rawText,
	}, nil
}
```

Also update the delegator in `evaluation/gemini.go` (the `GeminiEvaluator.CompareRegions`
method around line 310):

```go
func (e *GeminiEvaluator) BriefStorm(ctx context.Context, bc BriefingContext) (BriefingResult, error) {
	return e.gemini.BriefStorm(ctx, bc)
}
```

**Step 4: Run tests**

Run: `go build ./... && go test ./...`
Expected: PASS

**Step 5: Commit**

```bash
git add -A && git commit -m "feat: replace comparison prompt with storm briefing prompt

New prompt asks the LLM to synthesize a storm briefing rather than pick
a winner. Output is a 2-4 sentence briefing + action recommendation.
Works for both singletons and multi-region groups."
```

---

### Task 4: Update pipeline to run briefing for ALL storms (including singletons)

**Files:**
- Modify: `pipeline/pipeline.go:680-758` (BuildGroupedResults method)
- Modify: `pipeline/pipeline.go:764-881` (PostGrouped method)

**Step 1: Update BuildGroupedResults to call BriefStorm for all groups**

Currently at line 694, the comparison call only runs for groups with `len(members) >= 2`.
Change the condition to run for all groups:

```go
// Was: if len(members) >= 2 && p.comparer != nil {
// Now:
if p.briefer != nil {
```

Update the field name (`p.comparer` -> `p.briefer`) and method call
(`p.comparer.CompareRegions` -> `p.briefer.BriefStorm`).

Update the summary builder at lines 695-722 to use the new `RegionSummary` fields
(remove `TopPick`/`TopPickReason`, add `CrowdEstimate` and `Strategy`):

```go
rs := evaluation.RegionSummary{
	RegionID:       r.Region.ID,
	RegionName:     r.Region.Name,
	Tier:           string(r.Evaluation.Tier),
	Snowfall:       strings.Join(snowParts, ", "),
	SnowQuality:    r.Evaluation.SnowQuality,
	CrowdEstimate:  r.Evaluation.CrowdEstimate,
	Strategy:       r.Evaluation.Strategy,
	Recommendation: r.Evaluation.Recommendation,
	BestDay:        r.Evaluation.BestSkiDay.Format("Mon Jan 2"),
	BestDayReason:  r.Evaluation.BestSkiDayReason,
	LodgingCost:    r.Evaluation.LogisticsSummary.LodgingCost,
	FlightCost:     r.Evaluation.LogisticsSummary.FlightCost,
	CarRental:      r.Evaluation.LogisticsSummary.CarRental,
}
```

Also rename `GroupedResult.Comparison` to `GroupedResult.Briefing` with the new type.

**Step 2: Update PostGrouped to use unified posting for all groups**

Remove the singleton special case at line 783. ALL groups now follow the same path:
1. Post the briefing (opening post, creates thread)
2. Post detail for each region as follow-up in the thread

```go
func (p *Pipeline) PostGrouped(ctx context.Context, groups []GroupedResult) (int, error) {
	if p.dryRun || p.poster == nil {
		// ... dry-run logging unchanged ...
	}

	posted := 0
	for _, g := range groups {
		// All groups (singleton or multi) use the same posting path.
		threadID, err := p.poster.PostBriefing(ctx, g)
		if err != nil {
			// ... error handling ...
			continue
		}

		// Post full detail for each region as follow-up in the thread.
		for _, r := range g.Results {
			if err := p.poster.PostDetail(ctx, r.Evaluation, r.Region, threadID); err != nil {
				// ... error logging ...
			}
		}

		// Update storms and mark delivered (existing loop at lines 851-870)
		// ... unchanged ...

		posted += len(g.Results)
	}
	return posted, nil
}
```

**Step 3: Update the Pipeline struct field**

Rename `comparer` to `briefer` in the Pipeline struct and constructor. Update the
`Comparer` interface reference to `Briefer`.

**Step 4: Update the Poster interface**

In `discord/webhook.go`, add `PostBriefing` and `PostDetail` to the `Poster` interface.
These replace the current trio of `PostNew`/`PostUpdate`/`PostGrouped`:

```go
type Poster interface {
	PostBriefing(ctx context.Context, group GroupedResult) (threadID string, err error)
	PostDetail(ctx context.Context, eval domain.Evaluation, region domain.Region, threadID string) error
	// PostUpdate remains for re-evaluation updates to existing threads.
	PostUpdate(ctx context.Context, eval domain.Evaluation, region domain.Region, threadID string) error
}
```

Note: `PostNew` and `PostGrouped` are removed. `PostBriefing` replaces both. `PostDetail`
is essentially the same as `PostUpdate` but formats differently (no "Change" field, uses
the detail format). `PostUpdate` stays for re-evaluations.

Also update the fake in `discord/fake.go`.

**Step 5: Run tests**

Run: `go build ./... && go test ./...`
Expected: PASS (pipeline_test.go may need mock updates for the new interface)

**Step 6: Commit**

```bash
git add -A && git commit -m "feat: run storm briefing for all groups including singletons

Remove the singleton special case in PostGrouped. All storms now follow:
briefing post (opening) + detail post(s) per region. Update Poster
interface with PostBriefing/PostDetail methods."
```

---

### Task 5: Rewrite Discord formatter for unified thread format

**Files:**
- Modify: `discord/formatter.go` (major rewrite of posting functions)
- Modify: `discord/webhook.go` (implement new Poster methods)

**Step 1: Write FormatBriefing (replaces FormatGroupedStorm and FormatNewStorm)**

This produces the opening post for ALL storms. Short, scannable, no resort details.

```go
// FormatBriefing creates the opening post for a storm thread. This is the
// notification users see -- it should be scannable in 5 seconds.
func FormatBriefing(bp BriefingPost) WebhookPayload {
	highestTier := highestTierFromEvals(bp.Evaluations)
	emoji := tierEmoji(highestTier)

	embed := Embed{
		Title:       fmt.Sprintf("%s %s", emoji, bp.MacroRegionName),
		Description: bp.Briefing.Briefing,
		Color:       tierColor(highestTier),
	}

	if bp.Briefing.BestDay != "" {
		bestDay, _ := time.Parse("2006-01-02", bp.Briefing.BestDay)
		if !bestDay.IsZero() {
			text := bestDay.Format("Mon Jan 2")
			if bp.Briefing.BestDayReason != "" {
				text += " — " + bp.Briefing.BestDayReason
			}
			embed.Fields = append(embed.Fields, EmbedField{
				Name: "Best Day", Value: text, Inline: true,
			})
		}
	}

	dateRange := formatWindowDatesFromEvals(bp.Evaluations)
	threadName := fmt.Sprintf("%s %s — %s", emoji, bp.MacroRegionName, dateRange)

	payload := WebhookPayload{
		Content:    bp.Briefing.Briefing,
		Embeds:     []Embed{embed},
		ThreadName: threadName,
	}

	if highestTier == domain.TierDropEverything {
		payload.Content = "@here\n" + bp.Briefing.Briefing
		payload.AllowedMentions = &AllowedMentions{Parse: []string{"everyone"}}
	}

	return payload
}
```

New `BriefingPost` type:

```go
// BriefingPost holds everything needed for the opening storm thread post.
type BriefingPost struct {
	MacroRegionName string
	Briefing        evaluation.BriefingResult
	Evaluations     []EvalWithRegion
}
```

**Step 2: Write FormatDetail (replaces FormatUpdate for initial detail posts)**

This is the per-region detail posted as follow-up in the thread. Comprehensive but
no "Change" field.

```go
// FormatDetail creates a detail post for a single region within a storm thread.
func FormatDetail(eval domain.Evaluation, region domain.Region) WebhookPayload {
	embed := buildEmbed(eval, region)
	embed.Title = fmt.Sprintf("%s %s", tierEmoji(eval.Tier), region.Name)
	embed.Footer = &EmbedFooter{Text: fmt.Sprintf("powder-hunter · %s", eval.Tier)}

	return WebhookPayload{
		Content: eval.Summary,
		Embeds:  []Embed{embed},
	}
}
```

**Step 3: Update buildFields to use ResortInsights**

Change the "Top Picks" field to "Resort Insights" and update `formatResortPicks` to
`formatResortInsights`:

```go
if len(eval.ResortInsights) > 0 {
	fields = append(fields, EmbedField{
		Name: "Resort Insights", Value: formatResortInsights(eval.ResortInsights), Inline: false,
	})
}
```

```go
func formatResortInsights(insights []domain.ResortInsight) string {
	var sb strings.Builder
	for i, ins := range insights {
		fmt.Fprintf(&sb, "**%s** — %s", ins.Resort, ins.Insight)
		if i < len(insights)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
```

**Step 4: Update FormatUpdate for re-evaluations**

Keep `FormatUpdate` for re-evaluation posts but:
- Only show the "Change" field when `ChangeClass != ChangeNew`
- Filter 0"/trace days from `totalSnowfallLine`

```go
// In FormatUpdate, change:
if eval.ChangeClass != "" {
// To:
if eval.ChangeClass != "" && eval.ChangeClass != domain.ChangeNew {
```

In `totalSnowfallLine`, filter insignificant days:

```go
func totalSnowfallLine(eval domain.Evaluation) string {
	if len(eval.DayByDay) == 0 {
		return ""
	}
	var parts []string
	for _, d := range eval.DayByDay {
		if d.Snowfall != "" && d.Snowfall != "0\"" && d.Snowfall != "0.0\"" &&
			d.Snowfall != "Trace" && d.Snowfall != "0" {
			parts = append(parts, fmt.Sprintf("%s: %s", d.Date.Format("Jan 2"), d.Snowfall))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}
```

**Step 5: Remove old functions**

Delete `FormatGroupedStorm`, `FormatNewStorm`, `buildCompactEmbed`, and the old
`GroupedPost` type. These are fully replaced by `FormatBriefing` and `FormatDetail`.

**Step 6: Implement PostBriefing and PostDetail on WebhookClient**

In `discord/webhook.go`:

```go
func (w *WebhookClient) PostBriefing(ctx context.Context, group pipeline.GroupedResult) (string, error) {
	// This creates a circular import -- see note below about the import.
	// The solution is to pass a BriefingPost to PostBriefing instead.
}
```

**Import note:** `PostBriefing` can't take `pipeline.GroupedResult` directly due to circular
imports (discord -> pipeline -> discord). Instead, the pipeline should construct a
`discord.BriefingPost` and pass that. Update the Poster interface:

```go
type Poster interface {
	PostBriefing(ctx context.Context, bp BriefingPost) (threadID string, err error)
	PostDetail(ctx context.Context, eval domain.Evaluation, region domain.Region, threadID string) error
	PostUpdate(ctx context.Context, eval domain.Evaluation, region domain.Region, threadID string) error
}
```

```go
func (w *WebhookClient) PostBriefing(ctx context.Context, bp BriefingPost) (string, error) {
	payload := FormatBriefing(bp)
	url := w.webhookURL + "?wait=true"
	body, err := w.postWithRetry(ctx, url, payload)
	if err != nil {
		return "", fmt.Errorf("post storm briefing for %s: %w", bp.MacroRegionName, err)
	}
	var resp threadResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse discord response: %w", err)
	}
	if resp.ChannelID == "" {
		return "", fmt.Errorf("discord response missing channel_id")
	}
	return resp.ChannelID, nil
}

func (w *WebhookClient) PostDetail(ctx context.Context, eval domain.Evaluation, region domain.Region, threadID string) error {
	payload := FormatDetail(eval, region)
	url := fmt.Sprintf("%s?thread_id=%s", w.webhookURL, threadID)
	if _, err := w.postWithRetry(ctx, url, payload); err != nil {
		return fmt.Errorf("post detail for region %s thread %s: %w", region.ID, threadID, err)
	}
	return nil
}
```

**Step 7: Run tests**

Run: `go build ./... && go test ./...`
Expected: PASS

**Step 8: Commit**

```bash
git add -A && git commit -m "feat: unified Discord thread format with briefing + detail posts

Replace FormatGroupedStorm/FormatNewStorm with FormatBriefing (short
notification) and FormatDetail (comprehensive per-region). All storms
now use the same thread structure. Filter 0-inch days from snowfall.
Suppress 'Change: new' on initial evaluations."
```

---

### Task 6: Fix thread naming (MacroRegionDisplayName with friction suffix)

**Files:**
- Modify: `pipeline/pipeline.go:726` (where MacroRegionDisplayName is called)
- Modify: `domain/macro_region.go:33` (MacroRegionDisplayName function)

**Step 1: Write failing test**

In `domain/grouping_test.go`, add:

```go
func TestMacroRegionDisplayNameStripsGroupKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"co_front_range:local_drive", "CO Front Range"},
		{"pnw_cascades:flight", "PNW Cascades"},
		{"ak_chugach:flight", "Alaska Chugach"},
		{"unknown_region:flight", "unknown_region"},
	}
	for _, tt := range tests {
		got := MacroRegionDisplayNameFromKey(tt.key)
		if got != tt.want {
			t.Errorf("MacroRegionDisplayNameFromKey(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./domain/ -run TestMacroRegionDisplayNameStripsGroupKey -v`
Expected: FAIL (function doesn't exist yet)

**Step 3: Implement MacroRegionDisplayNameFromKey**

In `domain/macro_region.go`:

```go
// MacroRegionDisplayNameFromKey extracts the macro region from a group key
// (which has format "macro_region:friction_tier") and returns its display name.
func MacroRegionDisplayNameFromKey(groupKey string) string {
	macroRegion := groupKey
	if i := strings.Index(groupKey, ":"); i >= 0 {
		macroRegion = groupKey[:i]
	}
	return MacroRegionDisplayName(macroRegion)
}
```

Add `"strings"` to the import.

**Step 4: Run test to verify it passes**

Run: `go test ./domain/ -run TestMacroRegionDisplayNameStripsGroupKey -v`
Expected: PASS

**Step 5: Update pipeline to use the new function**

In `pipeline/pipeline.go`, change:

```go
// Was:
MacroRegionName: domain.MacroRegionDisplayName(sg.Key),
// Now:
MacroRegionName: domain.MacroRegionDisplayNameFromKey(sg.Key),
```

**Step 6: Run all tests**

Run: `go test ./...`
Expected: PASS

**Step 7: Commit**

```bash
git add -A && git commit -m "fix: thread names show human-readable region names

MacroRegionDisplayName was receiving the full group key including the
friction tier suffix (e.g. 'co_front_range:local_drive'). Add
MacroRegionDisplayNameFromKey that strips the suffix before lookup."
```

---

### Task 7: Rewrite per-region evaluation prompt

**Files:**
- Modify: `seed/prompts.go` (the prompt template and version)
- Modify: `evaluation/gemini.go` (schema changes for resort_insights)

**Step 1: Update the prompt template**

This is the largest single change. Key modifications to `stormEvalPromptTemplate`:

1. **Add a powder-chaser priority framework** section near the top, after the tier definitions
2. **Reframe the evaluation factors** to lead with powder amount, then untouched availability, then terrain fit
3. **Rewrite the crowd section** to focus on powder longevity, not just crowd level
4. **Rewrite the terrain section** to focus on how terrain serves the powder
5. **Reframe the output instructions** for recommendation, resort_insights, strategy
6. **Replace `top_resort_picks` with `resort_insights`** in the JSON output spec
7. **Bump version** to `v3.0.0`

Key sections to add/rewrite in the prompt:

After tier definitions, add:

```
## Your Evaluation Lens

You are a powder chaser evaluating whether this storm is worth pursuing. Think about it
in this priority order:

1. **Is there enough powder to justify the trip?** What counts as "enough" depends entirely
   on travel friction. 8" of quality snow justifies a 1-hour drive. It takes 15-20"+ of
   quality snow to justify a cross-country flight. Factor in density -- 12" of 8:1 Cascade
   concrete skis very differently than 12" of 18:1 champagne.

2. **Will I find untouched powder, and for how long?** This is the make-or-break question.
   Consider: How fast does this resort's terrain get tracked out? A 3,000-acre resort with
   extensive glades holds powder for days; a 600-acre resort with 3 main runs gets tracked
   by noon. Are there hike-to zones, sidecountry stashes, or lesser-known areas that hold
   powder longer? Does the timing create natural crowd filters -- mid-week storms, road
   closures that keep fair-weather skiers away, wind holds that preserve upper-mountain snow?
   Is the snow volume so large that it doesn't matter -- 30" at a big resort means days of
   untouched runs even with crowds?

3. **Will the terrain deliver for this specific storm?** If it's windy, are there protected
   trees? Is the terrain steep enough for the expected depth? Will avalanche control delays
   eat into the ski day? This is context for the storm assessment, not a resort review.
```

Rewrite the output instructions for `recommendation`:

```
- recommendation: 2-3 sentence assessment of the storm opportunity and what the subscriber
  should do about it. Focus on the powder opportunity, not on which resort to visit.
  Frame it as: "This storm is worth [action] because [powder/timing/crowd reasoning]."
  Resort names can appear naturally but shouldn't be the focus.
```

Replace `top_resort_picks` with `resort_insights`:

```
- resort_insights: array of notable findings about specific resorts that affect the storm
  decision. Each entry has:
  - resort: the resort name
  - insight: a specific finding that matters for this storm -- a closure that creates a
    powder stash, an operating schedule quirk, a pass coverage note, an access advantage.
    Not a ranking or "why this resort is best." Only include insights that would change
    the subscriber's decision or approach.
```

Rewrite the `strategy` instruction:

```
- strategy: how to approach this storm -- when to arrive, which days to ski vs work, what
  to watch for, what conditions would change the plan. Resort names appear naturally as
  part of the tactical advice, but the strategy is about the storm window, not about
  choosing between resorts.
```

Rewrite `crowd_estimate` instruction:

```
- crowd_estimate: assessment focused on powder longevity. Don't just say "moderate" or
  "heavy." Answer: will the subscriber find untouched powder? For how long? What factors
  help (mid-week timing, road closures filtering crowds, resort size absorbing skiers) or
  hurt (weekend timing, holiday proximity, small resort that tracks out fast)?
```

**Step 2: Update the Gemini schema**

In `evaluation/gemini.go`, update `stormEvalSchema()`:

Replace `top_resort_picks` with:

```go
"resort_insights": {
	Type:        genai.TypeArray,
	Description: "Notable findings about specific resorts that affect the storm decision. Not a ranking.",
	Items: &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"resort": {Type: genai.TypeString, Description: "Resort name"},
			"insight": {Type: genai.TypeString, Description: "A specific finding that matters for this storm (closure, schedule quirk, access advantage, pass coverage)"},
		},
		Required: []string{"resort", "insight"},
	},
},
```

Update `parseResortPicks` -> `parseResortInsights` to read `insight` field:

```go
func parseResortInsights(m map[string]any) []domain.ResortInsight {
	raw, ok := m["resort_insights"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]domain.ResortInsight, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, domain.ResortInsight{
			Resort:  stringField(entry, "resort"),
			Insight: stringField(entry, "insight"),
		})
	}
	return result
}
```

Update the field mapping in `EvaluateStorm`:

```go
result.ResortInsights = parseResortInsights(structured)
```

Update `GeminiResult` struct to use `ResortInsights []domain.ResortInsight`.

Update the required fields list to reference `"resort_insights"` instead of
`"top_resort_picks"`.

**Step 3: Update subscriber profile**

In `seed/prompts.go`, update `DefaultProfile().Preferences`:

```go
Preferences: "Primary goal is powder strike missions — finding deep, untouched snow. " +
	"Terrain preference is moderately steep trees and open bowls, but terrain is secondary " +
	"to powder quality and availability. Strong preference for situations where untouched " +
	"runs last for hours or days, not minutes. Crowds matter primarily as they affect powder " +
	"longevity — a big resort with extensive expert terrain can absorb crowds and still have " +
	"stashes, while a small resort gets tracked out fast.",
```

**Step 4: Run tests**

Run: `go build ./... && go test ./...`
Expected: PASS

**Step 5: Smoke test with trace**

Run: `go run ./cmd/powder-hunter trace --region co_front_range --show-prompt --weather-only`

Verify the rendered prompt shows the new evaluation lens, powder-chaser framing, and
resort_insights output spec.

Then run a full trace (requires GOOGLE_API_KEY):

Run: `go run ./cmd/powder-hunter trace --region co_front_range`

Verify the output shows recommendation framed around the storm opportunity, resort_insights
instead of ranked picks, and crowd assessment focused on powder longevity.

**Step 6: Commit**

```bash
git add -A && git commit -m "feat: rewrite evaluation prompt for powder-chaser framing

Prompt v3.0.0: lead with powder-chaser priority framework (powder amount,
untouched availability, terrain fit). Replace top_resort_picks with
resort_insights. Reframe recommendation, strategy, and crowd_estimate to
focus on the storm opportunity rather than resort ranking. Update
subscriber profile preferences to emphasize powder strike missions."
```

---

### Task 8: Update trace formatter for new field names

**Files:**
- Modify: `trace/formatter.go` (FormatEvaluation and FormatDiscordPreview)

**Step 1: Update FormatEvaluation**

Replace `TopResortPicks` references with `ResortInsights`. The trace output should show
insights rather than picks:

```go
// Was:
// (no explicit resort picks display in trace formatter currently)
// Update FormatDiscordPreview to use FormatDetail instead of FormatNewStorm
```

In `FormatDiscordPreview`, change:

```go
// Was:
payload := discord.FormatNewStorm(eval, region)
// Now:
payload := discord.FormatDetail(eval, region)
```

**Step 2: Run tests**

Run: `go build ./... && go test ./...`
Expected: PASS

**Step 3: Commit**

```bash
git add -A && git commit -m "fix: update trace formatter for new field names and FormatDetail"
```

---

### Task 9: Integration test with full trace

**Step 1: Run weather-only trace to verify prompt rendering**

Run: `go run ./cmd/powder-hunter trace --region co_front_range --show-prompt --weather-only`

Verify:
- Prompt includes "Your Evaluation Lens" section
- Output spec shows `resort_insights` not `top_resort_picks`
- Subscriber profile mentions "powder strike missions"

**Step 2: Run full trace with LLM (requires GOOGLE_API_KEY)**

Run: `go run ./cmd/powder-hunter trace --region co_front_range`

Verify:
- Recommendation is about the storm opportunity, not resort ranking
- Resort insights show notable findings, not "why this resort is best"
- Crowd estimate discusses powder longevity
- Strategy is about approaching the storm, not choosing resorts

**Step 3: Run full test suite**

Run: `go test ./...`
Expected: ALL PASS

**Step 4: Final commit if any fixups needed**

```bash
git add -A && git commit -m "fix: integration test fixups"
```

---

## Task Dependency Order

```
Task 1 (ResortInsight type) ──┐
                               ├── Task 5 (formatter rewrite)
Task 2 (BriefingResult type) ─┤
                               ├── Task 4 (pipeline unification)
Task 3 (briefing prompt) ─────┘
                                    │
Task 6 (thread naming fix) ─── independent, can run anytime
                                    │
Task 7 (eval prompt rewrite) ─── depends on Task 1 (ResortInsight)
                                    │
Task 8 (trace formatter) ──── depends on Tasks 1, 5
                                    │
Task 9 (integration test) ──── depends on all above
```

Recommended execution order: 1 → 2 → 3 → 6 → 4 → 5 → 7 → 8 → 9
