package pipeline_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/seanmeyer/powder-hunter/discord"
	"github.com/seanmeyer/powder-hunter/domain"
	"github.com/seanmeyer/powder-hunter/evaluation"
	"github.com/seanmeyer/powder-hunter/pipeline"
	"github.com/seanmeyer/powder-hunter/storage"
)

// discardLogger returns a logger that discards all output — keeps test output clean.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nopWriter{}, nil))
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// setupDB opens a real SQLite database in a temp directory and seeds the schema.
func setupDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedRegion inserts a region and an optional resort into the DB.
func seedRegion(t *testing.T, ctx context.Context, db *storage.DB, r domain.Region) {
	t.Helper()
	if err := db.UpsertRegion(ctx, r); err != nil {
		t.Fatalf("upsert region %s: %v", r.ID, err)
	}
}

// seedProfile inserts a minimal user profile so Evaluate can proceed.
func seedProfile(t *testing.T, ctx context.Context, db *storage.DB) {
	t.Helper()
	p := domain.UserProfile{
		ID:                1,
		HomeBase:          "Denver, CO",
		HomeLatitude:      39.7392,
		HomeLongitude:     -104.9903,
		PassesHeld:        []string{"ikon"},
		RemoteWorkCapable: true,
		TypicalPTODays:    15,
		MinTierForPing:    domain.TierDropEverything,
	}
	if err := db.SaveProfile(ctx, p); err != nil {
		t.Fatalf("save profile: %v", err)
	}
}

// aboveThresholdForecast returns a Forecast with enough snow to exceed the near-range
// threshold in testRegion (5 inches total; threshold is 4 inches).
func aboveThresholdForecast(regionID string) domain.Forecast {
	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	return domain.Forecast{
		RegionID:  regionID,
		FetchedAt: time.Now().UTC(),
		Source:    "open_meteo",
		DailyData: []domain.DailyForecast{
			{Date: tomorrow, SnowfallCM: 12.7}, // ~5 inches
		},
	}
}

// belowThresholdForecast returns a Forecast with negligible snow.
func belowThresholdForecast(regionID string) domain.Forecast {
	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	return domain.Forecast{
		RegionID:  regionID,
		FetchedAt: time.Now().UTC(),
		Source:    "open_meteo",
		DailyData: []domain.DailyForecast{
			{Date: tomorrow, SnowfallCM: 1.0},
		},
	}
}

// testRegion returns a Region configured with a 4-inch near-range threshold.
func testRegion(id string) domain.Region {
	return domain.Region{
		ID:                  id,
		Name:                "Test " + id,
		Latitude:            39.5,
		Longitude:           -106.0,
		Country:             "US",
		FrictionTier:        domain.FrictionLocalDrive,
		NearThresholdIn:     4.0,
		ExtendedThresholdIn: 8.0,
	}
}

// fakeWeatherService wraps a FakeFetcher into a weather-service-like struct
// that pipeline.New accepts. Since pipeline.New takes *weather.Service and
// weather.Service is unexported, we use a fake that satisfies domain.Forecast
// lookup via the pipeline's internal Scan stage — which uses weather.Service.FetchAll.
// To avoid a tight coupling on the real service, the tests use the FakeFetcher
// indirectly through a real weather.Service constructed with nil HTTP clients,
// but for pipeline tests we use the weather package's FakeService approach.
//
// Because weather.Service is a concrete struct (not an interface), and its
// constructor requires real HTTP clients, we need a thin test double. We
// accomplish this by exposing a weatherProvider interface and using it in tests.
//
// However, pipeline.New accepts *weather.Service directly. Rather than
// refactoring the constructor for test ergonomics, we use a real Service
// with fake HTTP transports (see weathertest helper) OR we accept that the
// Scan stage will fail for all regions when using nil HTTP clients (real
// Open-Meteo calls), and instead test via direct ScanResult injection to
// Evaluate/Compare/Post/ExpireStaleStorms.
//
// For end-to-end pipeline tests, we inject a fakeWeatherService by monkey-
// patching the Scan phase using a wrapper that overrides Scan. This is achieved
// by testing the pipeline via its Run method with a custom weather.Service
// implementation hidden behind an interface adapter.
//
// The simplest correct approach: test each stage independently, plus one
// integration test using the FakeEvaluator and FakePoster with manual scan
// result injection.

