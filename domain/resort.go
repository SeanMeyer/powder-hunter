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
	NearThresholdCM     float64 // 1-7 day snowfall threshold to trigger detection
	ExtendedThresholdCM float64 // 8-16 day snowfall threshold to trigger detection
	Country             string  // "US" or "CA"
}

// Resort is a ski area. Metadata carries extensible key-value pairs surfaced to the LLM
// for evaluation context (e.g. terrain rating, typical crowd level, best aspect).
type Resort struct {
	ID              string
	RegionID        string
	Name            string
	Latitude        float64
	Longitude       float64
	ElevationM      int
	PassAffiliation string // "ikon", "epic", "indy", "independent"
	VerticalDropM   int
	LiftCount       int
	Metadata        map[string]string
}
