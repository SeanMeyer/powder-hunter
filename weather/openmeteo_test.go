package weather

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestParseOpenMeteoHourly_SLRAdjusted(t *testing.T) {
	t.Run("cold snow calm wind — Vionnet density model", func(t *testing.T) {
		// -11.1°C, 0 wind → density = 109 + 6*(-11.1) + 0 = 42.4 kg/m3, SLR = 1000/42.4 = 23.58
		// 0.5" liquid = 12.7mm total, spread across 8 hours = 1.5875mm/hr
		// Each hour: 1.5875mm / 10 * 23.58 = 3.74cm → 8 hours = 29.95cm
		h := makeHourlyData(8, -11.1, 1.5875, 0, 0, 0)
		daily, err := parseOpenMeteoHourly(h)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(daily) != 1 {
			t.Fatalf("expected 1 day, got %d", len(daily))
		}
		assertApprox(t, "SnowfallCM", daily[0].SnowfallCM, 29.95, 0.5)
		assertApprox(t, "SLRatio", daily[0].SLRatio, 23.58, 0.1)
		if daily[0].RainHours != 0 {
			t.Errorf("RainHours = %d, want 0", daily[0].RainHours)
		}
	})

	t.Run("wet snow — 1in liquid at 28°F produces ~10in snow", func(t *testing.T) {
		// 28°F ≈ -2.2°C → wet snow band → 10:1 SLR
		// 1" liquid = 25.4mm total, spread across 12 hours ≈ 2.117mm/hr
		// Each hour: 2.117mm / 10 * 10 = 2.117cm → 12 hours ≈ 25.4cm ≈ 10"
		h := makeHourlyData(12, -2.2, 2.1167, 0, 0, 0)
		daily, err := parseOpenMeteoHourly(h)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertApprox(t, "SnowfallCM", daily[0].SnowfallCM, 26.51, 0.5)
		assertApprox(t, "SLRatio", daily[0].SLRatio, 10.44, 0.1)
	})

	t.Run("rain-to-snow transition — first 4 hours rain then 8 hours cold", func(t *testing.T) {
		// Build hourly data: 4 hours at 5°C (rain), then 8 hours at -11°C (cold smoke 20:1)
		// Each hour: 2.54mm precip (0.1"/hr)
		// Rain hours: 4 × 2.54mm = 10.16mm precip, 0 snow
		// Snow hours: 8 × 2.54mm = 20.32mm precip → 8 × (2.54/10*20) = 8 × 5.08 = 40.64cm
		times := make([]string, 12)
		temps := make([]float64, 12)
		precips := make([]float64, 12)
		winds := make([]float64, 12)
		gusts := make([]float64, 12)
		fzLvls := make([]float64, 12)

		for i := 0; i < 12; i++ {
			times[i] = hourTimestamp(0, 6+i) // hours 6-17 (all daytime)
			precips[i] = 2.54
			if i < 4 {
				temps[i] = 5.0   // rain
				fzLvls[i] = 3000 // high freezing level
			} else {
				temps[i] = -11.0 // cold smoke
				fzLvls[i] = 500  // low freezing level
			}
		}

		h := openMeteoHourlyData{
			Time:                times,
			Temperature2m:       temps,
			Precipitation:       precips,
			WindSpeed10m:        winds,
			WindGusts10m:        gusts,
			FreezingLevelHeight: fzLvls,
		}

		daily, err := parseOpenMeteoHourly(h)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(daily) != 1 {
			t.Fatalf("expected 1 day, got %d", len(daily))
		}

		assertApprox(t, "SnowfallCM", daily[0].SnowfallCM, 47.26, 0.5)
		if daily[0].RainHours != 4 {
			t.Errorf("RainHours = %d, want 4", daily[0].RainHours)
		}
		if daily[0].MixedHours != 0 {
			t.Errorf("MixedHours = %d, want 0", daily[0].MixedHours)
		}
	})

	t.Run("all rain — no snow produced", func(t *testing.T) {
		// 10°C for all hours, heavy precip
		h := makeHourlyData(6, 10.0, 5.0, 0, 0, 0)
		daily, err := parseOpenMeteoHourly(h)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if daily[0].SnowfallCM != 0 {
			t.Errorf("SnowfallCM = %v, want 0 (all rain)", daily[0].SnowfallCM)
		}
		if daily[0].RainHours != 6 {
			t.Errorf("RainHours = %d, want 6", daily[0].RainHours)
		}
	})

	t.Run("freezing level tracked per half-day", func(t *testing.T) {
		// 6 daytime hours with freezing level 1500-2000m, 6 nighttime hours with 800-1200m
		times := make([]string, 12)
		temps := make([]float64, 12)
		precips := make([]float64, 12)
		winds := make([]float64, 12)
		gusts := make([]float64, 12)
		fzLvls := make([]float64, 12)

		for i := 0; i < 6; i++ {
			times[i] = hourTimestamp(0, 6+i)
			temps[i] = -5.0
			precips[i] = 1.0
			fzLvls[i] = 1500 + float64(i)*100 // 1500-2000m during day
		}
		for i := 6; i < 12; i++ {
			times[i] = hourTimestamp(0, 18+i-6)
			temps[i] = -5.0
			precips[i] = 1.0
			fzLvls[i] = 800 + float64(i-6)*80 // 800-1200m during night
		}

		h := openMeteoHourlyData{
			Time:                times,
			Temperature2m:       temps,
			Precipitation:       precips,
			WindSpeed10m:        winds,
			WindGusts10m:        gusts,
			FreezingLevelHeight: fzLvls,
		}

		daily, err := parseOpenMeteoHourly(h)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertApprox(t, "Day.FreezingLevelMinM", daily[0].Day.FreezingLevelMinM, 1500, 1)
		assertApprox(t, "Day.FreezingLevelMaxM", daily[0].Day.FreezingLevelMaxM, 2000, 1)
		assertApprox(t, "Night.FreezingLevelMinM", daily[0].Night.FreezingLevelMinM, 800, 1)
		assertApprox(t, "Night.FreezingLevelMaxM", daily[0].Night.FreezingLevelMaxM, 1200, 1)
	})

	t.Run("no freezing level data degrades gracefully", func(t *testing.T) {
		h := openMeteoHourlyData{
			Time:          []string{hourTimestamp(0, 12)},
			Temperature2m: []float64{-5.0},
			Precipitation: []float64{2.0},
			WindSpeed10m:  []float64{10.0},
			WindGusts10m:  []float64{20.0},
			// FreezingLevelHeight omitted
		}

		daily, err := parseOpenMeteoHourly(h)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if daily[0].Day.FreezingLevelMinM != 0 {
			t.Errorf("expected 0 freezing level when data absent, got %v", daily[0].Day.FreezingLevelMinM)
		}
	})
}

