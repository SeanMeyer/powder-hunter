package discord

import (
	"fmt"
	"strings"
	"time"

	"github.com/seanmeyer/powder-hunter/domain"
	"github.com/seanmeyer/powder-hunter/evaluation"
)

const (
	colorDropEverything = 0xFF0000 // red
	colorWorthALook     = 0xFF8C00 // orange
	colorOnTheRadar     = 0x4169E1 // blue
)

// Embed represents a Discord rich embed ready for JSON serialization.
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
}

// EmbedField is a single named field within a Discord embed.
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

// EmbedFooter is the footer section of a Discord embed.
type EmbedFooter struct {
	Text string `json:"text"`
}

// WebhookPayload is the JSON body sent to Discord's webhook endpoint.
type WebhookPayload struct {
	Content         string           `json:"content,omitempty"`
	Embeds          []Embed          `json:"embeds,omitempty"`
	ThreadName      string           `json:"thread_name,omitempty"`
	AllowedMentions *AllowedMentions `json:"allowed_mentions,omitempty"`
}

// AllowedMentions controls which @mention types Discord will resolve.
type AllowedMentions struct {
	Parse []string `json:"parse"`
}

// BriefingPost holds everything needed for the opening storm thread post.
type BriefingPost struct {
	MacroRegionName string
	Briefing        evaluation.BriefingResult
	Evaluations     []EvalWithRegion
}

// EvalWithRegion pairs an evaluation with its region for formatting.
type EvalWithRegion struct {
	Evaluation domain.Evaluation
	Region     domain.Region
}

// FormatBriefing creates a webhook payload for the opening storm thread post.
// It produces a single embed with the briefing summary — per-region details are
// posted as follow-up messages via FormatDetail.
func FormatBriefing(bp BriefingPost) WebhookPayload {
	highestTier := highestTier(bp.Evaluations)
	emoji := tierEmoji(highestTier)

	embed := Embed{
		Title:       fmt.Sprintf("%s %s", emoji, bp.MacroRegionName),
		Description: bp.Briefing.Briefing,
		Color:       tierColor(highestTier),
	}

	if bp.Briefing.BestDay != "" {
		bestDay, _ := time.Parse("2006-01-02", bp.Briefing.BestDay)
		if !bestDay.IsZero() {
			text := bestDay.Format("Mon Jan 2")
			if bp.Briefing.BestDayReason != "" {
				text += " — " + bp.Briefing.BestDayReason
			}
			embed.Fields = append(embed.Fields, EmbedField{
				Name: "Best Day", Value: text, Inline: true,
			})
		}
	}

	dateRange := formatWindowDatesFromEvals(bp.Evaluations)
	threadName := fmt.Sprintf("%s %s — %s", emoji, bp.MacroRegionName, dateRange)

	payload := WebhookPayload{
		Content:    bp.Briefing.Briefing,
		Embeds:     []Embed{embed},
		ThreadName: threadName,
	}

	if highestTier == domain.TierDropEverything {
		payload.Content = "@here\n" + bp.Briefing.Briefing
		payload.AllowedMentions = &AllowedMentions{Parse: []string{"everyone"}}
	}

	return payload
}

// formatWindowDatesFromEvals extracts window dates from the first evaluation in a slice.
func formatWindowDatesFromEvals(evals []EvalWithRegion) string {
	if len(evals) == 0 {
		return ""
	}
	return formatWindowDates(evals[0].Evaluation)
}

// highestTier returns the most severe tier among the evaluations.
func highestTier(evals []EvalWithRegion) domain.Tier {
	best := domain.TierOnTheRadar
	for _, ew := range evals {
		if tierRank(ew.Evaluation.Tier) > tierRank(best) {
			best = ew.Evaluation.Tier
		}
	}
	return best
}

func tierRank(t domain.Tier) int {
	switch t {
	case domain.TierDropEverything:
		return 3
	case domain.TierWorthALook:
		return 2
	default:
		return 1
	}
}

// FormatDetail creates a detail post for a single region within a thread.
// It uses the full embed with all fields (recommendation, strategy, resort insights, etc.).
func FormatDetail(eval domain.Evaluation, region domain.Region) WebhookPayload {
	embed := buildEmbed(eval, region)
	embed.Title = fmt.Sprintf("%s %s", tierEmoji(eval.Tier), region.Name)
	embed.Footer = &EmbedFooter{Text: fmt.Sprintf("powder-hunter · %s", eval.Tier)}

	return WebhookPayload{
		Content: eval.Summary,
		Embeds:  []Embed{embed},
	}
}

