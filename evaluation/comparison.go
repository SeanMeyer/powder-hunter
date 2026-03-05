package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	genai "google.golang.org/genai"
)

// comparisonSchema defines the structured output schema for the comparison call.
func comparisonSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"top_pick_region_id": {
				Type:        genai.TypeString,
				Description: "The region ID of the best pick",
			},
			"top_pick_region_name": {
				Type:        genai.TypeString,
				Description: "Human-readable name of the best region",
			},
			"reasoning": {
				Type:        genai.TypeString,
				Description: "Why this region is the best play for this storm",
			},
			"runner_up_name": {
				Type:        genai.TypeString,
				Description: "Runner-up region name",
			},
			"runner_up_reason": {
				Type:        genai.TypeString,
				Description: "Why the runner-up is worth considering",
			},
		},
		Required: []string{
			"top_pick_region_id", "top_pick_region_name", "reasoning",
			"runner_up_name", "runner_up_reason",
		},
	}
}

// CompareRegions calls Gemini to synthesize multiple region evaluations and pick
// the best region within a macro-region group. No grounding search is needed
// because all data has already been evaluated.
func (g *GeminiClient) CompareRegions(ctx context.Context, cc CompareContext) (ComparisonResult, error) {
	prompt := buildComparisonPrompt(cc)

	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema:   comparisonSchema(),
	}

	contents := []*genai.Content{
		genai.NewContentFromText(prompt, genai.RoleUser),
	}

	resp, err := g.generateWithRetry(ctx, contents, config, "comparison")
	if err != nil {
		return ComparisonResult{}, fmt.Errorf("gemini comparison: %w", err)
	}

	rawText := resp.Text()

	var structured map[string]any
	if err := json.Unmarshal([]byte(rawText), &structured); err != nil {
		return ComparisonResult{}, fmt.Errorf("parse gemini comparison response: %w", err)
	}

	return ComparisonResult{
		TopPickRegion:  stringField(structured, "top_pick_region_id"),
		TopPickName:    stringField(structured, "top_pick_region_name"),
		Reasoning:      stringField(structured, "reasoning"),
		RunnerUp:       stringField(structured, "runner_up_name"),
		RunnerUpReason: stringField(structured, "runner_up_reason"),
		RawResponse:    rawText,
	}, nil
}

// buildComparisonPrompt creates the prompt that lists each region side-by-side.
func buildComparisonPrompt(cc CompareContext) string {
	var b strings.Builder

	fmt.Fprintf(&b, `You are comparing ski regions within the %s macro-region hit by the same storm system.
Pick the single best region for a ski trip and explain why. Consider: snow totals, snow quality,
resort terrain, logistics, cost, and crowds.

`, cc.MacroRegionName)

	for i, s := range cc.Summaries {
		fmt.Fprintf(&b, "## Region %d: %s (%s) [ID: %s]\n", i+1, s.RegionName, s.Tier, s.RegionID)
		fmt.Fprintf(&b, "- Snowfall: %s\n", s.Snowfall)
		fmt.Fprintf(&b, "- Snow Quality: %s\n", s.SnowQuality)
		fmt.Fprintf(&b, "- Best Resort: %s — %s\n", s.TopPick, s.TopPickReason)
		fmt.Fprintf(&b, "- Best Day: %s — %s\n", s.BestDay, s.BestDayReason)
		fmt.Fprintf(&b, "- Recommendation: %s\n", s.Recommendation)
		fmt.Fprintf(&b, "- Costs: Lodging %s, Flights %s, Car %s\n\n", s.LodgingCost, s.FlightCost, s.CarRental)
	}

	b.WriteString("Pick the best option and a runner-up. Explain your reasoning.\n")
	return b.String()
}
