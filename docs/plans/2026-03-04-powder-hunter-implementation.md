# Powder-ETL Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build an automated ski storm tracker that scans weather forecasts, evaluates storms via Gemini LLM with web search, and pushes rich briefings to Discord.

**Architecture:** I/O Sandwich — pure core domain types and business logic with no side effects; impure shell for weather APIs, Gemini, SQLite, and Discord. Domain-organized packages. All decisions persisted as data for full traceability.

**Tech Stack:** Go, SQLite (`modernc.org/sqlite`), Gemini API (`google.golang.org/genai` with Google Search grounding), Discord webhooks (plain HTTP), NWS + Open-Meteo weather APIs, `log/slog` for structured logging.

**Design doc:** `docs/plans/2026-03-04-powder-hunter-design.md`

**Go skills to follow:** `go-functional-pragmatic`, `go-code-structure`, `go-error-handling`, `go-naming-conventions`, `go-concurrency`, `go-testing-standards`, `go-performance-design`, `go-code-style`

---

## Package Structure

```
powder-hunter/
├── cmd/
│   └── powder-hunter/
│       └── main.go              # Entry point, run() pattern
├── domain/                      # Pure core — no I/O, no context.Context
│   ├── resort.go                # Resort, Region types
│   ├── storm.go                 # Storm, StormState, Evaluation types
│   ├── weather.go               # Forecast, SnowfallWindow types
│   ├── tier.go                  # Tier type, notification rules
│   ├── profile.go               # UserProfile, AlertPreferences
│   └── detection.go             # Pure storm detection logic (threshold filtering)
├── weather/                     # I/O shell — weather API clients
│   ├── openmeteo.go             # Open-Meteo client (16-day, US+Canada)
│   ├── nws.go                   # NWS client (7-day precision, US)
│   └── fetch.go                 # Parallel fetch orchestration
├── evaluation/                  # I/O shell — Gemini LLM evaluation
│   ├── gemini.go                # Gemini client with search grounding
│   └── prompt.go                # Prompt construction from domain types
├── discord/                     # I/O shell — Discord webhook posting
│   ├── webhook.go               # HTTP client for Discord webhooks
│   └── format.go                # Embed formatting from domain types
├── storage/                     # I/O shell — SQLite persistence
│   ├── db.go                    # Database initialization, migrations
│   ├── resort_repo.go           # Resort/Region CRUD
│   ├── storm_repo.go            # Storm lifecycle CRUD
│   ├── profile_repo.go          # User profile CRUD
│   └── run_log_repo.go          # Pipeline run audit log
├── pipeline/                    # Orchestration — the I/O sandwich assembly
│   └── run.go                   # Main pipeline: scan → filter → evaluate → post
├── seed/                        # Reference data
│   └── resorts.go               # Initial resort + region data
├── Dockerfile
├── docker-compose.yml
└── go.mod
```

---

## Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `cmd/powder-hunter/main.go`

**Step 1: Initialize Go module**

Run: `go mod init github.com/SeanMeyer/powder-hunter`
Expected: `go.mod` created

**Step 2: Write main.go with run() pattern**

```go
// cmd/powder-hunter/main.go
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		if err.Error() != "" {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func run() error {
	// TODO: wire up pipeline
	return nil
}
```

**Step 3: Verify it compiles**

Run: `go build ./cmd/powder-hunter/`
Expected: Builds successfully

**Step 4: Commit**

Message: `feat: initialize project scaffolding with run() entry point`

---

## Task 2: Domain Types — Resort and Region

Pure types, no I/O. These are the foundation everything else builds on.

**Files:**
- Create: `domain/resort.go`
- Test: `domain/resort_test.go`

**Step 1: Write tests for Resort and Region types**

Test that parsed types enforce validity: a Resort must have valid coordinates, a Region must have at least one resort and representative coordinates. Test the Pass type as a constrained string.

```go
// domain/resort_test.go
package domain_test

import (
	"testing"

	"github.com/SeanMeyer/powder-hunter/domain"
)

func TestNewResort(t *testing.T) {
	tests := []struct {
		name    string
		input   domain.ResortInput
		wantErr bool
	}{
		{
			name: "valid resort",
			input: domain.ResortInput{
				Name:      "Arapahoe Basin",
				Latitude:  39.6426,
				Longitude: -105.8718,
				Elevation: 10780,
				Pass:      domain.PassIkon,
				RegionID:  "summit-county",
			},
		},
		{
			name: "missing name",
			input: domain.ResortInput{
				Latitude:  39.6426,
				Longitude: -105.8718,
				Elevation: 10780,
				Pass:      domain.PassIkon,
				RegionID:  "summit-county",
			},
			wantErr: true,
		},
		{
			name: "invalid latitude",
			input: domain.ResortInput{
				Name:      "Arapahoe Basin",
				Latitude:  91.0,
				Longitude: -105.8718,
				Elevation: 10780,
				Pass:      domain.PassIkon,
				RegionID:  "summit-county",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resort, err := domain.NewResort(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resort.Name != tt.input.Name {
				t.Errorf("got name %q, want %q", resort.Name, tt.input.Name)
			}
		})
	}
}

func TestNewRegion(t *testing.T) {
	tests := []struct {
		name    string
		input   domain.RegionInput
		wantErr bool
	}{
		{
			name: "valid region",
			input: domain.RegionInput{
				ID:                 "summit-county",
				Name:               "Summit County",
				RepresentativeLat:  39.6333,
				RepresentativeLon:  -105.8694,
				NearestAirports:    []string{"DEN"},
				DriveTimeFromHome:  "1h30m",
				LodgingNotes:       "Silverthorne/Dillon have plenty of options",
			},
		},
		{
			name: "missing ID",
			input: domain.RegionInput{
				Name:               "Summit County",
				RepresentativeLat:  39.6333,
				RepresentativeLon:  -105.8694,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			region, err := domain.NewRegion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if region.Name != tt.input.Name {
				t.Errorf("got name %q, want %q", region.Name, tt.input.Name)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./domain/...`
Expected: FAIL — types don't exist yet

**Step 3: Implement Resort and Region types**

