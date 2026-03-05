package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	genai "google.golang.org/genai"

	"github.com/seanmeyer/powder-hunter/domain"
	"github.com/seanmeyer/powder-hunter/storage"
)

const geminiModel = "gemini-3-flash-preview"

// GeminiClient wraps the genai SDK for storm evaluation calls.
type GeminiClient struct {
	client *genai.Client
	model  string
}

// NewGeminiClient creates a Gemini client authenticated with the provided API key.
func NewGeminiClient(ctx context.Context, apiKey string) (*GeminiClient, error) {
	c, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, fmt.Errorf("create genai client: %w", err)
	}
	return &GeminiClient{client: c, model: geminiModel}, nil
}

// GeminiResult holds the parsed response from a single EvaluateStorm call.
type GeminiResult struct {
	Tier               domain.Tier
	Recommendation     string
	DayByDay           []domain.DayEvaluation
	KeyFactors         domain.KeyFactors
	LogisticsSummary   domain.LogisticsSummary
	Strategy           string
	SnowQuality        string
	CrowdEstimate      string
	ClosureRisk        string
	BestSkiDay         time.Time
	BestSkiDayReason   string
	RawResponse        string
	StructuredResponse map[string]any
	GroundingSources   []string
}

// EvaluateStorm performs a two-step evaluation:
//  1. Call Gemini WITH GoogleSearch grounding but WITHOUT structured output schema.
//  2. Call Gemini again WITH structured output schema to parse the research into JSON.
//
// Why two steps: When GoogleSearch grounding and ResponseSchema are combined in a
// single call, the grounding search behavior is severely degraded — the model does
// fewer searches, returns less specific results, and often falls back to training
// data instead of actually searching. Splitting them lets grounding work at full
// power (step 1) while still getting clean, schema-validated JSON (step 2).
// In testing, this reliably surfaced resort-specific info (operating schedules,
// road conditions, current news) that the single-call approach found inconsistently.
func (g *GeminiClient) EvaluateStorm(ctx context.Context, prompt string) (GeminiResult, error) {
	// Step 1: Grounded research — full Google Search, no schema constraints.
	researchConfig := &genai.GenerateContentConfig{
		Tools: []*genai.Tool{
			{GoogleSearch: &genai.GoogleSearch{}},
		},
	}

	researchContents := []*genai.Content{
		genai.NewContentFromText(prompt, genai.RoleUser),
	}

	researchResp, err := g.client.Models.GenerateContent(ctx, g.model, researchContents, researchConfig)
	if err != nil {
		return GeminiResult{}, fmt.Errorf("gemini research step: %w", err)
	}

	researchText := researchResp.Text()
	groundingSources := extractGroundingSources(researchResp)

	// Step 2: Structured extraction — parse the research into JSON schema.
	structurePrompt := fmt.Sprintf(`You are a JSON extraction assistant. Below is a detailed storm evaluation analysis
with research findings. Parse it into the required JSON schema exactly. Preserve all specific details,
numbers, dates, and findings from the research. Do not add information that isn't in the analysis.

## Research Analysis

%s`, researchText)

	structureConfig := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema:   stormEvalSchema(),
	}

	structureContents := []*genai.Content{
		genai.NewContentFromText(structurePrompt, genai.RoleUser),
	}

	structureResp, err := g.client.Models.GenerateContent(ctx, g.model, structureContents, structureConfig)
	if err != nil {
		return GeminiResult{}, fmt.Errorf("gemini structure step: %w", err)
	}

	rawText := structureResp.Text()

	var structured map[string]any
	if err := json.Unmarshal([]byte(rawText), &structured); err != nil {
		return GeminiResult{}, fmt.Errorf("parse gemini json response: %w", err)
	}

	result := GeminiResult{
		RawResponse:        researchText,
		StructuredResponse: structured,
		GroundingSources:   groundingSources,
	}

	result.Tier = domain.Tier(stringField(structured, "tier"))
	result.Recommendation = stringField(structured, "recommendation")
	result.Strategy = stringField(structured, "strategy")
	result.SnowQuality = stringField(structured, "snow_quality")
	result.CrowdEstimate = stringField(structured, "crowd_estimate")
	result.ClosureRisk = stringField(structured, "closure_risk")

	bestDay, _ := time.Parse("2006-01-02", stringField(structured, "best_ski_day"))
	result.BestSkiDay = bestDay
	result.BestSkiDayReason = stringField(structured, "best_ski_day_reason")

	result.KeyFactors = domain.KeyFactors{
		Pros: stringSliceField(structured, "key_factors_pros"),
		Cons: stringSliceField(structured, "key_factors_cons"),
	}

	result.LogisticsSummary = domain.LogisticsSummary{
		Lodging:        stringField(structured, "logistics_lodging"),
		Transportation: stringField(structured, "logistics_transportation"),
		RoadConditions: stringField(structured, "logistics_road_conditions"),
		FlightCost:     stringField(structured, "logistics_flight_cost"),
		CarRental:      stringField(structured, "logistics_car_rental"),
	}

	result.DayByDay = parseDayByDay(structured)
	// Grounding sources come from step 1 where grounding is fully active.
	// Fall back to research_sources from JSON if metadata was still empty.
	if len(result.GroundingSources) == 0 {
		result.GroundingSources = stringSliceField(structured, "research_sources")
	}

	return result, nil
}

