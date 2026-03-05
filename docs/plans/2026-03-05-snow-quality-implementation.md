# Snow Quality Assessment System Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a snow quality assessment system that produces pre-interpreted ride quality signals (crystal quality, density character, storm layering, base risk, bluebird detection) from weather data, displayed in trace output and fed to the LLM evaluation.

**Architecture:** Cloud cover is added to both weather parsers. A new `domain/snow_quality.go` contains the SnowQuality type and pure assessment functions. Quality notes are computed per-day from the full forecast sequence, then rendered in trace output and the LLM prompt as human-readable ride quality notes.

**Tech Stack:** Go 1.24+, pure domain logic, no new dependencies. Solar elevation is computed from latitude + date using standard astronomical formula.

**Design doc:** `docs/plans/2026-03-05-snow-quality-design.md`

---

### Task 1: Add cloud cover to domain model

**Files:**
- Modify: `domain/weather.go` (HalfDay struct, ~line 37)

**Step 1: Add CloudCoverPct field to HalfDay**

In `domain/weather.go`, add a new field to the `HalfDay` struct after `FreezingLevelMaxM`:

```go
type HalfDay struct {
	SnowfallCM        float64
	TemperatureC      float64
	PrecipitationMM   float64
	WindSpeedKmh      float64
	WindGustKmh       float64
	FreezingLevelMinM float64
	FreezingLevelMaxM float64
	CloudCoverPct     float64 // average cloud cover during period (0-100%)
}
```

**Step 2: Verify all tests still pass**

