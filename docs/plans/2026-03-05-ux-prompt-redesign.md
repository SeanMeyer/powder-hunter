# UX & Prompt Redesign: Storm Briefing Over Resort Picking

Date: 2026-03-05

## Problem Statement

The current output is too focused on ranking resorts and picking "winners" rather than
answering the core question: **is this storm worth going to, and what should I do about it?**

Specific issues observed from the Mar 5 pipeline run (14 evaluations):

1. **Triple repetition in grouped posts** — the comparison reasoning appears as the Content
   text, the embed description, AND the Top Pick field value. Users see the same paragraph
   three times before reaching any new information.

2. **Resort-ranking framing** — the comparison prompt asks "pick the single best region," the
   eval prompt asks for `top_resort_picks` ranked by suitability. This makes the LLM focus on
   which resort beats which, rather than whether the powder opportunity is worth pursuing.

3. **Inconsistent singleton vs. group format** — singleton storms (Alaska, Banff) use a
   single dense post with all details. Grouped storms (Colorado Front Range) use a
   winner-picking summary + detail posts. The user experience should be the same regardless.

4. **Information overload in the notification** — the opening post includes strategy,
   logistics, pros/cons, day-by-day, cost breakdowns. A phone notification should tell you
   "should I care?" in 5 seconds, with details available when you tap in.

5. **Crowd/powder-longevity under-emphasized** — crowd estimates exist but are generic
   ("Moderate Friday, Extreme Saturday"). They don't answer the real question: will I find
   untouched powder, and for how long?

6. **"Expert terrain" over-emphasis** — the LLM spends significant output on terrain
   character, steeps, bowls, and chutes. The user cares about terrain only insofar as it
   serves the powder — protected trees during wind, terrain that holds stashes, etc.

7. **Minor formatting issues** — thread names show raw IDs (`co_front_range:local_drive`),
   "Change: new" shown on first evaluations, 0" days in snowfall breakdowns.

## Design Goals

- Every storm notification follows the same format, whether it covers 1 region or 5
- The notification preview answers "should I care?" in one glance
- Detail is available in-thread for those who want to dive in
- The LLM evaluates storms through the lens of a powder chaser, not a resort reviewer
- Untouched powder availability is a central concern, not a sidebar

## User Priority Framework

The prompt and output should reflect these priorities in order:

1. **Is there going to be enough powder?** — Quantity relative to travel friction, density,
   quality. 8" of blower locally is worth it; 8" of concrete requiring a flight is not.
2. **Will I find untouched powder? For how long?** — Crowd dynamics, resort size and terrain
   spread, timing (mid-week vs weekend), natural filters (road closures, wind holds that
   keep fair-weather skiers away), hike-to zones and stash longevity.
3. **Will the terrain deliver for this powder?** — Protected trees if windy, steep enough for
   the depth, good access given conditions. This is context for the storm, not a resort
   review.

## Architecture

### Data Flow (Current)

```
Weather APIs --> Per-region eval (grounded research + structured extraction = 2 Gemini calls)
             --> Cross-region comparison (1 Gemini call, groups only)
             --> Discord posting (grouped summary + detail updates)
```

### Data Flow (New)

```
Weather APIs --> Per-region eval (grounded research + structured extraction = 2 Gemini calls)
             --> Storm briefing (1 Gemini call, ALL storms including singletons)
             --> Discord posting (unified format: briefing post + detail post(s))
```

Key change: every storm goes through the briefing call, whether singleton or group. This
replaces the current "comparison" call. For groups, the briefing synthesizes across regions.
For singletons, it distills the single evaluation into a concise briefing.

### LLM Call Responsibilities

**Per-region evaluation** (unchanged call structure, reframed output):
- Deep analysis with Google Search grounding
- Snow quality, crowd/powder-longevity assessment, logistics, day-by-day
- Resort insights (replaces resort picks) — notable findings that affect the decision
- No `recommendation` field — that responsibility moves to the briefing call
- Keeps `summary` as the short hook line

**Storm briefing** (replaces comparison call):
- No grounding needed — all data already gathered
- Input: condensed summaries from per-region evals
- Output: 2-4 sentence storm briefing answering "what's happening, should I care, what
  should I do" — plus best day and a brief action recommendation
- For groups: synthesizes across regions without picking a winner
- For singletons: distills the single eval into a briefing
- No winner/runner-up structure

### Discord Thread Structure (Unified)

Every storm thread, regardless of group size:

**Post 1 — Opening briefing** (this is the notification):
- Content: storm briefing text (2-4 sentences)
- Embed: tier color, macro region display name, best day, summary line
- Minimal — scannable in 5 seconds on a phone
- @here ping only for DROP_EVERYTHING

**Post 2..N — Detail per region**:
- One embed per region with full analysis
- Recommendation (storm opportunity framing, not resort ranking)
- Strategy (when to go, what to do)
- Resort insights (notable findings, not a leaderboard)
- Snow quality, crowd/powder assessment, closure risk
- Day-by-day breakdown
- Logistics (lodging, transport, costs)
- Pros/cons

