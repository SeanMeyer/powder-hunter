package domain

import "strings"

// MacroRegionNames maps macro-region keys to human-readable display names.
// Used for Discord thread titles and log messages.
var MacroRegionNames = map[string]string{
	"pnw_cascades":       "PNW Cascades",
	"pnw_interior":       "PNW Interior",
	"northern_rockies":   "Northern Rockies",
	"sierra_nevada":      "Sierra Nevada",
	"bc_coast":           "BC Coast",
	"bc_interior":        "BC Interior",
	"alberta_rockies":    "Alberta Rockies",
	"wasatch":            "Wasatch",
	"co_front_range":     "CO Front Range",
	"co_roaring_fork":    "CO Roaring Fork",
	"co_steamboat":       "CO Steamboat",
	"co_southern":        "CO Southern Mountains",
	"co_western_slope":   "CO Western Slope",
	"snake_river_tetons": "Snake River / Tetons",
	"northeast":          "Northeast",
	"nm_northern":        "Northern New Mexico",
	"nm_southern":        "Southern New Mexico",
	"az_flagstaff":       "Arizona Flagstaff",
	"ca_socal":           "Southern California",
	"ca_norcal":          "Northern California",
	"ak_chugach":         "Alaska Chugach",
	"mi_upper_peninsula": "Michigan UP",
	"ut_southern":        "Southern Utah",
}

// MacroRegionDisplayName returns the human-readable name for a macro-region key.
// Falls back to the key itself if no display name is defined.
func MacroRegionDisplayName(key string) string {
	if name, ok := MacroRegionNames[key]; ok {
		return name
	}
	return key
}

// MacroRegionDisplayNameFromKey extracts the macro region from a group key
// (which has format "macro_region:friction_tier") and returns its display name.
func MacroRegionDisplayNameFromKey(groupKey string) string {
	macroRegion := groupKey
	if i := strings.Index(groupKey, ":"); i >= 0 {
		macroRegion = groupKey[:i]
	}
	return MacroRegionDisplayName(macroRegion)
}
