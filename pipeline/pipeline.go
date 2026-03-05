package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/seanmeyer/powder-hunter/discord"
	"github.com/seanmeyer/powder-hunter/domain"
	"github.com/seanmeyer/powder-hunter/evaluation"
	"github.com/seanmeyer/powder-hunter/storage"
	"github.com/seanmeyer/powder-hunter/weather"
)

const scanConcurrency = 5
const evalConcurrency = 3

// CostTracker records and queries Gemini API call costs.
type CostTracker interface {
	RecordCost(ctx context.Context, stormID int64, regionID string, estimatedCost float64, success bool) error
	MonthlySpend(ctx context.Context) (float64, int, error)
}

// BudgetConfig controls monthly spend limits. A zero MonthlyLimitUSD disables budget enforcement.
type BudgetConfig struct {
	MonthlyLimitUSD  float64
	WarningThreshold float64 // fraction of budget that triggers a warning (default 0.8)
}

// Pipeline is the top-level scan-and-detect orchestrator.
type Pipeline struct {
	weather      *weather.Service
	store        *storage.DB
	evaluator    evaluation.Evaluator
	poster       discord.Poster
	dryRun       bool
	logger       *slog.Logger
	costTracker  CostTracker
	budgetConfig BudgetConfig
	briefer      evaluation.Briefer
}

func New(weather *weather.Service, store *storage.DB, evaluator evaluation.Evaluator, logger *slog.Logger) *Pipeline {
	return &Pipeline{weather: weather, store: store, evaluator: evaluator, logger: logger, budgetConfig: BudgetConfig{WarningThreshold: 0.8}}
}

// WithCostTracker attaches a cost tracker for budget management.
func (p *Pipeline) WithCostTracker(ct CostTracker) *Pipeline {
	p.costTracker = ct
	return p
}

// WithBudgetConfig sets the monthly budget limit.
func (p *Pipeline) WithBudgetConfig(bc BudgetConfig) *Pipeline {
	p.budgetConfig = bc
	return p
}

// WithBriefer attaches an LLM briefer for multi-region storm groups.
func (p *Pipeline) WithBriefer(b evaluation.Briefer) *Pipeline {
	p.briefer = b
	return p
}

// WithPoster attaches a Discord poster and returns the pipeline for chaining.
func (p *Pipeline) WithPoster(poster discord.Poster) *Pipeline {
	p.poster = poster
	return p
}

// WithDryRun disables actual Discord posts while keeping all other pipeline stages active.
func (p *Pipeline) WithDryRun(dryRun bool) *Pipeline {
	p.dryRun = dryRun
	return p
}

// ScanResult carries a region, its current storm, and the fresh forecasts that
// produced (or refreshed) the detection. Downstream stages use this to evaluate
// and post without re-fetching.
type ScanResult struct {
	Region           domain.Region
	Storm            domain.Storm
	Forecasts        []domain.Forecast
	ResortConsensus  map[string]domain.ModelConsensus // per-resort consensus keyed by resort ID
	Discussion       *domain.ForecastDiscussion       // NWS AFD (nil for non-US or fetch failure)
	IsNew            bool                             // true if storm was just created, false if existing
}

// RunSummary holds per-stage counts from a completed pipeline execution.
type RunSummary struct {
	Scanned          int
	Evaluated        int
	Posted           int
	Expired          int
	SkippedUnchanged int // storms skipped due to unchanged weather
	SkippedCooldown  int // storms skipped due to tier-based cooldown
	SkippedBudget    int // storms skipped due to budget limit
	EvalFailed       int // storms where evaluation errored (Gemini failure, etc.)
	Grouped          int // number of storm groups formed
	Comparisons      int // number of LLM comparison calls made (multi-member groups only)
}