// stormEvalSchema defines the structured output schema Gemini must conform to.
// Flat fields are used for logistics and key factors to avoid nested object
// complexity in Gemini's structured output mode.
func stormEvalSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"tier": {
				Type: genai.TypeString,
				Enum: []string{"DROP_EVERYTHING", "WORTH_A_LOOK", "ON_THE_RADAR"},
			},
			"recommendation":           {Type: genai.TypeString},
			"strategy":                 {Type: genai.TypeString},
			"snow_quality":             {Type: genai.TypeString},
			"crowd_estimate":           {Type: genai.TypeString},
			"closure_risk":             {Type: genai.TypeString},
			"best_ski_day": {
				Type:        genai.TypeString,
				Description: "The single best date to ski in YYYY-MM-DD format, based on your analysis of conditions, operations, and timing",
			},
			"best_ski_day_reason": {
				Type:        genai.TypeString,
				Description: "Why this is the best day — what specific conditions and factors led to this choice",
			},
			"key_factors_pros":         {Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeString}},
			"key_factors_cons":         {Type: genai.TypeArray, Items: &genai.Schema{Type: genai.TypeString}},
			"logistics_lodging":        {Type: genai.TypeString},
			"logistics_transportation": {Type: genai.TypeString},
			"logistics_road_conditions": {Type: genai.TypeString},
			"logistics_flight_cost":    {Type: genai.TypeString},
			"logistics_car_rental":     {Type: genai.TypeString},
			"day_by_day": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"date":           {Type: genai.TypeString},
						"snowfall":       {Type: genai.TypeString},
						"conditions":     {Type: genai.TypeString},
						"recommendation": {Type: genai.TypeString},
					},
					Required: []string{"date", "snowfall", "conditions", "recommendation"},
				},
			},
			"research_sources": {
				Type:        genai.TypeArray,
				Items:       &genai.Schema{Type: genai.TypeString},
				Description: "URLs of websites consulted during research (lodging, flights, road conditions, etc.)",
			},
		},
		Required: []string{
			"tier", "recommendation", "strategy",
			"snow_quality", "crowd_estimate", "closure_risk",
			"best_ski_day", "best_ski_day_reason",
			"key_factors_pros", "key_factors_cons",
			"logistics_lodging", "logistics_transportation",
			"logistics_road_conditions", "logistics_flight_cost", "logistics_car_rental",
			"day_by_day", "research_sources",
		},
	}
}

// extractGroundingSources pulls web URIs from the grounding metadata returned
// by the GoogleSearch tool so they can be stored alongside the evaluation.
func extractGroundingSources(resp *genai.GenerateContentResponse) []string {
	if resp == nil || len(resp.Candidates) == 0 {
		return nil
	}
	gm := resp.Candidates[0].GroundingMetadata
	if gm == nil {
		return nil
	}
	var sources []string
	for _, chunk := range gm.GroundingChunks {
		if chunk.Web != nil && chunk.Web.URI != "" &&
			!strings.Contains(chunk.Web.URI, "vertexaisearch.cloud.google.com") {
			sources = append(sources, chunk.Web.URI)
		}
	}
	return sources
}

