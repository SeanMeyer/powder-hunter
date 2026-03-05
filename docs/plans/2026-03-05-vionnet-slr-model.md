# Vionnet SLR Model Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the 5-band step-function SLR with the continuous Vionnet (2012) formula that incorporates wind speed, producing more accurate snowfall estimates — especially for cold, windy mountain storms.

**Architecture:** The Vionnet formula computes snow density from temperature and wind speed: `density = 109 + 6*T + 26*sqrt(u)`, then derives SLR as `1000/density`. This replaces `CalculateSLR(tempC)` with `CalculateDensity(tempC, windSpeedMs)` and updates `SnowfallFromPrecip` to accept wind speed. Both weather parsers (Open-Meteo and NWS) are updated to pass hourly wind into the calculation. Density is clamped to [40, 250] kg/m3 (SLR range 4:1 to 25:1).

**Tech Stack:** Go 1.24+, pure domain logic (no new dependencies)

**Key references:**
- Vionnet et al. (2012), Crocus snow model: `density = 109 + 6*T + 26*sqrt(u)` where T is Celsius, u is wind speed in m/s
- Density floor 40 kg/m3 (25:1 SLR) prevents unrealistic values at extreme cold
- Density ceiling 250 kg/m3 (4:1 SLR) prevents unrealistic values near freezing with high wind

---

### Task 1: Add `CalculateDensity` and `SLRFromDensity` — domain pure functions

**Files:**
- Modify: `domain/weather.go:99-136`
- Test: `domain/weather_test.go`

**Step 1: Write the failing tests for `CalculateDensity` and `SLRFromDensity`**

Add new test functions in `domain/weather_test.go`. These test the Vionnet formula across realistic mountain conditions, plus the density clamps.

```go
func TestCalculateDensity(t *testing.T) {
	tests := []struct {
		name        string
		tempC       float64
		windSpeedMs float64
		wantDensity float64
	}{
		// Core formula: density = 109 + 6*T + 26*sqrt(u)
		// Clamped to [40, 250]

		// Moderate calm: -8C, 2 m/s → 109 + (-48) + 26*1.414 = 97.8
		{"moderate calm", -8.0, 2.0, 97.8},
		// Cold windy: -15C, 10 m/s → 109 + (-90) + 26*3.162 = 101.2
		{"cold windy", -15.0, 10.0, 101.2},
		// Warm wet: -2C, 5 m/s → 109 + (-12) + 26*2.236 = 155.1
		{"warm wet", -2.0, 5.0, 155.1},
		// Cold calm: -15C, 2 m/s → 109 + (-90) + 26*1.414 = 55.8
		{"cold calm powder", -15.0, 2.0, 55.8},
		// Cold very windy: -15C, 15 m/s → 109 + (-90) + 26*3.873 = 119.7
		{"cold very windy", -15.0, 15.0, 119.7},
		// Zero wind: -10C, 0 m/s → 109 + (-60) + 0 = 49.0
		{"zero wind moderate cold", -10.0, 0.0, 49.0},

		// Clamp: floor at 40 kg/m3
		// Very cold calm: -22C, 1 m/s → 109 + (-132) + 26*1.0 = 3.0 → clamped to 40
		{"very cold calm clamps to floor", -22.0, 1.0, 40.0},
		// Extreme cold: -30C, 0 m/s → 109 + (-180) + 0 = -71 → clamped to 40
		{"extreme cold clamps to floor", -30.0, 0.0, 40.0},

		// Clamp: ceiling at 250 kg/m3
		// Warm high wind: 0C, 30 m/s → 109 + 0 + 26*5.477 = 251.4 → clamped to 250
		{"warm high wind clamps to ceiling", 0.0, 30.0, 250.0},

		// Rain threshold: above 1.67C → density 0 (rain, no snow)
		{"rain returns zero density", 2.0, 5.0, 0.0},
		{"rain at threshold", 1.67, 5.0, 0.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CalculateDensity(tc.tempC, tc.windSpeedMs)
			delta := got - tc.wantDensity
			if delta < 0 {
				delta = -delta
			}
			if delta > 0.5 {
				t.Errorf("CalculateDensity(%v, %v) = %.1f, want %.1f", tc.tempC, tc.windSpeedMs, got, tc.wantDensity)
			}
		})
	}
}

func TestSLRFromDensity(t *testing.T) {
	tests := []struct {
		name    string
		density float64
		wantSLR float64
	}{
		{"100 kg/m3 → 10:1", 100.0, 10.0},
		{"50 kg/m3 → 20:1", 50.0, 20.0},
		{"200 kg/m3 → 5:1", 200.0, 5.0},
		{"zero density → zero SLR", 0.0, 0.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SLRFromDensity(tc.density)
			delta := got - tc.wantSLR
			if delta < 0 {
				delta = -delta
			}
			if delta > 0.01 {
				t.Errorf("SLRFromDensity(%v) = %.2f, want %.2f", tc.density, got, tc.wantSLR)
			}
		})
	}
}
```