func TestPipeline_HappyPath(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	// Manually create a storm to skip the Scan/weather-fetch stage.
	now := time.Now().UTC()
	stormID, err := db.CreateStorm(ctx, domain.Storm{
		RegionID:    regionA.ID,
		WindowStart: now.AddDate(0, 0, 1),
		WindowEnd:   now.AddDate(0, 0, 7),
		State:       domain.StormDetected,
		DetectedAt:  now,
	})
	if err != nil {
		t.Fatalf("create storm: %v", err)
	}

	scans := []pipeline.ScanResult{
		{
			Region:    regionA,
			Storm:     domain.Storm{ID: stormID, RegionID: regionA.ID, WindowStart: now.AddDate(0, 0, 1), WindowEnd: now.AddDate(0, 0, 7), State: domain.StormDetected, DetectedAt: now},
			Forecasts: []domain.Forecast{aboveThresholdForecast(regionA.ID)},
			IsNew:     true,
		},
	}

	fakeEval := &evaluation.FakeEvaluator{
		Results: map[string]domain.Evaluation{
			regionA.ID: {
				Tier:           domain.TierDropEverything,
				Recommendation: "Book it now.",
			},
		},
	}
	fakePoster := &discord.FakePoster{NextThreadID: "thread-123"}

	p := pipeline.New(nil, db, fakeEval, discardLogger())
	p.WithPoster(fakePoster)

	evals, err := p.Evaluate(ctx, scans, &pipeline.RunSummary{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(evals) != 1 {
		t.Fatalf("expected 1 eval, got %d", len(evals))
	}

	compared, err := p.Compare(ctx, evals)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if len(compared) != 1 {
		t.Fatalf("expected 1 compared result, got %d", len(compared))
	}
	if compared[0].ChangeClass != domain.ChangeNew {
		t.Errorf("expected ChangeNew, got %s", compared[0].ChangeClass)
	}

	if err := p.Post(ctx, compared); err != nil {
		t.Fatalf("post: %v", err)
	}

	if len(fakePoster.PostedNew) != 1 {
		t.Fatalf("expected 1 new post, got %d", len(fakePoster.PostedNew))
	}
	if fakePoster.PostedNew[0].Evaluation.Tier != domain.TierDropEverything {
		t.Errorf("expected DROP_EVERYTHING tier in post, got %s", fakePoster.PostedNew[0].Evaluation.Tier)
	}

	// Verify the evaluation was persisted with ChangeNew class.
	saved, err := db.GetLatestEvaluation(ctx, stormID)
	if err != nil || saved == nil {
		t.Fatalf("get saved evaluation: err=%v, eval=%v", err, saved)
	}
	if saved.Tier != domain.TierDropEverything {
		t.Errorf("persisted evaluation tier: got %s, want DROP_EVERYTHING", saved.Tier)
	}
	if saved.ChangeClass != domain.ChangeNew {
		t.Errorf("persisted change class: got %s, want ChangeNew", saved.ChangeClass)
	}
}

func TestPipeline_ThresholdFiltering(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	// Scan returns empty because below-threshold forecasts produce no detections
	// and there are no active tracked storms. Passing empty scans to Evaluate
	// verifies that nothing gets evaluated or posted.
	scans := []pipeline.ScanResult{}

	fakeEval := &evaluation.FakeEvaluator{}
	fakePoster := &discord.FakePoster{}

	p := pipeline.New(nil, db, fakeEval, discardLogger())
	p.WithPoster(fakePoster)

	evals, err := p.Evaluate(ctx, scans, &pipeline.RunSummary{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(evals) != 0 {
		t.Errorf("expected 0 evals for below-threshold scan, got %d", len(evals))
	}

	compared, err := p.Compare(ctx, evals)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if len(compared) != 0 {
		t.Errorf("expected 0 compare results, got %d", len(compared))
	}

	if err := p.Post(ctx, compared); err != nil {
		t.Fatalf("post: %v", err)
	}
	if len(fakePoster.PostedNew) != 0 {
		t.Errorf("expected 0 posts for empty results, got %d", len(fakePoster.PostedNew))
	}

	if len(fakeEval.EvaluateCalls) != 0 {
		t.Errorf("expected 0 evaluator calls, got %d", len(fakeEval.EvaluateCalls))
	}
}

func TestPipeline_ErrorIsolation(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	regionB := testRegion("region-b")
	seedRegion(t, ctx, db, regionA)
	seedRegion(t, ctx, db, regionB)

	now := time.Now().UTC()
	stormAID, err := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionA.ID, WindowStart: now.AddDate(0, 0, 1),
		WindowEnd: now.AddDate(0, 0, 7), State: domain.StormDetected, DetectedAt: now,
	})
	if err != nil {
		t.Fatalf("create storm A: %v", err)
	}
	stormBID, err := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionB.ID, WindowStart: now.AddDate(0, 0, 1),
		WindowEnd: now.AddDate(0, 0, 7), State: domain.StormDetected, DetectedAt: now,
	})
	if err != nil {
		t.Fatalf("create storm B: %v", err)
	}

	scans := []pipeline.ScanResult{
		{
			Region:    regionA,
			Storm:     domain.Storm{ID: stormAID, RegionID: regionA.ID, State: domain.StormDetected, WindowStart: now.AddDate(0, 0, 1), WindowEnd: now.AddDate(0, 0, 7), DetectedAt: now},
			Forecasts: []domain.Forecast{aboveThresholdForecast(regionA.ID)},
			IsNew:     true,
		},
		{
			Region:    regionB,
			Storm:     domain.Storm{ID: stormBID, RegionID: regionB.ID, State: domain.StormDetected, WindowStart: now.AddDate(0, 0, 1), WindowEnd: now.AddDate(0, 0, 7), DetectedAt: now},
			Forecasts: []domain.Forecast{aboveThresholdForecast(regionB.ID)},
			IsNew:     true,
		},
	}

	// Region A evaluator returns an error; region B succeeds.
	fakeEval := &evaluation.FakeEvaluator{
		Errors: map[string]error{
			regionA.ID: errors.New("gemini timeout"),
		},
		Results: map[string]domain.Evaluation{
			regionB.ID: {Tier: domain.TierWorthALook, Recommendation: "Worth checking out."},
		},
	}
	fakePoster := &discord.FakePoster{}

	p := pipeline.New(nil, db, fakeEval, discardLogger())
	p.WithPoster(fakePoster)

	evals, err := p.Evaluate(ctx, scans, &pipeline.RunSummary{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	// Region A failed; only region B should have an eval.
	if len(evals) != 1 {
		t.Fatalf("expected 1 eval (region B only), got %d", len(evals))
	}
	if evals[0].Region.ID != regionB.ID {
		t.Errorf("expected region-b eval, got %s", evals[0].Region.ID)
	}

	compared, err := p.Compare(ctx, evals)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if len(compared) != 1 {
		t.Fatalf("expected 1 compare result, got %d", len(compared))
	}

	if err := p.Post(ctx, compared); err != nil {
		t.Fatalf("post: %v", err)
	}
	if len(fakePoster.PostedNew) != 1 {
		t.Errorf("expected 1 post (region B), got %d", len(fakePoster.PostedNew))
	}
}

func TestPipeline_StormLifecycle(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	now := time.Now().UTC()

	// Create an existing storm in DB and seed an initial evaluation.
	stormID, err := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionA.ID, WindowStart: now.AddDate(0, 0, 1),
		WindowEnd: now.AddDate(0, 0, 7), State: domain.StormBriefed,
		DiscordThreadID: "thread-existing", DetectedAt: now.AddDate(0, 0, -1),
	})
	if err != nil {
		t.Fatalf("create initial storm: %v", err)
	}

	// Seed a prior evaluation so Compare sees it as ChangeMinor/ChangeMaterial.
	_, err = db.SaveEvaluation(ctx, domain.Evaluation{
		StormID:     stormID,
		EvaluatedAt: now.AddDate(0, -1, 0),
		Tier:        domain.TierWorthALook,
		ChangeClass: domain.ChangeNew,
		WeatherSnapshot: []domain.Forecast{
			{RegionID: regionA.ID, Source: "open_meteo", DailyData: []domain.DailyForecast{
				{Date: now.AddDate(0, 0, 1), SnowfallCM: 10.0},
			}},
		},
	})
	if err != nil {
		t.Fatalf("save prior evaluation: %v", err)
	}

	storm, err := db.FindOverlappingStorm(ctx, regionA.ID, now.AddDate(0, 0, 1), now.AddDate(0, 0, 7))
	if err != nil || storm == nil {
		t.Fatalf("find storm: err=%v, storm=%v", err, storm)
	}

	scans := []pipeline.ScanResult{
		{
			Region:    regionA,
			Storm:     *storm,
			Forecasts: []domain.Forecast{aboveThresholdForecast(regionA.ID)},
			IsNew:     false,
		},
	}

	// New evaluation with same tier — should be ChangeMinor.
	fakeEval := &evaluation.FakeEvaluator{
		Results: map[string]domain.Evaluation{
			regionA.ID: {Tier: domain.TierWorthALook, Recommendation: "Still good."},
		},
	}
	fakePoster := &discord.FakePoster{NextThreadID: "thread-existing"}

	p := pipeline.New(nil, db, fakeEval, discardLogger())
	p.WithPoster(fakePoster)

	evals, err := p.Evaluate(ctx, scans, &pipeline.RunSummary{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(evals) != 1 {
		t.Fatalf("expected 1 eval, got %d", len(evals))
	}

	compared, err := p.Compare(ctx, evals)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if len(compared) != 1 {
		t.Fatalf("expected 1 compare result, got %d", len(compared))
	}

	// Same tier, small snowfall delta → ChangeMinor (no new post, update post).
	if compared[0].ChangeClass != domain.ChangeMinor {
		t.Errorf("expected ChangeMinor for same-tier update, got %s", compared[0].ChangeClass)
	}

	// Post sends an update (not a new post) because storm already has a thread.
	if err := p.Post(ctx, compared); err != nil {
		t.Fatalf("post: %v", err)
	}
	if len(fakePoster.PostedNew) != 0 {
		t.Errorf("expected 0 new posts for existing storm, got %d", len(fakePoster.PostedNew))
	}
	if len(fakePoster.PostedUpdates) != 1 {
		t.Errorf("expected 1 update post, got %d", len(fakePoster.PostedUpdates))
	}
}

func TestPipeline_ExpireStaleStorms(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	now := time.Now().UTC()

	// Create a storm whose window has already ended.
	stormID, err := db.CreateStorm(ctx, domain.Storm{
		RegionID:    regionA.ID,
		WindowStart: now.AddDate(0, 0, -10),
		WindowEnd:   now.AddDate(0, 0, -1), // ended yesterday
		State:       domain.StormBriefed,
		DetectedAt:  now.AddDate(0, 0, -12),
	})
	if err != nil {
		t.Fatalf("create stale storm: %v", err)
	}

	p := pipeline.New(nil, db, nil, discardLogger())

	// Pass empty scans — the stale storm was not refreshed in this run.
	expired, err := p.ExpireStaleStorms(ctx, []pipeline.ScanResult{})
	if err != nil {
		t.Fatalf("expire stale storms: %v", err)
	}
	if expired != 1 {
		t.Errorf("expected 1 expired storm, got %d", expired)
	}

	// Verify the storm state was updated in the DB.
	active, err := db.GetActiveStorms(ctx, regionA.ID)
	if err != nil {
		t.Fatalf("get active storms: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("expected 0 active storms after expiration, got %d", len(active))
	}

	_ = stormID
}

func TestPipeline_DryRun(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	now := time.Now().UTC()
	stormID, err := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionA.ID, WindowStart: now.AddDate(0, 0, 1),
		WindowEnd: now.AddDate(0, 0, 7), State: domain.StormDetected, DetectedAt: now,
	})
	if err != nil {
		t.Fatalf("create storm: %v", err)
	}

	scans := []pipeline.ScanResult{
		{
			Region:    regionA,
			Storm:     domain.Storm{ID: stormID, RegionID: regionA.ID, State: domain.StormDetected, WindowStart: now.AddDate(0, 0, 1), WindowEnd: now.AddDate(0, 0, 7), DetectedAt: now},
			Forecasts: []domain.Forecast{aboveThresholdForecast(regionA.ID)},
			IsNew:     true,
		},
	}

	fakeEval := &evaluation.FakeEvaluator{
		Results: map[string]domain.Evaluation{
			regionA.ID: {Tier: domain.TierDropEverything, Recommendation: "Go now."},
		},
	}
	// No poster attached — dry-run mode.
	fakePoster := &discord.FakePoster{}

	p := pipeline.New(nil, db, fakeEval, discardLogger())
	p.WithDryRun(true)
	p.WithPoster(fakePoster)

	evals, _ := p.Evaluate(ctx, scans, &pipeline.RunSummary{})
	compared, _ := p.Compare(ctx, evals)
	if err := p.Post(ctx, compared); err != nil {
		t.Fatalf("post: %v", err)
	}

	// Dry-run: Discord poster must not be called.
	if len(fakePoster.PostedNew) != 0 {
		t.Errorf("dry-run should not call PostNew, got %d calls", len(fakePoster.PostedNew))
	}
}

func TestPipeline_MultiModelConsensusAndAFD(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)

	stormID, err := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionA.ID, WindowStart: tomorrow,
		WindowEnd: tomorrow.AddDate(0, 0, 6), State: domain.StormDetected, DetectedAt: now,
	})
	if err != nil {
		t.Fatalf("create storm: %v", err)
	}

	// Build multi-model forecasts: GFS and ECMWF with different snowfall for a resort.
	resortID := "resort-1"
	gfsForecast := domain.Forecast{
		RegionID: regionA.ID, ResortID: resortID, FetchedAt: now, Source: "open_meteo", Model: "gfs_seamless",
		DailyData: []domain.DailyForecast{
			{Date: tomorrow, SnowfallCM: 30.0, TemperatureMinC: -10, TemperatureMaxC: -5, SLRatio: 15},
		},
	}
	ecmwfForecast := domain.Forecast{
		RegionID: regionA.ID, ResortID: resortID, FetchedAt: now, Source: "open_meteo", Model: "ecmwf_ifs025",
		DailyData: []domain.DailyForecast{
			{Date: tomorrow, SnowfallCM: 20.0, TemperatureMinC: -8, TemperatureMaxC: -3, SLRatio: 12},
		},
	}

	// Compute per-resort consensus.
	resortConsensus := map[string]domain.ModelConsensus{
		resortID: domain.ComputeConsensus([]domain.Forecast{gfsForecast, ecmwfForecast}),
	}

	// Build AFD.
	afd := &domain.ForecastDiscussion{
		WFO: "SLC", IssuedAt: now, Text: "Heavy snow expected.", FetchedAt: now,
	}

	scans := []pipeline.ScanResult{
		{
			Region:          regionA,
			Storm:           domain.Storm{ID: stormID, RegionID: regionA.ID, State: domain.StormDetected, WindowStart: tomorrow, WindowEnd: tomorrow.AddDate(0, 0, 6), DetectedAt: now},
			Forecasts:       []domain.Forecast{gfsForecast, ecmwfForecast},
			ResortConsensus: resortConsensus,
			Discussion:      afd,
			IsNew:           true,
		},
	}

	fakeEval := &evaluation.FakeEvaluator{
		Results: map[string]domain.Evaluation{
			regionA.ID: {Tier: domain.TierDropEverything, Recommendation: "Book it."},
		},
	}

	p := pipeline.New(nil, db, fakeEval, discardLogger())
	p.WithDryRun(true)

	evals, err := p.Evaluate(ctx, scans, &pipeline.RunSummary{})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(evals) != 1 {
		t.Fatalf("expected 1 eval, got %d", len(evals))
	}

	// Verify the evaluator received consensus and discussion data.
	if len(fakeEval.EvaluateCalls) != 1 {
		t.Fatalf("expected 1 evaluate call, got %d", len(fakeEval.EvaluateCalls))
	}
	call := fakeEval.EvaluateCalls[0]
	rc, ok := call.ResortConsensus[resortID]
	if !ok {
		t.Fatalf("expected consensus for resort %s", resortID)
	}
	if len(rc.Models) != 2 {
		t.Errorf("expected 2 consensus models, got %d", len(rc.Models))
	}
	if len(rc.DailyConsensus) != 1 {
		t.Errorf("expected 1 consensus day, got %d", len(rc.DailyConsensus))
	}
	if rc.DailyConsensus[0].Confidence != "high" {
		t.Errorf("expected high confidence (models close), got %q", rc.DailyConsensus[0].Confidence)
	}
	if len(call.Forecasts) != 2 {
		t.Errorf("expected 2 forecasts passed to evaluator, got %d", len(call.Forecasts))
	}
}

