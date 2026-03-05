package domain

import "testing"

func TestParseTier(t *testing.T) {
	tests := []struct {
		input string
		want  Tier
		ok    bool
	}{
		{"DROP_EVERYTHING", TierDropEverything, true},
		{"WORTH_A_LOOK", TierWorthALook, true},
		{"ON_THE_RADAR", TierOnTheRadar, true},
		{"INVALID", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseTier(tt.input)
			if tt.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("expected error, got nil")
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseStormState(t *testing.T) {
	tests := []struct {
		input string
		want  StormState
		ok    bool
	}{
		{"detected", StormDetected, true},
		{"evaluated", StormEvaluated, true},
		{"briefed", StormBriefed, true},
		{"updated", StormUpdated, true},
		{"expired", StormExpired, true},
		{"INVALID", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseStormState(tt.input)
			if tt.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("expected error, got nil")
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
