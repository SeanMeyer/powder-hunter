package config

import (
	"strconv"
	"strings"

	"github.com/seanmeyer/powder-hunter/domain"
	"github.com/seanmeyer/powder-hunter/seed"
)

// ProfileFromEnv builds a UserProfile starting from seed defaults, overriding
// fields with values from environment variables when present. The lookup
// function is injected so callers can pass os.Getenv in production and a map
// lookup in tests.
func ProfileFromEnv(lookup func(string) string) domain.UserProfile {
	p := seed.DefaultProfile()

	if v := lookup("HOME_BASE"); v != "" {
		p.HomeBase = v
	}
	if v := lookup("HOME_LATITUDE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.HomeLatitude = f
		}
	}
	if v := lookup("HOME_LONGITUDE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.HomeLongitude = f
		}
	}
	if v := lookup("PASSES"); v != "" {
		p.PassesHeld = strings.Split(v, ",")
	}
	if v := lookup("SKILL_LEVEL"); v != "" {
		p.SkillLevel = v
	}
	if v := lookup("PREFERENCES"); v != "" {
		p.Preferences = v
	}
	if v := lookup("REMOTE_WORK"); v != "" {
		p.RemoteWorkCapable = strings.EqualFold(v, "true")
	}
	if v := lookup("PTO_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			p.TypicalPTODays = n
		}
	}
	if v := lookup("MIN_TIER"); v != "" {
		switch v {
		case "DROP_EVERYTHING":
			p.MinTierForPing = domain.TierDropEverything
		case "WORTH_A_LOOK":
			p.MinTierForPing = domain.TierWorthALook
		case "ON_THE_RADAR":
			p.MinTierForPing = domain.TierOnTheRadar
		}
	}

	return p
}