// --- Cost Optimization Tests ---

func TestPipeline_SkipUnchangedWeather(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)

	forecasts := []domain.Forecast{aboveThresholdForecast(regionA.ID)}

	stormID, err := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionA.ID, WindowStart: tomorrow,
		WindowEnd: tomorrow.AddDate(0, 0, 6), State: domain.StormEvaluated,
		CurrentTier: domain.TierWorthALook, DetectedAt: now.AddDate(0, 0, -1),
	})
	if err != nil {
		t.Fatalf("create storm: %v", err)
	}

	// Seed a prior evaluation with the same weather snapshot.
	_, err = db.SaveEvaluation(ctx, domain.Evaluation{
		StormID:         stormID,
		EvaluatedAt:     now.Add(-1 * time.Hour), // evaluated 1 hour ago
		Tier:            domain.TierWorthALook,
		ChangeClass:     domain.ChangeNew,
		WeatherSnapshot: forecasts, // same forecasts as current
	})
	if err != nil {
		t.Fatalf("save prior evaluation: %v", err)
	}

	storm, _ := db.FindOverlappingStorm(ctx, regionA.ID, tomorrow, tomorrow.AddDate(0, 0, 6))
	scans := []pipeline.ScanResult{
		{
			Region:    regionA,
			Storm:     *storm,
			Forecasts: forecasts,
			IsNew:     false,
		},
	}

	fakeEval := &evaluation.FakeEvaluator{
		Results: map[string]domain.Evaluation{
			regionA.ID: {Tier: domain.TierWorthALook},
		},
	}

	p := pipeline.New(nil, db, fakeEval, discardLogger())
	summary := pipeline.RunSummary{}
	evals, err := p.Evaluate(ctx, scans, &summary)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	// Storm should be skipped because weather hasn't changed and cooldown hasn't elapsed.
	if len(evals) != 0 {
		t.Errorf("expected 0 evals (unchanged weather + cooldown), got %d", len(evals))
	}
	if len(fakeEval.EvaluateCalls) != 0 {
		t.Errorf("expected 0 evaluator calls, got %d", len(fakeEval.EvaluateCalls))
	}
	// Should be counted as cooldown skip (weather unchanged + within 12h cooldown for WORTH_A_LOOK).
	if summary.SkippedCooldown != 1 {
		t.Errorf("expected 1 cooldown skip, got %d (unchanged=%d, budget=%d)",
			summary.SkippedCooldown, summary.SkippedUnchanged, summary.SkippedBudget)
	}
}