**Step 2: Run the tests to verify they fail**

Run: `go test ./domain/ -run 'TestCalculateDensity|TestSLRFromDensity' -v`
Expected: FAIL — `CalculateDensity` and `SLRFromDensity` undefined

**Step 3: Implement `CalculateDensity` and `SLRFromDensity`**

Replace the old constants and `CalculateSLR` function in `domain/weather.go:99-123` with:

```go
import "math"

const (
	// slrRainThresholdC is the temperature above which precipitation falls as rain.
	slrRainThresholdC = 1.6667 // 35°F

	// Vionnet density clamps prevent unrealistic values at temperature/wind extremes.
	densityFloor = 40.0  // kg/m3 — coldest realistic snow (25:1 SLR)
	densityCeiling = 250.0 // kg/m3 — heaviest realistic wet snow (4:1 SLR)
)

// CalculateDensity returns fresh snow density in kg/m3 using the Vionnet et al. (2012)
// formula: density = 109 + 6*T + 26*sqrt(u), clamped to [40, 250] kg/m3.
// Returns 0 for rain (temp above 1.67°C / 35°F).
//
// T is air temperature in Celsius, windSpeedMs is wind speed in m/s.
//
// Reference: Vionnet et al. 2012, "The detailed snowpack scheme Crocus and its
// implementation in SURFEX v7.2", Geosci. Model Dev.
func CalculateDensity(tempC float64, windSpeedMs float64) float64 {
	if tempC > slrRainThresholdC {
		return 0 // rain
	}
	density := 109.0 + 6.0*tempC + 26.0*math.Sqrt(windSpeedMs)
	if density < densityFloor {
		return densityFloor
	}
	if density > densityCeiling {
		return densityCeiling
	}
	return density
}

// SLRFromDensity converts snow density (kg/m3) to snow-to-liquid ratio.
// Returns 0 if density is 0 (rain).
func SLRFromDensity(density float64) float64 {
	if density <= 0 {
		return 0
	}
	return 1000.0 / density
}
```

Keep the old `CalculateSLR` function for now — it will be removed in Task 3 after callers are migrated.

**Step 4: Run the tests to verify they pass**

Run: `go test ./domain/ -run 'TestCalculateDensity|TestSLRFromDensity' -v`
Expected: PASS

**Step 5: Commit**

```bash
git add domain/weather.go domain/weather_test.go
git commit -m "feat: add Vionnet snow density model (CalculateDensity + SLRFromDensity)"
```

---

### Task 2: Update `SnowfallFromPrecip` to use Vionnet formula

**Files:**
- Modify: `domain/weather.go:125-136`
- Modify: `domain/weather_test.go:50-97`

**Step 1: Update the `SnowfallFromPrecip` tests to include wind speed**

The function signature changes from `SnowfallFromPrecip(precipMM, tempC float64)` to `SnowfallFromPrecip(precipMM, tempC, windSpeedMs float64)`. Update existing test cases with expected values under the Vionnet model, and add wind-specific cases.

Replace the `TestSnowfallFromPrecip` function:

