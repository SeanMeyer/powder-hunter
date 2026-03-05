package trace

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/seanmeyer/powder-hunter/discord"
	"github.com/seanmeyer/powder-hunter/domain"
)

// FormatWeather renders weather forecast data as a human-readable table with day/night breakdown.
func FormatWeather(w io.Writer, region domain.Region, resorts []domain.Resort, forecasts []domain.Forecast) {
	fmt.Fprintf(w, "═══ WEATHER DATA ═══\n")
	fmt.Fprintf(w, "Region: %s (%s tier)\n", region.Name, region.FrictionTier)

	resortNames := make([]string, len(resorts))
	for i, r := range resorts {
		resortNames[i] = r.Name
	}
	fmt.Fprintf(w, "Resorts: %s\n", strings.Join(resortNames, ", "))

	// Build resort name lookup.
	resortName := make(map[string]string)
	for _, r := range resorts {
		resortName[r.ID] = r.Name
	}

	for _, f := range forecasts {
		label := sourceLabel(f.Source)
		if f.Model != "" {
			label += " [" + f.Model + "]"
		}
		if f.ResortID != "" {
			if name, ok := resortName[f.ResortID]; ok {
				label += " @ " + name
			}
		}
		fmt.Fprintf(w, "\n%s forecast:\n", label)

		hasHalfDay := false
		for _, d := range f.DailyData {
			if d.Day.SnowfallCM > 0 || d.Night.SnowfallCM > 0 || d.Day.WindGustKmh > 0 {
				hasHalfDay = true
				break
			}
		}

		if hasHalfDay {
			for _, d := range f.DailyData {
				totalIn := domain.CMToInches(d.SnowfallCM)
				dayIn := domain.CMToInches(d.Day.SnowfallCM)
				nightIn := domain.CMToInches(d.Night.SnowfallCM)
				daySLR := halfDaySLR(d.Day)
				nightSLR := halfDaySLR(d.Night)
				marker := ""
				if totalIn >= 4.0 {
					marker = "  ← notable"
				}
				fmt.Fprintf(w, "  %s: %4.1f\" total (day: %.1f\"%s / night: %.1f\"%s)    %3.0f°F / %3.0f°F",
					d.Date.Format("Jan 02"), totalIn,
					dayIn, fmtSLRInline(daySLR),
					nightIn, fmtSLRInline(nightSLR),
					cToF(d.TemperatureMinC), cToF(d.TemperatureMaxC))
				if d.Day.WindGustKmh > 0 {
					fmt.Fprintf(w, "    gusts: %.0f mph", d.Day.WindGustKmh*0.621371)
				}
				fmt.Fprintf(w, "%s", marker)

				// Context annotations (rain/mixed hours, freezing level).
				var notes []string
				if d.RainHours > 0 {
					notes = append(notes, fmt.Sprintf("%dh rain", d.RainHours))
				}
				if d.MixedHours > 0 {
					notes = append(notes, fmt.Sprintf("%dh mixed", d.MixedHours))
				}
				if d.FreezingLevelM > 0 {
					notes = append(notes, fmt.Sprintf("fz lvl ~%.0f'", d.FreezingLevelM*3.28084))
				}
				if len(notes) > 0 {
					fmt.Fprintf(w, "  [%s]", strings.Join(notes, ", "))
				}
				fmt.Fprintln(w)
			}
		} else {
			for _, d := range f.DailyData {
				snowIn := domain.CMToInches(d.SnowfallCM)
				marker := ""
				if snowIn >= 4.0 {
					marker = "  ← notable"
				}
				fmt.Fprintf(w, "  %s: %4.1f\"    %3.0f°F / %3.0f°F%s\n",
					d.Date.Format("Jan 02"), snowIn, cToF(d.TemperatureMinC), cToF(d.TemperatureMaxC), marker)
			}
		}
	}
	fmt.Fprintln(w)
}

// halfDaySLR computes the effective SLR for a half-day period from its snow and precip.
// Returns 0 if no snow or no precip (can't determine SLR).
func halfDaySLR(hd domain.HalfDay) float64 {
	if hd.SnowfallCM <= 0 || hd.PrecipitationMM <= 0 {
		return 0
	}
	return hd.SnowfallCM / (hd.PrecipitationMM / 10.0)
}