// Run executes the full pipeline: scan → evaluate → compare → post → expire.
// regionFilter, when non-empty, restricts scan results to a single region ID.
// Per-region errors are logged and skipped; Run returns nil unless a fatal
// cross-cutting failure prevents any useful work.
func (p *Pipeline) Run(ctx context.Context, regionFilter string) (RunSummary, error) {
	scans, err := p.Scan(ctx, regionFilter)
	if err != nil {
		return RunSummary{}, fmt.Errorf("scan: %w", err)
	}

	var summary RunSummary
	summary.Scanned = len(scans)

	evals, err := p.Evaluate(ctx, scans, &summary)
	if err != nil {
		return RunSummary{}, fmt.Errorf("evaluate: %w", err)
	}

	compared, err := p.Compare(ctx, evals)
	if err != nil {
		return RunSummary{}, fmt.Errorf("compare: %w", err)
	}

	grouped := p.Group(ctx, compared, &summary)

	posted, postErr := p.PostGrouped(ctx, grouped)
	if postErr != nil {
		p.logger.WarnContext(ctx, "post stage error", "error", postErr)
	}

	expiredCount, err := p.ExpireStaleStorms(ctx, scans)
	if err != nil {
		p.logger.WarnContext(ctx, "expire stage error", "error", err)
	}

	summary.Evaluated = len(evals)
	summary.Posted = posted
	summary.Expired = expiredCount
	return summary, nil
}