```go
// domain/resort.go
package domain

import "fmt"

// Pass represents a ski pass affiliation.
type Pass string

const (
	PassIkon        Pass = "Ikon"
	PassEpic        Pass = "Epic"
	PassIndy        Pass = "Indy"
	PassIndependent Pass = "Independent"
)

// Coordinates represents a validated lat/lon pair.
type Coordinates struct {
	Latitude  float64
	Longitude float64
}

// ResortInput is the raw input for constructing a Resort. Validated once at the boundary.
type ResortInput struct {
	ID        string
	Name      string
	Latitude  float64
	Longitude float64
	Elevation int // feet
	Pass      Pass
	RegionID  string

	// Optional metadata
	VerticalDrop    int
	LiftCount       int
	NearestMetro    string
	MetroDistanceMi int
	ReputationNotes string
}

// Resort is a validated, immutable ski resort. Construct via NewResort.
type Resort struct {
	ID              string
	Name            string
	Coords          Coordinates
	Elevation       int
	Pass            Pass
	RegionID        string
	VerticalDrop    int
	LiftCount       int
	NearestMetro    string
	MetroDistanceMi int
	ReputationNotes string
}

func NewResort(in ResortInput) (Resort, error) {
	if in.Name == "" {
		return Resort{}, fmt.Errorf("resort name is required")
	}
	if in.Latitude < -90 || in.Latitude > 90 {
		return Resort{}, fmt.Errorf("latitude %f out of range [-90, 90]", in.Latitude)
	}
	if in.Longitude < -180 || in.Longitude > 180 {
		return Resort{}, fmt.Errorf("longitude %f out of range [-180, 180]", in.Longitude)
	}
	if in.RegionID == "" {
		return Resort{}, fmt.Errorf("region ID is required")
	}

	return Resort{
		ID:              in.ID,
		Name:            in.Name,
		Coords:          Coordinates{Latitude: in.Latitude, Longitude: in.Longitude},
		Elevation:       in.Elevation,
		Pass:            in.Pass,
		RegionID:        in.RegionID,
		VerticalDrop:    in.VerticalDrop,
		LiftCount:       in.LiftCount,
		NearestMetro:    in.NearestMetro,
		MetroDistanceMi: in.MetroDistanceMi,
		ReputationNotes: in.ReputationNotes,
	}, nil
}

// RegionInput is the raw input for constructing a Region.
type RegionInput struct {
	ID                string
	Name              string
	RepresentativeLat float64
	RepresentativeLon float64
	NearestAirports   []string
	DriveTimeFromHome string
	LodgingNotes      string
}

// Region is a validated, immutable ski area cluster.
type Region struct {
	ID                string
	Name              string
	RepresentativePos Coordinates
	NearestAirports   []string
	DriveTimeFromHome string
	LodgingNotes      string
}

func NewRegion(in RegionInput) (Region, error) {
	if in.ID == "" {
		return Region{}, fmt.Errorf("region ID is required")
	}
	if in.Name == "" {
		return Region{}, fmt.Errorf("region name is required")
	}
	if in.RepresentativeLat < -90 || in.RepresentativeLat > 90 {
		return Region{}, fmt.Errorf("representative latitude %f out of range", in.RepresentativeLat)
	}
	if in.RepresentativeLon < -180 || in.RepresentativeLon > 180 {
		return Region{}, fmt.Errorf("representative longitude %f out of range", in.RepresentativeLon)
	}

	return Region{
		ID:                in.ID,
		Name:              in.Name,
		RepresentativePos: Coordinates{Latitude: in.RepresentativeLat, Longitude: in.RepresentativeLon},
		NearestAirports:   in.NearestAirports,
		DriveTimeFromHome: in.DriveTimeFromHome,
		LodgingNotes:      in.LodgingNotes,
	}, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./domain/...`
Expected: PASS

**Step 5: Commit**

Message: `feat: add Resort and Region domain types with parse-don't-validate`

---

## Task 3: Domain Types — Forecast and Weather

Pure types for weather data. These represent validated forecast data that has already been parsed from API responses.

**Files:**
- Create: `domain/weather.go`
- Test: `domain/weather_test.go`

**Step 1: Write tests for Forecast types**

Test that a Forecast can compute total snowfall over a date range, identify the heaviest snowfall days, and determine snow quality from temperature.

```go
// domain/weather_test.go
package domain_test

import (
	"testing"
	"time"

	"github.com/SeanMeyer/powder-hunter/domain"
)

func TestForecast_TotalSnowfall(t *testing.T) {
	f := domain.Forecast{
		RegionID: "summit-county",
		Source:   domain.SourceOpenMeteo,
		Days: []domain.ForecastDay{
			{Date: date(2026, 3, 8), SnowfallCM: 15, TempHighF: 28, TempLowF: 18},
			{Date: date(2026, 3, 9), SnowfallCM: 25, TempHighF: 25, TempLowF: 15},
			{Date: date(2026, 3, 10), SnowfallCM: 5, TempHighF: 35, TempLowF: 22},
		},
		FetchedAt: time.Now(),
	}

	total := f.TotalSnowfallCM()
	if total != 45 {
		t.Errorf("got total %f, want 45", total)
	}
}

func TestForecast_TotalSnowfallInches(t *testing.T) {
	f := domain.Forecast{
		Days: []domain.ForecastDay{
			{SnowfallCM: 25.4}, // exactly 10 inches
		},
	}

	inches := f.TotalSnowfallInches()
	if inches < 9.99 || inches > 10.01 {
		t.Errorf("got %f inches, want ~10", inches)
	}
}

func TestForecast_PeakSnowfallDay(t *testing.T) {
	days := []domain.ForecastDay{
		{Date: date(2026, 3, 8), SnowfallCM: 15},
		{Date: date(2026, 3, 9), SnowfallCM: 25},
		{Date: date(2026, 3, 10), SnowfallCM: 5},
	}
	f := domain.Forecast{Days: days}

	peak := f.PeakSnowfallDay()
	if peak.Date != date(2026, 3, 9) {
		t.Errorf("got peak day %v, want 2026-03-09", peak.Date)
	}
}

func TestForecastDay_SnowQuality(t *testing.T) {
	cold := domain.ForecastDay{TempHighF: 25, TempLowF: 15}
	if cold.SnowQuality() != domain.QualityDry {
		t.Errorf("cold temps should produce dry snow, got %s", cold.SnowQuality())
	}

	warm := domain.ForecastDay{TempHighF: 38, TempLowF: 30}
	if warm.SnowQuality() != domain.QualityWet {
		t.Errorf("warm temps should produce wet snow, got %s", warm.SnowQuality())
	}
}

func date(year, month, day int) time.Time {
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./domain/...`
Expected: FAIL

