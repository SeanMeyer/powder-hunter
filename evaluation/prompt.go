package evaluation

import "strings"

// PromptData holds the context values substituted into the active prompt template.
type PromptData struct {
	WeatherData       string // JSON-serialized forecast data
	RegionName        string
	Resorts           string // JSON-serialized resort metadata
	UserProfile       string // JSON-serialized profile
	EvaluationHistory string // JSON-serialized prior evaluations, or "No prior evaluations"
	PromptVersion     string
}

// RenderPrompt substitutes context values into the template string.
// Uses simple string replacement rather than text/template — the placeholders
// are controlled constants, not user input, so the overhead of a full template
// engine isn't justified.
func RenderPrompt(template string, data PromptData) string {
	r := template
	r = strings.ReplaceAll(r, "{{.WeatherData}}", data.WeatherData)
	r = strings.ReplaceAll(r, "{{.RegionName}}", data.RegionName)
	r = strings.ReplaceAll(r, "{{.Resorts}}", data.Resorts)
	r = strings.ReplaceAll(r, "{{.UserProfile}}", data.UserProfile)
	r = strings.ReplaceAll(r, "{{.EvaluationHistory}}", data.EvaluationHistory)
	r = strings.ReplaceAll(r, "{{.PromptVersion}}", data.PromptVersion)
	return r
}
