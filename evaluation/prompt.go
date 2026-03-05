package evaluation

import (
	"fmt"
	"strings"

	"github.com/seanmeyer/powder-hunter/domain"
)

// PromptData holds the context values substituted into the active prompt template.
type PromptData struct {
	WeatherData       string
	RegionName        string
	Resorts           string
	UserProfile       string
	StormWindow       string
	EvaluationHistory string
	PromptVersion     string
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
	return r
}

// FormatWeatherForPrompt converts forecast data into a human-readable summary
// suitable for LLM consumption.
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
		case "nws":
			label = "NWS gridpoint forecast"
		}
		fmt.Fprintf(&b, "### %s\nFetched: %s\n\n", label, f.FetchedAt.Format("2006-01-02 15:04 MST"))
		fmt.Fprintf(&b, "%-12s %8s %8s %8s %10s\n", "Date", "Snow(in)", "Low(°F)", "High(°F)", "Precip(in)")
		fmt.Fprintf(&b, "%-12s %8s %8s %8s %10s\n", "----------", "--------", "-------", "--------", "---------")
		for _, d := range f.DailyData {
			snowIn := domain.CMToInches(d.SnowfallCM)
			minF := d.TemperatureMinC*9.0/5.0 + 32.0
			maxF := d.TemperatureMaxC*9.0/5.0 + 32.0
			precipIn := d.PrecipitationMM / 25.4
			marker := ""
			if snowIn >= 4.0 {
				marker = " ← notable"
			}
			fmt.Fprintf(&b, "%-12s %7.1f\" %7.0f° %7.0f° %8.1f\"%s\n",
				d.Date.Format("Jan 02 Mon"), snowIn, minF, maxF, precipIn, marker)
		}
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

	var b strings.Builder
	for i, w := range detection.Windows {
		if i > 0 {
			b.WriteString("\n")
		}
		rangeLabel := "extended-range (8-16 days out, higher uncertainty)"
		if w.IsNearRange {
			rangeLabel = "near-range (1-7 days out, higher confidence)"
		}
		fmt.Fprintf(&b, "- %s to %s (%s): %.1f\" total forecasted snowfall in this detection window",
			w.StartDate.Format("Mon Jan 2"), w.EndDate.Format("Mon Jan 2"),
			rangeLabel, w.TotalIn)
	}
	return b.String()
}

// FormatProfileForPrompt converts a user profile into a human-readable summary.
func FormatProfileForPrompt(p domain.UserProfile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- Home base: %s (%.4f, %.4f)\n", p.HomeBase, p.HomeLatitude, p.HomeLongitude)
	fmt.Fprintf(&b, "- Passes held: %s\n", strings.Join(p.PassesHeld, ", "))
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