**Step 3: Implement Forecast types**

```go
// domain/weather.go
package domain

import "time"

// ForecastSource identifies where forecast data came from.
type ForecastSource string

const (
	SourceOpenMeteo ForecastSource = "open-meteo"
	SourceNWS       ForecastSource = "nws"
)

// SnowQuality represents the expected snow density.
type SnowQuality string

const (
	QualityDry    SnowQuality = "dry"    // Cold, light powder
	QualityMixed  SnowQuality = "mixed"  // Moderate density
	QualityWet    SnowQuality = "wet"    // Warm, heavy "cascade concrete"
)

// ForecastDay holds weather data for a single day.
type ForecastDay struct {
	Date        time.Time
	SnowfallCM  float64 // centimeters of snowfall
	TempHighF   float64 // Fahrenheit
	TempLowF    float64
	WindSpeedMPH float64
	Visibility   string // "good", "moderate", "poor", "whiteout"
}

// SnowQuality estimates snow density from temperature.
// Cold temps (<28F high) produce dry, light powder.
// Warm temps (>34F high) produce wet, heavy snow.
func (d ForecastDay) SnowQuality() SnowQuality {
	switch {
	case d.TempHighF <= 28:
		return QualityDry
	case d.TempHighF >= 34:
		return QualityWet
	default:
		return QualityMixed
	}
}

// SnowfallInches converts cm to inches.
func (d ForecastDay) SnowfallInches() float64 {
	return d.SnowfallCM / 2.54
}

// IsWeekend returns true if the day falls on Saturday or Sunday.
func (d ForecastDay) IsWeekend() bool {
	wd := d.Date.Weekday()
	return wd == time.Saturday || wd == time.Sunday
}

// Forecast is a validated, immutable multi-day forecast for a region.
type Forecast struct {
	RegionID  string
	Source    ForecastSource
	Days      []ForecastDay
	FetchedAt time.Time
}

// TotalSnowfallCM returns total snowfall across all days in centimeters.
func (f Forecast) TotalSnowfallCM() float64 {
	var total float64
	for _, d := range f.Days {
		total += d.SnowfallCM
	}
	return total
}

// TotalSnowfallInches returns total snowfall in inches.
func (f Forecast) TotalSnowfallInches() float64 {
	return f.TotalSnowfallCM() / 2.54
}

// PeakSnowfallDay returns the day with the most snowfall.
func (f Forecast) PeakSnowfallDay() ForecastDay {
	if len(f.Days) == 0 {
		return ForecastDay{}
	}
	peak := f.Days[0]
	for _, d := range f.Days[1:] {
		if d.SnowfallCM > peak.SnowfallCM {
			peak = d
		}
	}
	return peak
}

// NearTermDays returns forecast days within the next 7 days from fetchedAt.
func (f Forecast) NearTermDays() []ForecastDay {
	cutoff := f.FetchedAt.AddDate(0, 0, 7)
	var days []ForecastDay
	for _, d := range f.Days {
		if d.Date.Before(cutoff) {
			days = append(days, d)
		}
	}
	return days
}

// ExtendedDays returns forecast days beyond 7 days from fetchedAt.
func (f Forecast) ExtendedDays() []ForecastDay {
	cutoff := f.FetchedAt.AddDate(0, 0, 7)
	var days []ForecastDay
	for _, d := range f.Days {
		if !d.Date.Before(cutoff) {
			days = append(days, d)
		}
	}
	return days
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./domain/...`
Expected: PASS

**Step 5: Commit**

Message: `feat: add Forecast domain types with snowfall and quality methods`

---

## Task 4: Domain Types — Storm Lifecycle and Evaluation

The central entity. Storms track state over time and carry their full evaluation history.

**Files:**
- Create: `domain/storm.go`
- Create: `domain/tier.go`
- Test: `domain/storm_test.go`

**Step 1: Write tests for Storm and Tier types**

Test storm state transitions, tier ordering, and evaluation history.

```go
// domain/storm_test.go
package domain_test

import (
	"testing"
	"time"

	"github.com/SeanMeyer/powder-hunter/domain"
)

func TestTier_ShouldPing(t *testing.T) {
	if !domain.TierDropEverything.ShouldPing() {
		t.Error("DROP EVERYTHING should ping")
	}
	if domain.TierWorthALook.ShouldPing() {
		t.Error("WORTH A LOOK should not ping")
	}
	if domain.TierOnTheRadar.ShouldPing() {
		t.Error("ON THE RADAR should not ping")
	}
}

func TestTier_Ordering(t *testing.T) {
	if !domain.TierDropEverything.HigherThan(domain.TierWorthALook) {
		t.Error("DROP EVERYTHING should be higher than WORTH A LOOK")
	}
	if !domain.TierWorthALook.HigherThan(domain.TierOnTheRadar) {
		t.Error("WORTH A LOOK should be higher than ON THE RADAR")
	}
	if domain.TierOnTheRadar.HigherThan(domain.TierDropEverything) {
		t.Error("ON THE RADAR should not be higher than DROP EVERYTHING")
	}
}

func TestStormState_ValidTransitions(t *testing.T) {
	tests := []struct {
		from    domain.StormState
		to      domain.StormState
		allowed bool
	}{
		{domain.StateDetected, domain.StateEvaluated, true},
		{domain.StateEvaluated, domain.StateBriefed, true},
		{domain.StateBriefed, domain.StateUpdated, true},
		{domain.StateUpdated, domain.StateUpdated, true},
		{domain.StateDetected, domain.StateBriefed, false},
		{domain.StateExpired, domain.StateDetected, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			if got := tt.from.CanTransitionTo(tt.to); got != tt.allowed {
				t.Errorf("got %v, want %v", got, tt.allowed)
			}
		})
	}
}

func TestStorm_IsUpgrade(t *testing.T) {
	prev := domain.Evaluation{Tier: domain.TierOnTheRadar}
	curr := domain.Evaluation{Tier: domain.TierDropEverything}

	if !curr.IsUpgradeFrom(prev) {
		t.Error("ON THE RADAR -> DROP EVERYTHING should be an upgrade")
	}
	if prev.IsUpgradeFrom(curr) {
		t.Error("DROP EVERYTHING -> ON THE RADAR should not be an upgrade")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./domain/...`
