package discord

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/seanmeyer/powder-hunter/domain"
)

func skipUnlessSmoke(t *testing.T) {
	t.Helper()
	if os.Getenv("SMOKE_TEST") != "1" {
		t.Skip("set SMOKE_TEST=1 to run smoke tests")
	}
}

func TestSmoke_Discord_PostAndUpdate(t *testing.T) {
	skipUnlessSmoke(t)

	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" {
		t.Skip("set DISCORD_WEBHOOK_URL to run Discord smoke tests")
	}

	client := NewWebhookClient(webhookURL, http.DefaultClient)

	eval := domain.Evaluation{
		EvaluatedAt:    time.Now().UTC(),
		Tier:           domain.TierWorthALook,
		Recommendation: "[SMOKE TEST] This is an automated test — please ignore.",
		Strategy:       "Test strategy",
		SnowQuality:    "Test snow quality",
		CrowdEstimate:  "Test crowd estimate",
		ClosureRisk:    "Test closure risk",
		ChangeClass:    domain.ChangeNew,
		KeyFactors: domain.KeyFactors{
			Pros: []string{"Automated test pro 1", "Automated test pro 2"},
			Cons: []string{"Automated test con 1"},
		},
		LogisticsSummary: domain.LogisticsSummary{
			Lodging:        "Test lodging",
			Transportation: "Test transport",
			RoadConditions: "Test road conditions",
		},
	}
	region := domain.Region{
		ID:   "test_smoke",
		Name: "[SMOKE TEST] Test Region",
	}

	bp := BriefingPost{
		MacroRegionName: region.Name,
		Evaluations:     []EvalWithRegion{{Evaluation: eval, Region: region}},
	}
	threadID, err := client.PostBriefing(context.Background(), bp)
	if err != nil {
		t.Fatalf("PostBriefing failed: %v", err)
	}
	if threadID == "" {
		t.Fatal("PostBriefing returned empty thread ID")
	}
	t.Logf("Created forum thread: %s", threadID)

	eval.Recommendation = "[SMOKE TEST] Update message — please ignore."
	eval.ChangeClass = domain.ChangeMinor
	err = client.PostUpdate(context.Background(), eval, region, threadID)
	if err != nil {
		t.Fatalf("PostUpdate failed: %v", err)
	}
	t.Log("Successfully posted update to thread")

	t.Log("NOTE: Smoke test threads remain in your Discord forum channel — delete them manually if desired")
}