```go
func TestSnowfallFromPrecip(t *testing.T) {
	tests := []struct {
		name        string
		precipMM    float64
		tempC       float64
		windSpeedMs float64
		wantCM      float64
	}{
		// Zero precip → zero snow regardless of temp/wind.
		{"zero precip cold", 0.0, -15.0, 5.0, 0.0},
		{"negative precip", -1.0, -5.0, 5.0, 0.0},

		// Rain → zero snow.
		{"rain at 5C", 10.0, 5.0, 3.0, 0.0},

		// Cold calm: 12.7mm at -11C, 2 m/s
		// density = 109 + 6*(-11) + 26*sqrt(2) = 109 - 66 + 36.77 = 79.77
		// SLR = 1000/79.77 = 12.54
		// snow = 12.7/10 * 12.54 = 15.9 cm
		{"cold calm 0.5in liquid", 12.7, -11.0, 2.0, 15.9},

		// Cold windy: 12.7mm at -11C, 10 m/s
		// density = 109 - 66 + 26*3.162 = 125.2
		// SLR = 1000/125.2 = 7.99
		// snow = 12.7/10 * 7.99 = 10.1 cm
		{"cold windy 0.5in liquid", 12.7, -11.0, 10.0, 10.1},

		// Wet snow: 25.4mm at -2C, 3 m/s
		// density = 109 - 12 + 26*1.732 = 142.0
		// SLR = 1000/142.0 = 7.04
		// snow = 25.4/10 * 7.04 = 17.9 cm
		{"wet snow 1in liquid", 25.4, -2.0, 3.0, 17.9},

		// Very cold calm (hits density floor): 1mm at -25C, 1 m/s
		// density = 109 - 150 + 26*1.0 = -15 → clamped to 40
		// SLR = 1000/40 = 25.0
		// snow = 1/10 * 25.0 = 2.5 cm
		{"very cold hits density floor", 1.0, -25.0, 1.0, 2.5},

		// Calm moderate: 5mm at -8C, 0 m/s
		// density = 109 - 48 + 0 = 61.0 → below floor? No, 61 > 40
		// SLR = 1000/61 = 16.39
		// snow = 5/10 * 16.39 = 8.2 cm
		{"calm moderate cold", 5.0, -8.0, 0.0, 8.2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SnowfallFromPrecip(tc.precipMM, tc.tempC, tc.windSpeedMs)
			delta := got - tc.wantCM
			if delta < 0 {
				delta = -delta
			}
			if delta > 0.2 {
				t.Errorf("SnowfallFromPrecip(%v, %v, %v) = %.2f, want ~%.2f",
					tc.precipMM, tc.tempC, tc.windSpeedMs, got, tc.wantCM)
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./domain/ -run TestSnowfallFromPrecip -v`
Expected: FAIL — wrong number of arguments (old function takes 2, tests pass 3)

**Step 3: Update `SnowfallFromPrecip` implementation**

Replace the function in `domain/weather.go:125-136`:

```go
// SnowfallFromPrecip returns snowfall in cm for a given hour's precipitation (mm),
// temperature (°C), and wind speed (m/s). Uses Vionnet density model with
// unit conversion: precipMM / 10.0 * SLR (mm→cm × ratio).
func SnowfallFromPrecip(precipMM float64, tempC float64, windSpeedMs float64) float64 {
	if precipMM <= 0 {
		return 0
	}
	density := CalculateDensity(tempC, windSpeedMs)
	if density <= 0 {
		return 0 // rain
	}
	slr := SLRFromDensity(density)
	return precipMM / 10.0 * slr
}
```

This will cause compile errors in `weather/openmeteo.go` and `weather/nws.go` — that's expected and will be fixed in Tasks 3 and 4.

**Step 4: Run the domain tests to verify they pass**

Run: `go test ./domain/ -run TestSnowfallFromPrecip -v`
Expected: PASS

**Step 5: Commit**

```bash
git add domain/weather.go domain/weather_test.go
git commit -m "feat: SnowfallFromPrecip now uses Vionnet density model with wind speed"
```

---

### Task 3: Update Open-Meteo parser to pass wind speed

