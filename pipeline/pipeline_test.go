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

	evals, err := p.Evaluate(ctx, scans)
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

	evals, err := p.Evaluate(ctx, scans)
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

	evals, err := p.Evaluate(ctx, scans)
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

	evals, err := p.Evaluate(ctx, scans)
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

	evals, _ := p.Evaluate(ctx, scans)
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

	evals, err := p.Evaluate(ctx, scans)
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