Expected: FAIL

**Step 3: Implement Tier type**

```go
// domain/tier.go
package domain

import "fmt"

// Tier represents the alert urgency level.
type Tier int

const (
	TierOnTheRadar    Tier = 1
	TierWorthALook    Tier = 2
	TierDropEverything Tier = 3
)

func (t Tier) String() string {
	switch t {
	case TierDropEverything:
		return "DROP EVERYTHING"
	case TierWorthALook:
		return "WORTH A LOOK"
	case TierOnTheRadar:
		return "ON THE RADAR"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", t)
	}
}

// ShouldPing returns true if this tier warrants an @here notification.
func (t Tier) ShouldPing() bool {
	return t >= TierDropEverything
}

// HigherThan returns true if this tier is more urgent than other.
func (t Tier) HigherThan(other Tier) bool {
	return t > other
}

// ParseTier converts a string to a Tier. Returns error for unknown values.
func ParseTier(s string) (Tier, error) {
	switch s {
	case "DROP EVERYTHING":
		return TierDropEverything, nil
	case "WORTH A LOOK":
		return TierWorthALook, nil
	case "ON THE RADAR":
		return TierOnTheRadar, nil
	default:
		return 0, fmt.Errorf("unknown tier: %q", s)
	}
}
```

**Step 4: Implement Storm and Evaluation types**

```go
// domain/storm.go
package domain

import (
	"fmt"
	"time"
)

// StormState represents where a storm is in its lifecycle.
type StormState string

const (
	StateDetected  StormState = "detected"
	StateEvaluated StormState = "evaluated"
	StateBriefed   StormState = "briefed"
	StateUpdated   StormState = "updated"
	StateExpired   StormState = "expired"
)

var validTransitions = map[StormState][]StormState{
	StateDetected:  {StateEvaluated, StateExpired},
	StateEvaluated: {StateBriefed, StateExpired},
	StateBriefed:   {StateUpdated, StateExpired},
	StateUpdated:   {StateUpdated, StateExpired},
}

// CanTransitionTo checks if a state transition is valid.
func (s StormState) CanTransitionTo(next StormState) bool {
	for _, allowed := range validTransitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

// StormID uniquely identifies a storm by region and date window.
type StormID struct {
	RegionID  string
	StartDate time.Time
	EndDate   time.Time
}

func (id StormID) String() string {
	return fmt.Sprintf("%s/%s-%s", id.RegionID,
		id.StartDate.Format("2006-01-02"),
		id.EndDate.Format("2006-01-02"))
}

// Storm is a tracked storm entity with its full lifecycle and evaluation history.
type Storm struct {
	ID               StormID
	State            StormState
	Evaluations      []Evaluation
	DiscordThreadID  string
	DetectionSource  ForecastSource
	DetectedAt       time.Time
	LastEvaluatedAt  time.Time
}

// LatestEvaluation returns the most recent evaluation, or zero value if none.
func (s Storm) LatestEvaluation() (Evaluation, bool) {
	if len(s.Evaluations) == 0 {
		return Evaluation{}, false
	}
	return s.Evaluations[len(s.Evaluations)-1], true
}

// CurrentTier returns the tier from the latest evaluation.
func (s Storm) CurrentTier() Tier {
	if eval, ok := s.LatestEvaluation(); ok {
		return eval.Tier
	}
	return TierOnTheRadar
}

// Evaluation represents a single point-in-time assessment of a storm.
// Rich model: carries all inputs, outputs, and reasoning for full traceability.
type Evaluation struct {
	EvaluatedAt time.Time

	// Inputs (what we fed to the LLM)
	WeatherSnapshot Forecast
	RegionContext   string // serialized region + resort metadata
	UserContext     string // serialized user profile

	// Outputs (what the LLM returned)
	Tier             Tier
	Recommendation   string   // plain-language recommendation
	DayByDay         string   // day-by-day breakdown
	KeyFactorsPros   []string
	KeyFactorsCons   []string
	LogisticsSummary string
	Strategy         string   // recommended timing + travel plan
	SnowQuality      string
	CrowdEstimate    string
	ClosureRisk      string

	// Raw response for debugging
	RawLLMResponse string

	// Metadata
	Confidence string // "low", "medium", "high" based on forecast range
}

// IsUpgradeFrom returns true if this evaluation's tier is higher than prior.
func (e Evaluation) IsUpgradeFrom(prior Evaluation) bool {
	return e.Tier.HigherThan(prior.Tier)
}

// IsDowngradeFrom returns true if this evaluation's tier is lower than prior.
func (e Evaluation) IsDowngradeFrom(prior Evaluation) bool {
	return prior.Tier.HigherThan(e.Tier)
}
```

**Step 5: Run tests to verify they pass**

Run: `go test ./domain/...`
Expected: PASS

**Step 6: Commit**

Message: `feat: add Storm lifecycle, Tier, and Evaluation domain types`

---

## Task 5: Domain Types — User Profile

**Files:**
- Create: `domain/profile.go`
- Test: `domain/profile_test.go`

**Step 1: Write tests**

Test profile construction and pass-checking logic.

```go
// domain/profile_test.go
package domain_test

import (
	"testing"

	"github.com/SeanMeyer/powder-hunter/domain"
)

func TestUserProfile_HasPass(t *testing.T) {
	profile := domain.UserProfile{
		Passes: []domain.Pass{domain.PassIkon, domain.PassEpic},
	}

	if !profile.HasPass(domain.PassIkon) {
		t.Error("should have Ikon pass")
	}
	if profile.HasPass(domain.PassIndy) {
		t.Error("should not have Indy pass")
	}
}

func TestUserProfile_ResortCoveredByPass(t *testing.T) {
	profile := domain.UserProfile{
		Passes: []domain.Pass{domain.PassIkon},
	}

	ikonResort := domain.Resort{Pass: domain.PassIkon}
	epicResort := domain.Resort{Pass: domain.PassEpic}

	if !profile.ResortCoveredByPass(ikonResort) {
		t.Error("Ikon resort should be covered")
	}
	if profile.ResortCoveredByPass(epicResort) {
		t.Error("Epic resort should not be covered")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./domain/...`
Expected: FAIL

**Step 3: Implement UserProfile**

