package domain

// FrictionTier determines snowfall thresholds for storm detection. Local resorts
// need less snow to be worth the trip; flight destinations need more to justify the cost.
type FrictionTier string

const (
	FrictionLocal             FrictionTier = "local"
	FrictionRegionalDriveable FrictionTier = "regional_driveable"
	FrictionFlight            FrictionTier = "flight"
)

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
	Logistics           RegionLogistics
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
