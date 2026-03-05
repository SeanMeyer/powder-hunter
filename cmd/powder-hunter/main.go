package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/seanmeyer/powder-hunter/discord"
	"github.com/seanmeyer/powder-hunter/domain"
	"github.com/seanmeyer/powder-hunter/evaluation"
	"github.com/seanmeyer/powder-hunter/pipeline"
	"github.com/seanmeyer/powder-hunter/seed"
	"github.com/seanmeyer/powder-hunter/storage"
	"github.com/seanmeyer/powder-hunter/trace"
	"github.com/seanmeyer/powder-hunter/weather"
)

func main() {
	loadEnvFile(".env")
	os.Exit(run(context.Background(), os.Args[1:]))
}

// loadEnvFile reads KEY=VALUE lines from path and sets them as environment
// variables. Existing env vars take precedence (are not overwritten).
// Silently does nothing if the file doesn't exist.
func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		// Don't overwrite explicitly set env vars.
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
}

func run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		printUsage()
		return 1
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	switch args[0] {
	case "run":
		return runPipeline(ctx, args[1:])
	case "replay":
		return runReplay(ctx, args[1:])
	case "seed":
		return runSeed(ctx, args[1:])
	case "profile":
		return runProfile(ctx, args[1:])
	case "regions":
		return runRegions()
	case "trace":
		return runTrace(ctx, args[1:])
	case "reset":
		return runReset(ctx, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: powder-hunter <command> [flags]

Commands:
  run       Execute the full pipeline (scan → evaluate → compare → post)
  replay    Re-run a past evaluation with a different prompt version
  seed      Initialize or update region/resort database
  profile   View or update user profile
  regions   List all regions from seed data
  trace     Run pipeline for one region with human-readable debug output
  reset     Delete all storms and evaluations (keeps regions, profiles, prompts)`)
}

func runPipeline(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	dbPath := fs.String("db", "./powder-hunter.db", "SQLite database path")
	dryRun := fs.Bool("dry-run", false, "Run pipeline but skip Discord posting")
	regionFilter := fs.String("region", "", "Evaluate only this region (for debugging)")
	verbose := fs.Bool("verbose", false, "Enable debug-level logging")
	loop := fs.Bool("loop", false, "Run pipeline repeatedly on an interval")
	interval := fs.Duration("interval", 12*time.Hour, "Time between pipeline runs (requires --loop)")
	budget := fs.Float64("budget", 0, "Monthly budget limit in USD (0 = disabled)")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *verbose {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})))
	}

	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		slog.Error("GOOGLE_API_KEY environment variable is required")
		return 1
	}

	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")
	if webhookURL == "" && !*dryRun {
		slog.Error("DISCORD_WEBHOOK_URL environment variable is required (or use --dry-run)")
		return 1
	}

	db, err := storage.Open(*dbPath)
	if err != nil {
		slog.Error("open database", "error", err)
		return 1
	}
	defer db.Close()

	httpClient := &http.Client{Timeout: 30 * time.Second}
	omClient := weather.NewOpenMeteoClient(httpClient)
	nwsClient := weather.NewNWSClient(httpClient)
	weatherSvc := weather.NewService(omClient, nwsClient)

	geminiClient, err := evaluation.NewGeminiClient(ctx, apiKey)
	if err != nil {
		slog.Error("create gemini client", "error", err)
		return 1
	}
	evaluator := evaluation.NewGeminiEvaluator(geminiClient, db)

	p := pipeline.New(weatherSvc, db, evaluator, slog.Default())
	p.WithDryRun(*dryRun)
	p.WithCostTracker(db)
	if *budget > 0 {
		p.WithBudgetConfig(pipeline.BudgetConfig{
			MonthlyLimitUSD:  *budget,
			WarningThreshold: 0.8,
		})
	}

	if !*dryRun && webhookURL != "" {
		poster := discord.NewWebhookClient(webhookURL, &http.Client{})
		p.WithPoster(poster)
	}

	if *regionFilter != "" {
		slog.Info("region filter active", "region", *regionFilter)
	}

	if *loop {
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runLoop(ctx, p, *regionFilter, *interval)
	}

	summary, err := p.Run(ctx, *regionFilter)
	if err != nil {
		slog.Error("pipeline failed", "error", err)
		return 1
	}

	slog.Info("pipeline complete",
		"scanned", summary.Scanned,
		"evaluated", summary.Evaluated,
		"posted", summary.Posted,
		"expired", summary.Expired,
		"skipped_unchanged", summary.SkippedUnchanged,
		"skipped_cooldown", summary.SkippedCooldown,
		"skipped_budget", summary.SkippedBudget,
	)
	return 0
}

func runLoop(ctx context.Context, p *pipeline.Pipeline, region string, interval time.Duration) int {
	slog.Info("starting pipeline loop", "interval", interval)
	run := 0
	for {
		run++
		summary, err := p.Run(ctx, region)
		if err != nil {
			slog.Error("pipeline run failed", "run", run, "error", err)
		} else {
			slog.Info("pipeline complete",
				"run", run,
				"scanned", summary.Scanned,
				"evaluated", summary.Evaluated,
				"posted", summary.Posted,
				"expired", summary.Expired,
				"skipped_unchanged", summary.SkippedUnchanged,
				"skipped_cooldown", summary.SkippedCooldown,
				"skipped_budget", summary.SkippedBudget,
				"next_run", time.Now().Add(interval).Format(time.RFC3339),
			)
		}

		select {
		case <-ctx.Done():
			slog.Info("shutdown signal received, exiting")
			return 0
		case <-time.After(interval):
		}
	}
}

func runReplay(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	dbPath := fs.String("db", "./powder-hunter.db", "SQLite database path")
	stormID := fs.Int64("storm-id", 0, "Storm ID to replay (required)")
	evalID := fs.Int64("evaluation-id", 0, "Specific evaluation to replay (optional)")
	promptVersion := fs.String("prompt-version", "", "Prompt version to use (required)")
	outputFmt := fs.String("output", "text", "Output format: json | text")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *stormID == 0 {
		slog.Error("--storm-id is required")
		return 1
	}
	if *promptVersion == "" {
		slog.Error("--prompt-version is required")
		return 1
	}

	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		slog.Error("GOOGLE_API_KEY environment variable is required")
		return 1
	}

	db, err := storage.Open(*dbPath)
	if err != nil {
		slog.Error("open database", "error", err)
		return 1
	}
	defer db.Close()

	var eval *domain.Evaluation
	if *evalID != 0 {
		eval, err = db.GetEvaluation(ctx, *evalID)
		if err != nil {
			slog.Error("load evaluation", "eval_id", *evalID, "error", err)
			return 1
		}
		if eval == nil {
			slog.Error("evaluation not found", "eval_id", *evalID)
			return 1
		}
	} else {
		eval, err = db.GetLatestEvaluation(ctx, *stormID)
		if err != nil {
			slog.Error("load latest evaluation", "storm_id", *stormID, "error", err)
			return 1
		}
		if eval == nil {
			slog.Error("no evaluations found for storm", "storm_id", *stormID)
			return 1
		}
	}

	_, promptTemplate, err := db.GetPromptByVersion(ctx, "storm_eval", *promptVersion)
	if err != nil {
		slog.Error("load prompt template", "version", *promptVersion, "error", err)
		return 1
	}

	region, resorts, err := db.GetRegionWithResorts(ctx, eval.WeatherSnapshot[0].RegionID)
	if err != nil {
		slog.Error("load region", "error", err)
		return 1
	}

	profile, err := db.GetProfile(ctx)
	if err != nil {
		slog.Error("load profile", "error", err)
		return 1
	}
	if profile == nil {
		slog.Error("no user profile configured; run 'powder-hunter seed' first")
		return 1
	}

	replayDetection := domain.Detect(region, eval.WeatherSnapshot)

	renderedPrompt := evaluation.RenderPrompt(promptTemplate, evaluation.PromptData{
		WeatherData:       evaluation.FormatWeatherForPrompt(eval.WeatherSnapshot),
		RegionName:        region.Name,
		Resorts:           evaluation.FormatResortsForPrompt(resorts),
		UserProfile:       evaluation.FormatProfileForPrompt(*profile),
		StormWindow:       evaluation.FormatDetectionForPrompt(replayDetection),
		EvaluationHistory: "Replay — no history applied",
		PromptVersion:     *promptVersion,
	})

	geminiClient, err := evaluation.NewGeminiClient(ctx, apiKey)
	if err != nil {
		slog.Error("create gemini client", "error", err)
		return 1
	}

	result, err := geminiClient.EvaluateStorm(ctx, renderedPrompt)
	if err != nil {
		slog.Error("evaluate storm", "error", err)
		return 1
	}

	switch *outputFmt {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(result); encErr != nil {
			slog.Error("encode json output", "error", encErr)
			return 1
		}
	default:
		fmt.Printf("Tier:           %s\n", result.Tier)
		fmt.Printf("Recommendation: %s\n", result.Recommendation)
		fmt.Printf("Strategy:       %s\n", result.Strategy)
		fmt.Printf("Snow Quality:   %s\n", result.SnowQuality)
		fmt.Printf("Crowd Estimate: %s\n", result.CrowdEstimate)
		fmt.Printf("Closure Risk:   %s\n", result.ClosureRisk)
		if len(result.KeyFactors.Pros) > 0 {
			fmt.Printf("Pros:\n")
			for _, p := range result.KeyFactors.Pros {
				fmt.Printf("  + %s\n", p)
			}
		}
		if len(result.KeyFactors.Cons) > 0 {
			fmt.Printf("Cons:\n")
			for _, c := range result.KeyFactors.Cons {
				fmt.Printf("  - %s\n", c)
			}
		}
	}

	return 0
}

func runSeed(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	dbPath := fs.String("db", "./powder-hunter.db", "SQLite database path")
	force := fs.Bool("force", false, "Overwrite existing region/resort data")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	db, err := storage.Open(*dbPath)
	if err != nil {
		slog.Error("open database", "error", err)
		return 1
	}
	defer db.Close()

	regions := seed.Regions()
	for _, r := range regions {
		if *force {
			if err := db.UpsertRegion(ctx, r.Region); err != nil {
				slog.Error("upsert region", "region", r.Region.ID, "error", err)
				return 1
			}
			for _, resort := range r.Resorts {
				if err := db.UpsertResort(ctx, resort); err != nil {
					slog.Error("upsert resort", "resort", resort.ID, "error", err)
					return 1
				}
			}
		} else {
			if err := db.UpsertRegion(ctx, r.Region); err != nil {
				slog.Error("upsert region", "region", r.Region.ID, "error", err)
				return 1
			}
			for _, resort := range r.Resorts {
				if err := db.UpsertResort(ctx, resort); err != nil {
					slog.Error("upsert resort", "resort", resort.ID, "error", err)
					return 1
				}
			}
		}
	}

	// Create default profile if none exists.
	profile, err := db.GetProfile(ctx)
	if err != nil {
		slog.Error("check profile", "error", err)
		return 1
	}
	if profile == nil {
		defaultProfile := seed.DefaultProfile()
		if err := db.SaveProfile(ctx, defaultProfile); err != nil {
			slog.Error("create default profile", "error", err)
			return 1
		}
		slog.Info("created default user profile", "home", defaultProfile.HomeBase)
	}

	// Seed the initial prompt template if it doesn't already exist.
	promptID, promptVersion, promptTemplate := seed.InitialPromptTemplate()
	if err := db.SavePromptTemplate(ctx, promptID, promptVersion, promptTemplate); err != nil {
		slog.Error("seed prompt template", "error", err)
		return 1
	}
	slog.Info("prompt template seeded", "id", promptID, "version", promptVersion)

	slog.Info("seed complete", "regions", len(regions))
	return 0
}

func runProfile(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("profile", flag.ContinueOnError)
	dbPath := fs.String("db", "./powder-hunter.db", "SQLite database path")
	home := fs.String("home", "", "Set home base (e.g., \"Denver, CO\")")
	passes := fs.String("passes", "", "Comma-separated pass list (e.g., \"ikon,epic\")")
	remote := fs.String("remote", "", "Set remote work capability (true/false)")
	show := fs.Bool("show", false, "Display current profile")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	db, err := storage.Open(*dbPath)
	if err != nil {
		slog.Error("open database", "error", err)
		return 1
	}
	defer db.Close()

	profile, err := db.GetProfile(ctx)
	if err != nil {
		slog.Error("get profile", "error", err)
		return 1
	}
	if profile == nil {
		profile = &domain.UserProfile{ID: 1}
	}

	updated := false
	if *home != "" {
		profile.HomeBase = *home
		updated = true
	}
	if *passes != "" {
		profile.PassesHeld = strings.Split(*passes, ",")
		updated = true
	}
	if *remote != "" {
		profile.RemoteWorkCapable = *remote == "true"
		updated = true
	}

	if updated {
		if err := db.SaveProfile(ctx, *profile); err != nil {
			slog.Error("save profile", "error", err)
			return 1
		}
		slog.Info("profile updated")
	}

	if *show || !updated {
		fmt.Printf("Home: %s\n", profile.HomeBase)
		fmt.Printf("Passes: %s\n", strings.Join(profile.PassesHeld, ", "))
		fmt.Printf("Remote work: %v\n", profile.RemoteWorkCapable)
		fmt.Printf("PTO days: %d\n", profile.TypicalPTODays)
		fmt.Printf("Min tier for ping: %s\n", profile.MinTierForPing)
	}

	return 0
}

func runRegions() int {
	regions := seed.Regions()
	rows := make([]trace.RegionRow, len(regions))
	for i, r := range regions {
		rows[i] = trace.RegionRow{
			ID:          r.Region.ID,
			Name:        r.Region.Name,
			Tier:        string(r.Region.FrictionTier),
			ResortCount: len(r.Resorts),
		}
	}
	trace.FormatRegionsTable(os.Stdout, rows)
	return 0
}

func runReset(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("reset", flag.ContinueOnError)
	dbPath := fs.String("db", "./powder-hunter.db", "SQLite database path")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	db, err := storage.Open(*dbPath)
	if err != nil {
		slog.Error("open database", "error", err)
		return 1
	}
	defer db.Close()

	count, err := db.ResetStormData(ctx)
	if err != nil {
		slog.Error("reset failed", "error", err)
		return 1
	}

	slog.Info("reset complete", "storms_deleted", count)
	return 0
}

func runTrace(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("trace", flag.ContinueOnError)
	regionID := fs.String("region", "", "Region ID to trace (required)")
	dbPath := fs.String("db", "", "SQLite database path (default: temp dir)")
	weatherOnly := fs.Bool("weather-only", false, "Only fetch and display weather data")
	showPrompt := fs.Bool("show-prompt", false, "Display the rendered LLM prompt")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *regionID == "" {
		fmt.Fprintln(os.Stderr, "error: --region is required")
		fmt.Fprintln(os.Stderr, "usage: powder-hunter trace --region <id> [--weather-only] [--db <path>]")
		return 1
	}

	// Find region in seed data.
	allRegions := seed.Regions()
	var found *seed.RegionWithResorts
	for i := range allRegions {
		if allRegions[i].Region.ID == *regionID {
			found = &allRegions[i]
			break
		}
	}
	if found == nil {
		fmt.Fprintf(os.Stderr, "error: region %q not found in seed data\n", *regionID)
		fmt.Fprintln(os.Stderr, "run 'powder-hunter regions' to see available region IDs")
		return 1
	}

	w := os.Stdout
	trace.FormatTimestamp(w, time.Now())

	// ── Weather ──────────────────────────────────────────────────────────────
	httpClient := &http.Client{Timeout: 30 * time.Second}
	omClient := weather.NewOpenMeteoClient(httpClient)
	nwsClient := weather.NewNWSClient(httpClient)
	weatherSvc := weather.NewService(omClient, nwsClient)

	fetchResult, err := weatherSvc.FetchAll(ctx, found.Region, found.Resorts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: weather fetch failed: %v\n", err)
		return 1
	}
	forecasts := fetchResult.Forecasts

	trace.FormatWeather(w, found.Region, found.Resorts, forecasts)

	// ── Per-Resort Multi-Model Consensus ────────────────────────────────────
	resortModels := make(map[string][]domain.Forecast)
	for _, f := range forecasts {
		if f.Source == "open_meteo" && f.Model != "" && f.ResortID != "" {
			resortModels[f.ResortID] = append(resortModels[f.ResortID], f)
		}
	}
	resortConsensus := make(map[string]domain.ModelConsensus, len(resortModels))
	for resortID, mf := range resortModels {
		resortConsensus[resortID] = domain.ComputeConsensus(mf)
	}
	trace.FormatConsensus(w, resortConsensus, found.Resorts)

	// ── NWS Forecast Discussion ─────────────────────────────────────────────
	trace.FormatAFD(w, fetchResult.Discussion, forecasts)

	// ── Detection ────────────────────────────────────────────────────────────
	detection := domain.Detect(found.Region, forecasts)
	trace.FormatDetection(w, found.Region, detection)

	if *weatherOnly {
		if *showPrompt {
			prompt := buildPrompt(found.Region, found.Resorts, forecasts)
			trace.FormatPrompt(w, prompt)
		}
		trace.FormatWeatherOnly(w)
		return 0
	}

	// ── LLM Evaluation ──────────────────────────────────────────────────────
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: GOOGLE_API_KEY environment variable is required for LLM evaluation")
		fmt.Fprintln(os.Stderr, "use --weather-only to skip LLM evaluation")
		return 1
	}

	// Use a temp DB if none specified, so trace works without a pre-seeded database.
	actualDBPath := *dbPath
	if actualDBPath == "" {
		tmpDir, tmpErr := os.MkdirTemp("", "powder-hunter-trace-*")
		if tmpErr != nil {
			fmt.Fprintf(os.Stderr, "error: create temp dir: %v\n", tmpErr)
			return 1
		}
		defer os.RemoveAll(tmpDir)
		actualDBPath = filepath.Join(tmpDir, "trace.db")
	}

	db, err := storage.Open(actualDBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open database: %v\n", err)
		return 1
	}
	defer db.Close()

	// Seed the DB with regions and a default profile so the evaluator has context.
	for _, r := range allRegions {
		if upsertErr := db.UpsertRegion(ctx, r.Region); upsertErr != nil {
			fmt.Fprintf(os.Stderr, "error: seed region: %v\n", upsertErr)
			return 1
		}
		for _, resort := range r.Resorts {
			if upsertErr := db.UpsertResort(ctx, resort); upsertErr != nil {
				fmt.Fprintf(os.Stderr, "error: seed resort: %v\n", upsertErr)
				return 1
			}
		}
	}

	profile, err := db.GetProfile(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: get profile: %v\n", err)
		return 1
	}
	if profile == nil {
		defaultProfile := seed.DefaultProfile()
		if saveErr := db.SaveProfile(ctx, defaultProfile); saveErr != nil {
			fmt.Fprintf(os.Stderr, "error: create default profile: %v\n", saveErr)
			return 1
		}
		profile = &defaultProfile
	}

	// Seed the prompt template.
	promptID, promptVersion, promptTemplate := seed.InitialPromptTemplate()
	if seedErr := db.SavePromptTemplate(ctx, promptID, promptVersion, promptTemplate); seedErr != nil {
		fmt.Fprintf(os.Stderr, "error: seed prompt template: %v\n", seedErr)
		return 1
	}

	geminiClient, err := evaluation.NewGeminiClient(ctx, apiKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: create gemini client: %v\n", err)
		return 1
	}
	evaluator := evaluation.NewGeminiEvaluator(geminiClient, db)

	eval, err := evaluator.Evaluate(ctx, evaluation.EvalContext{
		Forecasts: forecasts,
		Region:    found.Region,
		Resorts:   found.Resorts,
		Profile:   *profile,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: LLM evaluation failed: %v\n", err)
		return 1
	}

	if *showPrompt {
		trace.FormatPrompt(w, eval.RenderedPrompt)
	}

	trace.FormatEvaluation(w, eval)

	// ── Change Detection ────────────────────────────────────────────────────
	// Trace always compares against an empty prior (no DB history for this storm).
	changeClass := domain.Compare(domain.Evaluation{}, eval)
	trace.FormatComparison(w, changeClass, "")

	// ── Discord Preview ─────────────────────────────────────────────────────
	trace.FormatDiscordPreview(w, eval, found.Region)

	return 0
}

// buildPrompt renders the LLM prompt using seed data and a default profile,
// without requiring a database or API key. Used by --show-prompt in weather-only mode.
func buildPrompt(region domain.Region, resorts []domain.Resort, forecasts []domain.Forecast) string {
	_, promptVersion, promptTemplate := seed.InitialPromptTemplate()

	defaultProfile := seed.DefaultProfile()

	detection := domain.Detect(region, forecasts)

	return evaluation.RenderPrompt(promptTemplate, evaluation.PromptData{
		WeatherData:       evaluation.FormatWeatherForPrompt(forecasts),
		RegionName:        region.Name,
		Resorts:           evaluation.FormatResortsForPrompt(resorts),
		UserProfile:       evaluation.FormatProfileForPrompt(defaultProfile),
		StormWindow:       evaluation.FormatDetectionForPrompt(detection),
		EvaluationHistory: "No prior evaluations",
		PromptVersion:     promptVersion,
	})
}
