package evaluation

import (
	"context"
	"os"
	"testing"

	"github.com/seanmeyer/powder-hunter/domain"
)

func skipUnlessSmoke(t *testing.T) {
	t.Helper()
	if os.Getenv("SMOKE_TEST") != "1" {
		t.Skip("set SMOKE_TEST=1 to run smoke tests")
	}
}

func TestSmoke_Gemini_EvaluateStorm(t *testing.T) {
	skipUnlessSmoke(t)

	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		t.Skip("set GOOGLE_API_KEY to run Gemini smoke tests")
	}

	ctx := context.Background()
	client, err := NewGeminiClient(ctx, apiKey)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	prompt := `You are evaluating a ski storm for a powder chaser based in Denver, CO.

Weather Data: Summit County, CO is forecasted to receive 18 inches of snow over the next 3 days (March 8-10). Temperatures will be 15-25°F (cold, dry powder). Winds 15-25mph from the NW. Clearing day expected March 11 (Tuesday).

Region: Summit County, CO (Breckenridge, Keystone, A-Basin, Copper Mountain)
User Profile: Based in Denver, holds Ikon pass, can work remotely, 1.5 hour drive.

Evaluate this storm and assign a tier. Research current lodging and conditions.

Respond with JSON matching this schema:
- tier: "DROP_EVERYTHING" or "WORTH_A_LOOK" or "ON_THE_RADAR"
- recommendation: plain language recommendation
- strategy: recommended timing and travel approach
- snow_quality: assessment of expected snow quality
- crowd_estimate: expected crowd levels
- closure_risk: road/resort closure risk
- key_factors_pros: array of positive factors
- key_factors_cons: array of negative factors
- logistics_lodging: lodging info
- logistics_transportation: transport info
- logistics_road_conditions: road info
- logistics_flight_cost: "N/A - local drive"
- logistics_car_rental: "N/A - local drive"
- day_by_day: array of {date, snowfall, conditions, recommendation}`

	result, err := client.EvaluateStorm(ctx, prompt)
	if err != nil {
		t.Fatalf("EvaluateStorm failed: %v", err)
	}

	validTiers := map[domain.Tier]bool{
		domain.TierDropEverything: true,
		domain.TierWorthALook:     true,
		domain.TierOnTheRadar:     true,
	}
	if !validTiers[result.Tier] {
		t.Errorf("Tier %q is not a valid tier value", result.Tier)
	}
	if result.Recommendation == "" {
		t.Error("Recommendation is empty")
	}
	if result.Strategy == "" {
		t.Error("Strategy is empty")
	}
	if result.RawResponse == "" {
		t.Error("RawResponse is empty")
	}
	if len(result.GroundingSources) == 0 {
		t.Error("GroundingSources is empty — grounding search did not return any sources")
	}
	if len(result.KeyFactors.Pros) == 0 {
		t.Error("KeyFactors.Pros is empty")
	}

	t.Logf("Tier: %s", result.Tier)
	t.Logf("Recommendation: %s", result.Recommendation)
	t.Logf("Strategy: %s", result.Strategy)
	t.Logf("Grounding sources: %d", len(result.GroundingSources))
	for _, src := range result.GroundingSources {
		t.Logf("  - %s", src)
	}
}
