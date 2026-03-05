package evaluation

import (
	"fmt"
	"strings"
	"time"

	"github.com/seanmeyer/powder-hunter/domain"
)

// PromptData holds the context values substituted into the active prompt template.
type PromptData struct {
	WeatherData        string
	RegionName         string
	Resorts            string
	UserProfile        string
	StormWindow        string
	EvaluationHistory  string
	PromptVersion      string
	ModelConsensus     string
	ForecastDiscussion string
}

// RenderPrompt substitutes context values into the template string.
func RenderPrompt(template string, data PromptData) string {
	r := template
	r = strings.ReplaceAll(r, "{{.WeatherData}}", data.WeatherData)
	r = strings.ReplaceAll(r, "{{.RegionName}}", data.RegionName)
	r = strings.ReplaceAll(r, "{{.Resorts}}", data.Resorts)
	r = strings.ReplaceAll(r, "{{.UserProfile}}", data.UserProfile)
	r = strings.ReplaceAll(r, "{{.StormWindow}}", data.StormWindow)
	r = strings.ReplaceAll(r, "{{.EvaluationHistory}}", data.EvaluationHistory)
	r = strings.ReplaceAll(r, "{{.PromptVersion}}", data.PromptVersion)
	r = strings.ReplaceAll(r, "{{.ModelConsensus}}", data.ModelConsensus)
	r = strings.ReplaceAll(r, "{{.ForecastDiscussion}}", data.ForecastDiscussion)
	return r
}

// FormatWeatherForPrompt converts forecast data into a human-readable summary
// with day/night breakdown suitable for LLM consumption.
// Day = 6am-6pm local, Night = 6pm-6am local (snow that falls at night is what you ski next morning).
func FormatWeatherForPrompt(forecasts []domain.Forecast) string {
	var b strings.Builder
	for i, f := range forecasts {
		if i > 0 {
			b.WriteString("\n")
		}
		label := f.Source
		switch f.Source {
		case "open_meteo":
			label = "Open-Meteo 16-day forecast"
			if f.Model != "" {
				label += " [" + f.Model + "]"
			}
		case "nws":
			label = "NWS gridpoint forecast"
		}
		fmt.Fprintf(&b, "### %s\nFetched: %s\n\n", label, f.FetchedAt.Format("2006-01-02 15:04 MST"))

		hasHalfDay := false
		for _, d := range f.DailyData {
			if d.Day.SnowfallCM > 0 || d.Night.SnowfallCM > 0 || d.Day.WindGustKmh > 0 {
				hasHalfDay = true
				break
			}
		}

		if hasHalfDay {
			fmt.Fprintf(&b, "%-12s %-8s %8s %8s %8s %10s %10s\n",
				"Date", "Period", "Snow(in)", "Temp(°F)", "Precip\"", "Wind(mph)", "Gust(mph)")
			fmt.Fprintf(&b, "%-12s %-8s %8s %8s %8s %10s %10s\n",
				"----------", "------", "--------", "--------", "------", "---------", "---------")
			for _, d := range f.DailyData {
				daySnow := domain.CMToInches(d.Day.SnowfallCM)
				nightSnow := domain.CMToInches(d.Night.SnowfallCM)
				totalSnow := domain.CMToInches(d.SnowfallCM)
				dayTempF := cToF(d.Day.TemperatureC)
				nightTempF := cToF(d.Night.TemperatureC)
				dayPrecip := d.Day.PrecipitationMM / 25.4
				nightPrecip := d.Night.PrecipitationMM / 25.4
				dayWind := d.Day.WindSpeedKmh * 0.621371
				dayGust := d.Day.WindGustKmh * 0.621371
				nightWind := d.Night.WindSpeedKmh * 0.621371
				nightGust := d.Night.WindGustKmh * 0.621371

				marker := ""
				if totalSnow >= 4.0 {
					marker = " ← notable"
				}

				fmt.Fprintf(&b, "%-12s %-8s %7.1f\" %7.0f° %7.1f\" %9.0f %9.0f%s\n",
					d.Date.Format("Jan 02 Mon"), "Day", daySnow, dayTempF, dayPrecip, dayWind, dayGust, "")
				fmt.Fprintf(&b, "%-12s %-8s %7.1f\" %7.0f° %7.1f\" %9.0f %9.0f%s\n",
					"", "Night", nightSnow, nightTempF, nightPrecip, nightWind, nightGust, marker)

				// SLR context line when there's notable snow or precipitation concerns.
				if totalSnow > 0 || d.RainHours > 0 || d.MixedHours > 0 {
					var notes []string
					if d.SLRatio > 0 {
						notes = append(notes, fmt.Sprintf("SLR %.0f:1", d.SLRatio))
					}
					if d.RainHours > 0 {
						notes = append(notes, fmt.Sprintf("%dh rain", d.RainHours))
					}
					if d.MixedHours > 0 {
						notes = append(notes, fmt.Sprintf("%dh mixed", d.MixedHours))
					}
					if d.FreezingLevelM > 0 {
						fzLvlFt := d.FreezingLevelM * 3.28084
						notes = append(notes, fmt.Sprintf("freezing lvl ~%.0f'", fzLvlFt))
					}
					if len(notes) > 0 {
						fmt.Fprintf(&b, "%-12s          [%s]\n", "", strings.Join(notes, ", "))
					}
				}
			}
		} else {
			// Fallback for sources without half-day data.
			fmt.Fprintf(&b, "%-12s %8s %8s %8s %10s\n", "Date", "Snow(in)", "Low(°F)", "High(°F)", "Precip(in)")
			fmt.Fprintf(&b, "%-12s %8s %8s %8s %10s\n", "----------", "--------", "-------", "--------", "---------")
			for _, d := range f.DailyData {
				snowIn := domain.CMToInches(d.SnowfallCM)
				minF := cToF(d.TemperatureMinC)
				maxF := cToF(d.TemperatureMaxC)
				precipIn := d.PrecipitationMM / 25.4
				marker := ""
				if snowIn >= 4.0 {
					marker = " ← notable"
				}
				fmt.Fprintf(&b, "%-12s %7.1f\" %7.0f° %7.0f° %8.1f\"%s\n",
					d.Date.Format("Jan 02 Mon"), snowIn, minF, maxF, precipIn, marker)
			}
		}
	}
	return b.String()
}