func parseDayByDay(m map[string]any) []domain.DayEvaluation {
	raw, ok := m["day_by_day"]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]domain.DayEvaluation, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		dateStr := stringField(entry, "date")
		t, _ := time.Parse("2006-01-02", dateStr)
		result = append(result, domain.DayEvaluation{
			Date:           t,
			Snowfall:       stringField(entry, "snowfall"),
			Conditions:     stringField(entry, "conditions"),
			Recommendation: stringField(entry, "recommendation"),
		})
	}
	return result
}

func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func stringSliceField(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// GeminiEvaluator implements the Evaluator interface using Gemini with
// GoogleSearch grounding. It loads the active prompt from the DB, renders it
// with per-evaluation context, and calls GeminiClient.EvaluateStorm.
type GeminiEvaluator struct {
	gemini *GeminiClient
	store  *storage.DB
}

// NewGeminiEvaluator creates an evaluator backed by Gemini and the given store.
func NewGeminiEvaluator(gemini *GeminiClient, store *storage.DB) *GeminiEvaluator {
	return &GeminiEvaluator{gemini: gemini, store: store}
}

// Evaluate loads the active prompt, renders it with context data, calls Gemini,
// and returns a domain.Evaluation ready for persistence.
func (e *GeminiEvaluator) Evaluate(ctx context.Context, ec EvalContext) (domain.Evaluation, error) {
	forecasts := ec.Forecasts
	region := ec.Region
	resorts := ec.Resorts
	profile := ec.Profile
	history := ec.History
	promptVersion, promptTemplate, err := e.store.GetActivePrompt(ctx, "storm_eval")
	if err != nil {
		return domain.Evaluation{}, fmt.Errorf("load active prompt: %w", err)
	}

	historyStr := "No prior evaluations"
	if len(history) > 0 {
		histJSON, err := json.Marshal(history)
		if err != nil {
			return domain.Evaluation{}, fmt.Errorf("marshal evaluation history: %w", err)
		}
		historyStr = string(histJSON)
	}

	detection := domain.Detect(region, forecasts)

	weatherData := FormatConsolidatedWeatherForPrompt(forecasts, resorts)
	if rainRisk := FormatRainLineRisk(forecasts, resorts); rainRisk != "" {
		weatherData += "\n" + rainRisk
	}

	renderedPrompt := RenderPrompt(promptTemplate, PromptData{
		WeatherData:        weatherData,
		RegionName:         region.Name,
		Resorts:            FormatResortsForPrompt(resorts),
		UserProfile:        FormatProfileForPrompt(profile),
		StormWindow:        FormatDetectionForPrompt(detection),
		EvaluationHistory:  historyStr,
		PromptVersion:      promptVersion,
		ModelConsensus:     FormatResortConsensusForPrompt(ec.ResortConsensus, resorts),
		ForecastDiscussion: FormatDiscussionForPrompt(ec.Discussion, forecasts),
	})

	gemResult, err := e.gemini.EvaluateStorm(ctx, renderedPrompt)
	if err != nil {
		return domain.Evaluation{}, fmt.Errorf("gemini evaluate storm for region %s: %w", region.ID, err)
	}

	return domain.Evaluation{
		EvaluatedAt:        time.Now().UTC(),
		PromptVersion:      promptVersion,
		RenderedPrompt:     renderedPrompt,
		Tier:               gemResult.Tier,
		Recommendation:     gemResult.Recommendation,
		DayByDay:           gemResult.DayByDay,
		KeyFactors:         gemResult.KeyFactors,
		LogisticsSummary:   gemResult.LogisticsSummary,
		Strategy:           gemResult.Strategy,
		SnowQuality:        gemResult.SnowQuality,
		CrowdEstimate:      gemResult.CrowdEstimate,
		ClosureRisk:        gemResult.ClosureRisk,
		BestSkiDay:         gemResult.BestSkiDay,
		BestSkiDayReason:   gemResult.BestSkiDayReason,
		WeatherSnapshot:    forecasts,
		RawLLMResponse:     gemResult.RawResponse,
		StructuredResponse: gemResult.StructuredResponse,
		GroundingSources:   gemResult.GroundingSources,
	}, nil
}