// FormatUpdate creates a webhook payload for a storm update posted to an existing thread.
// An @here ping is sent if the storm has been upgraded to DROP_EVERYTHING.
func FormatUpdate(eval domain.Evaluation, region domain.Region) WebhookPayload {
	embed := buildEmbed(eval, region)
	embed.Title = fmt.Sprintf("%s Update: %s", tierEmoji(eval.Tier), region.Name)

	if eval.ChangeClass != "" && eval.ChangeClass != domain.ChangeNew {
		embed.Fields = append([]EmbedField{changeField(eval)}, embed.Fields...)
	}

	embed.Footer = &EmbedFooter{Text: fmt.Sprintf("powder-hunter · %s · %s", eval.Tier, string(eval.ChangeClass))}

	payload := WebhookPayload{
		Content: eval.Summary,
		Embeds:  []Embed{embed},
	}

	// Ping on upgrade to DROP_EVERYTHING so subscribers don't miss an escalation.
	if eval.Tier == domain.TierDropEverything && eval.ChangeClass == domain.ChangeMaterial {
		payload.Content = "@here\n" + eval.Summary
		payload.AllowedMentions = &AllowedMentions{Parse: []string{"everyone"}}
	}

	return payload
}

// buildEmbed constructs the shared embed body used by both new and update payloads.
func buildEmbed(eval domain.Evaluation, region domain.Region) Embed {
	return Embed{
		Description: buildDescription(eval),
		Color:       tierColor(eval.Tier),
		Fields:      buildFields(eval, region),
		Timestamp:   eval.EvaluatedAt.UTC().Format(time.RFC3339),
	}
}

