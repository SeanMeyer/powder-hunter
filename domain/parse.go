package domain

import "fmt"

// ParseTier validates and converts a string to a Tier.
func ParseTier(s string) (Tier, error) {
	switch Tier(s) {
	case TierDropEverything, TierWorthALook, TierOnTheRadar:
		return Tier(s), nil
	default:
		return "", fmt.Errorf("invalid tier: %q", s)
	}
}

// ParseStormState validates and converts a string to a StormState.
func ParseStormState(s string) (StormState, error) {
	switch StormState(s) {
	case StormDetected, StormEvaluated, StormBriefed, StormUpdated, StormExpired:
		return StormState(s), nil
	default:
		return "", fmt.Errorf("invalid storm state: %q", s)
	}
}