// Scan fetches weather for all regions, detects storms, and persists new/updated storms.
// Errors in one region do not block processing of other regions.
// Returns the list of storms that need evaluation (new detections + tracked storms).
func (p *Pipeline) Scan(ctx context.Context, regionFilter string) ([]ScanResult, error) {
	regions, err := p.store.ListRegions(ctx)
	if err != nil {
		return nil, err
	}

	if regionFilter != "" {
		filtered := regions[:0]
		for _, r := range regions {
			if r.ID == regionFilter {
				filtered = append(filtered, r)
				break
			}
		}
		regions = filtered
	}

	activeByRegion, err := p.store.GetActiveStormsByRegion(ctx)
	if err != nil {
		return nil, err
	}

	type regionResult struct {
		result ScanResult
		ok     bool
	}

	results := make([]regionResult, len(regions))
	sem := make(chan struct{}, scanConcurrency)

	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)

	for i, region := range regions {
		i, region := i, region
		g.Go(func() error {
			sem <- struct{}{}
			defer func() { <-sem }()

			_, resorts, resortErr := p.store.GetRegionWithResorts(gctx, region.ID)
			if resortErr != nil {
				p.logger.WarnContext(gctx, "load resorts failed, skipping region",
					"region_id", region.ID, "error", resortErr)
				return nil
			}

			fetchResult, fetchErr := p.weather.FetchAll(gctx, region, resorts)
			if fetchErr != nil {
				p.logger.WarnContext(gctx, "weather fetch failed, skipping region",
					"region_id", region.ID,
					"error", fetchErr,
				)
				return nil
			}
			forecasts := fetchResult.Forecasts

			// Compute per-resort consensus from Open-Meteo model forecasts.
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

			detection := domain.Detect(region, forecasts)
			p.logDetection(gctx, region, detection)

			activeStorms := activeByRegion[region.ID]

			if detection.Detected {
				storm, isNew, persistErr := p.persistDetection(gctx, region, detection)
				if persistErr != nil {
					p.logger.WarnContext(gctx, "failed to persist detection, skipping region",
						"region_id", region.ID,
						"error", persistErr,
					)
					return nil
				}
				mu.Lock()
				results[i] = regionResult{
					result: ScanResult{
						Region:     region,
						Storm:      storm,
						Forecasts:  forecasts,
						ResortConsensus: resortConsensus,
						Discussion: fetchResult.Discussion,
						IsNew:      isNew,
					},
					ok: true,
				}
				mu.Unlock()
				return nil
			}

			// FR-003: regions with active tracked storms are always re-evaluated
			// even when current forecasts fall below threshold, so downstream stages
			// can update or expire the storm as conditions change.
			if len(activeStorms) > 0 {
				mu.Lock()
				results[i] = regionResult{
					result: ScanResult{
						Region:     region,
						Storm:      activeStorms[0],
						Forecasts:  forecasts,
						ResortConsensus: resortConsensus,
						Discussion: fetchResult.Discussion,
						IsNew:      false,
					},
					ok: true,
				}
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	var out []ScanResult
	for _, r := range results {
		if r.ok {
			out = append(out, r.result)
		}
	}
	return out, nil
}

// persistDetection upserts a storm record for the detection. It merges into an
// existing overlapping storm to avoid creating duplicates across pipeline runs.
func (p *Pipeline) persistDetection(ctx context.Context, region domain.Region, detection domain.DetectionResult) (domain.Storm, bool, error) {
	// Use the union of all detected windows as the storm's time span.
	windowStart, windowEnd := detectionWindow(detection.Windows)

	existing, err := p.store.FindOverlappingStorm(ctx, region.ID, windowStart, windowEnd)
	if err != nil {
		return domain.Storm{}, false, err
	}

	if existing != nil {
		expanded := existing.ExpandWindow(windowStart, windowEnd)
		if err := p.store.UpdateStorm(ctx, expanded); err != nil {
			return domain.Storm{}, false, err
		}
		return expanded, false, nil
	}

	newStorm := domain.Storm{
		RegionID:    region.ID,
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
		State:       domain.StormDetected,
		DetectedAt:  time.Now().UTC(),
	}
	id, err := p.store.CreateStorm(ctx, newStorm)
	if err != nil {
		return domain.Storm{}, false, err
	}
	newStorm.ID = id
	return newStorm, true, nil
}

// logDetection emits a structured log line per region so operators can trace
// what the scanner found without querying the database.
func (p *Pipeline) logDetection(ctx context.Context, region domain.Region, d domain.DetectionResult) {
	attrs := []any{
		"region_id", region.ID,
		"detected", d.Detected,
	}
	for _, w := range d.Windows {
		if w.IsNearRange {
			attrs = append(attrs, "near_range_in", w.TotalIn)
		} else {
			attrs = append(attrs, "extended_range_in", w.TotalIn)
		}
	}
	p.logger.InfoContext(ctx, "scan region", attrs...)
}

// detectionWindow returns the earliest start and latest end across all windows.
func detectionWindow(windows []domain.SnowfallWindow) (start, end time.Time) {
	for i, w := range windows {
		if i == 0 || w.StartDate.Before(start) {
			start = w.StartDate
		}
		if i == 0 || w.EndDate.After(end) {
			end = w.EndDate
		}
	}
	return start, end
}

// EvalResult extends ScanResult with the completed LLM evaluation.
type EvalResult struct {
	ScanResult
	Evaluation domain.Evaluation
}

// Evaluate scores each scanned storm using the configured evaluator. Runs up to
// evalConcurrency evaluations concurrently. Before calling the evaluator, each
// storm is checked against the gating logic (weather-change detection, tier
// cooldown, budget). Skipped storms are counted in the summary.
// The resulting evaluations are NOT persisted here — Compare adds the change
// class and saves them in a single write.
// Errors for individual regions are logged and skipped.
func (p *Pipeline) Evaluate(ctx context.Context, scans []ScanResult, summary *RunSummary) ([]EvalResult, error) {
	profile, err := p.store.GetProfile(ctx)
	if err != nil {
		return nil, fmt.Errorf("load user profile: %w", err)
	}
	if profile == nil {
		return nil, fmt.Errorf("no user profile configured; run 'powder-hunter seed' first")
	}

	// Check budget once at the start of the Evaluate stage.
	budgetExceeded := false
	if p.costTracker != nil && p.budgetConfig.MonthlyLimitUSD > 0 {
		spent, calls, budgetErr := p.costTracker.MonthlySpend(ctx)
		if budgetErr != nil {
			p.logger.WarnContext(ctx, "check monthly spend failed, proceeding without budget enforcement",
				"error", budgetErr)
		} else {
			if spent >= p.budgetConfig.MonthlyLimitUSD {
				budgetExceeded = true
			}
			threshold := p.budgetConfig.WarningThreshold
			if threshold <= 0 {
				threshold = 0.8
			}
			if spent >= p.budgetConfig.MonthlyLimitUSD*threshold && !budgetExceeded {
				p.logger.WarnContext(ctx, "monthly budget approaching limit",
					"budget_usd", p.budgetConfig.MonthlyLimitUSD,
					"spent_usd", spent,
					"remaining_usd", p.budgetConfig.MonthlyLimitUSD-spent,
					"calls_this_month", calls,
				)
			}
		}
	}

	type evalEntry struct {
		result EvalResult
		ok     bool
	}
	entries := make([]evalEntry, len(scans))

	sem := make(chan struct{}, evalConcurrency)
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)

	for i, scan := range scans {
		i, scan := i, scan
		g.Go(func() error {
			sem <- struct{}{}
			defer func() { <-sem }()

			// Gating: check whether this storm should be re-evaluated.
			lastEval, lastEvalErr := p.store.GetLatestEvaluation(gctx, scan.Storm.ID)
			if lastEvalErr != nil {
				p.logger.WarnContext(gctx, "load latest eval for gating failed, proceeding with evaluation",
					"storm_id", scan.Storm.ID, "error", lastEvalErr)
			}

			isFirstEval := lastEval == nil
			var weatherChange domain.WeatherChangeSummary
			var timeSinceLastEval time.Duration
			currentTier := scan.Storm.CurrentTier

			if !isFirstEval {
				weatherChange = domain.ForecastsChanged(lastEval.WeatherSnapshot, scan.Forecasts)
				timeSinceLastEval = time.Since(lastEval.EvaluatedAt)
			} else {
				weatherChange = domain.WeatherChangeSummary{Changed: true, Reason: "first evaluation"}
			}

			decision := ShouldEvaluate(isFirstEval, currentTier, timeSinceLastEval, weatherChange, budgetExceeded)

			if !decision.ShouldEvaluate {
				p.logger.InfoContext(gctx, "evaluation skipped",
					"region_id", scan.Region.ID,
					"storm_id", scan.Storm.ID,
					"skip_reason", string(decision.Reason),
					"hours_since_last_eval", timeSinceLastEval.Hours(),
				)
				mu.Lock()
				switch decision.Reason {
				case domain.SkipUnchangedWeather:
					summary.SkippedUnchanged++
				case domain.SkipCooldown:
					summary.SkippedCooldown++
				case domain.SkipBudgetExceeded:
					summary.SkippedBudget++
				}
				mu.Unlock()
				return nil
			}

			_, resorts, regionErr := p.store.GetRegionWithResorts(gctx, scan.Region.ID)
			if regionErr != nil {
				p.logger.WarnContext(gctx, "load resorts failed, skipping eval",
					"region_id", scan.Region.ID,
					"error", regionErr,
				)
				return nil
			}

			history, histErr := p.store.GetEvaluationHistory(gctx, scan.Storm.ID)
			if histErr != nil {
				p.logger.WarnContext(gctx, "load eval history failed, skipping eval",
					"region_id", scan.Region.ID,
					"storm_id", scan.Storm.ID,
					"error", histErr,
				)
				return nil
			}

			eval, evalErr := p.evaluator.Evaluate(gctx, evaluation.EvalContext{
				Forecasts:       scan.Forecasts,
				Region:          scan.Region,
				Resorts:         resorts,
				Profile:         *profile,
				History:         history,
				ResortConsensus: scan.ResortConsensus,
				Discussion:      scan.Discussion,
			})
			if evalErr != nil {
				p.logger.WarnContext(gctx, "evaluation failed, skipping region",
					"region_id", scan.Region.ID,
					"error", evalErr,
				)
				mu.Lock()
				summary.EvalFailed++
				mu.Unlock()
				return nil
			}

			eval.StormID = scan.Storm.ID

			// Record cost after successful evaluation.
			if p.costTracker != nil {
				if costErr := p.costTracker.RecordCost(gctx, scan.Storm.ID, scan.Region.ID, 0.015, true); costErr != nil {
					p.logger.WarnContext(gctx, "record eval cost failed",
						"storm_id", scan.Storm.ID, "error", costErr)
				}
			}

			p.logger.InfoContext(gctx, "storm evaluated",
				"region_id", scan.Region.ID,
				"storm_id", scan.Storm.ID,
				"tier", string(eval.Tier),
			)

			mu.Lock()
			entries[i] = evalEntry{
				result: EvalResult{ScanResult: scan, Evaluation: eval},
				ok:     true,
			}
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	var out []EvalResult
	for _, e := range entries {
		if e.ok {
			out = append(out, e.result)
		}
	}
	return out, nil
}

// CompareResult extends EvalResult with the change classification against the
// prior evaluation for the same storm.
type CompareResult struct {
	EvalResult
	ChangeClass domain.ChangeClass
	PrevTier    domain.Tier // empty string if this is the first evaluation
}

// GroupedResult wraps a storm group with its optional LLM briefing and the
// original CompareResults for posting. Singleton groups have an empty Briefing.
type GroupedResult struct {
	Group   domain.StormGroup
	Briefing evaluation.BriefingResult // empty for singletons
	Results []CompareResult            // the individual CompareResults in this group
}

// Compare classifies the change for each evaluation against the storm's prior
// evaluation, then saves the evaluation (with change class set) and updates the
// storm state. This is the single write point for evaluations so change_class is
// always populated on insert. Errors per-region are logged and skipped.
func (p *Pipeline) Compare(ctx context.Context, evals []EvalResult) ([]CompareResult, error) {
	out := make([]CompareResult, 0, len(evals))
	for _, e := range evals {
		prev, err := p.store.GetLatestEvaluation(ctx, e.Storm.ID)
		if err != nil {
			p.logger.WarnContext(ctx, "load prior evaluation failed, skipping compare",
				"storm_id", e.Storm.ID,
				"error", err,
			)
			continue
		}

		var prevEval domain.Evaluation
		if prev != nil {
			prevEval = *prev
		}

		changeClass := domain.Compare(prevEval, e.Evaluation)

		evalToSave := e.Evaluation
		evalToSave.ChangeClass = changeClass

		evalID, saveErr := p.store.SaveEvaluation(ctx, evalToSave)
		if saveErr != nil {
			p.logger.WarnContext(ctx, "save evaluation failed, skipping compare",
				"region_id", e.Region.ID,
				"error", saveErr,
			)
			continue
		}
		evalToSave.ID = evalID

		updatedStorm := e.Storm
		updatedStorm.State = domain.StormEvaluated
		updatedStorm.CurrentTier = evalToSave.Tier
		updatedStorm.LastEvaluatedAt = evalToSave.EvaluatedAt
		if updateErr := p.store.UpdateStorm(ctx, updatedStorm); updateErr != nil {
			p.logger.WarnContext(ctx, "update storm state failed",
				"storm_id", e.Storm.ID,
				"error", updateErr,
			)
		}

		p.logger.InfoContext(ctx, "storm compared",
			"region_id", e.Region.ID,
			"storm_id", e.Storm.ID,
			"change_class", string(changeClass),
			"prev_tier", string(prevEval.Tier),
			"curr_tier", string(evalToSave.Tier),
			"eval_id", evalID,
		)

		out = append(out, CompareResult{
			EvalResult:  EvalResult{ScanResult: e.ScanResult, Evaluation: evalToSave},
			ChangeClass: changeClass,
			PrevTier:    prevEval.Tier,
		})
	}
	return out, nil
}

// ExpireStaleStorms transitions active storms to StormExpired when their
// detection window has ended and no fresh detection was found in this scan run.
// Storms whose WindowEnd is in the past and that were not included in scans
// (meaning current forecasts are below threshold) are considered expired.
// Returns the number of storms successfully expired.
func (p *Pipeline) ExpireStaleStorms(ctx context.Context, scans []ScanResult) (int, error) {
	activeByRegion, err := p.store.GetActiveStormsByRegion(ctx)
	if err != nil {
		return 0, fmt.Errorf("load active storms for expiration: %w", err)
	}

	scannedStormIDs := make(map[int64]struct{}, len(scans))
	for _, s := range scans {
		scannedStormIDs[s.Storm.ID] = struct{}{}
	}

	now := time.Now().UTC()
	expired := 0
	for _, storms := range activeByRegion {
		for _, storm := range storms {
			if _, inCurrentRun := scannedStormIDs[storm.ID]; inCurrentRun {
				continue
			}
			// Storm was not detected in this scan run and its window has passed.
			if storm.WindowEnd.Before(now) {
				if !domain.ValidTransition(storm.State, domain.StormExpired) {
					p.logger.WarnContext(ctx, "invalid expiration transition",
						"storm_id", storm.ID,
						"state", string(storm.State),
					)
					continue
				}
				expiredStorm := storm
				expiredStorm.State = domain.StormExpired
				if err := p.store.UpdateStorm(ctx, expiredStorm); err != nil {
					p.logger.WarnContext(ctx, "expire storm failed",
						"storm_id", storm.ID,
						"error", err,
					)
					continue
				}
				expired++
				p.logger.InfoContext(ctx, "storm expired",
					"storm_id", storm.ID,
					"region_id", storm.RegionID,
					"window_end", storm.WindowEnd,
				)
			}
		}
	}
	return expired, nil
}

// Group buckets CompareResults by macro-region + friction tier, then runs an LLM
// comparison for multi-member groups. Singleton groups pass through without a
// comparison call.
func (p *Pipeline) Group(ctx context.Context, results []CompareResult, summary *RunSummary) []GroupedResult {
	inputs := make([]domain.StormGroupInput, len(results))
	for i, r := range results {
		inputs[i] = domain.StormGroupInput{
			RegionID:    r.Region.ID,
			MacroRegion: r.Region.MacroRegion,
			Friction:    r.Region.FrictionTier,
			WindowStart: r.Storm.WindowStart,
			WindowEnd:   r.Storm.WindowEnd,
			Tier:        r.Evaluation.Tier,
			Index:       i,
		}
	}

	stormGroups := domain.GroupByMacroRegion(inputs)

	grouped := make([]GroupedResult, 0, len(stormGroups))
	for _, sg := range stormGroups {
		members := make([]CompareResult, len(sg.Members))
		for i, m := range sg.Members {
			members[i] = results[m.Index]
		}

		var briefing evaluation.BriefingResult

		if len(members) >= 2 && p.briefer != nil {
			summaries := make([]evaluation.RegionSummary, len(members))
			for i, r := range members {
				// Build snowfall summary from day-by-day data.
				var snowParts []string
				for _, d := range r.Evaluation.DayByDay {
					if d.Snowfall != "" {
						snowParts = append(snowParts, fmt.Sprintf("%s: %s", d.Date.Format("Jan 2"), d.Snowfall))
					}
				}

				summaries[i] = evaluation.RegionSummary{
					RegionID:       r.Region.ID,
					RegionName:     r.Region.Name,
					Tier:           string(r.Evaluation.Tier),
					Snowfall:       strings.Join(snowParts, ", "),
					SnowQuality:    r.Evaluation.SnowQuality,
					CrowdEstimate:  r.Evaluation.CrowdEstimate,
					Strategy:       r.Evaluation.Strategy,
					Recommendation: r.Evaluation.Recommendation,
					LodgingCost:    r.Evaluation.LogisticsSummary.LodgingCost,
					FlightCost:     r.Evaluation.LogisticsSummary.FlightCost,
					CarRental:      r.Evaluation.LogisticsSummary.CarRental,
					BestDay:        r.Evaluation.BestSkiDay.Format("Mon Jan 2"),
					BestDayReason:  r.Evaluation.BestSkiDayReason,
				}
			}

			bc := evaluation.BriefingContext{
				MacroRegionName: domain.MacroRegionDisplayNameFromKey(sg.Key),
				FrictionTier:    string(members[0].Region.FrictionTier),
				Summaries:       summaries,
			}

			result, err := p.briefer.BriefStorm(ctx, bc)
			if err != nil {
				p.logger.WarnContext(ctx, "LLM briefing failed, posting without briefing",
					"group_key", sg.Key,
					"members", len(members),
					"error", err,
				)
			} else {
				briefing = result
				p.logger.InfoContext(ctx, "group briefed",
					"group_key", sg.Key,
					"members", len(members),
				)
			}
			summary.Comparisons++
		}

		grouped = append(grouped, GroupedResult{
			Group:    sg,
			Briefing: briefing,
			Results:  members,
		})
		summary.Grouped++
	}

	return grouped
}

// PostGrouped delivers Discord briefings for grouped storm results.
// Singleton groups use the existing per-region posting path.
// Multi-member groups use the grouped posting path with comparison.
// Returns the number of individual results posted and any error.
func (p *Pipeline) PostGrouped(ctx context.Context, groups []GroupedResult) (int, error) {
	if p.dryRun || p.poster == nil {
		total := 0
		for _, g := range groups {
			for _, r := range g.Results {
				p.logger.InfoContext(ctx, "dry-run: skip discord post",
					"region_id", r.Region.ID,
					"storm_id", r.Storm.ID,
					"change_class", string(r.ChangeClass),
					"group_key", g.Group.Key,
				)
			}
			total += len(g.Results)
		}
		return total, nil
	}

	posted := 0
	for _, g := range groups {
		if len(g.Results) == 1 {
			// Singleton group: use existing per-region posting.
			if err := p.postOne(ctx, g.Results[0]); err != nil {
				p.logger.WarnContext(ctx, "discord post failed, marking undelivered",
					"region_id", g.Results[0].Region.ID,
					"storm_id", g.Results[0].Storm.ID,
					"eval_id", g.Results[0].Evaluation.ID,
					"error", err,
				)
				if markErr := p.store.MarkEvaluationDelivered(ctx, g.Results[0].Evaluation.ID, false); markErr != nil {
					p.logger.WarnContext(ctx, "mark evaluation undelivered failed",
						"eval_id", g.Results[0].Evaluation.ID,
						"error", markErr,
					)
				}
			}
			posted++
			continue
		}

		// Multi-member group: build and post a grouped Discord message.
		evals := make([]discord.EvalWithRegion, len(g.Results))
		for i, r := range g.Results {
			evals[i] = discord.EvalWithRegion{
				Evaluation: r.Evaluation,
				Region:     r.Region,
			}
		}

		gp := discord.GroupedPost{
			MacroRegionName: domain.MacroRegionDisplayNameFromKey(g.Group.Key),
			FrictionTier:    g.Results[0].Region.FrictionTier,
			Briefing:        g.Briefing,
			Evaluations:     evals,
		}

		threadID, err := p.poster.PostGrouped(ctx, gp)
		if err != nil {
			p.logger.WarnContext(ctx, "grouped discord post failed",
				"group_key", g.Group.Key,
				"members", len(g.Results),
				"error", err,
			)
			for _, r := range g.Results {
				if markErr := p.store.MarkEvaluationDelivered(ctx, r.Evaluation.ID, false); markErr != nil {
					p.logger.WarnContext(ctx, "mark evaluation undelivered failed",
						"eval_id", r.Evaluation.ID,
						"error", markErr,
					)
				}
			}
			posted += len(g.Results)
			continue
		}

		// Post full detail for each region as follow-up messages in the thread.
		for _, r := range g.Results {
			if err := p.poster.PostUpdate(ctx, r.Evaluation, r.Region, threadID); err != nil {
				p.logger.WarnContext(ctx, "grouped detail post failed",
					"region_id", r.Region.ID,
					"storm_id", r.Storm.ID,
					"thread_id", threadID,
					"error", err,
				)
			}
		}

		// Update all storms in the group with the shared thread ID.
		for _, r := range g.Results {
			updatedStorm := r.Storm
			updatedStorm.State = domain.StormBriefed
			updatedStorm.DiscordThreadID = threadID
			updatedStorm.LastPostedAt = time.Now().UTC()
			if updateErr := p.store.UpdateStorm(ctx, updatedStorm); updateErr != nil {
				p.logger.WarnContext(ctx, "update storm to briefed failed after grouped post",
					"storm_id", r.Storm.ID,
					"thread_id", threadID,
					"error", updateErr,
				)
			}

			if markErr := p.store.MarkEvaluationDelivered(ctx, r.Evaluation.ID, true); markErr != nil {
				p.logger.WarnContext(ctx, "mark evaluation delivered failed",
					"eval_id", r.Evaluation.ID,
					"error", markErr,
				)
			}
		}

		p.logger.InfoContext(ctx, "group posted",
			"group_key", g.Group.Key,
			"members", len(g.Results),
			"thread_id", threadID,
		)
		posted += len(g.Results)
	}

	return posted, nil
}

// Post delivers Discord briefings for each CompareResult. For new storms it opens a
// forum thread; for updates it posts into the existing thread. Failures from Discord
// are logged and the evaluation is marked undelivered so operators can diagnose
// without blocking other regions. Returns nil even when individual posts fail —
// the caller inspects logs or the DB delivered flag to detect partial failures.
func (p *Pipeline) Post(ctx context.Context, results []CompareResult) error {
	if p.dryRun || p.poster == nil {
		for _, r := range results {
			p.logger.InfoContext(ctx, "dry-run: skip discord post",
				"region_id", r.Region.ID,
				"storm_id", r.Storm.ID,
				"change_class", string(r.ChangeClass),
			)
		}
		return nil
	}

	for _, r := range results {
		if err := p.postOne(ctx, r); err != nil {
			p.logger.WarnContext(ctx, "discord post failed, marking undelivered",
				"region_id", r.Region.ID,
				"storm_id", r.Storm.ID,
				"eval_id", r.Evaluation.ID,
				"change_class", string(r.ChangeClass),
				"error", err,
			)
			if markErr := p.store.MarkEvaluationDelivered(ctx, r.Evaluation.ID, false); markErr != nil {
				p.logger.WarnContext(ctx, "mark evaluation undelivered failed",
					"eval_id", r.Evaluation.ID,
					"error", markErr,
				)
			}
		}
	}
	return nil
}

// postOne handles the Discord delivery for a single CompareResult, updating storm
// state and marking the evaluation delivered on success.
func (p *Pipeline) postOne(ctx context.Context, r CompareResult) error {
	switch r.ChangeClass {
	case domain.ChangeNew:
		return p.postNew(ctx, r)
	case domain.ChangeMaterial, domain.ChangeMinor, domain.ChangeDowngrade:
		return p.postUpdate(ctx, r)
	default:
		// Unknown change classes are not posted; log for observability.
		p.logger.InfoContext(ctx, "skip post for unknown change class",
			"region_id", r.Region.ID,
			"change_class", string(r.ChangeClass),
		)
		return nil
	}
}

func (p *Pipeline) postNew(ctx context.Context, r CompareResult) error {
	threadID, err := p.poster.PostNew(ctx, r.Evaluation, r.Region)
	if err != nil {
		return err
	}

	updatedStorm := r.Storm
	updatedStorm.State = domain.StormBriefed
	updatedStorm.DiscordThreadID = threadID
	updatedStorm.LastPostedAt = time.Now().UTC()
	if err := p.store.UpdateStorm(ctx, updatedStorm); err != nil {
		// Storm state update failure is logged but doesn't invalidate the post.
		p.logger.WarnContext(ctx, "update storm to briefed failed after discord post",
			"storm_id", r.Storm.ID,
			"thread_id", threadID,
			"error", err,
		)
	}

	if err := p.store.MarkEvaluationDelivered(ctx, r.Evaluation.ID, true); err != nil {
		p.logger.WarnContext(ctx, "mark evaluation delivered failed",
			"eval_id", r.Evaluation.ID,
			"error", err,
		)
	}

	p.logger.InfoContext(ctx, "storm briefed",
		"region_id", r.Region.ID,
		"storm_id", r.Storm.ID,
		"eval_id", r.Evaluation.ID,
		"thread_id", threadID,
	)
	return nil
}

func (p *Pipeline) postUpdate(ctx context.Context, r CompareResult) error {
	if r.Storm.DiscordThreadID == "" {
		return fmt.Errorf("storm %d has no discord_thread_id for update post", r.Storm.ID)
	}

	if err := p.poster.PostUpdate(ctx, r.Evaluation, r.Region, r.Storm.DiscordThreadID); err != nil {
		return err
	}

	updatedStorm := r.Storm
	updatedStorm.State = domain.StormUpdated
	updatedStorm.LastPostedAt = time.Now().UTC()
	if err := p.store.UpdateStorm(ctx, updatedStorm); err != nil {
		p.logger.WarnContext(ctx, "update storm to updated failed after discord post",
			"storm_id", r.Storm.ID,
			"error", err,
		)
	}

	if err := p.store.MarkEvaluationDelivered(ctx, r.Evaluation.ID, true); err != nil {
		p.logger.WarnContext(ctx, "mark evaluation delivered failed",
			"eval_id", r.Evaluation.ID,
			"error", err,
		)
	}

	p.logger.InfoContext(ctx, "storm updated",
		"region_id", r.Region.ID,
		"storm_id", r.Storm.ID,
		"eval_id", r.Evaluation.ID,
		"thread_id", r.Storm.DiscordThreadID,
		"change_class", string(r.ChangeClass),
	)
	return nil
}