// buildDescription renders the LLM recommendation plus pro/con key factors.
// Key factors give readers a quick bulleted breakdown without opening the thread.
func buildDescription(eval domain.Evaluation) string {
	var sb strings.Builder

	if eval.Recommendation != "" {
		sb.WriteString(eval.Recommendation)
		sb.WriteString("\n\n")
	}

	if len(eval.KeyFactors.Pros) > 0 {
		sb.WriteString("**Pros**\n")
		for _, p := range eval.KeyFactors.Pros {
			sb.WriteString("+ ")
			sb.WriteString(p)
			sb.WriteString("\n")
		}
	}

	if len(eval.KeyFactors.Cons) > 0 {
		if len(eval.KeyFactors.Pros) > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("**Cons**\n")
		for _, c := range eval.KeyFactors.Cons {
			sb.WriteString("- ")
			sb.WriteString(c)
			sb.WriteString("\n")
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

// buildFields produces the structured inline fields for totals, quality, and logistics.
func buildFields(eval domain.Evaluation, region domain.Region) []EmbedField {
	fields := []EmbedField{}

	if eval.Strategy != "" {
		fields = append(fields, EmbedField{Name: "Strategy", Value: eval.Strategy, Inline: false})
	}

	if len(eval.ResortInsights) > 0 {
		fields = append(fields, EmbedField{Name: "Resort Insights", Value: formatResortInsights(eval.ResortInsights), Inline: false})
	}

	bestDay := bestDayLine(eval)
	if bestDay != "" {
		fields = append(fields, EmbedField{Name: "Best Day", Value: bestDay, Inline: true})
	}

	if eval.SnowQuality != "" {
		fields = append(fields, EmbedField{Name: "Snow Quality", Value: eval.SnowQuality, Inline: true})
	}

	if eval.CrowdEstimate != "" {
		fields = append(fields, EmbedField{Name: "Crowd Estimate", Value: eval.CrowdEstimate, Inline: true})
	}

	if eval.ClosureRisk != "" {
		fields = append(fields, EmbedField{Name: "Closure Risk", Value: eval.ClosureRisk, Inline: true})
	}

	totalSnowfall := totalSnowfallLine(eval)
	if totalSnowfall != "" {
		fields = append(fields, EmbedField{Name: "Total Snowfall", Value: totalSnowfall, Inline: true})
	}

	if eval.LogisticsSummary.Lodging != "" {
		fields = append(fields, EmbedField{Name: "Lodging Info", Value: eval.LogisticsSummary.Lodging, Inline: false})
	}

	if eval.LogisticsSummary.Transportation != "" {
		fields = append(fields, EmbedField{Name: "Getting There", Value: eval.LogisticsSummary.Transportation, Inline: false})
	}

	if eval.LogisticsSummary.LodgingCost != "" && eval.LogisticsSummary.LodgingCost != "N/A" {
		fields = append(fields, EmbedField{Name: "Lodging", Value: eval.LogisticsSummary.LodgingCost, Inline: true})
	}

	if eval.LogisticsSummary.FlightCost != "" && eval.LogisticsSummary.FlightCost != "N/A" {
		fields = append(fields, EmbedField{Name: "Flights", Value: eval.LogisticsSummary.FlightCost, Inline: true})
	}

	if eval.LogisticsSummary.CarRental != "" && eval.LogisticsSummary.CarRental != "N/A" {
		fields = append(fields, EmbedField{Name: "Car Rental", Value: eval.LogisticsSummary.CarRental, Inline: true})
	}

	if eval.LogisticsSummary.TotalEstimatedCost != "" && eval.LogisticsSummary.TotalEstimatedCost != "N/A" {
		fields = append(fields, EmbedField{Name: "Total Est. Cost", Value: eval.LogisticsSummary.TotalEstimatedCost, Inline: false})
	}

	return fields
}

// changeField summarizes what changed since the prior evaluation.
func changeField(eval domain.Evaluation) EmbedField {
	label := changeLabel(eval.ChangeClass)
	return EmbedField{Name: "Change", Value: label, Inline: false}
}

func changeLabel(cc domain.ChangeClass) string {
	switch cc {
	case domain.ChangeMaterial:
		return "Material change — tier or conditions significantly shifted"
	case domain.ChangeMinor:
		return "Minor update — same tier, details refreshed"
	case domain.ChangeDowngrade:
		return "Downgrade — conditions have weakened"
	default:
		return string(cc)
	}
}

// totalSnowfallLine pulls the total snowfall from the first DayEvaluation if available.
// Days with zero or trace snowfall are filtered out to keep the display concise.
func totalSnowfallLine(eval domain.Evaluation) string {
	if len(eval.DayByDay) == 0 {
		return ""
	}
	var parts []string
	for _, d := range eval.DayByDay {
		if d.Snowfall != "" && d.Snowfall != "0\"" && d.Snowfall != "0.0\"" &&
			d.Snowfall != "Trace" && d.Snowfall != "0" && !strings.HasPrefix(d.Snowfall, "0.0") {
			parts = append(parts, fmt.Sprintf("%s: %s", d.Date.Format("Jan 2"), d.Snowfall))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

// bestDayLine renders the LLM's explicitly chosen best ski day.
func bestDayLine(eval domain.Evaluation) string {
	if eval.BestSkiDay.IsZero() {
		return ""
	}
	text := eval.BestSkiDay.Format("Mon Jan 2")
	if eval.BestSkiDayReason != "" {
		text += " — " + eval.BestSkiDayReason
	}
	return text
}

// formatWindowDates formats the storm window as a human-readable date range for the thread name.
func formatWindowDates(eval domain.Evaluation) string {
	if len(eval.DayByDay) == 0 {
		return eval.EvaluatedAt.Format("Jan 2006")
	}
	first := eval.DayByDay[0].Date
	last := eval.DayByDay[len(eval.DayByDay)-1].Date
	if first.Month() == last.Month() {
		return fmt.Sprintf("%s–%s", first.Format("Jan 2"), last.Format("2"))
	}
	return fmt.Sprintf("%s–%s", first.Format("Jan 2"), last.Format("Jan 2"))
}

func formatResortInsights(insights []domain.ResortInsight) string {
	var sb strings.Builder
	for i, ins := range insights {
		fmt.Fprintf(&sb, "%d. **%s** — %s", i+1, ins.Resort, ins.Insight)
		if i < len(insights)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func tierColor(t domain.Tier) int {
	switch t {
	case domain.TierDropEverything:
		return colorDropEverything
	case domain.TierWorthALook:
		return colorWorthALook
	default:
		return colorOnTheRadar
	}
}

func tierEmoji(t domain.Tier) string {
	switch t {
	case domain.TierDropEverything:
		return "🚨"
	case domain.TierWorthALook:
		return "👀"
	default:
		return "📡"
	}
}