**Files:**
- Modify: `weather/openmeteo.go:388-401`
- Modify: `weather/openmeteo_test.go`

The Open-Meteo parser already has hourly wind speed available at the exact point where `SnowfallFromPrecip` is called (line 384: `wind = h.WindSpeed10m[i]`). The wind is in km/h and needs to be converted to m/s.

**Step 1: Update the Open-Meteo parser tests**

The existing tests use `makeHourlyData` which already accepts `windKmh`. Update the test expectations to match Vionnet-based output. Replace the first two test cases in `TestParseOpenMeteoHourly_SLRAdjusted`:

```go
t.Run("cold snow with wind — Vionnet density model", func(t *testing.T) {
	// -11.1°C, 1.5875mm/hr precip, 20 km/h wind (5.56 m/s), 8 daytime hours
	// density = 109 + 6*(-11.1) + 26*sqrt(5.56) = 109 - 66.6 + 61.3 = 103.7
	// SLR = 1000/103.7 = 9.64
	// Per hour: 1.5875/10 * 9.64 = 1.530 cm → 8 hours = 12.24 cm
	h := makeHourlyData(8, -11.1, 1.5875, 20.0, 30.0, 0)
	daily, err := parseOpenMeteoHourly(h)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertApprox(t, "SnowfallCM", daily[0].SnowfallCM, 12.24, 0.5)
	if daily[0].RainHours != 0 {
		t.Errorf("RainHours = %d, want 0", daily[0].RainHours)
	}
})

t.Run("cold snow calm wind — higher SLR", func(t *testing.T) {
	// Same temp/precip as above but 0 km/h wind
	// density = 109 + 6*(-11.1) + 26*sqrt(0) = 109 - 66.6 = 42.4
	// SLR = 1000/42.4 = 23.58
	// Per hour: 1.5875/10 * 23.58 = 3.74 cm → 8 hours = 29.95 cm
	h := makeHourlyData(8, -11.1, 1.5875, 0, 0, 0)
	daily, err := parseOpenMeteoHourly(h)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertApprox(t, "SnowfallCM", daily[0].SnowfallCM, 29.95, 0.5)
})
```

Update the "wet snow" test:

```go
t.Run("wet snow with light wind", func(t *testing.T) {
	// -2.2°C, 2.117mm/hr, 10 km/h wind (2.78 m/s), 12 daytime hours
	// density = 109 + 6*(-2.2) + 26*sqrt(2.78) = 109 - 13.2 + 43.3 = 139.1
	// SLR = 1000/139.1 = 7.19
	// Per hour: 2.117/10 * 7.19 = 1.522 cm → 12 hours = 18.27 cm
	h := makeHourlyData(12, -2.2, 2.1167, 10.0, 15.0, 0)
	daily, err := parseOpenMeteoHourly(h)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertApprox(t, "SnowfallCM", daily[0].SnowfallCM, 18.27, 0.5)
})
```

Update the "rain-to-snow transition" test's expected snowfall. The snow portion is 8 hours at -11°C with 2.54mm/hr and 0 wind:
- density = 109 + 6*(-11) + 26*sqrt(0) = 43.0, SLR = 1000/43 = 23.26
- Per hour: 2.54/10 * 23.26 = 5.91 cm → 8 hours = 47.26 cm

Update the assertion:
```go
assertApprox(t, "SnowfallCM", daily[0].SnowfallCM, 47.26, 0.5)
```

Update the "all rain — no snow produced" test — no change needed (rain is still rain).

**Step 2: Run tests to verify they fail**

Run: `go test ./weather/ -run TestParseOpenMeteoHourly -v`
Expected: FAIL — compile error (`SnowfallFromPrecip` now requires 3 args)

**Step 3: Update the Open-Meteo parser**

In `weather/openmeteo.go`, modify the hourly loop around line 388. The wind speed is already available as `wind` (km/h). Convert to m/s and pass to `SnowfallFromPrecip`:

