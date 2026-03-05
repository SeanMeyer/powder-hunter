package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	genai "google.golang.org/genai"
)

// briefingSchema defines the structured output schema for the storm briefing call.
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

// BriefStorm calls Gemini to synthesize multiple region evaluations into a storm
// briefing. No grounding search is needed because all data has already been evaluated.
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

// buildBriefingPrompt creates the prompt that synthesizes region evaluations into
// a storm briefing. The prompt focuses on the overall storm opportunity rather than
// ranking individual resorts.
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