**Post N+1... — Updates** (re-evaluations):
- Appended to existing thread
- Shows what changed and updated recommendation
- @here ping if upgraded to DROP_EVERYTHING

## Prompt Changes

### Per-Region Eval Prompt

**Reframe the evaluation factors section:**
- Lead with the powder-chaser priority framework (powder amount, untouched availability,
  terrain fit for this storm)
- Make crowd/powder-longevity a first-class concern: "How fast does powder get tracked out
  at this resort? Are there stashes that last into day 2-3? Does the timing create natural
  crowd filters?"
- Terrain section reframed: terrain matters only as it serves the powder (protected trees
  for wind, steep enough for depth, holds stashes)
- De-emphasize "expert terrain" language — the subscriber is expert, that's assumed

**Output schema changes:**
- Keep `recommendation` — reframe from "go to Resort X" to "this storm is worth chasing
  because..." The per-region eval has full grounded context (resort schedules, road
  conditions, real-time data) that the briefing call won't have. The recommendation is the
  context-rich "what to do" advice that shows up in the detail post. The briefing call
  summarizes it, not replaces it.
- Replace `top_resort_picks` with `resort_insights`: array of `{resort, insight}` where
  insight is a notable finding (closure creating powder stash, special access consideration,
  pass coverage note) — not a ranking or "why this resort is best"
- Keep all other fields: summary, strategy, snow_quality, crowd_estimate, closure_risk,
  best_ski_day, best_ski_day_reason, key_factors, logistics, day_by_day

**Strategy field reframe:**
- Currently: "which resort to go to and when"
- New: "how to approach this storm" — when to arrive, when to ski vs work, what to watch for.
  Naturally references resorts but isn't structured as a resort recommendation.

### Storm Briefing Prompt (Replaces Comparison)

New prompt that works for both singletons and groups:

```
You are synthesizing storm evaluation data into a concise briefing for a powder chaser.

Your job is to write a 2-4 sentence storm briefing that answers:
- What's the powder situation? (totals, quality, how long the window is)
- Will there be untouched powder? (crowd dynamics, timing, stash longevity)
- What should the subscriber do? (go now, keep watching, take PTO Friday, etc.)

Do NOT rank resorts or pick winners. The briefing is about the storm opportunity,
not which specific resort to visit. Resort-level detail belongs in the individual
region briefings that follow.

For multi-region groups: synthesize the overall picture. If one zone is dramatically
better, mention it naturally, but don't frame it as a competition.

[region summaries]

Return JSON:
- briefing: 2-4 sentence storm briefing (the main notification text)
- best_day: YYYY-MM-DD
- best_day_reason: why this is the day
- action: one of "go_now", "book_flexibly", "keep_watching"
```

### Subscriber Profile Tweak

Current `Preferences` field:
> "Favorite is moderately steep untouched powder -- trees, open bowls, whatever. Steep and
> deep is fun too. Strong preference for avoiding crowds because the goal is untracked
> powder, but a big enough resort can let you find it even with crowds, so it's nuanced."

Consider strengthening the powder-chase framing:
> "Primary goal is powder strike missions -- finding deep, untouched snow. Terrain preference
> is moderately steep trees and open bowls, but terrain is secondary to powder quality and
> availability. Strong preference for situations where untouched runs last for hours or days,
> not minutes. Crowds matter primarily as they affect powder longevity -- a big resort with
> extensive expert terrain can absorb crowds and still have stashes, while a small resort
> gets tracked out fast."

## Formatter Changes

### FormatGroupedStorm -> FormatStormBriefing

New function that produces the opening post for ALL storms:

```go
func FormatStormBriefing(briefing StormBriefing) WebhookPayload {
    // Content: briefing text (2-4 sentences)
    // Embed: tier color, region name, best day field
    // ThreadName: "{emoji} {MacroRegionDisplayName} -- {date range}"
    // @here only for DROP_EVERYTHING
}
```

### FormatUpdate (detail post)

Simplify the existing FormatUpdate to serve as the detail post:
- Remove "Change: new" display
- Filter 0"/trace days from Total Snowfall
- Replace "Top Picks" with "Resort Insights" (different framing, same layout)
- Keep all other fields — the detail post should be comprehensive

### Thread Naming Fix

`MacroRegionDisplayName` is called with the group key `"co_front_range:local_drive"` but
the lookup map only has `"co_front_range"`. Fix: split on `:` and look up just the macro
region portion. Drop the friction tier suffix from the thread name entirely.

### Singleton Unification

Remove the `postOne()` special case for singletons. All storms go through:
1. Post briefing (opening post, creates thread)
2. Post detail(s) (follow-up in thread)

## Migration Notes

- Prompt version bumps to v3.0.0 (breaking schema change: recommendation removed,
  top_resort_picks replaced with resort_insights)
- Comparison schema replaced with briefing schema
- Existing threads in Discord are unaffected — new storms get the new format
- `delivered` flag bug should be investigated separately (likely WAL timing or
  a subtle issue in the grouped delivery path)

## Out of Scope

- Discord MCP integration for reading messages
- Historical data migration
- Changes to storm detection or weather fetching
- Changes to re-evaluation cooldown logic
- `delivered` flag bug fix (separate investigation)
