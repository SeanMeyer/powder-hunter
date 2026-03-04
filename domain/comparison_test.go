package domain

import "testing"

// evalWithTierAndSnowfall constructs a minimal Evaluation with the given tier and
// a single forecast day carrying the specified snowfall (in inches, stored as CM).
func evalWithTierAndSnowfall(id int64, tier Tier, snowfallIn float64) Evaluation {
	return Evaluation{
		ID:   id,
		Tier: tier,
		WeatherSnapshot: []Forecast{
			{
				DailyData: []DailyForecast{
					{SnowfallCM: inchesToCM(snowfallIn)},
				},
			},
		},
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		name       string
		prev       Evaluation
		curr       Evaluation
		wantChange ChangeClass
	}{
		{
			name:       "first evaluation (ID=0 prev) → ChangeNew",
			prev:       Evaluation{ID: 0},
			curr:       evalWithTierAndSnowfall(1, TierOnTheRadar, 10),
			wantChange: ChangeNew,
		},
		{
			name:       "tier upgrade WORTH_A_LOOK → DROP_EVERYTHING → ChangeMaterial",
			prev:       evalWithTierAndSnowfall(1, TierWorthALook, 20),
			curr:       evalWithTierAndSnowfall(2, TierDropEverything, 20),
			wantChange: ChangeMaterial,
		},
		{
			name:       "tier downgrade DROP_EVERYTHING → WORTH_A_LOOK → ChangeDowngrade",
			prev:       evalWithTierAndSnowfall(1, TierDropEverything, 20),
			curr:       evalWithTierAndSnowfall(2, TierWorthALook, 20),
			wantChange: ChangeDowngrade,
		},
		{
			name: "same tier, snowfall delta > 4in → ChangeMaterial",
			// delta = 24 - 18 = 6" > 4"
			prev:       evalWithTierAndSnowfall(1, TierWorthALook, 18),
			curr:       evalWithTierAndSnowfall(2, TierWorthALook, 24),
			wantChange: ChangeMaterial,
		},
		{
			name: "same tier, snowfall delta < 4in → ChangeMinor",
			// delta = 20 - 18 = 2" < 4"
			prev:       evalWithTierAndSnowfall(1, TierWorthALook, 18),
			curr:       evalWithTierAndSnowfall(2, TierWorthALook, 20),
			wantChange: ChangeMinor,
		},
		{
			// materialSnowfallDeltaIn = 4.0; condition is strictly >
			// exactly 4" delta should fall on the ChangeMinor side
			name:       "same tier, snowfall delta exactly 4in → ChangeMinor (boundary: strictly >)",
			prev:       evalWithTierAndSnowfall(1, TierOnTheRadar, 10),
			curr:       evalWithTierAndSnowfall(2, TierOnTheRadar, 14),
			wantChange: ChangeMinor,
		},
		{
			name:       "tier upgrade ON_THE_RADAR → WORTH_A_LOOK → ChangeMaterial",
			prev:       evalWithTierAndSnowfall(1, TierOnTheRadar, 10),
			curr:       evalWithTierAndSnowfall(2, TierWorthALook, 10),
			wantChange: ChangeMaterial,
		},
		{
			name:       "same tier, same snowfall → ChangeMinor",
			prev:       evalWithTierAndSnowfall(1, TierWorthALook, 20),
			curr:       evalWithTierAndSnowfall(2, TierWorthALook, 20),
			wantChange: ChangeMinor,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Compare(tc.prev, tc.curr)
			if got != tc.wantChange {
				t.Errorf("Compare() = %q, want %q", got, tc.wantChange)
			}
		})
	}
}
