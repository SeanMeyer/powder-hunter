package domain

// FrictionTier determines snowfall thresholds for storm detection. Local resorts
// need less snow to be worth the trip; flight destinations need more to justify the cost.
type FrictionTier string

const (
	FrictionLocalDrive        FrictionTier = "local_drive"
	FrictionRegionalDrive     FrictionTier = "regional_drive"
	FrictionHighFrictionDrive FrictionTier = "high_friction_drive"
	FrictionFlight            FrictionTier = "flight"
)

// Thresholds returns the near-range and extended-range snowfall thresholds (in inches)
// for this friction tier. These define how much forecasted snow triggers a detection.
func (ft FrictionTier) Thresholds() (nearIn, extendedIn float64) {
	switch ft {
	case FrictionLocalDrive:
		return 6, 12
	case FrictionRegionalDrive:
		return 14, 20
	case FrictionHighFrictionDrive:
		return 18, 24
	case FrictionFlight:
		return 24, 36
	default:
		return 14, 20 // safe default: regional
	}
}

// Region is the geographic unit for weather fetching and storm detection.
// Resorts share a region when they receive effectively the same snowfall.
type Region struct {
	ID                  string
	Name                string
	Latitude            float64
	Longitude           float64
	FrictionTier        FrictionTier
	NearThresholdIn     float64 // 1-7 day snowfall threshold in inches
	ExtendedThresholdIn float64 // 8-16 day snowfall threshold in inches
	Country             string  // "US" or "CA"
	Timezone            string  // IANA timezone (e.g. "America/Los_Angeles")
	Logistics           RegionLogistics
	MacroRegion         string  // static grouping for storm correlation (e.g. "pnw_cascades")
}

// RegionLogistics captures travel context for LLM evaluation prompts.
type RegionLogistics struct {
	NearestAirport string  // IATA code (e.g., "DEN")
	DriveTimeHours float64 // from user's home base
	DriveNotes     string  // road conditions, traffic patterns
	LodgingNotes   string  // price range, availability patterns
}

// Resort is a ski area within a region. Core stats are typed fields the system
// uses programmatically; metadata is an extensible map surfaced to the LLM.
type Resort struct {
	ID               string
	RegionID         string
	Name             string
	Latitude         float64
	Longitude        float64
	SummitElevationFt int
	BaseElevationFt  int
	VerticalDropFt   int
	SkiableAcres     int
	LiftCount        int
	PassAffiliations []string         // ["ikon"], ["epic"], ["ikon", "indy"], etc.
	Metadata         map[string]string // LLM-facing context (see seed data JSON)
}

// MidMountainElevationM returns the mid-mountain elevation in meters,
// used as the query elevation for weather APIs. Mid-mountain best represents
// the typical skiing experience (lifts, glades, bowls).
func (r Resort) MidMountainElevationM() int {
	midFt := (r.BaseElevationFt + r.SummitElevationFt) / 2
	return int(float64(midFt) * 0.3048)
}