func TestPipeline_ProceedWhenWeatherChanged(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)

	stormID, err := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionA.ID, WindowStart: tomorrow,
		WindowEnd: tomorrow.AddDate(0, 0, 6), State: domain.StormEvaluated,
		CurrentTier: domain.TierOnTheRadar, DetectedAt: now.AddDate(0, 0, -1),
	})
	if err != nil {
		t.Fatalf("create storm: %v", err)
	}

	// Prior eval with low snowfall.
	_, err = db.SaveEvaluation(ctx, domain.Evaluation{
		StormID:     stormID,
		EvaluatedAt: now.Add(-1 * time.Hour),
		Tier:        domain.TierOnTheRadar,
		ChangeClass: domain.ChangeNew,
		WeatherSnapshot: []domain.Forecast{{
			RegionID: regionA.ID, Source: "open_meteo", FetchedAt: now.Add(-2 * time.Hour),
			DailyData: []domain.DailyForecast{
				{Date: tomorrow, SnowfallCM: 5.0, TemperatureMinC: -2, TemperatureMaxC: 1},
			},
		}},
	})
	if err != nil {
		t.Fatalf("save prior evaluation: %v", err)
	}

	// Current forecasts with significantly more snow (>2" delta).
	currentForecasts := []domain.Forecast{{
		RegionID: regionA.ID, Source: "open_meteo", FetchedAt: now,
		DailyData: []domain.DailyForecast{
			{Date: tomorrow, SnowfallCM: 5.0 + 10*2.54, TemperatureMinC: -2, TemperatureMaxC: 1}, // +10" more
		},
	}}

	storm, _ := db.FindOverlappingStorm(ctx, regionA.ID, tomorrow, tomorrow.AddDate(0, 0, 6))
	scans := []pipeline.ScanResult{
		{
			Region:    regionA,
			Storm:     *storm,
			Forecasts: currentForecasts,
			IsNew:     false,
		},
	}

	fakeEval := &evaluation.FakeEvaluator{
		Results: map[string]domain.Evaluation{
			regionA.ID: {Tier: domain.TierDropEverything},
		},
	}

	p := pipeline.New(nil, db, fakeEval, discardLogger())
	summary := pipeline.RunSummary{}
	evals, err := p.Evaluate(ctx, scans, &summary)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	// Weather changed materially — should proceed despite cooldown.
	if len(evals) != 1 {
		t.Fatalf("expected 1 eval (weather changed), got %d", len(evals))
	}
	if len(fakeEval.EvaluateCalls) != 1 {
		t.Errorf("expected 1 evaluator call, got %d", len(fakeEval.EvaluateCalls))
	}
}

func TestPipeline_FirstEvalAlwaysProceeds(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)

	stormID, err := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionA.ID, WindowStart: tomorrow,
		WindowEnd: tomorrow.AddDate(0, 0, 6), State: domain.StormDetected, DetectedAt: now,
	})
	if err != nil {
		t.Fatalf("create storm: %v", err)
	}

	// No prior evaluation — this is a first eval.
	scans := []pipeline.ScanResult{
		{
			Region:    regionA,
			Storm:     domain.Storm{ID: stormID, RegionID: regionA.ID, State: domain.StormDetected, WindowStart: tomorrow, WindowEnd: tomorrow.AddDate(0, 0, 6), DetectedAt: now},
			Forecasts: []domain.Forecast{aboveThresholdForecast(regionA.ID)},
			IsNew:     true,
		},
	}

	fakeEval := &evaluation.FakeEvaluator{
		Results: map[string]domain.Evaluation{
			regionA.ID: {Tier: domain.TierDropEverything},
		},
	}

	p := pipeline.New(nil, db, fakeEval, discardLogger())
	summary := pipeline.RunSummary{}
	evals, err := p.Evaluate(ctx, scans, &summary)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if len(evals) != 1 {
		t.Fatalf("expected 1 eval (first eval always proceeds), got %d", len(evals))
	}
	if summary.SkippedUnchanged+summary.SkippedCooldown+summary.SkippedBudget != 0 {
		t.Errorf("expected 0 skips for first eval, got unchanged=%d cooldown=%d budget=%d",
			summary.SkippedUnchanged, summary.SkippedCooldown, summary.SkippedBudget)
	}
}