// makeHourlyData creates a simple hourly dataset with uniform values starting at hour 6 (daytime).
func makeHourlyData(hours int, tempC, precipMM, windKmh, gustKmh, fzLvlM float64) openMeteoHourlyData {
	times := make([]string, hours)
	temps := make([]float64, hours)
	precips := make([]float64, hours)
	winds := make([]float64, hours)
	gustsArr := make([]float64, hours)
	fzLvls := make([]float64, hours)

	for i := 0; i < hours; i++ {
		times[i] = hourTimestamp(0, 6+i)
		temps[i] = tempC
		precips[i] = precipMM
		winds[i] = windKmh
		gustsArr[i] = gustKmh
		fzLvls[i] = fzLvlM
	}

	return openMeteoHourlyData{
		Time:                times,
		Temperature2m:       temps,
		Precipitation:       precips,
		WindSpeed10m:        winds,
		WindGusts10m:        gustsArr,
		FreezingLevelHeight: fzLvls,
	}
}

// hourTimestamp returns an Open-Meteo formatted timestamp for a given day offset and hour.
func hourTimestamp(dayOffset, hour int) string {
	return "2026-03-05T" + padHour(hour+dayOffset*24) + ":00"
}

func padHour(h int) string {
	h = h % 24
	return fmt.Sprintf("%02d", h)
}

func assertApprox(t *testing.T, name string, got, want, tolerance float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Errorf("%s = %.4f, want ~%.4f (tolerance %.4f)", name, got, want, tolerance)
	}
}

// makeMultiModelHourly builds a raw hourly JSON map simulating multi-model Open-Meteo response.
func makeMultiModelHourly(models []string, times []string, perModel map[string]struct {
	temp, precip []float64
}) map[string]json.RawMessage {
	hourly := make(map[string]json.RawMessage)
	hourly["time"], _ = json.Marshal(times)

	for model, data := range perModel {
		suffix := "_" + model
		hourly["temperature_2m"+suffix], _ = json.Marshal(data.temp)
		hourly["precipitation"+suffix], _ = json.Marshal(data.precip)
		// Wind/freezing level: use zeros for simplicity.
		zeros := make([]float64, len(times))
		hourly["wind_speed_10m"+suffix], _ = json.Marshal(zeros)
		hourly["wind_gusts_10m"+suffix], _ = json.Marshal(zeros)
		hourly["freezing_level_height"+suffix], _ = json.Marshal(zeros)
	}
	return hourly
}