Run: `go test ./... -count=1`
Expected: PASS (adding a field with zero default doesn't break anything)

**Step 3: Commit**

```bash
git add domain/weather.go
git commit -m "feat: add CloudCoverPct field to HalfDay struct"
```

---

### Task 2: Add cloud cover to Open-Meteo parser

**Files:**
- Modify: `weather/openmeteo.go` (lines 23, 35-42, 195, 332-363, hourly loop)
- Modify: `weather/openmeteo_test.go`

**Step 1: Add cloud_cover to API request and parsing**

In `weather/openmeteo.go`:

1. Update `openMeteoHourlyVars` (line 23) to add `cloud_cover`:
```go
const openMeteoHourlyVars = "temperature_2m,precipitation,wind_speed_10m,wind_gusts_10m,freezing_level_height,cloud_cover"
```

2. Add `CloudCover` field to `openMeteoHourlyData` struct (after line 41):
```go
type openMeteoHourlyData struct {
	Time                []string
	Temperature2m       []float64
	Precipitation       []float64
	WindSpeed10m        []float64
	WindGusts10m        []float64
	FreezingLevelHeight []float64
	CloudCover          []float64
}
```

3. In `extractMultiModelData` (around line 195), add cloud cover decoding after freezing level:
```go
cloudCover := decodeFloat64Array(hourly["cloud_cover"+suffix])
```
And include it in the struct initialization:
```go
CloudCover: cloudCover,
```

4. In `parseOpenMeteoHourly`, add cloud cover tracking to `dayAccum` struct (around line 360):
```go
dayCloudCoverSum   float64
dayCloudCoverCount int
nightCloudCoverSum   float64
nightCloudCoverCount int
```

5. Add cloud cover availability check after `hasFreezingLevel` (around line 330):
```go
hasCloudCover := len(h.CloudCover) == n
```

6. In the hourly loop, accumulate cloud cover (after the freezing level block, around line 446):
```go
if hasCloudCover {
	cc := h.CloudCover[i]
	if hour >= 6 && hour < 18 {
		acc.dayCloudCoverSum += cc
		acc.dayCloudCoverCount++
	} else {
		acc.nightCloudCoverSum += cc
		acc.nightCloudCoverCount++
	}
}
```

7. In the daily assembly (where DailyForecast is built), set cloud cover on HalfDay:
```go
// In the Day HalfDay:
CloudCoverPct: safeDivide(acc.dayCloudCoverSum, float64(acc.dayCloudCoverCount)),
// In the Night HalfDay:
CloudCoverPct: safeDivide(acc.nightCloudCoverSum, float64(acc.nightCloudCoverCount)),
```

Add a `safeDivide` helper if one doesn't exist:
```go
func safeDivide(num, denom float64) float64 {
	if denom == 0 {
		return 0
	}
	return num / denom
}
```

**Step 2: Update tests**

Add a test to `weather/openmeteo_test.go` in `TestParseOpenMeteoHourly_SLRAdjusted`:

```go
t.Run("cloud cover tracked per half-day", func(t *testing.T) {
	times := make([]string, 12)
	temps := make([]float64, 12)
	precips := make([]float64, 12)
	winds := make([]float64, 12)
	gusts := make([]float64, 12)
	fzLvls := make([]float64, 12)
	clouds := make([]float64, 12)

	for i := 0; i < 6; i++ {
		times[i] = hourTimestamp(0, 6+i)
		temps[i] = -5.0
		precips[i] = 1.0
		clouds[i] = 20.0 // clear daytime
	}
	for i := 6; i < 12; i++ {
		times[i] = hourTimestamp(0, 18+i-6)
		temps[i] = -5.0
		precips[i] = 1.0
		clouds[i] = 80.0 // cloudy nighttime
	}

	h := openMeteoHourlyData{
		Time:                times,
		Temperature2m:       temps,
		Precipitation:       precips,
		WindSpeed10m:        winds,
		WindGusts10m:        gusts,
		FreezingLevelHeight: fzLvls,
		CloudCover:          clouds,
	}

	daily, err := parseOpenMeteoHourly(h)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertApprox(t, "Day.CloudCoverPct", daily[0].Day.CloudCoverPct, 20.0, 0.1)
	assertApprox(t, "Night.CloudCoverPct", daily[0].Night.CloudCoverPct, 80.0, 0.1)
})
```

Also update `makeHourlyData` to include cloud cover (add a `cloudPct` parameter or default to 0 and extend the struct initialization).

**Step 3: Run tests**

Run: `go test ./weather/ -run TestParseOpenMeteoHourly -v`
Expected: PASS

**Step 4: Commit**

```bash
git add weather/openmeteo.go weather/openmeteo_test.go
git commit -m "feat: add cloud cover to Open-Meteo parser"
```

---

### Task 3: Add cloud cover to NWS parser

**Files:**
- Modify: `weather/nws.go` (lines 168-180, 576-584)

**Step 1: Add SkyCover to NWS response struct**

In `weather/nws.go`, add `SkyCover` to `nwsGridpointResponse` (around line 580):

```go
type nwsGridpointResponse struct {
	Properties struct {
		SnowfallAmount            nwsTimeSeries `json:"snowfallAmount"`
		Temperature               nwsTimeSeries `json:"temperature"`
		QuantitativePrecipitation nwsTimeSeries `json:"quantitativePrecipitation"`
		WindSpeed                 nwsTimeSeries `json:"windSpeed"`
		WindGust                  nwsTimeSeries `json:"windGust"`
		SkyCover                  nwsTimeSeries `json:"skyCover"`
	} `json:"properties"`
}
```

**Step 2: Add cloud cover tracking to NWS dayAccum and processing**

Add to NWS `dayAccum` struct:
```go
dayCloudCoverSum, nightCloudCoverSum     float64
dayCloudCoverCount, nightCloudCoverCount int
```

After Step 3 (wind processing), add Step 4 for cloud cover:
```go
// Step 4: Sky cover (cloud cover percentage).
walkHourly(resp.Properties.SkyCover.Values, func(acc *dayAccum, hour int, pct float64) {
	if hour >= 6 && hour < 18 {
		acc.dayCloudCoverSum += pct
		acc.dayCloudCoverCount++
	} else {
		acc.nightCloudCoverSum += pct
		acc.nightCloudCoverCount++
	}
})
```

In the daily assembly section, set cloud cover on HalfDay structs:
```go
Day: domain.HalfDay{
	// ... existing fields ...
	CloudCoverPct: safeDivide(acc.dayCloudCoverSum, float64(acc.dayCloudCoverCount)),
},
Night: domain.HalfDay{
	// ... existing fields ...
	CloudCoverPct: safeDivide(acc.nightCloudCoverSum, float64(acc.nightCloudCoverCount)),
},
```

Add `safeDivide` helper if not already added in Task 2 (or import from a shared location).

**Step 3: Run tests**

Run: `go test ./... -count=1`
Expected: PASS

**Step 4: Commit**

```bash
git add weather/nws.go
git commit -m "feat: add cloud cover (skyCover) to NWS parser"
```

---

### Task 4: Create SnowQuality type and density category logic

**Files:**
- Create: `domain/snow_quality.go`
- Create: `domain/snow_quality_test.go`

**Step 1: Write tests for density categorization**

Create `domain/snow_quality_test.go`:

```go
package domain

import "testing"

func TestDensityCategory(t *testing.T) {
	tests := []struct {
		name     string
		density  float64
		wantCat  string
	}{
		{"cold smoke", 45.0, "cold_smoke"},
		{"cold smoke boundary", 59.9, "cold_smoke"},
		{"dry powder low", 60.0, "dry_powder"},
		{"dry powder mid", 75.0, "dry_powder"},
		{"dry powder high", 89.9, "dry_powder"},
		{"standard low", 90.0, "standard"},
		{"standard mid", 110.0, "standard"},
		{"standard high", 129.9, "standard"},
		{"heavy low", 130.0, "heavy"},
		{"heavy mid", 150.0, "heavy"},
		{"heavy high", 179.9, "heavy"},
		{"wet cement", 180.0, "wet_cement"},
		{"wet cement high", 250.0, "wet_cement"},
		{"zero density (rain)", 0.0, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DensityCategory(tc.density)
			if got != tc.wantCat {
				t.Errorf("DensityCategory(%v) = %q, want %q", tc.density, got, tc.wantCat)
			}
		})
	}
}

func TestCrystalQuality(t *testing.T) {
	tests := []struct {
		name    string
		windMph float64
		want    string
	}{
		{"calm", 5.0, "intact"},
		{"light breeze", 14.9, "intact"},
		{"moderate wind", 15.0, "partially_broken"},
		{"strong wind", 24.9, "partially_broken"},
		{"very strong", 25.0, "wind_broken"},
		{"gale", 40.0, "wind_broken"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CrystalQuality(tc.windMph)
			if got != tc.want {
				t.Errorf("CrystalQuality(%v) = %q, want %q", tc.windMph, got, tc.want)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./domain/ -run 'TestDensityCategory|TestCrystalQuality' -v`
Expected: FAIL — functions undefined

**Step 3: Implement**

Create `domain/snow_quality.go`:

```go
package domain

// SnowQuality holds per-day ride quality signals computed from weather data.
// RideQualityNotes contains pre-interpreted human-readable assessments.
type SnowQuality struct {
	CrystalQuality    string  // "intact" / "partially_broken" / "wind_broken"
	WindDuringSnowMph float64 // avg wind speed during hours with precipitation
	DensityCategory   string  // "cold_smoke" / "dry_powder" / "standard" / "heavy" / "wet_cement"
	AvgDensityKgM3    float64 // average Vionnet density during precip hours
	Bluebird          bool    // clear skies + recent fresh snow
	CloudCoverPct     float64 // daytime average cloud cover

	BaseRisk       string // "low" / "moderate" / "high"
	BaseRiskReason string // human-readable explanation

	RideQualityNotes []string // pre-interpreted assessments for LLM and trace output
}

// DensityCategory returns a human-readable density classification from kg/m3.
// Returns empty string for zero density (rain).
func DensityCategory(densityKgM3 float64) string {
	switch {
	case densityKgM3 <= 0:
		return ""
	case densityKgM3 < 60:
		return "cold_smoke"
	case densityKgM3 < 90:
		return "dry_powder"
	case densityKgM3 < 130:
		return "standard"
	case densityKgM3 < 180:
		return "heavy"
	default:
		return "wet_cement"
	}
}

// CrystalQuality classifies crystal integrity based on average wind during snowfall.
func CrystalQuality(avgWindMph float64) string {
	switch {
	case avgWindMph < 15:
		return "intact"
	case avgWindMph < 25:
		return "partially_broken"
	default:
		return "wind_broken"
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./domain/ -run 'TestDensityCategory|TestCrystalQuality' -v`
Expected: PASS

**Step 5: Commit**

```bash
git add domain/snow_quality.go domain/snow_quality_test.go
git commit -m "feat: add SnowQuality type with density category and crystal quality"
```

---

### Task 5: Add solar elevation calculation

**Files:**
- Modify: `domain/snow_quality.go`
- Modify: `domain/snow_quality_test.go`

**Step 1: Write test for solar elevation**

Add to `domain/snow_quality_test.go`:

```go
func TestSolarElevationAtNoon(t *testing.T) {
	tests := []struct {
		name     string
		lat      float64
		month    int
		day      int
		wantMin  float64 // approximate, degrees
		wantMax  float64
	}{
		// Denver (~40°N) winter solstice: ~27°
		{"Denver Dec 21", 40.0, 12, 21, 25.0, 29.0},
		// Denver March equinox: ~50°
		{"Denver Mar 21", 40.0, 3, 21, 48.0, 52.0},
		// Denver April 15: ~58°
		{"Denver Apr 15", 40.0, 4, 15, 56.0, 60.0},
		// Whistler (~50°N) Dec 21: ~17°
		{"Whistler Dec 21", 50.0, 12, 21, 15.0, 19.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			date := time.Date(2026, time.Month(tc.month), tc.day, 0, 0, 0, 0, time.UTC)
			got := SolarElevationAtNoon(tc.lat, date)
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("SolarElevationAtNoon(%v, %v) = %.1f, want [%.1f, %.1f]",
					tc.lat, date.Format("Jan 02"), got, tc.wantMin, tc.wantMax)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./domain/ -run TestSolarElevationAtNoon -v`
Expected: FAIL

**Step 3: Implement**

Add to `domain/snow_quality.go`:

```go
// SolarElevationAtNoon returns the sun's elevation angle in degrees at solar noon
// for a given latitude and date. Uses standard astronomical formula.
func SolarElevationAtNoon(latitudeDeg float64, date time.Time) float64 {
	// Day of year (1-365)
	doy := float64(date.YearDay())

	// Solar declination (simplified formula, accurate to ~0.5°)
	declination := 23.45 * math.Sin(2*math.Pi*(284+doy)/365.0)

	// Solar elevation at noon = 90 - |latitude - declination|
	elevation := 90.0 - math.Abs(latitudeDeg-declination)

	return elevation
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./domain/ -run TestSolarElevationAtNoon -v`
Expected: PASS

**Step 5: Commit**

```bash
git add domain/snow_quality.go domain/snow_quality_test.go
git commit -m "feat: add solar elevation calculation for crust risk assessment"
```

---

### Task 6: Implement base risk assessment

**Files:**
- Modify: `domain/snow_quality.go`
- Modify: `domain/snow_quality_test.go`

**Step 1: Write tests**

Add to `domain/snow_quality_test.go`:

```go
func TestAssessBaseRisk(t *testing.T) {
	tests := []struct {
		name           string
		preStormDays   []DailyForecast
		latitude       float64
		stormStartDate time.Time
		wantRisk       string
		wantHasReason  bool
	}{
		{
			name:           "no warm period — low risk",
			preStormDays:   makeDailySlice(-10, -5, 50.0), // cold, cloudy
			latitude:       40.0,
			stormStartDate: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			wantRisk:       "low",
		},
		{
			name:           "above freezing 6+ hours — high risk",
			preStormDays:   makeDailySlice(3, 5, 50.0), // warm
			latitude:       40.0,
			stormStartDate: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			wantRisk:       "high",
			wantHasReason:  true,
		},
		{
			name:           "above freezing briefly — moderate risk",
			preStormDays:   makeDailySliceMixed(-5, 2, 50.0), // mostly cold, brief warm
			latitude:       40.0,
			stormStartDate: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			wantRisk:       "moderate",
			wantHasReason:  true,
		},
		{
			name:           "March clear sky moderate temp — solar crust risk",
			preStormDays:   makeDailySlice(-3, -1, 15.0), // cold but clear, March
			latitude:       40.0,
			stormStartDate: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
			wantRisk:       "moderate",
			wantHasReason:  true,
		},
		{
			name:           "April clear sky — high solar risk",
			preStormDays:   makeDailySlice(-5, -2, 10.0), // clear April
			latitude:       40.0,
			stormStartDate: time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC),
			wantRisk:       "moderate",
			wantHasReason:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			risk, reason := AssessBaseRisk(tc.preStormDays, tc.latitude, tc.stormStartDate)
			if risk != tc.wantRisk {
				t.Errorf("risk = %q, want %q (reason: %s)", risk, tc.wantRisk, reason)
			}
			if tc.wantHasReason && reason == "" {
				t.Error("expected a reason but got empty string")
			}
		})
	}
}

// Test helpers

func makeDailySlice(tempMinC, tempMaxC, cloudCoverPct float64) []DailyForecast {
	return []DailyForecast{
		{
			TemperatureMinC: tempMinC,
			TemperatureMaxC: tempMaxC,
			Day:             HalfDay{TemperatureC: tempMaxC, CloudCoverPct: cloudCoverPct},
			Night:           HalfDay{TemperatureC: tempMinC, CloudCoverPct: cloudCoverPct},
		},
		{
			TemperatureMinC: tempMinC,
			TemperatureMaxC: tempMaxC,
			Day:             HalfDay{TemperatureC: tempMaxC, CloudCoverPct: cloudCoverPct},
			Night:           HalfDay{TemperatureC: tempMinC, CloudCoverPct: cloudCoverPct},
		},
	}
}

func makeDailySliceMixed(tempMinC, tempMaxC, cloudCoverPct float64) []DailyForecast {
	return []DailyForecast{
		{
			TemperatureMinC: tempMinC,
			TemperatureMaxC: tempMinC, // first day cold
			Day:             HalfDay{TemperatureC: tempMinC, CloudCoverPct: cloudCoverPct},
			Night:           HalfDay{TemperatureC: tempMinC, CloudCoverPct: cloudCoverPct},
		},
		{
			TemperatureMinC: tempMinC,
			TemperatureMaxC: tempMaxC, // second day briefly warm
			Day:             HalfDay{TemperatureC: tempMaxC, CloudCoverPct: cloudCoverPct},
			Night:           HalfDay{TemperatureC: tempMinC, CloudCoverPct: cloudCoverPct},
		},
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./domain/ -run TestAssessBaseRisk -v`
Expected: FAIL

**Step 3: Implement**

Add to `domain/snow_quality.go`:

```go
// AssessBaseRisk evaluates the risk of a hard layer (melt-freeze or sun crust)
// under new snow based on pre-storm weather conditions.
func AssessBaseRisk(preStormDays []DailyForecast, latitude float64, stormStartDate time.Time) (risk string, reason string) {
	if len(preStormDays) == 0 {
		return "low", ""
	}

	// Count warm hours (above freezing) and check for clear sky solar risk.
	var warmHours int
	var solarRisk bool

	solarElevation := SolarElevationAtNoon(latitude, stormStartDate)

	for _, d := range preStormDays {
		// Melt-freeze: above 0°C
		if d.TemperatureMaxC > 0 {
			// Estimate warm hours from max temp being above freezing.
			// If max is well above 0, more hours were likely warm.
			if d.TemperatureMaxC > 3 {
				warmHours += 6 // substantial warm period
			} else {
				warmHours += 2 // brief warm period
			}
		}

		// Solar crust: clear skies + moderate temps + high sun angle.
		// Higher solar elevation needs less temperature to form crust.
		tempThresholdC := -3.0 // default for mid-season
		if solarElevation > 55 {
			tempThresholdC = -8.0 // late season, strong sun
		} else if solarElevation > 45 {
			tempThresholdC = -5.0 // mid-March
		} else if solarElevation < 35 {
			continue // Dec-Jan: sun too low for significant crust
		}

		if d.Day.CloudCoverPct < 30 && d.Day.TemperatureC > tempThresholdC {
			solarRisk = true
		}
	}

	// Determine risk level.
	switch {
	case warmHours >= 6:
		risk = "high"
		reason = fmt.Sprintf("above freezing for ~%d hours before storm — melt-freeze crust likely", warmHours)
	case warmHours >= 1 && solarRisk:
		risk = "high"
		reason = "melt-freeze and sun crust both likely"
	case solarRisk:
		risk = "moderate"
		reason = fmt.Sprintf("clear skies with solar elevation %.0f° — sun crust possible on south-facing terrain", solarElevation)
	case warmHours >= 1:
		risk = "moderate"
		reason = "brief above-freezing period — possible melt-freeze layer"
	default:
		risk = "low"
		reason = ""
	}

	return risk, reason
}
```

Add `"fmt"` to imports if not already present.

**Step 4: Run test to verify it passes**

Run: `go test ./domain/ -run TestAssessBaseRisk -v`
Expected: PASS

**Step 5: Commit**

```bash
git add domain/snow_quality.go domain/snow_quality_test.go
git commit -m "feat: add base risk assessment with solar crust detection"
```

---

### Task 7: Implement full ride quality assessment

**Files:**
- Modify: `domain/snow_quality.go`
- Modify: `domain/snow_quality_test.go`

This is the main assessment function that produces the pre-interpreted `RideQualityNotes` for each day.

**Step 1: Write tests**

Add to `domain/snow_quality_test.go`:

```go
func TestAssessRideQuality(t *testing.T) {
	t.Run("bluebird powder day", func(t *testing.T) {
		days := []DailyForecast{
			{ // Day 0: storm day with snow
				SnowfallCM: 25.0, // ~10"
				Day:        HalfDay{WindSpeedKmh: 15, CloudCoverPct: 90, PrecipitationMM: 5},
				Night:      HalfDay{WindSpeedKmh: 10, CloudCoverPct: 95, SnowfallCM: 15, PrecipitationMM: 10},
			},
			{ // Day 1: clear after storm
				SnowfallCM: 0,
				Day:        HalfDay{WindSpeedKmh: 5, CloudCoverPct: 10},
				Night:      HalfDay{CloudCoverPct: 15},
			},
		}

		qualities := AssessRideQuality(days, nil, 40.0, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))

		// Day 1 (index 1) should have bluebird flag
		if len(qualities) < 2 {
			t.Fatalf("expected 2 quality assessments, got %d", len(qualities))
		}
		if !qualities[1].Bluebird {
			t.Error("expected bluebird flag on day after storm")
		}
		found := false
		for _, note := range qualities[1].RideQualityNotes {
			if contains(note, "luebird") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected bluebird note, got: %v", qualities[1].RideQualityNotes)
		}
	})

	t.Run("windy storm gets crystal quality note", func(t *testing.T) {
		days := []DailyForecast{
			{
				SnowfallCM:  20.0,
				Day:         HalfDay{WindSpeedKmh: 50, CloudCoverPct: 100, PrecipitationMM: 8, SnowfallCM: 10},
				Night:       HalfDay{WindSpeedKmh: 40, CloudCoverPct: 100, PrecipitationMM: 6, SnowfallCM: 10},
			},
		}

		qualities := AssessRideQuality(days, nil, 40.0, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))
		if len(qualities) == 0 {
			t.Fatal("expected quality assessment")
		}
		if qualities[0].CrystalQuality != "wind_broken" {
			t.Errorf("crystal quality = %q, want wind_broken", qualities[0].CrystalQuality)
		}
	})

	t.Run("favorable layering dense then light", func(t *testing.T) {
		days := []DailyForecast{
			{ // Day 0: heavy/standard snow
				SnowfallCM:  15.0,
				SLRatio:     8.0, // standard density
				Day:         HalfDay{WindSpeedKmh: 30, CloudCoverPct: 100, PrecipitationMM: 10, SnowfallCM: 8},
				Night:       HalfDay{WindSpeedKmh: 20, CloudCoverPct: 100, PrecipitationMM: 5, SnowfallCM: 7},
			},
			{ // Day 1: light powder
				SnowfallCM:  20.0,
				SLRatio:     18.0, // dry powder / cold smoke
				Day:         HalfDay{WindSpeedKmh: 8, CloudCoverPct: 80, PrecipitationMM: 5, SnowfallCM: 12},
				Night:       HalfDay{WindSpeedKmh: 5, CloudCoverPct: 90, PrecipitationMM: 3, SnowfallCM: 8},
			},
		}

		qualities := AssessRideQuality(days, nil, 40.0, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))
		if len(qualities) < 2 {
			t.Fatalf("expected 2 quality assessments, got %d", len(qualities))
		}
		found := false
		for _, note := range qualities[1].RideQualityNotes {
			if contains(note, "avorable layering") {
				found = true
			}
		}
		if !found {
			t.Errorf("expected favorable layering note on day 2, got: %v", qualities[1].RideQualityNotes)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./domain/ -run TestAssessRideQuality -v`
Expected: FAIL

**Step 3: Implement**

Add to `domain/snow_quality.go`:

```go
// AssessRideQuality computes snow quality signals for each day in the forecast.
// preStormDays are the 1-2 days before snowfall begins (for base risk).
// Returns one SnowQuality per day in the input slice.
func AssessRideQuality(days []DailyForecast, preStormDays []DailyForecast, latitude float64, stormStartDate time.Time) []SnowQuality {
	if len(days) == 0 {
		return nil
	}

	// Compute base risk once for the storm.
	baseRisk, baseRiskReason := AssessBaseRisk(preStormDays, latitude, stormStartDate)

	// Compute per-day density from SLRatio (inverse of the Vionnet output).
	densityForDay := func(d DailyForecast) float64 {
		if d.SLRatio <= 0 {
			return 0
		}
		return 1000.0 / d.SLRatio
	}

	// Compute average wind during snowfall for a day.
	avgWindDuringSnow := func(d DailyForecast) float64 {
		var totalWind, totalPrecip float64
		if d.Day.PrecipitationMM > 0 {
			totalWind += d.Day.WindSpeedKmh * d.Day.PrecipitationMM
			totalPrecip += d.Day.PrecipitationMM
		}
		if d.Night.PrecipitationMM > 0 {
			totalWind += d.Night.WindSpeedKmh * d.Night.PrecipitationMM
			totalPrecip += d.Night.PrecipitationMM
		}
		if totalPrecip <= 0 {
			return 0
		}
		return totalWind / totalPrecip * 0.621371 // km/h → mph
	}

	qualities := make([]SnowQuality, len(days))

	for i, d := range days {
		snowIn := CMToInches(d.SnowfallCM)
		density := densityForDay(d)
		densCat := DensityCategory(density)
		windMph := avgWindDuringSnow(d)
		crystalQ := CrystalQuality(windMph)

		q := SnowQuality{
			DensityCategory:   densCat,
			AvgDensityKgM3:    density,
			CrystalQuality:    crystalQ,
			WindDuringSnowMph: windMph,
			CloudCoverPct:     d.Day.CloudCoverPct,
			BaseRisk:          baseRisk,
			BaseRiskReason:    baseRiskReason,
		}

		isSnowDay := snowIn >= 0.5
		var notes []string

		// Crystal quality note (only on snow days).
		if isSnowDay {
			switch crystalQ {
			case "intact":
				notes = append(notes, "Fresh dendrites likely — expect true powder feel")
			case "partially_broken":
				notes = append(notes, "Moderate wind during snowfall — crystals partially broken, still good but not the lightest")
			case "wind_broken":
				notes = append(notes, "Heavy wind during snowfall — snow will feel chalky on exposed terrain, best quality in protected trees")
			}
		}

		// Layering note (compare to prior day).
		if isSnowDay && i > 0 {
			prevDensity := densityForDay(days[i-1])
			prevCat := DensityCategory(prevDensity)
			prevSnowIn := CMToInches(days[i-1].SnowfallCM)

			if prevSnowIn >= 0.5 && prevCat != "" && densCat != "" && prevCat != densCat {
				prevIsLight := prevCat == "cold_smoke" || prevCat == "dry_powder"
				curIsLight := densCat == "cold_smoke" || densCat == "dry_powder"

				if !prevIsLight && curIsLight {
					notes = append(notes, fmt.Sprintf("Favorable layering — light snow over supportive dense base from %s",
						days[i-1].Date.Format("Jan 02")))
				} else if prevIsLight && !curIsLight {
					notes = append(notes, "New heavy snow over lighter layer — may feel punchy and inconsistent")
				}
			}
		}

		// Base risk + punch-through (only on first snow day).
		if isSnowDay && baseRisk != "low" {
			isFirstSnowDay := true
			for j := 0; j < i; j++ {
				if CMToInches(days[j].SnowfallCM) >= 0.5 {
					isFirstSnowDay = false
					break
				}
			}

			if isFirstSnowDay {
				notes = append(notes, baseRiskReason)

				// Punch-through assessment.
				if baseRisk == "high" {
					switch {
					case densCat == "cold_smoke" && snowIn < 12:
						notes = append(notes, "Likely punching through to hard layer underneath")
					case densCat == "cold_smoke" && snowIn >= 12:
						notes = append(notes, "Deep enough to float above the crust, but may hit it in thin spots")
					case densCat == "dry_powder" && snowIn < 8:
						notes = append(notes, "May punch through to crust in spots")
					case densCat == "dry_powder" && snowIn >= 8:
						notes = append(notes, "Should have enough depth over the crust")
					case snowIn >= 4: // standard or denser
						notes = append(notes, "Dense enough to ride without hitting the crust")
					}
				} else if baseRisk == "moderate" && densCat == "cold_smoke" && snowIn < 8 {
					notes = append(notes, "Thin — could feel inconsistent over variable base")
				}
			}
		}

		// Bluebird detection.
		if i > 0 {
			prevSnowIn := CMToInches(days[i-1].SnowfallCM)
			prevNightSnowIn := CMToInches(days[i-1].Night.SnowfallCM)
			if d.Day.CloudCoverPct < 20 && (prevNightSnowIn >= 4 || prevSnowIn >= 4) {
				q.Bluebird = true
				notes = append(notes, "Bluebird powder day — clear skies with fresh snow")
			}
		}

		q.RideQualityNotes = notes
		qualities[i] = q
	}

	return qualities
}
```

**Step 4: Run test**

Run: `go test ./domain/ -run TestAssessRideQuality -v`
Expected: PASS

**Step 5: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS

**Step 6: Commit**

```bash
git add domain/snow_quality.go domain/snow_quality_test.go
git commit -m "feat: implement full ride quality assessment with layering and bluebird detection"
```

---

### Task 8: Add ride quality notes to trace output

**Files:**
- Modify: `trace/formatter.go`

**Step 1: Update FormatWeather to show ride quality notes**

In `trace/formatter.go`, the `FormatWeather` function renders per-day weather lines. After the existing notes block (rain hours, mixed hours, freezing level — around line 84), add ride quality note rendering.

The function signature will need to accept quality data. There are two approaches:
1. Compute quality inline in the formatter (simpler but couples trace to domain logic)
2. Accept pre-computed quality alongside forecasts

Use approach 2: add an optional `qualities map[string][]SnowQuality` parameter or compute quality in the caller and pass it.

The simplest approach: after each resort's daily weather lines, append ride quality notes if present. Since `FormatWeather` currently receives `[]domain.Forecast`, extend the trace command to compute quality and pass it.

In `trace/formatter.go`, add a new function:

```go
// FormatRideQualityNotes renders ride quality assessment notes for a day.
func FormatRideQualityNotes(w io.Writer, notes []string) {
	if len(notes) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Ride quality:")
	for _, note := range notes {
		fmt.Fprintf(w, "    - %s\n", note)
	}
}
```

Then update the trace command (in `cmd/powder-hunter/main.go` or wherever the trace output is assembled) to compute and render quality notes after weather data. Read the trace command code to find the right integration point.

**Step 2: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS

**Step 3: Commit**

```bash
git add trace/formatter.go
git commit -m "feat: add ride quality notes to trace output"
```

---

### Task 9: Add ride quality notes to LLM prompt

**Files:**
- Modify: `evaluation/prompt.go`

**Step 1: Add ride quality notes to FormatConsolidatedWeatherForPrompt**

In `evaluation/prompt.go`, in the `FormatConsolidatedWeatherForPrompt` function (around lines 237-260), after the existing annotation block, add ride quality notes.

The function will need to compute or receive SnowQuality data. The simplest approach: compute quality inline using the forecast data that's already available, and add the notes to the annotation section.

Add a new function that wraps the quality computation:

```go
// FormatRideQualityForPrompt computes and formats ride quality notes for the LLM prompt.
func FormatRideQualityForPrompt(forecasts []domain.Forecast, resorts []domain.Resort) string {
	var b strings.Builder
	for _, resort := range resorts {
		var resortForecasts []domain.Forecast
		for _, f := range forecasts {
			if f.ResortID == resort.ID {
				resortForecasts = append(resortForecasts, f)
			}
		}
		if len(resortForecasts) == 0 {
			continue
		}

		// Use first forecast for daily data (quality computed from template forecast).
		templateF := resortForecasts[0]
		qualities := domain.AssessRideQuality(templateF.DailyData, nil, resort.Latitude, time.Now())

		hasNotes := false
		for _, q := range qualities {
			if len(q.RideQualityNotes) > 0 {
				hasNotes = true
				break
			}
		}
		if !hasNotes {
			continue
		}

		fmt.Fprintf(&b, "\n### %s — Ride Quality Notes\n", resort.Name)
		for i, q := range qualities {
			if len(q.RideQualityNotes) == 0 {
				continue
			}
			d := templateF.DailyData[i]
			snowIn := domain.CMToInches(d.SnowfallCM)
			fmt.Fprintf(&b, "%s (%.1f\" %s, %.0f kg/m3):\n",
				d.Date.Format("Jan 02"), snowIn, q.DensityCategory, q.AvgDensityKgM3)
			for _, note := range q.RideQualityNotes {
				fmt.Fprintf(&b, "  - %s\n", note)
			}
		}
	}
	return b.String()
}
```

Then call this function from wherever the LLM prompt is assembled (likely in `evaluation/gemini.go` or `pipeline/pipeline.go`), appending its output to the weather context.

**Step 2: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS

**Step 3: Commit**

```bash
git add evaluation/prompt.go
git commit -m "feat: add ride quality notes to LLM prompt context"
```

---

### Task 10: Integration test — verify with live trace

**Step 1: Run the San Juans trace**

Run: `go run ./cmd/powder-hunter trace --region co_san_juans --weather-only`

**Step 2: Verify**

Check that:
- Cloud cover data appears (may be subtle — check the raw numbers)
- Ride quality notes appear for snow days
- Crystal quality reflects wind conditions
- Bluebird detection works on clear post-storm days
- No crashes or errors

**Step 3: Commit any fixes if needed**

---

## Dependency Graph

```
Task 1 (Cloud cover domain model)
  ↓
Task 2 (Open-Meteo cloud cover) ──┐
Task 3 (NWS cloud cover) ─────────┤
  ↓                                │
Task 4 (SnowQuality type) ────────┤
  ↓                                │
Task 5 (Solar elevation) ─────────┤
  ↓                                │
Task 6 (Base risk assessment) ─────┤
  ↓                                │
Task 7 (Full ride quality) ────────┘
  ↓
Task 8 (Trace output) ──┐
Task 9 (LLM prompt) ────┘
  ↓
Task 10 (Integration test)
```

Tasks 2 and 3 can be done in parallel. Tasks 8 and 9 can be done in parallel.