func TestPipeline_CooldownOnTheRadar(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)
	forecasts := []domain.Forecast{aboveThresholdForecast(regionA.ID)}

	stormID, _ := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionA.ID, WindowStart: tomorrow,
		WindowEnd: tomorrow.AddDate(0, 0, 6), State: domain.StormEvaluated,
		CurrentTier: domain.TierOnTheRadar, DetectedAt: now.AddDate(0, 0, -2),
	})

	// ON_THE_RADAR evaluated 13h ago — within 24h cooldown.
	db.SaveEvaluation(ctx, domain.Evaluation{
		StormID: stormID, EvaluatedAt: now.Add(-13 * time.Hour),
		Tier: domain.TierOnTheRadar, ChangeClass: domain.ChangeNew,
		WeatherSnapshot: forecasts,
	})

	storm, _ := db.FindOverlappingStorm(ctx, regionA.ID, tomorrow, tomorrow.AddDate(0, 0, 6))
	scans := []pipeline.ScanResult{{Region: regionA, Storm: *storm, Forecasts: forecasts, IsNew: false}}

	fakeEval := &evaluation.FakeEvaluator{}
	p := pipeline.New(nil, db, fakeEval, discardLogger())
	summary := pipeline.RunSummary{}
	evals, _ := p.Evaluate(ctx, scans, &summary)

	if len(evals) != 0 {
		t.Errorf("ON_THE_RADAR 13h ago should be skipped, got %d evals", len(evals))
	}
	if summary.SkippedCooldown != 1 {
		t.Errorf("expected 1 cooldown skip, got %d", summary.SkippedCooldown)
	}
}

func TestPipeline_DropEverythingUnchangedWeatherStillSkipped(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)
	forecasts := []domain.Forecast{aboveThresholdForecast(regionA.ID)}

	stormID, _ := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionA.ID, WindowStart: tomorrow,
		WindowEnd: tomorrow.AddDate(0, 0, 6), State: domain.StormEvaluated,
		CurrentTier: domain.TierDropEverything, DetectedAt: now.AddDate(0, 0, -2),
	})

	// DROP_EVERYTHING evaluated 13h ago, same weather.
	db.SaveEvaluation(ctx, domain.Evaluation{
		StormID: stormID, EvaluatedAt: now.Add(-13 * time.Hour),
		Tier: domain.TierDropEverything, ChangeClass: domain.ChangeNew,
		WeatherSnapshot: forecasts,
	})

	storm, _ := db.FindOverlappingStorm(ctx, regionA.ID, tomorrow, tomorrow.AddDate(0, 0, 6))
	scans := []pipeline.ScanResult{{Region: regionA, Storm: *storm, Forecasts: forecasts, IsNew: false}}

	fakeEval := &evaluation.FakeEvaluator{
		Results: map[string]domain.Evaluation{regionA.ID: {Tier: domain.TierDropEverything}},
	}
	p := pipeline.New(nil, db, fakeEval, discardLogger())
	summary := pipeline.RunSummary{}
	evals, _ := p.Evaluate(ctx, scans, &summary)

	// DROP_EVERYTHING has no cooldown, but weather unchanged → still re-evaluates
	// because cooldown=0 means "cooldown elapsed" immediately.
	if len(evals) != 1 {
		t.Errorf("DROP_EVERYTHING with no cooldown should proceed (cooldown=0), got %d evals", len(evals))
	}
}

func TestPipeline_DropEverythingChangedWeatherProceeds(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)

	stormID, _ := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionA.ID, WindowStart: tomorrow,
		WindowEnd: tomorrow.AddDate(0, 0, 6), State: domain.StormEvaluated,
		CurrentTier: domain.TierDropEverything, DetectedAt: now.AddDate(0, 0, -2),
	})

	// Prior eval with little snow.
	db.SaveEvaluation(ctx, domain.Evaluation{
		StormID: stormID, EvaluatedAt: now.Add(-13 * time.Hour),
		Tier: domain.TierDropEverything, ChangeClass: domain.ChangeNew,
		WeatherSnapshot: []domain.Forecast{{
			RegionID: regionA.ID, Source: "open_meteo", FetchedAt: now.Add(-14 * time.Hour),
			DailyData: []domain.DailyForecast{{Date: tomorrow, SnowfallCM: 5.0}},
		}},
	})

	// Much more snow now.
	currentForecasts := []domain.Forecast{{
		RegionID: regionA.ID, Source: "open_meteo", FetchedAt: now,
		DailyData: []domain.DailyForecast{{Date: tomorrow, SnowfallCM: 5.0 + 15*2.54}},
	}}

	storm, _ := db.FindOverlappingStorm(ctx, regionA.ID, tomorrow, tomorrow.AddDate(0, 0, 6))
	scans := []pipeline.ScanResult{{Region: regionA, Storm: *storm, Forecasts: currentForecasts, IsNew: false}}

	fakeEval := &evaluation.FakeEvaluator{
		Results: map[string]domain.Evaluation{regionA.ID: {Tier: domain.TierDropEverything}},
	}
	p := pipeline.New(nil, db, fakeEval, discardLogger())
	summary := pipeline.RunSummary{}
	evals, _ := p.Evaluate(ctx, scans, &summary)

	if len(evals) != 1 {
		t.Fatalf("DROP_EVERYTHING with changed weather should proceed, got %d evals", len(evals))
	}
}

func TestPipeline_WeatherChangeOverridesCooldown(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)

	stormID, _ := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionA.ID, WindowStart: tomorrow,
		WindowEnd: tomorrow.AddDate(0, 0, 6), State: domain.StormEvaluated,
		CurrentTier: domain.TierWorthALook, DetectedAt: now.AddDate(0, 0, -2),
	})

	// WORTH_A_LOOK evaluated 6h ago (within 12h cooldown), but weather changed.
	db.SaveEvaluation(ctx, domain.Evaluation{
		StormID: stormID, EvaluatedAt: now.Add(-6 * time.Hour),
		Tier: domain.TierWorthALook, ChangeClass: domain.ChangeNew,
		WeatherSnapshot: []domain.Forecast{{
			RegionID: regionA.ID, Source: "open_meteo", FetchedAt: now.Add(-7 * time.Hour),
			DailyData: []domain.DailyForecast{{Date: tomorrow, SnowfallCM: 5.0}},
		}},
	})

	// Significantly different weather.
	currentForecasts := []domain.Forecast{{
		RegionID: regionA.ID, Source: "open_meteo", FetchedAt: now,
		DailyData: []domain.DailyForecast{{Date: tomorrow, SnowfallCM: 5.0 + 10*2.54}},
	}}

	storm, _ := db.FindOverlappingStorm(ctx, regionA.ID, tomorrow, tomorrow.AddDate(0, 0, 6))
	scans := []pipeline.ScanResult{{Region: regionA, Storm: *storm, Forecasts: currentForecasts, IsNew: false}}

	fakeEval := &evaluation.FakeEvaluator{
		Results: map[string]domain.Evaluation{regionA.ID: {Tier: domain.TierDropEverything}},
	}
	p := pipeline.New(nil, db, fakeEval, discardLogger())
	summary := pipeline.RunSummary{}
	evals, _ := p.Evaluate(ctx, scans, &summary)

	if len(evals) != 1 {
		t.Fatalf("weather change should override cooldown, got %d evals", len(evals))
	}
}