Change line 388 from:
```go
snowCM := domain.SnowfallFromPrecip(precip, temp)
```
to:
```go
windMs := wind / 3.6 // km/h → m/s
snowCM := domain.SnowfallFromPrecip(precip, temp, windMs)
```

Also update line 389. `CalculateSLR` is no longer the right function for computing the weighted SLR. Replace the SLR tracking block (lines 389-401):

```go
density := domain.CalculateDensity(temp, windMs)
slr := domain.SLRFromDensity(density)

if precip > 0 {
	if density <= 0 {
		acc.rainHours++
	} else if temp >= 0 && temp <= domain.SlrRainThresholdC {
		acc.mixedHours++
	}
}

if snowCM > 0 && precip > 0 {
	acc.totalSnowPrecipMM += precip
	acc.weightedSLRSum += precip * slr
}
```

Note: You need to export `SlrRainThresholdC` (or define a `IsMixedPrecip` helper) for the mixed-hours check. The simplest approach is to add a helper in `domain/weather.go`:

```go
// IsMixedPrecip returns true if the temperature is in the mixed precipitation zone
// (32-35°F / 0-1.67°C) — snow may be mixed with rain/sleet.
func IsMixedPrecip(tempC float64) bool {
	return tempC >= 0 && tempC <= slrRainThresholdC
}

// IsRain returns true if the temperature is above the rain threshold (35°F / 1.67°C).
func IsRain(tempC float64) bool {
	return tempC > slrRainThresholdC
}
```

Then the parser code becomes:
```go
if precip > 0 {
	if domain.IsRain(temp) {
		acc.rainHours++
	} else if domain.IsMixedPrecip(temp) {
		acc.mixedHours++
	}
}
```

**Step 4: Run the Open-Meteo tests to verify they pass**

Run: `go test ./weather/ -run TestParseOpenMeteoHourly -v`
Expected: PASS

**Step 5: Commit**

```bash
git add domain/weather.go weather/openmeteo.go weather/openmeteo_test.go
git commit -m "feat: Open-Meteo parser uses Vionnet density with hourly wind speed"
```

---

### Task 4: Update NWS parser to pass wind speed

**Files:**
- Modify: `weather/nws.go:168-315`

The NWS parser is trickier than Open-Meteo because wind is processed in Step 3 (lines 317-339), **after** precipitation in Step 2 (lines 262-315). We need to build an hourly wind lookup (like the hourly temperature lookup) so wind is available during precipitation processing.

**Step 1: Run the existing NWS tests to see current state**

Run: `go test ./weather/ -run TestSmoke_NWS -v`
Expected: FAIL — compile error from `SnowfallFromPrecip` signature change

**Step 2: Build hourly wind lookup before precipitation processing**

In `weather/nws.go`, move wind processing **before** Step 2. After the temperature loop (ending at line 260), add a wind speed lookup:

```go
// Step 1b: Build per-hour wind speed lookup (dateKey+hour → km/h).
hourlyWind := make(map[string]float64)
for _, v := range resp.Properties.WindSpeed.Values {
	if v.Value == nil {
		continue
	}
	start, dur, err := parseISO8601Interval(v.ValidTime)
	if err != nil || dur <= 0 {
		continue
	}
	end := start.Add(dur)
	hourlyVal := *v.Value / dur.Hours()
	for t := start; t.Before(end); t = t.Add(time.Hour) {
		local := t.In(loc)
		dateKey := local.Format("2006-01-02")
		hourKey := fmt.Sprintf("%s-%02d", dateKey, local.Hour())
		hourlyWind[hourKey] = hourlyVal
	}
}
```

**Step 3: Update precipitation loop to use wind speed**

In the precipitation processing loop (Step 2), after the temperature lookup, add wind lookup and pass it to `SnowfallFromPrecip`. Around line 291:

Change:
```go
snowCM := domain.SnowfallFromPrecip(hourlyMM, tempC)
slr := domain.CalculateSLR(tempC)
```

To:
```go
windKmh, _ := hourlyWind[hourKey]
windMs := windKmh / 3.6

snowCM := domain.SnowfallFromPrecip(hourlyMM, tempC, windMs)
density := domain.CalculateDensity(tempC, windMs)
slr := domain.SLRFromDensity(density)
```

