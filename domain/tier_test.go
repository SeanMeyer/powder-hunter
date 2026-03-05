package domain

import (
	"testing"
	"time"
)

func TestCooldownFor(t *testing.T) {
	tests := []struct {
		tier Tier
		want time.Duration
	}{
		{TierDropEverything, 0},
		{TierWorthALook, 12 * time.Hour},
		{TierOnTheRadar, 24 * time.Hour},
		{Tier("UNKNOWN"), 24 * time.Hour}, // conservative default
	}
	for _, tt := range tests {
		t.Run(string(tt.tier), func(t *testing.T) {
			got := CooldownFor(tt.tier)
			if got != tt.want {
				t.Errorf("CooldownFor(%s) = %v, want %v", tt.tier, got, tt.want)
			}
		})
	}
}