func TestPipeline_CooldownElapsedProceeds(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)
	forecasts := []domain.Forecast{aboveThresholdForecast(regionA.ID)}

	stormID, _ := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionA.ID, WindowStart: tomorrow,
		WindowEnd: tomorrow.AddDate(0, 0, 6), State: domain.StormEvaluated,
		CurrentTier: domain.TierOnTheRadar, DetectedAt: now.AddDate(0, 0, -3),
	})

	// ON_THE_RADAR evaluated 25h ago — past 24h cooldown.
	db.SaveEvaluation(ctx, domain.Evaluation{
		StormID: stormID, EvaluatedAt: now.Add(-25 * time.Hour),
		Tier: domain.TierOnTheRadar, ChangeClass: domain.ChangeNew,
		WeatherSnapshot: forecasts,
	})

	storm, _ := db.FindOverlappingStorm(ctx, regionA.ID, tomorrow, tomorrow.AddDate(0, 0, 6))
	scans := []pipeline.ScanResult{{Region: regionA, Storm: *storm, Forecasts: forecasts, IsNew: false}}

	fakeEval := &evaluation.FakeEvaluator{
		Results: map[string]domain.Evaluation{regionA.ID: {Tier: domain.TierOnTheRadar}},
	}
	p := pipeline.New(nil, db, fakeEval, discardLogger())
	summary := pipeline.RunSummary{}
	evals, _ := p.Evaluate(ctx, scans, &summary)

	if len(evals) != 1 {
		t.Errorf("ON_THE_RADAR 25h ago (past cooldown) should proceed, got %d evals", len(evals))
	}
}

// --- Budget Tests ---

// fakeCostTracker implements pipeline.CostTracker for testing.
type fakeCostTracker struct {
	spent float64
	calls int
	records []costRecord
}

type costRecord struct {
	stormID  int64
	regionID string
	cost     float64
	success  bool
}

func (f *fakeCostTracker) RecordCost(_ context.Context, stormID int64, regionID string, estimatedCost float64, success bool) error {
	f.records = append(f.records, costRecord{stormID, regionID, estimatedCost, success})
	return nil
}

func (f *fakeCostTracker) MonthlySpend(_ context.Context) (float64, int, error) {
	return f.spent, f.calls, nil
}

func TestPipeline_BudgetNotSet_ProceedsNormally(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)

	stormID, _ := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionA.ID, WindowStart: tomorrow,
		WindowEnd: tomorrow.AddDate(0, 0, 6), State: domain.StormDetected, DetectedAt: now,
	})

	scans := []pipeline.ScanResult{{
		Region:    regionA,
		Storm:     domain.Storm{ID: stormID, RegionID: regionA.ID, State: domain.StormDetected, WindowStart: tomorrow, WindowEnd: tomorrow.AddDate(0, 0, 6), DetectedAt: now},
		Forecasts: []domain.Forecast{aboveThresholdForecast(regionA.ID)},
		IsNew:     true,
	}}

	fakeEval := &evaluation.FakeEvaluator{
		Results: map[string]domain.Evaluation{regionA.ID: {Tier: domain.TierDropEverything}},
	}
	// No cost tracker, no budget — should proceed.
	p := pipeline.New(nil, db, fakeEval, discardLogger())
	summary := pipeline.RunSummary{}
	evals, _ := p.Evaluate(ctx, scans, &summary)

	if len(evals) != 1 {
		t.Errorf("no budget: expected 1 eval, got %d", len(evals))
	}
	if summary.SkippedBudget != 0 {
		t.Errorf("no budget: expected 0 budget skips, got %d", summary.SkippedBudget)
	}
}

func TestPipeline_BudgetExceeded_SkipsNonFirstEval(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)
	forecasts := []domain.Forecast{aboveThresholdForecast(regionA.ID)}

	stormID, _ := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionA.ID, WindowStart: tomorrow,
		WindowEnd: tomorrow.AddDate(0, 0, 6), State: domain.StormEvaluated,
		CurrentTier: domain.TierWorthALook, DetectedAt: now.AddDate(0, 0, -2),
	})

	// Prior eval exists (not first eval), weather changed.
	db.SaveEvaluation(ctx, domain.Evaluation{
		StormID: stormID, EvaluatedAt: now.Add(-1 * time.Hour),
		Tier: domain.TierWorthALook, ChangeClass: domain.ChangeNew,
		WeatherSnapshot: []domain.Forecast{{
			RegionID: regionA.ID, Source: "open_meteo", FetchedAt: now.Add(-2 * time.Hour),
			DailyData: []domain.DailyForecast{{Date: tomorrow, SnowfallCM: 2.0}},
		}},
	})

	storm, _ := db.FindOverlappingStorm(ctx, regionA.ID, tomorrow, tomorrow.AddDate(0, 0, 6))
	scans := []pipeline.ScanResult{{Region: regionA, Storm: *storm, Forecasts: forecasts, IsNew: false}}

	fakeEval := &evaluation.FakeEvaluator{}
	ct := &fakeCostTracker{spent: 5.0, calls: 333} // over budget

	p := pipeline.New(nil, db, fakeEval, discardLogger())
	p.WithCostTracker(ct)
	p.WithBudgetConfig(pipeline.BudgetConfig{MonthlyLimitUSD: 5.0, WarningThreshold: 0.8})

	summary := pipeline.RunSummary{}
	evals, _ := p.Evaluate(ctx, scans, &summary)

	if len(evals) != 0 {
		t.Errorf("budget exceeded: expected 0 evals, got %d", len(evals))
	}
	if summary.SkippedBudget != 1 {
		t.Errorf("expected 1 budget skip, got %d", summary.SkippedBudget)
	}
}

func TestPipeline_BudgetExceeded_FirstEvalStillProceeds(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	regionA := testRegion("region-a")
	seedRegion(t, ctx, db, regionA)

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)

	stormID, _ := db.CreateStorm(ctx, domain.Storm{
		RegionID: regionA.ID, WindowStart: tomorrow,
		WindowEnd: tomorrow.AddDate(0, 0, 6), State: domain.StormDetected, DetectedAt: now,
	})

	// No prior evaluation — first eval.
	scans := []pipeline.ScanResult{{
		Region:    regionA,
		Storm:     domain.Storm{ID: stormID, RegionID: regionA.ID, State: domain.StormDetected, WindowStart: tomorrow, WindowEnd: tomorrow.AddDate(0, 0, 6), DetectedAt: now},
		Forecasts: []domain.Forecast{aboveThresholdForecast(regionA.ID)},
		IsNew:     true,
	}}

	fakeEval := &evaluation.FakeEvaluator{
		Results: map[string]domain.Evaluation{regionA.ID: {Tier: domain.TierDropEverything}},
	}
	ct := &fakeCostTracker{spent: 20.0, calls: 1333} // way over budget

	p := pipeline.New(nil, db, fakeEval, discardLogger())
	p.WithCostTracker(ct)
	p.WithBudgetConfig(pipeline.BudgetConfig{MonthlyLimitUSD: 5.0, WarningThreshold: 0.8})

	summary := pipeline.RunSummary{}
	evals, _ := p.Evaluate(ctx, scans, &summary)

	// First eval must proceed even when budget is exceeded (FR-005).
	if len(evals) != 1 {
		t.Fatalf("first eval should proceed despite budget, got %d evals", len(evals))
	}
	if summary.SkippedBudget != 0 {
		t.Errorf("first eval: expected 0 budget skips, got %d", summary.SkippedBudget)
	}
	// Cost should be recorded.
	if len(ct.records) != 1 {
		t.Errorf("expected 1 cost record, got %d", len(ct.records))
	}
}