// fmtSLRInline returns " @Nx:1" suffix for non-zero SLR, empty string otherwise.
func fmtSLRInline(slr float64) string {
	if slr <= 0 {
		return ""
	}
	return fmt.Sprintf(" @%.0f:1", slr)
}

// FormatDetection renders storm detection results.
func FormatDetection(w io.Writer, region domain.Region, detection domain.DetectionResult) {
	if !detection.Detected {
		fmt.Fprintf(w, "Detection: NOT triggered\n")
		fmt.Fprintf(w, "  Near-range threshold:     %.0f\" (%s tier)\n", region.NearThresholdIn, region.FrictionTier)
		fmt.Fprintf(w, "  Extended-range threshold:  %.0f\" (%s tier)\n", region.ExtendedThresholdIn, region.FrictionTier)
		fmt.Fprintln(w)
		return
	}

	for _, win := range detection.Windows {
		rangeLabel := "extended"
		threshold := region.ExtendedThresholdIn
		if win.IsNearRange {
			rangeLabel = "near-range"
			threshold = region.NearThresholdIn
		}
		fmt.Fprintf(w, "Detection: TRIGGERED (%s: %.1f\" over %s–%s, threshold: %.0f\")\n",
			rangeLabel, win.TotalIn,
			win.StartDate.Format("Jan 2"), win.EndDate.Format("Jan 2"),
			threshold)
	}
	fmt.Fprintln(w)
}

// FormatPrompt renders the full rendered prompt that was (or would be) sent to the LLM.
func FormatPrompt(w io.Writer, prompt string) {
	fmt.Fprintf(w, "═══ LLM PROMPT ═══\n")
	fmt.Fprintln(w, prompt)
	fmt.Fprintln(w)
}

// FormatEvaluation renders the LLM evaluation results.
func FormatEvaluation(w io.Writer, eval domain.Evaluation) {
	fmt.Fprintf(w, "═══ LLM EVALUATION ═══\n")
	fmt.Fprintf(w, "Prompt version: %s\n", eval.PromptVersion)
	fmt.Fprintf(w, "\nGemini response:\n")
	fmt.Fprintf(w, "  Tier: %s\n", eval.Tier)

	if eval.Recommendation != "" {
		fmt.Fprintf(w, "  Recommendation: %s\n", eval.Recommendation)
	}
	if eval.Strategy != "" {
		fmt.Fprintf(w, "  Strategy: %s\n", eval.Strategy)
	}
	if eval.SnowQuality != "" {
		fmt.Fprintf(w, "  Snow Quality: %s\n", eval.SnowQuality)
	}
	if eval.CrowdEstimate != "" {
		fmt.Fprintf(w, "  Crowd Estimate: %s\n", eval.CrowdEstimate)
	}
	if eval.ClosureRisk != "" {
		fmt.Fprintf(w, "  Closure Risk: %s\n", eval.ClosureRisk)
	}
	if !eval.BestSkiDay.IsZero() {
		fmt.Fprintf(w, "  Best Ski Day: %s\n", eval.BestSkiDay.Format("Mon Jan 2"))
		if eval.BestSkiDayReason != "" {
			fmt.Fprintf(w, "    Reason: %s\n", eval.BestSkiDayReason)
		}
	}

	if len(eval.KeyFactors.Pros) > 0 || len(eval.KeyFactors.Cons) > 0 {
		fmt.Fprintf(w, "  Key Factors:\n")
		for _, p := range eval.KeyFactors.Pros {
			fmt.Fprintf(w, "    + %s\n", p)
		}
		for _, c := range eval.KeyFactors.Cons {
			fmt.Fprintf(w, "    - %s\n", c)
		}
	}

	if eval.LogisticsSummary.Lodging != "" || eval.LogisticsSummary.Transportation != "" || eval.LogisticsSummary.RoadConditions != "" {
		fmt.Fprintf(w, "  Logistics:\n")
		if eval.LogisticsSummary.Lodging != "" {
			fmt.Fprintf(w, "    Lodging: %s\n", eval.LogisticsSummary.Lodging)
		}
		if eval.LogisticsSummary.Transportation != "" {
			fmt.Fprintf(w, "    Transportation: %s\n", eval.LogisticsSummary.Transportation)
		}
		if eval.LogisticsSummary.RoadConditions != "" {
			fmt.Fprintf(w, "    Road Conditions: %s\n", eval.LogisticsSummary.RoadConditions)
		}
		if eval.LogisticsSummary.FlightCost != "" {
			fmt.Fprintf(w, "    Flight Cost: %s\n", eval.LogisticsSummary.FlightCost)
		}
		if eval.LogisticsSummary.CarRental != "" {
			fmt.Fprintf(w, "    Car Rental: %s\n", eval.LogisticsSummary.CarRental)
		}
	}

	if len(eval.DayByDay) > 0 {
		fmt.Fprintf(w, "  Day-by-Day:\n")
		for _, d := range eval.DayByDay {
			fmt.Fprintf(w, "    %s: %s", d.Date.Format("Jan 02"), d.Snowfall)
			if d.Conditions != "" {
				fmt.Fprintf(w, " — %s", d.Conditions)
			}
			fmt.Fprintln(w)
		}
	}

	if len(eval.GroundingSources) > 0 {
		fmt.Fprintf(w, "  Sources:\n")
		for _, s := range eval.GroundingSources {
			fmt.Fprintf(w, "    %s\n", s)
		}
	}
	fmt.Fprintln(w)
}