```go
// domain/profile.go
package domain

// UserProfile holds user preferences and constraints. Stored in SQLite.
type UserProfile struct {
	HomeBase         Coordinates
	HomeCity         string
	Passes           []Pass
	RemoteWorkCapable bool
	ExcludedRegions  []string

	// Alert preferences
	MinTierForPing Tier
}

// HasPass returns true if the user holds the given pass.
func (p UserProfile) HasPass(pass Pass) bool {
	for _, held := range p.Passes {
		if held == pass {
			return true
		}
	}
	return false
}

// ResortCoveredByPass returns true if the user's passes cover this resort.
func (p UserProfile) ResortCoveredByPass(r Resort) bool {
	return p.HasPass(r.Pass)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./domain/...`
Expected: PASS

**Step 5: Commit**

Message: `feat: add UserProfile domain type with pass coverage logic`

---

## Task 6: Storm Detection — Pure Filtering Logic

Pure function: given forecasts and thresholds, return which regions have flaggable storms. No I/O.

**Files:**
- Create: `domain/detection.go`
- Test: `domain/detection_test.go`

**Step 1: Write tests for storm detection**

```go
// domain/detection_test.go
package domain_test

import (
	"testing"
	"time"

	"github.com/SeanMeyer/powder-hunter/domain"
)

func TestDetectStorms(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	thresholds := domain.DetectionThresholds{
		NearTermSnowfallCM:  20, // ~8 inches
		ExtendedSnowfallCM:  38, // ~15 inches
	}

	forecasts := []domain.Forecast{
		{
			RegionID:  "summit-county",
			Source:    domain.SourceOpenMeteo,
			FetchedAt: now,
			Days: []domain.ForecastDay{
				{Date: now.AddDate(0, 0, 1), SnowfallCM: 15},
				{Date: now.AddDate(0, 0, 2), SnowfallCM: 20},
				{Date: now.AddDate(0, 0, 3), SnowfallCM: 5},
			},
		},
		{
			RegionID:  "wasatch-front",
			Source:    domain.SourceOpenMeteo,
			FetchedAt: now,
			Days: []domain.ForecastDay{
				{Date: now.AddDate(0, 0, 1), SnowfallCM: 3},
			},
		},
		{
			RegionID:  "cascades-stevens",
			Source:    domain.SourceOpenMeteo,
			FetchedAt: now,
			Days: []domain.ForecastDay{
				{Date: now.AddDate(0, 0, 10), SnowfallCM: 25},
				{Date: now.AddDate(0, 0, 11), SnowfallCM: 20},
			},
		},
	}

	results := domain.DetectStorms(forecasts, thresholds, nil)

	if len(results) != 2 {
		t.Fatalf("expected 2 detected storms, got %d", len(results))
	}

	regionIDs := make(map[string]bool)
	for _, r := range results {
		regionIDs[r.RegionID] = true
	}
	if !regionIDs["summit-county"] {
		t.Error("summit-county should be detected (40cm near-term > 20cm threshold)")
	}
	if !regionIDs["cascades-stevens"] {
		t.Error("cascades-stevens should be detected (45cm extended > 38cm threshold)")
	}
	if regionIDs["wasatch-front"] {
		t.Error("wasatch-front should NOT be detected (3cm < threshold)")
	}
}

func TestDetectStorms_AlwaysIncludesTrackedStorms(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	thresholds := domain.DetectionThresholds{
		NearTermSnowfallCM: 20,
		ExtendedSnowfallCM: 38,
	}

	// Low snowfall, but already tracked
	forecasts := []domain.Forecast{
		{
			RegionID:  "summit-county",
			Source:    domain.SourceOpenMeteo,
			FetchedAt: now,
			Days: []domain.ForecastDay{
				{Date: now.AddDate(0, 0, 1), SnowfallCM: 5},
			},
		},
	}

	trackedRegions := map[string]bool{"summit-county": true}
	results := domain.DetectStorms(forecasts, thresholds, trackedRegions)

	if len(results) != 1 {
		t.Fatalf("expected 1 result (tracked storm), got %d", len(results))
	}
	if results[0].Reason != domain.DetectionReasonTracked {
		t.Errorf("expected reason %q, got %q", domain.DetectionReasonTracked, results[0].Reason)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./domain/...`
Expected: FAIL

**Step 3: Implement detection logic**

```go
// domain/detection.go
package domain

import "time"

// DetectionThresholds configures when a forecast triggers evaluation.
type DetectionThresholds struct {
	NearTermSnowfallCM float64 // threshold for 1-7 day forecasts
	ExtendedSnowfallCM float64 // threshold for 8-16 day forecasts (higher, less accurate)
}

// DetectionReason explains why a region was flagged.
type DetectionReason string

const (
	DetectionReasonNearTerm DetectionReason = "near_term_snowfall"
	DetectionReasonExtended DetectionReason = "extended_snowfall"
	DetectionReasonTracked  DetectionReason = "already_tracked"
)

// DetectedStorm represents a region flagged for LLM evaluation, with the reason why.
type DetectedStorm struct {
	RegionID    string
	Forecast    Forecast
	Reason      DetectionReason
	SnowfallCM  float64       // the snowfall amount that triggered detection
	StormWindow [2]time.Time  // estimated start and end dates of snowfall
}

// DetectStorms is pure business logic: given forecasts and thresholds, return flagged regions.
// trackedRegions is a set of region IDs for storms already being tracked (always re-evaluated).
func DetectStorms(forecasts []Forecast, thresholds DetectionThresholds, trackedRegions map[string]bool) []DetectedStorm {
	var detected []DetectedStorm

	for _, f := range forecasts {
		// Always include tracked storms
		if trackedRegions[f.RegionID] {
			window := snowfallWindow(f.Days)
			detected = append(detected, DetectedStorm{
				RegionID:    f.RegionID,
				Forecast:    f,
				Reason:      DetectionReasonTracked,
				SnowfallCM:  f.TotalSnowfallCM(),
				StormWindow: window,
			})
			continue
		}

		// Check near-term days (1-7)
		nearTerm := f.NearTermDays()
		nearTotal := snowfallTotal(nearTerm)
		if nearTotal >= thresholds.NearTermSnowfallCM {
			window := snowfallWindow(nearTerm)
			detected = append(detected, DetectedStorm{
				RegionID:    f.RegionID,
				Forecast:    f,
				Reason:      DetectionReasonNearTerm,
				SnowfallCM:  nearTotal,
				StormWindow: window,
			})
			continue
		}

		// Check extended days (8-16)
		extended := f.ExtendedDays()
		extTotal := snowfallTotal(extended)
		if extTotal >= thresholds.ExtendedSnowfallCM {
			window := snowfallWindow(extended)
			detected = append(detected, DetectedStorm{
				RegionID:    f.RegionID,
				Forecast:    f,
				Reason:      DetectionReasonExtended,
				SnowfallCM:  extTotal,
				StormWindow: window,
			})
		}
	}

	return detected
}

func snowfallTotal(days []ForecastDay) float64 {
	var total float64
	for _, d := range days {
		total += d.SnowfallCM
	}
	return total
}

func snowfallWindow(days []ForecastDay) [2]time.Time {
	if len(days) == 0 {
		return [2]time.Time{}
	}
	var start, end time.Time
	for _, d := range days {
		if d.SnowfallCM > 0 {
			if start.IsZero() {
				start = d.Date
			}
			end = d.Date
		}
	}
	return [2]time.Time{start, end}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./domain/...`