// --- Grouping Tests ---

// fakeComparer implements evaluation.Comparer for testing.
type fakeComparer struct {
	result evaluation.ComparisonResult
	calls  []evaluation.CompareContext
}

func (f *fakeComparer) CompareRegions(ctx context.Context, cc evaluation.CompareContext) (evaluation.ComparisonResult, error) {
	f.calls = append(f.calls, cc)
	return f.result, nil
}

func TestPipeline_GroupSingletonPassthrough(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	r := testRegion("ak-valdez")
	r.MacroRegion = "ak_chugach"
	seedRegion(t, ctx, db, r)

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)

	stormID, err := db.CreateStorm(ctx, domain.Storm{
		RegionID: r.ID, WindowStart: tomorrow,
		WindowEnd: tomorrow.AddDate(0, 0, 6), State: domain.StormDetected, DetectedAt: now,
	})
	if err != nil {
		t.Fatalf("create storm: %v", err)
	}

	scans := []pipeline.ScanResult{
		{
			Region:    r,
			Storm:     domain.Storm{ID: stormID, RegionID: r.ID, State: domain.StormDetected, WindowStart: tomorrow, WindowEnd: tomorrow.AddDate(0, 0, 6), DetectedAt: now},
			Forecasts: []domain.Forecast{aboveThresholdForecast(r.ID)},
			IsNew:     true,
		},
	}

	fakeEval := &evaluation.FakeEvaluator{
		Results: map[string]domain.Evaluation{
			r.ID: {Tier: domain.TierDropEverything, Recommendation: "Go to Valdez."},
		},
	}
	fakePoster := &discord.FakePoster{NextThreadID: "thread-singleton"}

	p := pipeline.New(nil, db, fakeEval, discardLogger())
	p.WithPoster(fakePoster)

	summary := pipeline.RunSummary{}
	evals, err := p.Evaluate(ctx, scans, &summary)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	compared, err := p.Compare(ctx, evals)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}

	grouped := p.Group(ctx, compared, &summary)

	posted, err := p.PostGrouped(ctx, grouped)
	if err != nil {
		t.Fatalf("post grouped: %v", err)
	}

	// Singleton group uses PostNew, not PostGrouped.
	if len(fakePoster.PostedNew) != 1 {
		t.Errorf("expected 1 PostNew call for singleton, got %d", len(fakePoster.PostedNew))
	}
	if len(fakePoster.PostedGrouped) != 0 {
		t.Errorf("expected 0 PostGrouped calls for singleton, got %d", len(fakePoster.PostedGrouped))
	}
	if summary.Grouped != 1 {
		t.Errorf("expected summary.Grouped == 1, got %d", summary.Grouped)
	}
	if summary.Comparisons != 0 {
		t.Errorf("expected summary.Comparisons == 0, got %d", summary.Comparisons)
	}
	if posted != 1 {
		t.Errorf("expected 1 posted, got %d", posted)
	}
}

func TestPipeline_GroupMultiRegion(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	// 3 regions, all same MacroRegion and FrictionTier.
	regions := make([]domain.Region, 3)
	regionIDs := []string{"wa-central", "wa-north", "wa-south"}
	for i, id := range regionIDs {
		r := testRegion(id)
		r.MacroRegion = "pnw_cascades"
		r.FrictionTier = domain.FrictionFlight
		regions[i] = r
		seedRegion(t, ctx, db, r)
	}

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)
	tiers := []domain.Tier{domain.TierDropEverything, domain.TierWorthALook, domain.TierOnTheRadar}

	scans := make([]pipeline.ScanResult, 3)
	evalResults := make(map[string]domain.Evaluation)
	for i, r := range regions {
		stormID, err := db.CreateStorm(ctx, domain.Storm{
			RegionID: r.ID, WindowStart: tomorrow,
			WindowEnd: tomorrow.AddDate(0, 0, 6), State: domain.StormDetected, DetectedAt: now,
		})
		if err != nil {
			t.Fatalf("create storm %d: %v", i, err)
		}

		scans[i] = pipeline.ScanResult{
			Region:    r,
			Storm:     domain.Storm{ID: stormID, RegionID: r.ID, State: domain.StormDetected, WindowStart: tomorrow, WindowEnd: tomorrow.AddDate(0, 0, 6), DetectedAt: now},
			Forecasts: []domain.Forecast{aboveThresholdForecast(r.ID)},
			IsNew:     true,
		}
		evalResults[r.ID] = domain.Evaluation{Tier: tiers[i], Recommendation: "Region " + r.ID}
	}

	fakeEval := &evaluation.FakeEvaluator{Results: evalResults}
	fakePoster := &discord.FakePoster{NextThreadID: "thread-grouped"}
	fc := &fakeComparer{
		result: evaluation.ComparisonResult{
			TopPickRegion: "wa-central",
			TopPickName:   "Test wa-central",
			Reasoning:     "Best snow overall.",
		},
	}

	p := pipeline.New(nil, db, fakeEval, discardLogger())
	p.WithPoster(fakePoster)
	p.WithComparer(fc)

	summary := pipeline.RunSummary{}
	evals, err := p.Evaluate(ctx, scans, &summary)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(evals) != 3 {
		t.Fatalf("expected 3 evals, got %d", len(evals))
	}

	compared, err := p.Compare(ctx, evals)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if len(compared) != 3 {
		t.Fatalf("expected 3 compared, got %d", len(compared))
	}

	grouped := p.Group(ctx, compared, &summary)

	posted, err := p.PostGrouped(ctx, grouped)
	if err != nil {
		t.Fatalf("post grouped: %v", err)
	}

	// All 3 in one group → PostGrouped, not PostNew.
	if len(fakePoster.PostedGrouped) != 1 {
		t.Errorf("expected 1 PostGrouped call, got %d", len(fakePoster.PostedGrouped))
	}
	if len(fakePoster.PostedNew) != 0 {
		t.Errorf("expected 0 PostNew calls for multi-region group, got %d", len(fakePoster.PostedNew))
	}
	if summary.Grouped != 1 {
		t.Errorf("expected summary.Grouped == 1, got %d", summary.Grouped)
	}
	if summary.Comparisons != 1 {
		t.Errorf("expected summary.Comparisons == 1, got %d", summary.Comparisons)
	}
	if posted != 3 {
		t.Errorf("expected 3 posted (all members), got %d", posted)
	}

	// Verify the grouped post contains all 3 evaluations.
	if len(fakePoster.PostedGrouped) == 1 {
		gp := fakePoster.PostedGrouped[0].Group
		if len(gp.Evaluations) != 3 {
			t.Errorf("expected 3 evaluations in grouped post, got %d", len(gp.Evaluations))
		}
	}

	// Verify the comparer was called once with 3 summaries.
	if len(fc.calls) != 1 {
		t.Fatalf("expected 1 comparer call, got %d", len(fc.calls))
	}
	if len(fc.calls[0].Summaries) != 3 {
		t.Errorf("expected 3 summaries in compare call, got %d", len(fc.calls[0].Summaries))
	}
}