// FormatComparison renders change detection results.
func FormatComparison(w io.Writer, changeClass domain.ChangeClass, prevTier domain.Tier) {
	fmt.Fprintf(w, "═══ CHANGE DETECTION ═══\n")
	if changeClass == domain.ChangeNew {
		fmt.Fprintf(w, "Prior evaluation: none (new storm)\n")
		fmt.Fprintf(w, "Change class: NEW\n")
	} else {
		fmt.Fprintf(w, "Prior tier: %s\n", prevTier)
		fmt.Fprintf(w, "Change class: %s\n", strings.ToUpper(string(changeClass)))
	}
	fmt.Fprintln(w)
}

// FormatDiscordPreview renders what the Discord post would look like.
func FormatDiscordPreview(w io.Writer, eval domain.Evaluation, region domain.Region) {
	fmt.Fprintf(w, "═══ DISCORD OUTPUT (dry-run) ═══\n")

	payload := discord.FormatNewStorm(eval, region)
	fmt.Fprintf(w, "Thread name: %q\n", payload.ThreadName)

	hasPing := payload.Content == "@here"
	fmt.Fprintf(w, "@here: %v (%s)\n", hasPing, eval.Tier)

	if len(payload.Embeds) > 0 {
		embed := payload.Embeds[0]
		fmt.Fprintf(w, "\n[Embed]\n")
		if embed.Title != "" {
			fmt.Fprintf(w, "  Title: %s\n", embed.Title)
		}
		if embed.Description != "" {
			lines := strings.Split(embed.Description, "\n")
			fmt.Fprintf(w, "  Description:\n")
			for _, line := range lines {
				fmt.Fprintf(w, "    %s\n", line)
			}
		}
		for _, field := range embed.Fields {
			fmt.Fprintf(w, "  [%s]: %s\n", field.Name, field.Value)
		}
		if embed.Footer != nil {
			fmt.Fprintf(w, "  Footer: %s\n", embed.Footer.Text)
		}
	}
	fmt.Fprintln(w)
}

// FormatWeatherOnly renders a summary header when running in weather-only mode.
func FormatWeatherOnly(w io.Writer) {
	fmt.Fprintf(w, "═══ WEATHER-ONLY MODE ═══\n")
	fmt.Fprintf(w, "Skipping LLM evaluation, change detection, and Discord preview.\n")
	fmt.Fprintf(w, "(Use without --weather-only and set GOOGLE_API_KEY for full trace.)\n\n")
}