Expected: PASS

**Step 5: Commit**

Message: `feat: add pure storm detection logic with threshold filtering`

---

## Task 7: SQLite Storage Layer

I/O shell. Schema creation, migrations, and repository functions.

**Files:**
- Create: `storage/db.go`
- Create: `storage/resort_repo.go`
- Create: `storage/storm_repo.go`
- Create: `storage/profile_repo.go`
- Create: `storage/run_log_repo.go`
- Test: `storage/db_test.go`
- Test: `storage/storm_repo_test.go`

**Step 1: Write test for database initialization**

```go
// storage/db_test.go
package storage_test

import (
	"testing"

	"github.com/SeanMeyer/powder-hunter/storage"
)

func TestOpenDB(t *testing.T) {
	db, err := storage.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	// Verify tables exist
	tables := []string{"resorts", "regions", "storms", "evaluations", "user_profiles", "run_log"}
	for _, table := range tables {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./storage/...`
Expected: FAIL

**Step 3: Implement database initialization**

```go
// storage/db.go
package storage

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// OpenDB opens (or creates) the SQLite database and runs migrations.
func OpenDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling WAL mode: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS regions (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	representative_lat REAL NOT NULL,
	representative_lon REAL NOT NULL,
	nearest_airports TEXT, -- JSON array
	drive_time_from_home TEXT,
	lodging_notes TEXT
);

CREATE TABLE IF NOT EXISTS resorts (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	latitude REAL NOT NULL,
	longitude REAL NOT NULL,
	elevation INTEGER,
	pass TEXT,
	region_id TEXT NOT NULL REFERENCES regions(id),
	vertical_drop INTEGER,
	lift_count INTEGER,
	nearest_metro TEXT,
	metro_distance_mi INTEGER,
	reputation_notes TEXT
);

CREATE TABLE IF NOT EXISTS storms (
	id TEXT PRIMARY KEY, -- region_id/start_date-end_date
	region_id TEXT NOT NULL REFERENCES regions(id),
	start_date TEXT NOT NULL,
	end_date TEXT NOT NULL,
	state TEXT NOT NULL DEFAULT 'detected',
	discord_thread_id TEXT,
	detection_source TEXT,
	detected_at TEXT NOT NULL,
	last_evaluated_at TEXT
);

CREATE TABLE IF NOT EXISTS evaluations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	storm_id TEXT NOT NULL REFERENCES storms(id),
	evaluated_at TEXT NOT NULL,
	tier INTEGER NOT NULL,
	confidence TEXT,
	recommendation TEXT,
	day_by_day TEXT,
	key_factors_pros TEXT, -- JSON array
	key_factors_cons TEXT, -- JSON array
	logistics_summary TEXT,
	strategy TEXT,
	snow_quality TEXT,
	crowd_estimate TEXT,
	closure_risk TEXT,
	weather_snapshot TEXT, -- JSON blob of forecast data
	region_context TEXT,
	user_context TEXT,
	raw_llm_response TEXT
);

CREATE TABLE IF NOT EXISTS user_profiles (
	id INTEGER PRIMARY KEY DEFAULT 1,
	home_lat REAL,
	home_lon REAL,
	home_city TEXT,
	passes TEXT, -- JSON array
	remote_work_capable INTEGER DEFAULT 1,
	excluded_regions TEXT, -- JSON array
	min_tier_for_ping INTEGER DEFAULT 3,
	near_term_threshold_cm REAL DEFAULT 20,
	extended_threshold_cm REAL DEFAULT 38
);

CREATE TABLE IF NOT EXISTS run_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	started_at TEXT NOT NULL,
	finished_at TEXT,
	regions_scanned INTEGER,
	storms_detected INTEGER,
	storms_evaluated INTEGER,
	alerts_sent INTEGER,
	errors TEXT, -- JSON array of error strings
	summary TEXT
);
`
```

**Step 4: Install SQLite dependency and run tests**

Run: `go get modernc.org/sqlite && go test ./storage/...`
Expected: PASS

**Step 5: Implement storm repository (key operations)**

Write `storage/storm_repo.go` with functions to upsert storms, add evaluations, list active storms, and get tracked region IDs. Also write `storage/resort_repo.go` for region/resort CRUD and `storage/profile_repo.go` for user profile. Also write `storage/run_log_repo.go` for audit logging.

These follow straightforward SQL patterns — use `database/sql` directly, parse results into domain types using constructor functions. Each repo function takes `*sql.DB` as the first argument (no repository struct needed for now).

**Step 6: Write storm repo tests**

```go
// storage/storm_repo_test.go
package storage_test

import (
	"testing"
	"time"

	"github.com/SeanMeyer/powder-hunter/domain"
	"github.com/SeanMeyer/powder-hunter/storage"
)