func cToF(c float64) float64 {
	return c*9.0/5.0 + 32.0
}

// FormatConsolidatedWeatherForPrompt produces a compact per-resort weather summary
// for the LLM. For each resort, it averages across models to show consensus values
// with a confidence indicator. Only notable snow days get SLR/freezing level annotations.
// Model disagreement is called out only for low-confidence days.
func FormatConsolidatedWeatherForPrompt(forecasts []domain.Forecast, resorts []domain.Resort) string {
	// Group forecasts by resort.
	byResort := make(map[string][]domain.Forecast)
	var noResort []domain.Forecast
	for _, f := range forecasts {
		if f.ResortID != "" {
			byResort[f.ResortID] = append(byResort[f.ResortID], f)
		} else {
			noResort = append(noResort, f)
		}
	}

	// If no resort-tagged forecasts, fall back to the original formatter.
	if len(byResort) == 0 {
		return FormatWeatherForPrompt(noResort)
	}

	var b strings.Builder
	for _, resort := range resorts {
		resortForecasts, ok := byResort[resort.ID]
		if !ok || len(resortForecasts) == 0 {
			continue
		}

		fmt.Fprintf(&b, "### %s (%d' base / %d' summit)\n", resort.Name, resort.BaseElevationFt, resort.SummitElevationFt)

		// Separate by source for display.
		var omForecasts []domain.Forecast
		var nwsForecasts []domain.Forecast
		for _, f := range resortForecasts {
			if f.Source == "open_meteo" {
				omForecasts = append(omForecasts, f)
			} else {
				nwsForecasts = append(nwsForecasts, f)
			}
		}

		// Compute consensus across Open-Meteo models for this resort.
		consensus := domain.ComputeConsensus(omForecasts)

		// Build a merged daily table from consensus + NWS.
		fmt.Fprintf(&b, "%-12s %-8s %8s %8s %8s %10s %10s\n",
			"Date", "Period", "Snow(in)", "Temp(°F)", "Precip\"", "Wind(mph)", "Confidence")
		fmt.Fprintf(&b, "%-12s %-8s %8s %8s %8s %10s %10s\n",
			"----------", "------", "--------", "--------", "------", "---------", "----------")

		// Use the first OM forecast for temperature/precip/wind (these are similar across models).
		// Snowfall comes from consensus mean.
		var templateForecast domain.Forecast
		if len(omForecasts) > 0 {
			templateForecast = omForecasts[0]
		} else if len(nwsForecasts) > 0 {
			templateForecast = nwsForecasts[0]
		}

		consensusByDate := make(map[string]domain.DayConsensus)
		for _, dc := range consensus.DailyConsensus {
			consensusByDate[dc.Date.Format("2006-01-02")] = dc
		}

		for _, d := range templateForecast.DailyData {
			dateKey := d.Date.Format("2006-01-02")
			dc := consensusByDate[dateKey]

			// Use consensus mean for snowfall if available, otherwise template value.
			totalSnowCM := d.SnowfallCM
			if dc.SnowfallMeanCM > 0 {
				totalSnowCM = dc.SnowfallMeanCM
			}
			totalSnow := domain.CMToInches(totalSnowCM)

			daySnow := domain.CMToInches(d.Day.SnowfallCM)
			nightSnow := domain.CMToInches(d.Night.SnowfallCM)
			dayTempF := cToF(d.Day.TemperatureC)
			nightTempF := cToF(d.Night.TemperatureC)
			dayPrecip := d.Day.PrecipitationMM / 25.4
			nightPrecip := d.Night.PrecipitationMM / 25.4
			dayWind := d.Day.WindGustKmh * 0.621371
			nightWind := d.Night.WindGustKmh * 0.621371

			conf := dc.Confidence
			if conf == "" {
				conf = "—"
			}

			marker := ""
			if totalSnow >= 4.0 {
				marker = " ← notable"
			}

			fmt.Fprintf(&b, "%-12s %-8s %7.1f\" %7.0f° %7.1f\" %9.0f %10s%s\n",
				d.Date.Format("Jan 02 Mon"), "Day", daySnow, dayTempF, dayPrecip, dayWind, "", "")
			fmt.Fprintf(&b, "%-12s %-8s %7.1f\" %7.0f° %7.1f\" %9.0f %10s%s\n",
				"", "Night", nightSnow, nightTempF, nightPrecip, nightWind, conf, marker)

			// Annotations on notable snow days only.
			if totalSnow > 1.0 || d.RainHours > 0 {
				var notes []string
				if d.SLRatio > 0 {
					notes = append(notes, fmt.Sprintf("SLR %.0f:1", d.SLRatio))
				}
				if d.RainHours > 0 {
					notes = append(notes, fmt.Sprintf("%dh rain", d.RainHours))
				}
				if d.MixedHours > 0 {
					notes = append(notes, fmt.Sprintf("%dh mixed", d.MixedHours))
				}
				if d.FreezingLevelM > 0 {
					fzLvlFt := d.FreezingLevelM * 3.28084
					notes = append(notes, fmt.Sprintf("freezing lvl ~%.0f'", fzLvlFt))
				}
				if dc.Confidence == "low" {
					notes = append(notes, fmt.Sprintf("models disagree: %.1f\"–%.1f\"",
						domain.CMToInches(dc.SnowfallMinCM), domain.CMToInches(dc.SnowfallMaxCM)))
				}
				if len(notes) > 0 {
					fmt.Fprintf(&b, "%-12s          [%s]\n", "", strings.Join(notes, ", "))
				}
			}
		}

		// If NWS data exists, add a brief comparison line.
		if len(nwsForecasts) > 0 {
			nws := nwsForecasts[0]
			var nwsTotal float64
			for _, d := range nws.DailyData {
				nwsTotal += d.SnowfallCM
			}
			if nwsTotal > 0 {
				fmt.Fprintf(&b, "\nNWS gridpoint comparison: %.1f\" total over %d days\n",
					domain.CMToInches(nwsTotal), len(nws.DailyData))
			}
		}

		b.WriteString("\n")
	}
	return b.String()
}

