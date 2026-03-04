package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/seanmeyer/powder-hunter/discord"
	"github.com/seanmeyer/powder-hunter/domain"
	"github.com/seanmeyer/powder-hunter/evaluation"
	"github.com/seanmeyer/powder-hunter/pipeline"
	"github.com/seanmeyer/powder-hunter/seed"
	"github.com/seanmeyer/powder-hunter/storage"
	"github.com/seanmeyer/powder-hunter/weather"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:]))
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
  profile   View or update user profile`)
}

func runPipeline(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	dbPath := fs.String("db", "./powder-hunter.db", "SQLite database path")
	dryRun := fs.Bool("dry-run", false, "Run pipeline but skip Discord posting")
	regionFilter := fs.String("region", "", "Evaluate only this region (for debugging)")
	verbose := fs.Bool("verbose", false, "Enable debug-level logging")
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

	omClient := weather.NewOpenMeteoClient(nil)
	nwsClient := weather.NewNWSClient(nil)
	weatherSvc := weather.NewService(omClient, nwsClient)

	geminiClient, err := evaluation.NewGeminiClient(ctx, apiKey)
	if err != nil {
		slog.Error("create gemini client", "error", err)
		return 1
	}
	evaluator := evaluation.NewGeminiEvaluator(geminiClient, db)

	p := pipeline.New(weatherSvc, db, evaluator, slog.Default())
	p.WithDryRun(*dryRun)

	if !*dryRun && webhookURL != "" {
		poster := discord.NewWebhookClient(webhookURL, &http.Client{})
		p.WithPoster(poster)
	}

	if *regionFilter != "" {
		slog.Info("region filter active", "region", *regionFilter)
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
	)
	return 0
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

	weatherJSON, _ := json.Marshal(eval.WeatherSnapshot)
	resortsJSON, _ := json.Marshal(resorts)
	profileJSON, _ := json.Marshal(profile)

	renderedPrompt := evaluation.RenderPrompt(promptTemplate, evaluation.PromptData{
		WeatherData:       string(weatherJSON),
		RegionName:        region.Name,
		Resorts:           string(resortsJSON),
		UserProfile:       string(profileJSON),
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
		defaultProfile := domain.UserProfile{
			ID:                1,
			HomeBase:          "Denver, CO",
			HomeLatitude:      39.7392,
			HomeLongitude:     -104.9903,
			PassesHeld:        []string{"ikon"},
			RemoteWorkCapable: true,
			TypicalPTODays:    15,
			MinTierForPing:    domain.TierDropEverything,
			QuietHoursStart:   "22:00",
			QuietHoursEnd:     "07:00",
		}
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