func TestStormRepo_UpsertAndGet(t *testing.T) {
	db, err := storage.OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Seed a region first
	err = storage.UpsertRegion(db, domain.Region{
		ID:   "summit-county",
		Name: "Summit County",
		RepresentativePos: domain.Coordinates{Latitude: 39.63, Longitude: -105.87},
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	stormID := domain.StormID{
		RegionID:  "summit-county",
		StartDate: now,
		EndDate:   now.AddDate(0, 0, 3),
	}

	// Upsert new storm
	err = storage.UpsertStorm(db, domain.Storm{
		ID:              stormID,
		State:           domain.StateDetected,
		DetectionSource: domain.SourceOpenMeteo,
		DetectedAt:      now,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Retrieve it
	storm, err := storage.GetStorm(db, stormID.String())
	if err != nil {
		t.Fatal(err)
	}
	if storm.State != domain.StateDetected {
		t.Errorf("got state %q, want %q", storm.State, domain.StateDetected)
	}

	// List tracked region IDs
	tracked, err := storage.TrackedRegionIDs(db)
	if err != nil {
		t.Fatal(err)
	}
	if !tracked["summit-county"] {
		t.Error("summit-county should be in tracked regions")
	}
}
```

**Step 7: Implement all repo functions and run tests**

Run: `go test ./storage/...`
Expected: PASS

**Step 8: Commit**

Message: `feat: add SQLite storage layer with schema and repositories`

---

## Task 8: Weather Clients — Open-Meteo

I/O shell. HTTP client for Open-Meteo's forecast API.

**Files:**
- Create: `weather/openmeteo.go`
- Test: `weather/openmeteo_test.go`

**Step 1: Write test with a fake HTTP server**

Use `net/http/httptest` to create a test server that returns canned Open-Meteo JSON. Test that the client correctly parses it into domain.Forecast.

```go
// weather/openmeteo_test.go
package weather_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SeanMeyer/powder-hunter/domain"
	"github.com/SeanMeyer/powder-hunter/weather"
)

func TestOpenMeteoClient_FetchForecast(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(openMeteoResponse))
	}))
	defer server.Close()

	client := weather.NewOpenMeteoClient(weather.OpenMeteoConfig{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})

	region := domain.Region{
		ID:                "summit-county",
		RepresentativePos: domain.Coordinates{Latitude: 39.63, Longitude: -105.87},
	}

	forecast, err := client.FetchForecast(context.Background(), region)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if forecast.RegionID != "summit-county" {
		t.Errorf("got region %q, want summit-county", forecast.RegionID)
	}
	if forecast.Source != domain.SourceOpenMeteo {
		t.Errorf("got source %q, want open-meteo", forecast.Source)
	}
	if len(forecast.Days) == 0 {
		t.Fatal("expected forecast days, got 0")
	}
}

const openMeteoResponse = `{
	"daily": {
		"time": ["2026-03-05", "2026-03-06", "2026-03-07"],
		"snowfall_sum": [5.2, 12.8, 3.1],
		"temperature_2m_max": [28.0, 25.0, 32.0],
		"temperature_2m_min": [18.0, 15.0, 22.0],
		"wind_speed_10m_max": [15.0, 25.0, 10.0]
	}
}`
```

**Step 2: Run test to verify it fails**

Run: `go test ./weather/...`
Expected: FAIL

**Step 3: Implement Open-Meteo client**

Parse the JSON response into `domain.Forecast`. The client takes a configurable base URL (for testing) and HTTP client. It constructs the query with `latitude`, `longitude`, `daily=snowfall_sum,temperature_2m_max,temperature_2m_min,wind_speed_10m_max`, `forecast_days=16`, `temperature_unit=fahrenheit`.

**Step 4: Run tests**

Run: `go test ./weather/...`
Expected: PASS

**Step 5: Commit**

Message: `feat: add Open-Meteo weather client`

---

## Task 9: Weather Clients — NWS

**Files:**
- Create: `weather/nws.go`
- Test: `weather/nws_test.go`

Same pattern as Task 8: test with httptest server, parse NWS grid-point forecast JSON into `domain.Forecast`. NWS requires a two-step flow: `/points/{lat},{lon}` to get the grid URL, then fetch the grid forecast. Cache the points → grid mapping so we don't re-resolve every run.

**Step 1-5:** Same TDD flow as Task 8.

**Commit message:** `feat: add NWS weather client with grid-point resolution`

---

## Task 10: Weather Fetch Orchestration

Parallel fetching using `errgroup`. This is the shell that calls both weather clients for all regions concurrently.

**Files:**
- Create: `weather/fetch.go`
- Test: `weather/fetch_test.go`

**Step 1: Write test**

Test that `FetchAllForecasts` calls Open-Meteo for all regions and NWS for US-only regions, returning merged results. Use fake clients (interfaces) for testing.

**Step 2-5:** TDD flow.

**Commit message:** `feat: add parallel weather fetch orchestration`

---

## Task 11: Gemini Evaluation Client

I/O shell. Calls Gemini with search grounding to evaluate a storm.

**Files:**
- Create: `evaluation/gemini.go`
- Create: `evaluation/prompt.go`
- Test: `evaluation/prompt_test.go`

**Step 1: Write tests for prompt construction (pure)**

The prompt builder is pure — it takes domain types and produces a string. Test that it includes weather data, region metadata, user profile, and prior evaluation history. Test that the response parser correctly extracts structured JSON into `domain.Evaluation`.

**Step 2: Implement prompt builder**

The system prompt should instruct Gemini to:
- Use Google Search to research current lodging prices, flight costs, road conditions, car rental options
- Evaluate snow quality based on temperature and elevation
- Assess crowd levels based on day-of-week, resort proximity to metros, resort reputation
- Estimate closure risk based on snowfall intensity and wind
- Determine the optimal days to ski (considering clearing day / hero day concept)
- Consider the user's pass coverage and cost implications
- Factor in work flexibility (remote work + PTO strategy)
- Return structured JSON matching the `domain.Evaluation` fields
- Assign one of exactly three tiers: DROP EVERYTHING, WORTH A LOOK, ON THE RADAR
- Provide a plain-language recommendation explaining the reasoning

**Step 3: Implement Gemini client**

Use `google.golang.org/genai` SDK. Enable `GoogleSearch` tool. Request JSON response format.

The client should NOT be tested against the real Gemini API in unit tests. Instead, test the prompt builder and response parser as pure functions. Integration testing against the real API will be manual.

**Step 4: Commit**

Message: `feat: add Gemini evaluation client with search grounding`

---

## Task 12: Discord Webhook Client

**Files:**
- Create: `discord/webhook.go`
- Create: `discord/format.go`
- Test: `discord/format_test.go`
- Test: `discord/webhook_test.go`

**Step 1: Write tests for embed formatting (pure)**

Test that `FormatNewStormEmbed` produces the correct Discord embed structure from a `domain.Storm` + `domain.Evaluation`. Test that `FormatStormUpdate` includes what changed.

**Step 2: Write test for webhook posting**

Use httptest to verify the webhook client sends correct JSON to the Discord API. Verify that new storms create messages and updates go to threads. Verify `@here` is included for DROP EVERYTHING tier.

**Step 3-5:** TDD flow.

Discord threading: post the initial message with the webhook, then use the `thread_id` query parameter on subsequent webhook calls to post in that thread. Note: Discord webhooks return message ID in the response when `?wait=true` is appended — use this to get the thread ID.

**Commit message:** `feat: add Discord webhook client with embed formatting and threading`

---

## Task 13: Pipeline Orchestration

The main I/O sandwich. Wires everything together.

**Files:**
- Create: `pipeline/run.go`
- Test: `pipeline/run_test.go`

**Step 1: Define the Pipeline struct and Run method**

```go
// pipeline/run.go
type Pipeline struct {
	DB         *sql.DB
	Weather    WeatherFetcher    // interface
	Evaluator  StormEvaluator    // interface
	Discord    DiscordPoster     // interface
	Logger     *slog.Logger
}