func TestBuildOpenMeteoURL_ModelSelection(t *testing.T) {
	t.Run("US query includes HRRR in models param", func(t *testing.T) {
		q := openMeteoQuery{Country: "US", Lat: 47.0, Lon: -121.0, Timezone: "auto"}
		u := buildOpenMeteoURL(q)
		if !strings.Contains(u, "gfs_hrrr") {
			t.Errorf("US URL should include gfs_hrrr, got %s", u)
		}
	})

	t.Run("non-US query excludes HRRR", func(t *testing.T) {
		q := openMeteoQuery{Country: "CA", Lat: 50.0, Lon: -117.0, Timezone: "auto"}
		u := buildOpenMeteoURL(q)
		if strings.Contains(u, "gfs_hrrr") {
			t.Errorf("non-US URL should not include gfs_hrrr, got %s", u)
		}
	})

	t.Run("elevation param included when set", func(t *testing.T) {
		q := openMeteoQuery{Country: "US", Lat: 47.0, Lon: -121.0, ElevationM: 1500, Timezone: "auto"}
		u := buildOpenMeteoURL(q)
		if !strings.Contains(u, "elevation=1500") {
			t.Errorf("expected elevation=1500 in URL, got %s", u)
		}
	})

	t.Run("no elevation param when zero", func(t *testing.T) {
		q := openMeteoQuery{Country: "US", Lat: 47.0, Lon: -121.0, ElevationM: 0, Timezone: "auto"}
		u := buildOpenMeteoURL(q)
		if strings.Contains(u, "elevation") {
			t.Errorf("expected no elevation param, got %s", u)
		}
	})
}

func TestExtractMultiModelData_HRRRShortHorizon(t *testing.T) {
	// HRRR has 48 hours of data while global models have 384 (16 days).
	longTimes := make([]string, 96) // 4 days
	for i := range longTimes {
		longTimes[i] = fmt.Sprintf("2026-03-05T%02d:00", i%24)
	}

	hourly := make(map[string]json.RawMessage)
	hourly["time"], _ = json.Marshal(longTimes)

	// GFS has full 96 hours.
	gfsTemp := make([]float64, 96)
	gfsPrecip := make([]float64, 96)
	for i := range gfsTemp {
		gfsTemp[i] = -5.0
		gfsPrecip[i] = 1.0
	}
	hourly["temperature_2m_gfs_seamless"], _ = json.Marshal(gfsTemp)
	hourly["precipitation_gfs_seamless"], _ = json.Marshal(gfsPrecip)
	zeros96 := make([]float64, 96)
	hourly["wind_speed_10m_gfs_seamless"], _ = json.Marshal(zeros96)
	hourly["wind_gusts_10m_gfs_seamless"], _ = json.Marshal(zeros96)

	// HRRR has only 48 hours.
	hrrrTemp := make([]float64, 48)
	hrrrPrecip := make([]float64, 48)
	for i := range hrrrTemp {
		hrrrTemp[i] = -10.0
		hrrrPrecip[i] = 2.0
	}
	hourly["temperature_2m_gfs_hrrr"], _ = json.Marshal(hrrrTemp)
	hourly["precipitation_gfs_hrrr"], _ = json.Marshal(hrrrPrecip)
	zeros48 := make([]float64, 48)
	hourly["wind_speed_10m_gfs_hrrr"], _ = json.Marshal(zeros48)
	hourly["wind_gusts_10m_gfs_hrrr"], _ = json.Marshal(zeros48)

	result := extractMultiModelData(hourly, []string{"gfs_seamless", "gfs_hrrr"})
	if len(result) != 2 {
		t.Fatalf("expected 2 models, got %d", len(result))
	}

	gfs := result["gfs_seamless"]
	hrrr := result["gfs_hrrr"]

	if len(gfs.Time) != 96 {
		t.Errorf("GFS time length = %d, want 96", len(gfs.Time))
	}
	if len(hrrr.Time) != 48 {
		t.Errorf("HRRR time length = %d, want 48 (truncated to match data)", len(hrrr.Time))
	}
	if len(hrrr.Temperature2m) != 48 {
		t.Errorf("HRRR temp length = %d, want 48", len(hrrr.Temperature2m))
	}
}