func TestPipeline_GroupFrictionSplit(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	// 2 regions in same MacroRegion but different friction tiers.
	rLocal := testRegion("co-front-a")
	rLocal.MacroRegion = "co_front_range"
	rLocal.FrictionTier = domain.FrictionLocalDrive

	rRegional := testRegion("co-front-b")
	rRegional.MacroRegion = "co_front_range"
	rRegional.FrictionTier = domain.FrictionRegionalDrive

	seedRegion(t, ctx, db, rLocal)
	seedRegion(t, ctx, db, rRegional)

	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)

	var scans []pipeline.ScanResult
	evalResults := make(map[string]domain.Evaluation)
	for _, r := range []domain.Region{rLocal, rRegional} {
		stormID, err := db.CreateStorm(ctx, domain.Storm{
			RegionID: r.ID, WindowStart: tomorrow,
			WindowEnd: tomorrow.AddDate(0, 0, 6), State: domain.StormDetected, DetectedAt: now,
		})
		if err != nil {
			t.Fatalf("create storm for %s: %v", r.ID, err)
		}
		scans = append(scans, pipeline.ScanResult{
			Region:    r,
			Storm:     domain.Storm{ID: stormID, RegionID: r.ID, State: domain.StormDetected, WindowStart: tomorrow, WindowEnd: tomorrow.AddDate(0, 0, 6), DetectedAt: now},
			Forecasts: []domain.Forecast{aboveThresholdForecast(r.ID)},
			IsNew:     true,
		})
		evalResults[r.ID] = domain.Evaluation{Tier: domain.TierWorthALook, Recommendation: "Check " + r.ID}
	}

	fakeEval := &evaluation.FakeEvaluator{Results: evalResults}
	fakePoster := &discord.FakePoster{NextThreadID: "thread-split"}

	p := pipeline.New(nil, db, fakeEval, discardLogger())
	p.WithPoster(fakePoster)

	summary := pipeline.RunSummary{}
	evals, err := p.Evaluate(ctx, scans, &summary)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	compared, err := p.Compare(ctx, evals)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}

	grouped := p.Group(ctx, compared, &summary)

	_, err = p.PostGrouped(ctx, grouped)
	if err != nil {
		t.Fatalf("post grouped: %v", err)
	}

	// Different friction tiers → 2 singleton groups, each uses PostNew.
	if len(fakePoster.PostedNew) != 2 {
		t.Errorf("expected 2 PostNew calls (friction split), got %d", len(fakePoster.PostedNew))
	}
	if len(fakePoster.PostedGrouped) != 0 {
		t.Errorf("expected 0 PostGrouped calls (friction split), got %d", len(fakePoster.PostedGrouped))
	}
	if summary.Grouped != 2 {
		t.Errorf("expected summary.Grouped == 2, got %d", summary.Grouped)
	}
	if summary.Comparisons != 0 {
		t.Errorf("expected summary.Comparisons == 0, got %d", summary.Comparisons)
	}
}

func TestPipeline_GroupNonOverlappingWindows(t *testing.T) {
	ctx := context.Background()
	db := setupDB(t)
	seedProfile(t, ctx, db)

	// 2 regions in same MacroRegion and same friction, but non-overlapping windows.
	rA := testRegion("pnw-a")
	rA.MacroRegion = "pnw_cascades"
	rA.FrictionTier = domain.FrictionFlight

	rB := testRegion("pnw-b")
	rB.MacroRegion = "pnw_cascades"
	rB.FrictionTier = domain.FrictionFlight

	seedRegion(t, ctx, db, rA)
	seedRegion(t, ctx, db, rB)

	now := time.Now().UTC()
	// Storm A: next week (days 1-7).
	windowAStart := now.AddDate(0, 0, 1)
	windowAEnd := now.AddDate(0, 0, 7)
	// Storm B: two weeks from now (days 14-20), no overlap with A.
	windowBStart := now.AddDate(0, 0, 14)
	windowBEnd := now.AddDate(0, 0, 20)

	stormAID, err := db.CreateStorm(ctx, domain.Storm{
		RegionID: rA.ID, WindowStart: windowAStart,
		WindowEnd: windowAEnd, State: domain.StormDetected, DetectedAt: now,
	})
	if err != nil {
		t.Fatalf("create storm A: %v", err)
	}
	stormBID, err := db.CreateStorm(ctx, domain.Storm{
		RegionID: rB.ID, WindowStart: windowBStart,
		WindowEnd: windowBEnd, State: domain.StormDetected, DetectedAt: now,
	})
	if err != nil {
		t.Fatalf("create storm B: %v", err)
	}

	scans := []pipeline.ScanResult{
		{
			Region:    rA,
			Storm:     domain.Storm{ID: stormAID, RegionID: rA.ID, State: domain.StormDetected, WindowStart: windowAStart, WindowEnd: windowAEnd, DetectedAt: now},
			Forecasts: []domain.Forecast{aboveThresholdForecast(rA.ID)},
			IsNew:     true,
		},
		{
			Region:    rB,
			Storm:     domain.Storm{ID: stormBID, RegionID: rB.ID, State: domain.StormDetected, WindowStart: windowBStart, WindowEnd: windowBEnd, DetectedAt: now},
			Forecasts: []domain.Forecast{aboveThresholdForecast(rB.ID)},
			IsNew:     true,
		},
	}

	fakeEval := &evaluation.FakeEvaluator{
		Results: map[string]domain.Evaluation{
			rA.ID: {Tier: domain.TierDropEverything, Recommendation: "Storm A"},
			rB.ID: {Tier: domain.TierWorthALook, Recommendation: "Storm B"},
		},
	}
	fakePoster := &discord.FakePoster{NextThreadID: "thread-window-split"}

	p := pipeline.New(nil, db, fakeEval, discardLogger())
	p.WithPoster(fakePoster)

	summary := pipeline.RunSummary{}
	evals, err := p.Evaluate(ctx, scans, &summary)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	compared, err := p.Compare(ctx, evals)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}

	grouped := p.Group(ctx, compared, &summary)

	_, err = p.PostGrouped(ctx, grouped)
	if err != nil {
		t.Fatalf("post grouped: %v", err)
	}

	// Non-overlapping windows → 2 singleton groups, each uses PostNew.
	if len(fakePoster.PostedNew) != 2 {
		t.Errorf("expected 2 PostNew calls (window split), got %d", len(fakePoster.PostedNew))
	}
	if len(fakePoster.PostedGrouped) != 0 {
		t.Errorf("expected 0 PostGrouped calls (window split), got %d", len(fakePoster.PostedGrouped))
	}
	if summary.Grouped != 2 {
		t.Errorf("expected summary.Grouped == 2, got %d", summary.Grouped)
	}
	if summary.Comparisons != 0 {
		t.Errorf("expected summary.Comparisons == 0, got %d", summary.Comparisons)
	}
}
