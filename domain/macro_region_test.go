package domain

import "testing"

func TestMacroRegionDisplayNameFromKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"co_front_range:local_drive", "CO Front Range"},
		{"pnw_cascades:flight", "PNW Cascades"},
		{"ak_chugach:flight", "Alaska Chugach"},
		{"co_front_range", "CO Front Range"},         // no suffix should also work
		{"unknown_region:flight", "unknown_region"},   // unknown falls back to raw key
	}
	for _, tt := range tests {
		got := MacroRegionDisplayNameFromKey(tt.key)
		if got != tt.want {
			t.Errorf("MacroRegionDisplayNameFromKey(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}
