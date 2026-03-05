package catalog

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/seanmeyer/powder-hunter/domain"
)

//go:embed data/regions.json
var regionsJSON []byte

// RegionWithResorts pairs a region with the ski areas that share its weather.
type RegionWithResorts struct {
	Region  domain.Region
	Resorts []domain.Resort
}

// ── JSON intermediate types ──────────────────────────────────────────────────
// These mirror the seed JSON structure exactly. Regions() converts them to
// domain types at the boundary (parse, don't validate).

type regionsFile struct {
	Regions []regionJSON `json:"regions"`
}

type regionJSON struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Country     string        `json:"country"`
	Timezone    string        `json:"timezone"`
	MacroRegion string        `json:"macro_region"`
	Coords      coordsJSON    `json:"coords"`
	Logistics   logisticsJSON `json:"logistics"`
	Resorts     []resortJSON  `json:"resorts"`
}

type coordsJSON struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type logisticsJSON struct {
	NearestAirport string `json:"nearest_airport"`
	DriveNotes     string `json:"drive_notes"`
	LodgingNotes   string `json:"lodging_notes"`
}

type resortJSON struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Coords   coordsJSON        `json:"coords"`
	Stats    resortStatsJSON   `json:"stats"`
	Metadata map[string]string `json:"metadata"`
}

type resortStatsJSON struct {
	SummitElevationFt int      `json:"summit_elevation_ft"`
	BaseElevationFt   int      `json:"base_elevation_ft"`
	VerticalDropFt    int      `json:"vertical_drop_ft"`
	SkiableAcres      int      `json:"skiable_acres"`
	Lifts             int      `json:"lifts"`
	PassAffiliations  []string `json:"pass_affiliations"`
}

// ── Loader ───────────────────────────────────────────────────────────────────

// Regions loads the embedded seed data and converts to domain types.
// Panics on parse failure so misconfigured seed data is caught at startup, not silently ignored.
func Regions() []RegionWithResorts {
	var f regionsFile
	if err := json.Unmarshal(regionsJSON, &f); err != nil {
		panic(fmt.Sprintf("seed: parse regions.json: %v", err))
	}

	result := make([]RegionWithResorts, 0, len(f.Regions))
	for _, rj := range f.Regions {
		result = append(result, RegionWithResorts{
			Region:  toRegion(rj),
			Resorts: toResorts(rj.ID, rj.Resorts),
		})
	}
	return result
}

func toRegion(j regionJSON) domain.Region {
	return domain.Region{
		ID:        j.ID,
		Name:      j.Name,
		Country:   j.Country,
		Timezone:  j.Timezone,
		Latitude:  j.Coords.Lat,
		Longitude: j.Coords.Lon,
		Logistics: domain.RegionLogistics{
			NearestAirport: j.Logistics.NearestAirport,
			DriveNotes:     j.Logistics.DriveNotes,
			LodgingNotes:   j.Logistics.LodgingNotes,
		},
		StormGroup: j.MacroRegion,
	}
}

// RegionsForUser loads regions and assigns friction tiers based on the user's
// home coordinates. Each region's tier is calculated from the straight-line
// distance between the user's home and the region.
func RegionsForUser(homeLat, homeLon float64) []RegionWithResorts {
	regions := Regions()
	for i := range regions {
		r := &regions[i].Region
		dist := domain.HaversineDistanceKM(homeLat, homeLon, r.Latitude, r.Longitude)
		r.FrictionTier = domain.FrictionTierFromDistance(dist)
		near, ext := r.FrictionTier.Thresholds()
		r.NearThresholdIn = near
		r.ExtendedThresholdIn = ext
	}
	return regions
}

func toResorts(regionID string, js []resortJSON) []domain.Resort {
	resorts := make([]domain.Resort, 0, len(js))
	for _, j := range js {
		resorts = append(resorts, domain.Resort{
			ID:                j.ID,
			RegionID:          regionID,
			Name:              j.Name,
			Latitude:          j.Coords.Lat,
			Longitude:         j.Coords.Lon,
			SummitElevationFt: j.Stats.SummitElevationFt,
			BaseElevationFt:   j.Stats.BaseElevationFt,
			VerticalDropFt:    j.Stats.VerticalDropFt,
			SkiableAcres:      j.Stats.SkiableAcres,
			LiftCount:         j.Stats.Lifts,
			PassAffiliations:  j.Stats.PassAffiliations,
			Metadata:          j.Metadata,
		})
	}
	return resorts
}