func TestExtractMultiModelData(t *testing.T) {
	times := []string{hourTimestamp(0, 6), hourTimestamp(0, 7), hourTimestamp(0, 8)}

	t.Run("two models parsed correctly", func(t *testing.T) {
		hourly := makeMultiModelHourly(
			[]string{"gfs_seamless", "ecmwf_ifs025"},
			times,
			map[string]struct{ temp, precip []float64 }{
				"gfs_seamless":  {temp: []float64{-5.0, -5.0, -5.0}, precip: []float64{2.0, 2.0, 2.0}},
				"ecmwf_ifs025":  {temp: []float64{-3.0, -3.0, -3.0}, precip: []float64{3.0, 3.0, 3.0}},
			},
		)

		result := extractMultiModelData(hourly, []string{"gfs_seamless", "ecmwf_ifs025"})
		if len(result) != 2 {
			t.Fatalf("expected 2 models, got %d", len(result))
		}

		gfs := result["gfs_seamless"]
		if len(gfs.Temperature2m) != 3 {
			t.Errorf("GFS temp array length = %d, want 3", len(gfs.Temperature2m))
		}
		if gfs.Temperature2m[0] != -5.0 {
			t.Errorf("GFS temp[0] = %v, want -5.0", gfs.Temperature2m[0])
		}

		ecmwf := result["ecmwf_ifs025"]
		if ecmwf.Precipitation[0] != 3.0 {
			t.Errorf("ECMWF precip[0] = %v, want 3.0", ecmwf.Precipitation[0])
		}
	})

	t.Run("model with missing data is skipped", func(t *testing.T) {
		hourly := makeMultiModelHourly(
			[]string{"gfs_seamless"},
			times,
			map[string]struct{ temp, precip []float64 }{
				"gfs_seamless": {temp: []float64{-5.0, -5.0, -5.0}, precip: []float64{2.0, 2.0, 2.0}},
			},
		)
		// ecmwf keys are absent from the hourly data.
		result := extractMultiModelData(hourly, []string{"gfs_seamless", "ecmwf_ifs025"})
		if len(result) != 1 {
			t.Fatalf("expected 1 model (ecmwf skipped), got %d", len(result))
		}
		if _, ok := result["gfs_seamless"]; !ok {
			t.Error("expected gfs_seamless in result")
		}
	})

	t.Run("single model fallback", func(t *testing.T) {
		// Only one model in response.
		hourly := makeMultiModelHourly(
			[]string{"gfs_seamless"},
			times,
			map[string]struct{ temp, precip []float64 }{
				"gfs_seamless": {temp: []float64{-10.0, -10.0, -10.0}, precip: []float64{1.0, 1.0, 1.0}},
			},
		)

		result := extractMultiModelData(hourly, []string{"gfs_seamless"})
		if len(result) != 1 {
			t.Fatalf("expected 1 model, got %d", len(result))
		}
	})

	t.Run("partial API response — one model truncated", func(t *testing.T) {
		hourly := make(map[string]json.RawMessage)
		hourly["time"], _ = json.Marshal(times)

		// GFS has full data.
		hourly["temperature_2m_gfs_seamless"], _ = json.Marshal([]float64{-5, -5, -5})
		hourly["precipitation_gfs_seamless"], _ = json.Marshal([]float64{2, 2, 2})
		zeros := []float64{0, 0, 0}
		hourly["wind_speed_10m_gfs_seamless"], _ = json.Marshal(zeros)
		hourly["wind_gusts_10m_gfs_seamless"], _ = json.Marshal(zeros)

		// ECMWF has temp but no precip (truncated response).
		hourly["temperature_2m_ecmwf_ifs025"], _ = json.Marshal([]float64{-3, -3, -3})
		// precipitation_ecmwf_ifs025 is missing.

		result := extractMultiModelData(hourly, []string{"gfs_seamless", "ecmwf_ifs025"})
		if len(result) != 1 {
			t.Fatalf("expected 1 model (ecmwf skipped due to missing precip), got %d", len(result))
		}
	})

	t.Run("empty time array returns nil", func(t *testing.T) {
		hourly := make(map[string]json.RawMessage)
		hourly["time"], _ = json.Marshal([]string{})
		result := extractMultiModelData(hourly, []string{"gfs_seamless"})
		if result != nil {
			t.Errorf("expected nil for empty time array, got %v", result)
		}
	})
}
