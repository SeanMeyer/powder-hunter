package domain

import "time"

// StormGroupInput holds the fields needed for grouping decisions.
type StormGroupInput struct {
	RegionID    string
	MacroRegion string
	Friction    FrictionTier
	WindowStart time.Time
	WindowEnd   time.Time
	Tier        Tier
	Index       int // position in the original CompareResult slice
}

// StormGroup is a set of storm results that should be presented together.
type StormGroup struct {
	Key     string            // "macro_region:friction_tier" (for logging/dedup)
	Members []StormGroupInput // ordered by tier (highest first)
}

// GroupByMacroRegion buckets inputs by macro-region + friction tier, splitting
// groups whose storm windows don't overlap. Pure function, no I/O.
func GroupByMacroRegion(inputs []StormGroupInput) []StormGroup {
	type bucketKey struct {
		macroRegion string
		friction    FrictionTier
	}

	buckets := make(map[bucketKey][]StormGroupInput)
	var keyOrder []bucketKey

	for _, in := range inputs {
		k := bucketKey{macroRegion: in.MacroRegion, friction: in.Friction}
		if _, exists := buckets[k]; !exists {
			keyOrder = append(keyOrder, k)
		}
		buckets[k] = append(buckets[k], in)
	}

	var groups []StormGroup
	for _, k := range keyOrder {
		members := buckets[k]
		for _, subgroup := range splitByWindowOverlap(members) {
			sortByTierDesc(subgroup)
			groups = append(groups, StormGroup{
				Key:     k.macroRegion + ":" + string(k.friction),
				Members: subgroup,
			})
		}
	}
	return groups
}

// splitByWindowOverlap partitions members into groups where all members'
// windows overlap with at least one other member in the group.
func splitByWindowOverlap(members []StormGroupInput) [][]StormGroupInput {
	if len(members) <= 1 {
		return [][]StormGroupInput{members}
	}

	type cluster struct {
		start   time.Time
		end     time.Time
		members []StormGroupInput
	}

	var clusters []cluster
	for _, m := range members {
		merged := false
		for i := range clusters {
			if windowsOverlap(clusters[i].start, clusters[i].end, m.WindowStart, m.WindowEnd) {
				clusters[i].members = append(clusters[i].members, m)
				if m.WindowStart.Before(clusters[i].start) {
					clusters[i].start = m.WindowStart
				}
				if m.WindowEnd.After(clusters[i].end) {
					clusters[i].end = m.WindowEnd
				}
				merged = true
				break
			}
		}
		if !merged {
			clusters = append(clusters, cluster{
				start:   m.WindowStart,
				end:     m.WindowEnd,
				members: []StormGroupInput{m},
			})
		}
	}

	result := make([][]StormGroupInput, len(clusters))
	for i, c := range clusters {
		result[i] = c.members
	}
	return result
}

func windowsOverlap(s1, e1, s2, e2 time.Time) bool {
	return !e1.Before(s2) && !e2.Before(s1)
}

// sortByTierDesc sorts members so DROP_EVERYTHING comes first, then WORTH_A_LOOK, then ON_THE_RADAR.
func sortByTierDesc(members []StormGroupInput) {
	tierRank := map[Tier]int{
		TierDropEverything: 0,
		TierWorthALook:     1,
		TierOnTheRadar:     2,
	}
	for i := 1; i < len(members); i++ {
		for j := i; j > 0 && tierRank[members[j].Tier] < tierRank[members[j-1].Tier]; j-- {
			members[j], members[j-1] = members[j-1], members[j]
		}
	}
}