// FormatResortsForPrompt converts resort data into a human-readable summary.
func FormatResortsForPrompt(resorts []domain.Resort) string {
	var b strings.Builder
	for i, r := range resorts {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "### %s\n", r.Name)
		fmt.Fprintf(&b, "- Elevation: %d' base / %d' summit (%d' vertical)\n", r.BaseElevationFt, r.SummitElevationFt, r.VerticalDropFt)
		fmt.Fprintf(&b, "- Skiable acres: %d, Lifts: %d\n", r.SkiableAcres, r.LiftCount)
		if len(r.PassAffiliations) > 0 {
			fmt.Fprintf(&b, "- Pass affiliations: %s\n", strings.Join(r.PassAffiliations, ", "))
		}
		for key, val := range r.Metadata {
			label := strings.ReplaceAll(key, "_", " ")
			label = strings.ToUpper(label[:1]) + label[1:]
			fmt.Fprintf(&b, "- %s: %s\n", label, val)
		}
	}
	return b.String()
}

// FormatDetectionForPrompt converts detection results into a human-readable summary
// giving the LLM the detection signal as context for identifying optimal travel dates.
func FormatDetectionForPrompt(detection domain.DetectionResult) string {
	if !detection.Detected || len(detection.Windows) == 0 {
		return "No storm window detected. Evaluate the full forecast period for any emerging opportunities."
	}

	now := time.Now().UTC()
	var b strings.Builder
	for i, w := range detection.Windows {
		if i > 0 {
			b.WriteString("\n")
		}
		rangeLabel := "extended-range (8-16 days out, higher uncertainty)"
		if w.IsNearRange {
			rangeLabel = "near-range (1-7 days out, higher confidence)"
		}
		leadDays := int(w.StartDate.Sub(now).Hours()/24) + 1
		if leadDays < 1 {
			leadDays = 1
		}
		fmt.Fprintf(&b, "- %s to %s (%s, %d days out): %.1f\" total forecasted snowfall in this detection window",
			w.StartDate.Format("Mon Jan 2"), w.EndDate.Format("Mon Jan 2"),
			rangeLabel, leadDays, w.TotalIn)
	}
	return b.String()
}


