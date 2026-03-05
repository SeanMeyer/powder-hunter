# 004: Accuracy Verification Pass

## Status: Future / Proposal

## Problem

LLM evaluations include operational claims that could be stale or
hallucinated: resort operating schedules, lift status, road closures,
current base depths. Grounding search helps but doesn't guarantee
accuracy, especially for rapidly changing conditions.

Example: the WA Cascades evaluation correctly identified Alpental's
Mon/Tue closure schedule — but this detail could easily become outdated
if the resort changes its schedule mid-season.

## Proposed Solution

After the primary evaluation, run a lightweight **verification pass** —
a second LLM call focused specifically on fact-checking operational
claims against live data.

## What to Verify

- **Operating schedules**: Resort open/close days, holiday hours
- **Road closures**: Pass conditions, chain requirements, traction law
- **Lift status**: Key lifts operational or closed for maintenance
- **Current base depth**: Claimed vs. reported snow depth
- **Pricing**: Lift ticket prices, lodging rates (if cited)

These are all things that change frequently and where grounding search
results may lag reality.

## How

1. Extract verifiable claims from the evaluation (schedule times, dates,
   road status, specific numbers)
2. Run a second Gemini call with GoogleSearch grounding, prompt:
   "Verify these specific claims against current data sources"
3. Compare verification results against original claims
4. Flag discrepancies or unverifiable claims

## When to Run

Only for evaluations that reach **WORTH_A_LOOK** or higher. Don't waste
API calls verifying ON_THE_RADAR storms that likely won't lead to action.

## Output

Options (not mutually exclusive):

- Add `VerificationNotes []string` to `domain.Evaluation` — free-text
  notes about what was verified and what couldn't be confirmed
- Add `Confidence string` field — "high" / "medium" / "low" based on
  how many claims were verifiable
- Append a "Verification" section to the Discord embed for WORTH_A_LOOK+
  storms

## Cost Considerations

- Each verification is one additional Gemini API call with grounding
- Only runs on ~30% of evaluations (WORTH_A_LOOK+ filter)
- Could batch-verify multiple claims in a single call
- Estimated cost: ~$0.01-0.03 per verification pass

## Open Questions

- Should verification block the initial Discord post, or post first and
  update with verification results?
- How to handle claims that can't be verified (no search results)?
- Should we cache verification results for claims that don't change
  within a storm window (e.g., resort schedules)?
