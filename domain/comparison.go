package domain

// materialSnowfallDeltaIn is the minimum snowfall difference (inches) between
// two evaluations that triggers a material change classification even when the
// tier hasn't changed. 4" is enough to meaningfully affect pow skiing.
const materialSnowfallDeltaIn = 4.0

// Compare classifies how much the current evaluation differs from the previous one.
// The result drives whether a Discord update is sent and at what urgency level.
//
// Rules applied in priority order:
//  1. No previous evaluation → ChangeNew
//  2. Tier downgraded → ChangeDowngrade
//  3. Tier changed (upgrade) → ChangeMaterial
//  4. Total snowfall delta across the window exceeds threshold → ChangeMaterial
//  5. Otherwise → ChangeMinor
func Compare(prev, curr Evaluation) ChangeClass {
	if prev.ID == 0 {
		return ChangeNew
	}

	if TierRank(curr.Tier) < TierRank(prev.Tier) {
		return ChangeDowngrade
	}

	if curr.Tier != prev.Tier {
		return ChangeMaterial
	}

	if snowfallDelta(prev, curr) > materialSnowfallDeltaIn {
		return ChangeMaterial
	}

	return ChangeMinor
}

// TierRank maps tiers to an ordinal so we can compare direction of change.
// Higher rank = better storm.
func TierRank(t Tier) int {
	switch t {
	case TierDropEverything:
		return 3
	case TierWorthALook:
		return 2
	case TierOnTheRadar:
		return 1
	default:
		return 0
	}
}

// snowfallDelta computes the absolute difference in total snowfall between two
// evaluations by summing the SnowfallCM values from the weather snapshots.
func snowfallDelta(prev, curr Evaluation) float64 {
	prevIn := totalSnowfallIn(prev.WeatherSnapshot)
	currIn := totalSnowfallIn(curr.WeatherSnapshot)

	delta := currIn - prevIn
	if delta < 0 {
		delta = -delta
	}
	return delta
}

func totalSnowfallIn(forecasts []Forecast) float64 {
	var totalCM float64
	for _, f := range forecasts {
		for _, d := range f.DailyData {
			totalCM += d.SnowfallCM
		}
	}
	return CMToInches(totalCM)
}
