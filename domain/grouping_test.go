package domain

import (
	"testing"
	"time"
)

func TestGroupByMacroRegion(t *testing.T) {
	now := time.Now().UTC()
	tomorrow := now.AddDate(0, 0, 1)
	nextWeek := now.AddDate(0, 0, 7)

	makeResult := func(regionID, macroRegion string, friction FrictionTier, windowStart, windowEnd time.Time, tier Tier) StormGroupInput {
		return StormGroupInput{
			RegionID:    regionID,
			StormGroup: macroRegion,
			Friction:    friction,
			WindowStart: windowStart,
			WindowEnd:   windowEnd,
			Tier:        tier,
		}
	}

	tests := []struct {
		name       string
		inputs     []StormGroupInput
		wantGroups int
	}{
		{
			name: "same macro-region and friction groups together",
			inputs: []StormGroupInput{
				makeResult("wa_central", "pnw_cascades", FrictionFlight, tomorrow, nextWeek, TierWorthALook),
				makeResult("wa_north", "pnw_cascades", FrictionFlight, tomorrow, nextWeek, TierWorthALook),
				makeResult("wa_south", "pnw_cascades", FrictionFlight, tomorrow, nextWeek, TierOnTheRadar),
			},
			wantGroups: 1,
		},
		{
			name: "different friction tiers split groups",
			inputs: []StormGroupInput{
				makeResult("co_i70", "co_front_range", FrictionLocalDrive, tomorrow, nextWeek, TierWorthALook),
				makeResult("co_roaring", "co_roaring_fork", FrictionRegionalDrive, tomorrow, nextWeek, TierWorthALook),
			},
			wantGroups: 2,
		},
		{
			name: "non-overlapping windows split groups",
			inputs: []StormGroupInput{
				makeResult("wa_central", "pnw_cascades", FrictionFlight, tomorrow, tomorrow.AddDate(0, 0, 3), TierWorthALook),
				makeResult("wa_north", "pnw_cascades", FrictionFlight, tomorrow.AddDate(0, 0, 10), nextWeek.AddDate(0, 0, 10), TierOnTheRadar),
			},
			wantGroups: 2,
		},
		{
			name: "single region stays as singleton group",
			inputs: []StormGroupInput{
				makeResult("ak_chugach", "ak_chugach", FrictionFlight, tomorrow, nextWeek, TierDropEverything),
			},
			wantGroups: 1,
		},
		{
			name: "different macro-regions split even with same friction",
			inputs: []StormGroupInput{
				makeResult("wa_central", "pnw_cascades", FrictionFlight, tomorrow, nextWeek, TierWorthALook),
				makeResult("mt_western", "northern_rockies", FrictionFlight, tomorrow, nextWeek, TierWorthALook),
			},
			wantGroups: 2,
		},
		{
			name:       "empty input returns no groups",
			inputs:     nil,
			wantGroups: 0,
		},
		{
			name: "tier sorting puts DROP_EVERYTHING first",
			inputs: []StormGroupInput{
				makeResult("wa_south", "pnw_cascades", FrictionFlight, tomorrow, nextWeek, TierOnTheRadar),
				makeResult("wa_central", "pnw_cascades", FrictionFlight, tomorrow, nextWeek, TierDropEverything),
				makeResult("wa_north", "pnw_cascades", FrictionFlight, tomorrow, nextWeek, TierWorthALook),
			},
			wantGroups: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups := GroupByMacroRegion(tt.inputs)
			if len(groups) != tt.wantGroups {
				t.Errorf("got %d groups, want %d", len(groups), tt.wantGroups)
				for i, g := range groups {
					t.Logf("  group %d: key=%s, %d members", i, g.Key, len(g.Members))
				}
			}
			// Check tier sorting for the last test case
			if tt.name == "tier sorting puts DROP_EVERYTHING first" && len(groups) == 1 {
				if groups[0].Members[0].Tier != TierDropEverything {
					t.Errorf("first member should be DROP_EVERYTHING, got %s", groups[0].Members[0].Tier)
				}
				if groups[0].Members[1].Tier != TierWorthALook {
					t.Errorf("second member should be WORTH_A_LOOK, got %s", groups[0].Members[1].Tier)
				}
			}
		})
	}
}
