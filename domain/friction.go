package domain

import "math"

// HaversineDistanceKM returns the great-circle distance in km between two lat/lon points.
func HaversineDistanceKM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKM = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusKM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// FrictionTierFromDistance assigns a friction tier based on straight-line distance.
// Uses a 1.3x multiplier to estimate mountain road driving distance from straight-line.
func FrictionTierFromDistance(distKM float64) FrictionTier {
	estimatedDriveHours := (distKM * 1.3) / 100.0
	switch {
	case estimatedDriveHours <= 3:
		return FrictionLocalDrive
	case estimatedDriveHours <= 8:
		return FrictionRegionalDrive
	case estimatedDriveHours <= 14:
		return FrictionHighFrictionDrive
	default:
		return FrictionFlight
	}
}
