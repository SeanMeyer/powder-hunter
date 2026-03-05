package evaluation

import (
	"strings"
	"testing"
	"time"

	"github.com/seanmeyer/powder-hunter/domain"
)

func TestCToF(t *testing.T) {
	tests := []struct {
		name  string
		input float64
		want  float64
	}{
		{"freezing point", 0, 32},
		{"boiling point", 100, 212},
		{"body temp", 37, 98.6},
		{"cold smoke", -20, -4},
		{"negative Fahrenheit", -40, -40}, // the magic crossover point
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.CToF(tt.input)
			delta := got - tt.want
			if delta < 0 {
				delta = -delta
			}
			if delta > 0.001 {
				t.Errorf("CToF(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatRainLineRisk(t *testing.T) {
	baseElevFt := 8000
	summitElevFt := 11000
	baseM := float64(baseElevFt) * 0.3048    // ~2438m
	summitM := float64(summitElevFt) * 0.3048 // ~3353m

	now := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)

	resort := domain.Resort{
		ID:                "test_resort",
		Name:              "Test Mountain",
		BaseElevationFt:   baseElevFt,
		SummitElevationFt: summitElevFt,
	}

	tests := []struct {
		name         string
		freezingLvlM float64 // set on HalfDay.FreezingLevelMinM
		precipMM     float64
		wantRisk     bool
	}{
		{
			name:         "freezing level below base → no rain risk at resort",
			freezingLvlM: baseM - 200,
			precipMM:     10,
			wantRisk:     false,
		},
		{
			name:         "freezing level between base and summit → rain-line risk",
			freezingLvlM: baseM + 300,
			precipMM:     10,
			wantRisk:     true,
		},
		{
			name:         "freezing level above summit → rain to all elevations (> summit → no in-resort risk per function)",
			freezingLvlM: summitM + 200,
			precipMM:     10,
			// FormatRainLineRisk only warns when fzMin > baseM && fzMin < summitM
			wantRisk: false,
		},
		{
			name:         "no precipitation → no risk even if freezing level is high",
			freezingLvlM: baseM + 300,
			precipMM:     0,
			wantRisk:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forecast := domain.Forecast{
				RegionID:  "test",
				FetchedAt: now,
				Source:    "open_meteo",
				DailyData: []domain.DailyForecast{
					{
						Date:            now,
						PrecipitationMM: tt.precipMM,
						Day: domain.HalfDay{
							FreezingLevelMinM: tt.freezingLvlM,
							PrecipitationMM:   tt.precipMM,
						},
					},
				},
			}
			result := FormatRainLineRisk([]domain.Forecast{forecast}, []domain.Resort{resort})
			hasRisk := result != ""
			if hasRisk != tt.wantRisk {
				t.Errorf("FormatRainLineRisk risk=%v, want %v (freezing=%.0fm, result=%q)",
					hasRisk, tt.wantRisk, tt.freezingLvlM, result)
			}
		})
	}
}

func TestAFDCoversSnowDays(t *testing.T) {
	now := time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)

	// >=2" of snowfall in inches, converted to cm for the domain type.
	significantSnowCM := 2.0 * 2.54 // exactly 2 inches

	tests := []struct {
		name    string
		afd     *domain.ForecastDiscussion
		snowDay time.Time
		snowCM  float64
		want    bool
	}{
		{
			name:    "snow day within 7-day AFD horizon → true",
			afd:     &domain.ForecastDiscussion{IssuedAt: now},
			snowDay: now.AddDate(0, 0, 5),
			snowCM:  significantSnowCM,
			want:    true,
		},
		{
			name:    "snow day exactly on 7-day boundary → true",
			afd:     &domain.ForecastDiscussion{IssuedAt: now},
			snowDay: now.AddDate(0, 0, 7),
			snowCM:  significantSnowCM,
			want:    true,
		},
		{
			name:    "snow day beyond 7-day horizon → false",
			afd:     &domain.ForecastDiscussion{IssuedAt: now},
			snowDay: now.AddDate(0, 0, 8),
			snowCM:  significantSnowCM,
			want:    false,
		},
		{
			name:    "no significant snow (below 2in threshold) → false",
			afd:     &domain.ForecastDiscussion{IssuedAt: now},
			snowDay: now.AddDate(0, 0, 3),
			snowCM:  0.5 * 2.54, // 0.5 inches — below 2" threshold
			want:    false,
		},
		{
			name:    "exactly 2 inches → true (>= threshold)",
			afd:     &domain.ForecastDiscussion{IssuedAt: now},
			snowDay: now.AddDate(0, 0, 3),
			snowCM:  significantSnowCM,
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forecasts := []domain.Forecast{
				{
					RegionID: "test",
					Source:   "open_meteo",
					DailyData: []domain.DailyForecast{
						{Date: tt.snowDay, SnowfallCM: tt.snowCM},
					},
				},
			}
			got := domain.AFDCoversSnowDays(tt.afd, forecasts)
			if got != tt.want {
				t.Errorf("AFDCoversSnowDays = %v, want %v (snowDay=%v, snowCM=%.2f)",
					got, tt.want, tt.snowDay, tt.snowCM)
			}
		})
	}
}

func TestRenderPrompt(t *testing.T) {
	template := `Region: {{.RegionName}}
Weather: {{.WeatherData}}
Resorts: {{.Resorts}}
Profile: {{.UserProfile}}
Window: {{.StormWindow}}
History: {{.EvaluationHistory}}
Version: {{.PromptVersion}}
Consensus: {{.ModelConsensus}}
Discussion: {{.ForecastDiscussion}}`

	data := PromptData{
		RegionName:         "Colorado Front Range",
		WeatherData:        "36 inches over 3 days",
		Resorts:            "Breckenridge, Keystone",
		UserProfile:        "Expert skier, Ikon pass",
		StormWindow:        "Mar 10 – Mar 13",
		EvaluationHistory:  "Previous tier: WORTH_A_LOOK",
		PromptVersion:      "v2",
		ModelConsensus:     "High confidence across models",
		ForecastDiscussion: "Active pattern continues...",
	}

	t.Run("all placeholders replaced", func(t *testing.T) {
		result := RenderPrompt(template, data)
		if strings.Contains(result, "{{.") {
			t.Errorf("rendered prompt still contains unreplaced placeholders:\n%s", result)
		}
	})

	t.Run("each value appears in output", func(t *testing.T) {
		result := RenderPrompt(template, data)
		checks := map[string]string{
			"RegionName":         data.RegionName,
			"WeatherData":        data.WeatherData,
			"Resorts":            data.Resorts,
			"UserProfile":        data.UserProfile,
			"StormWindow":        data.StormWindow,
			"EvaluationHistory":  data.EvaluationHistory,
			"PromptVersion":      data.PromptVersion,
			"ModelConsensus":     data.ModelConsensus,
			"ForecastDiscussion": data.ForecastDiscussion,
		}
		for field, value := range checks {
			if !strings.Contains(result, value) {
				t.Errorf("rendered prompt missing value for %s: %q not found in output", field, value)
			}
		}
	})

	t.Run("empty data leaves no placeholder residue", func(t *testing.T) {
		emptyData := PromptData{}
		result := RenderPrompt(template, emptyData)
		if strings.Contains(result, "{{.") {
			t.Errorf("rendered prompt with empty data still contains placeholders:\n%s", result)
		}
	})
}