Update the rain/mixed hours check (lines 294-300):
```go
if hourlyMM > 0 {
	if domain.IsRain(tempC) {
		acc.rainHours++
	} else if domain.IsMixedPrecip(tempC) {
		acc.mixedHours++
	}
}
```

Keep Step 3 (wind speed/gust max tracking for HalfDay display) as-is — it still populates `dayWindMax`/`nightWindMax` for the trace output.

**Step 4: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS (all packages compile and tests pass)

**Step 5: Commit**

```bash
git add weather/nws.go
git commit -m "feat: NWS parser uses Vionnet density with hourly wind speed"
```

---

### Task 5: Remove old `CalculateSLR` function and clean up

**Files:**
- Modify: `domain/weather.go`
- Modify: `domain/weather_test.go`

**Step 1: Verify no remaining callers of `CalculateSLR`**

Run: `grep -r 'CalculateSLR' --include='*.go' .`
Expected: Only `domain/weather.go` (definition) and `domain/weather_test.go` (tests) remain. If any other callers exist, update them first.

**Step 2: Remove `CalculateSLR` and its tests**

Remove the `CalculateSLR` function from `domain/weather.go` and the old SLR threshold constants (`slrThresholdMixedC`, `slrThresholdWetC`, `slrThresholdDryC`). Keep `slrRainThresholdC` (used by `CalculateDensity`, `IsRain`, `IsMixedPrecip`).

Remove `TestCalculateSLR` from `domain/weather_test.go`.

**Step 3: Run all tests**

Run: `go test ./... -count=1`
Expected: PASS

**Step 4: Commit**

```bash
git add domain/weather.go domain/weather_test.go
git commit -m "refactor: remove old CalculateSLR step-function (replaced by Vionnet)"
```

---

### Task 6: Verify with a live trace and compare results

**Step 1: Run the same Silverton trace to compare against the old output**

Run: `go run ./cmd/powder-hunter trace --region co_san_juans --weather-only`

**Step 2: Check the output**

Verify:
- SLR values are now continuous (no more fixed 5/10/15/20 values)
- Silverton March 6 should show significantly lower totals (roughly 6-10" range instead of 12-18")
- Windy periods should show lower SLR than calm periods at the same temperature
- The `@N:1` annotations in trace output should show decimal-ish SLR values
- Rain hours and mixed hours should still be counted correctly
- No crashes or zero-division errors

**Step 3: Sanity check a few numbers by hand**

Pick one half-day from the trace output and verify:
- Look at the temperature and wind gust shown
- Estimate the density: `109 + 6*T(celsius) + 26*sqrt(wind_m/s)`
- Compute expected SLR: `1000 / density`
- Check it roughly matches the `@N:1` shown

**Step 4: Commit (if any trace formatter adjustments were needed)**

No commit expected unless trace output revealed issues.

---

### Task 7: Update the Open-Meteo parser comment block

**Files:**
- Modify: `weather/openmeteo.go:311-317`

**Step 1: Update the function comment**

The comment at lines 315-317 references the old SLR bands. Update it:

```go
// Snowfall is computed per-hour from precipitation, temperature, and wind speed
// using the Vionnet (2012) snow density model. This produces physically-informed
// estimates that account for wind compaction (windy storms = denser snow = less depth).
```

**Step 2: Commit**

```bash
git add weather/openmeteo.go
git commit -m "docs: update parser comment to reflect Vionnet density model"
```

---

## Dependency Graph

```
Task 1 (CalculateDensity + SLRFromDensity)
  ↓
Task 2 (Update SnowfallFromPrecip signature)
  ↓
Task 3 (Open-Meteo parser) ─┬─→ Task 5 (Remove old CalculateSLR)
Task 4 (NWS parser) ────────┘         ↓
                                 Task 6 (Live trace verification)
                                       ↓
                                 Task 7 (Comment cleanup)
```

Tasks 3 and 4 are independent of each other and can be done in parallel.