// FormatRegionsTable prints all regions in a formatted table.
func FormatRegionsTable(w io.Writer, regions []RegionRow) {
	if len(regions) == 0 {
		fmt.Fprintln(w, "No regions found.")
		return
	}

	idW, nameW, tierW := 26, 34, 22
	fmt.Fprintf(w, "%-*s %-*s %-*s %s\n", idW, "ID", nameW, "Name", tierW, "Tier", "Resorts")
	fmt.Fprintf(w, "%-*s %-*s %-*s %s\n", idW, strings.Repeat("─", idW-1), nameW, strings.Repeat("─", nameW-1), tierW, strings.Repeat("─", tierW-1), strings.Repeat("─", 7))
	for _, r := range regions {
		fmt.Fprintf(w, "%-*s %-*s %-*s %d\n", idW, r.ID, nameW, r.Name, tierW, r.Tier, r.ResortCount)
	}
}

// RegionRow is a display-ready row for the regions table.
type RegionRow struct {
	ID          string
	Name        string
	Tier        string
	ResortCount int
}

// FormatConsensus renders per-resort model consensus data in the trace output.
func FormatConsensus(w io.Writer, rc map[string]domain.ModelConsensus, resorts []domain.Resort) {
	fmt.Fprintf(w, "═══ MODEL CONSENSUS ═══\n")

	if len(rc) == 0 {
		fmt.Fprintf(w, "Single model — no consensus available\n\n")
		return
	}

	for _, resort := range resorts {
		c, ok := rc[resort.ID]
		if !ok || len(c.DailyConsensus) == 0 {
			continue
		}

		fmt.Fprintf(w, "\n%s (models: %s)\n", resort.Name, strings.Join(c.Models, ", "))

		for _, d := range c.DailyConsensus {
			minIn := domain.CMToInches(d.SnowfallMinCM)
			maxIn := domain.CMToInches(d.SnowfallMaxCM)
			meanIn := domain.CMToInches(d.SnowfallMeanCM)
			if meanIn < 0.1 && minIn < 0.1 {
				continue
			}
			fmt.Fprintf(w, "  %s: %.1f\"–%.1f\" (mean %.1f\", spread %.2f, %s)\n",
				d.Date.Format("Jan 02"), minIn, maxIn, meanIn, d.SpreadToMean, d.Confidence)
		}
	}
	fmt.Fprintln(w)
}

// FormatAFD renders the NWS Area Forecast Discussion in the trace output.
// When detection windows are provided, it notes whether the AFD likely covers
// the storm window dates (AFDs typically discuss the next ~7 days).
func FormatAFD(w io.Writer, d *domain.ForecastDiscussion, detection domain.DetectionResult) {
	fmt.Fprintf(w, "═══ NWS FORECAST DISCUSSION ═══\n")
	if d == nil || d.Text == "" {
		fmt.Fprintf(w, "Not available (non-US region or fetch failed)\n\n")
		return
	}

	fmt.Fprintf(w, "WFO: %s (issued %s)\n", d.WFO, d.IssuedAt.Format("2006-01-02 15:04 MST"))

	if detection.Detected && len(detection.Windows) > 0 {
		earliest := detection.Windows[0].StartDate
		for _, win := range detection.Windows[1:] {
			if win.StartDate.Before(earliest) {
				earliest = win.StartDate
			}
		}
		afdCoverage := d.IssuedAt.AddDate(0, 0, 7)
		if earliest.After(afdCoverage) {
			daysOut := int(earliest.Sub(d.IssuedAt).Hours()/24) + 1
			fmt.Fprintf(w, "⚠ Storm window starts %d days after AFD issuance — may not be covered\n", daysOut)
		} else {
			fmt.Fprintf(w, "✓ AFD covers storm window dates\n")
		}
	}

	fmt.Fprintln(w)
	text := d.Text
	if len(text) > 2000 {
		text = text[:2000] + "\n... [truncated]"
	}
	fmt.Fprintln(w, text)
	fmt.Fprintln(w)
}

func sourceLabel(source string) string {
	switch source {
	case "open_meteo":
		return "Open-Meteo 16-day"
	case "nws":
		return "NWS gridpoint"
	default:
		return source
	}
}

func cToF(c float64) float64 {
	return c*9.0/5.0 + 32.0
}

// FormatTimestamp prints the trace execution timestamp.
func FormatTimestamp(w io.Writer, t time.Time) {
	fmt.Fprintf(w, "Trace run at %s\n\n", t.Format("2006-01-02 15:04:05 MST"))
}