// Run executes a single pipeline cycle: scan → detect → evaluate → alert → persist.
func (p *Pipeline) Run(ctx context.Context) error {
	// 1. Load regions and user profile from DB
	// 2. Load tracked storm region IDs from DB
	// 3. Fetch weather for all regions (parallel)
	// 4. Detect storms (pure) — threshold filter + tracked storms
	// 5. For each detected storm:
	//    a. Load prior evaluation from DB (if exists)
	//    b. Evaluate via Gemini
	//    c. Compare to prior evaluation
	//    d. Post/update Discord
	//    e. Persist storm + evaluation to DB
	// 6. Log run summary to run_log table
	return nil
}
```

**Step 2: Write test with fakes for all dependencies**

Test the full pipeline using fake weather fetcher, fake evaluator, and fake Discord poster. Verify that:
- Regions below threshold are not evaluated
- Already-tracked storms are always re-evaluated
- New storms create Discord messages
- Storm updates go to existing threads
- Everything is persisted to the DB
- Run log is recorded

**Step 3-5:** TDD flow.

**Commit message:** `feat: add pipeline orchestration with full scan-evaluate-alert cycle`

---

## Task 14: Seed Data — Initial Resort and Region Database

**Files:**
- Create: `seed/resorts.go`

**Step 1: Create initial seed data**

Start with a representative set of regions and resorts. This doesn't need to be complete for MVP — start with ~20 regions covering:
- Colorado (Summit County, Vail/Beaver Creek, Aspen, Steamboat, Telluride, Wolf Creek, Powderhorn, etc.)
- Utah (Wasatch Front, Park City area, Southern Utah)
- Pacific Northwest (Stevens Pass area, Crystal Mountain, Mt. Baker, Whistler)
- Wyoming/Montana (Jackson Hole, Big Sky)
- Northeast (optional, lower priority)

Each resort needs: name, lat/lon, elevation, pass affiliation, region ID, and metadata (reputation notes, lift count, nearest metro).

**Step 2: Write a function to seed the database**

```go
func SeedDatabase(db *sql.DB) error {
	// Insert regions, then resorts
}
```

**Step 3: Commit**

Message: `feat: add initial resort and region seed data`

---

## Task 15: Wire Up main.go

**Files:**
- Modify: `cmd/powder-hunter/main.go`

**Step 1: Wire the run() function**

Read environment variables (GEMINI_API_KEY, DISCORD_WEBHOOK_URL, DB_PATH). Open DB. Seed if empty. Construct pipeline. Run.

```go
func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	db, err := storage.OpenDB(envOrDefault("DB_PATH", "./powder.db"))
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	// Seed if no regions exist
	// Construct weather clients, evaluator, discord client
	// Construct pipeline
	// Run pipeline
}
```

**Step 2: Verify it compiles and runs (with empty DB)**

Run: `go build ./cmd/powder-hunter/ && ./powder-hunter`
Expected: Runs, seeds DB, attempts weather fetch (may fail without network — that's fine)

**Step 3: Commit**

Message: `feat: wire up main entry point with pipeline construction`

---

## Task 16: Docker Deployment

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yml`

**Step 1: Write multi-stage Dockerfile**

```dockerfile
FROM golang:1.23 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /powder-hunter ./cmd/powder-hunter/

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /powder-hunter /usr/local/bin/powder-hunter
VOLUME /data
ENV DB_PATH=/data/powder.db
ENTRYPOINT ["powder-hunter"]
```

**Step 2: Write docker-compose.yml**

```yaml
services:
  powder-hunter:
    build: .
    environment:
      - GEMINI_API_KEY=${GEMINI_API_KEY}
      - DISCORD_WEBHOOK_URL=${DISCORD_WEBHOOK_URL}
      - DB_PATH=/data/powder.db
    volumes:
      - powder-data:/data

volumes:
  powder-data:
```

**Step 3: Verify it builds**

Run: `docker build -t powder-hunter .`
Expected: Image builds successfully

**Step 4: Commit**

Message: `feat: add Dockerfile and docker-compose for Unraid deployment`

---

## Task 17: End-to-End Manual Test

Not automated — this is a manual verification step.

**Step 1:** Set up environment variables (GEMINI_API_KEY, DISCORD_WEBHOOK_URL)

**Step 2:** Run the binary locally: `DB_PATH=./test.db GEMINI_API_KEY=... DISCORD_WEBHOOK_URL=... go run ./cmd/powder-hunter/`

**Step 3:** Verify:
- Weather data is fetched for all seeded regions
- Storms above threshold are detected
- Gemini evaluates with web search (check logs for grounding metadata)
- Discord message is posted with rich embed
- Database contains full evaluation records
- Run log entry exists with summary

**Step 4:** Run again to verify:
- Tracked storms are re-evaluated
- Updates go to existing Discord threads
- No duplicate storm entries

**Step 5:** Commit any fixes needed

---

## Dependency Summary

```
go get google.golang.org/genai
go get modernc.org/sqlite
```

No other external dependencies needed. Discord webhook is plain HTTP. Weather APIs are plain HTTP. Logging is stdlib `log/slog`.