// FormatProfileForPrompt converts a user profile into a human-readable summary.
func FormatProfileForPrompt(p domain.UserProfile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- Home base: %s (%.4f, %.4f)\n", p.HomeBase, p.HomeLatitude, p.HomeLongitude)
	fmt.Fprintf(&b, "- Passes held: %s\n", strings.Join(p.PassesHeld, ", "))
	if p.SkillLevel != "" {
		fmt.Fprintf(&b, "- Skill level: %s\n", p.SkillLevel)
	}
	if p.Preferences != "" {
		fmt.Fprintf(&b, "- Preferences: %s\n", p.Preferences)
	}
	fmt.Fprintf(&b, "- Remote work capable: %v\n", p.RemoteWorkCapable)
	fmt.Fprintf(&b, "- Typical PTO days per year: %d\n", p.TypicalPTODays)
	if len(p.BlackoutDates) > 0 {
		dates := make([]string, len(p.BlackoutDates))
		for i, d := range p.BlackoutDates {
			dates[i] = fmt.Sprintf("%s to %s", d.Start.Format("Jan 2"), d.End.Format("Jan 2"))
		}
		fmt.Fprintf(&b, "- Blackout dates: %s\n", strings.Join(dates, "; "))
	}
	return b.String()
}

// FormatRainLineRisk checks whether the freezing level threatens rain at resort
// base elevations during precipitation. Returns a warning string for the LLM
// or empty string if no risk detected.
func FormatRainLineRisk(forecasts []domain.Forecast, resorts []domain.Resort) string {
	if len(resorts) == 0 {
		return ""
	}

	// Find the lowest base elevation across all resorts.
	var lowestBaseFt int
	var highestSummitFt int
	for i, r := range resorts {
		if i == 0 || r.BaseElevationFt < lowestBaseFt {
			lowestBaseFt = r.BaseElevationFt
		}
		if i == 0 || r.SummitElevationFt > highestSummitFt {
			highestSummitFt = r.SummitElevationFt
		}
	}

	baseM := float64(lowestBaseFt) * 0.3048
	summitM := float64(highestSummitFt) * 0.3048

	// Check each forecast day for rain-line risk.
	type riskDay struct {
		date     string
		fzLvlFt int
	}
	var risks []riskDay

	for _, f := range forecasts {
		for _, d := range f.DailyData {
			if d.PrecipitationMM <= 0 {
				continue
			}
			// Check both half-day periods.
			for _, hd := range []domain.HalfDay{d.Day, d.Night} {
				fzMin := hd.FreezingLevelMinM
				if fzMin <= 0 {
					fzMin = d.FreezingLevelM
				}
				if fzMin <= 0 {
					continue
				}
				// Rain-line risk: freezing level above base but below summit.
				if fzMin > baseM && fzMin < summitM {
					risks = append(risks, riskDay{
						date:    d.Date.Format("Jan 02"),
						fzLvlFt: int(fzMin * 3.28084),
					})
					break // one risk per day is enough
				}
			}
		}
	}

	if len(risks) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**Rain-line warning**: Freezing level may be above base elevation (%d') on some days:\n", lowestBaseFt)
	for _, r := range risks {
		fmt.Fprintf(&b, "- %s: freezing level ~%d' (rain possible below this elevation)\n", r.date, r.fzLvlFt)
	}
	return b.String()
}

// FormatResortConsensusForPrompt renders per-resort consensus data for the LLM prompt.
func FormatResortConsensusForPrompt(rc map[string]domain.ModelConsensus, resorts []domain.Resort) string {
	if len(rc) == 0 {
		return "Single model forecast — no multi-model consensus available."
	}

	var b strings.Builder
	for _, resort := range resorts {
		c, ok := rc[resort.ID]
		if !ok || len(c.DailyConsensus) == 0 {
			continue
		}
		fmt.Fprintf(&b, "**%s** (models: %s)\n", resort.Name, strings.Join(c.Models, ", "))
		for _, d := range c.DailyConsensus {
			meanIn := domain.CMToInches(d.SnowfallMeanCM)
			if meanIn < 0.1 {
				continue
			}
			minIn := domain.CMToInches(d.SnowfallMinCM)
			maxIn := domain.CMToInches(d.SnowfallMaxCM)
			fmt.Fprintf(&b, "  %s: %.1f\" mean (%.1f\"–%.1f\", %s confidence)\n",
				d.Date.Format("Jan 02 Mon"), meanIn, minIn, maxIn, d.Confidence)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// FormatConsensusForPrompt converts model consensus data into a human-readable
// summary showing per-day model agreement and confidence levels.
func FormatConsensusForPrompt(c domain.ModelConsensus) string {
	if len(c.Models) == 0 || len(c.DailyConsensus) == 0 {
		return "Single model forecast — no multi-model consensus available."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Models compared: %s\n\n", strings.Join(c.Models, ", "))
	fmt.Fprintf(&b, "%-12s %10s %10s %10s %10s %10s\n",
		"Date", "Min(in)", "Max(in)", "Mean(in)", "Spread", "Confidence")
	fmt.Fprintf(&b, "%-12s %10s %10s %10s %10s %10s\n",
		"----------", "---------", "---------", "---------", "---------", "----------")

	for _, d := range c.DailyConsensus {
		minIn := domain.CMToInches(d.SnowfallMinCM)
		maxIn := domain.CMToInches(d.SnowfallMaxCM)
		meanIn := domain.CMToInches(d.SnowfallMeanCM)
		fmt.Fprintf(&b, "%-12s %9.1f\" %9.1f\" %9.1f\" %9.2f %10s\n",
			d.Date.Format("Jan 02 Mon"), minIn, maxIn, meanIn, d.SpreadToMean, d.Confidence)
	}
	return b.String()
}

// FormatDiscussionForPrompt converts a NWS forecast discussion into prompt context.
// When detection windows are provided, it checks whether the AFD's coverage
// (typically ~7 days from issuance) overlaps with the storm window dates and
// adds a caveat note if it doesn't.
func FormatDiscussionForPrompt(d *domain.ForecastDiscussion, detection domain.DetectionResult) string {
	if d == nil || d.Text == "" {
		return "No NWS forecast discussion available for this region."
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**WFO: %s** (issued %s)\n\n", d.WFO, d.IssuedAt.Format("2006-01-02 15:04 MST"))

	if caveat := afdRelevanceCaveat(d, detection); caveat != "" {
		fmt.Fprintf(&b, "NOTE: %s\n\n", caveat)
	}

	b.WriteString(d.Text)
	return b.String()
}

// afdRelevanceCaveat checks whether the AFD likely covers the detected storm
// window. NWS AFDs typically discuss weather for the next 5-7 days from
// issuance. If the earliest storm window starts beyond that horizon, the AFD
// may not contain relevant information about the storm.
func afdRelevanceCaveat(d *domain.ForecastDiscussion, detection domain.DetectionResult) string {
	if !detection.Detected || len(detection.Windows) == 0 {
		return ""
	}

	const afdHorizonDays = 7

	earliest := detection.Windows[0].StartDate
	for _, w := range detection.Windows[1:] {
		if w.StartDate.Before(earliest) {
			earliest = w.StartDate
		}
	}

	afdCoverage := d.IssuedAt.AddDate(0, 0, afdHorizonDays)
	if earliest.After(afdCoverage) {
		daysOut := int(earliest.Sub(d.IssuedAt).Hours()/24) + 1
		return fmt.Sprintf("This AFD was issued %d days before the storm window starts. "+
			"AFDs typically cover ~7 days, so this discussion may not address the detected storm. "+
			"Weight numerical forecast data more heavily than this discussion for timing beyond day 7.",
			daysOut)
	}

	return ""
}
