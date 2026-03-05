package config

import (
	"testing"

	"github.com/seanmeyer/powder-hunter/catalog"
)

// mapLookup returns a lookup function backed by a map. Missing keys return "".
func mapLookup(m map[string]string) func(string) string {
	return func(key string) string {
		return m[key]
	}
}

func TestProfileFromEnv_AllDefaults(t *testing.T) {
	p := ProfileFromEnv(mapLookup(map[string]string{}))
	d := catalog.DefaultProfile()

	if p.HomeBase != d.HomeBase {
		t.Errorf("HomeBase = %q, want %q", p.HomeBase, d.HomeBase)
	}
	if p.HomeLatitude != d.HomeLatitude {
		t.Errorf("HomeLatitude = %v, want %v", p.HomeLatitude, d.HomeLatitude)
	}
	if p.HomeLongitude != d.HomeLongitude {
		t.Errorf("HomeLongitude = %v, want %v", p.HomeLongitude, d.HomeLongitude)
	}
	if len(p.PassesHeld) != len(d.PassesHeld) || p.PassesHeld[0] != d.PassesHeld[0] {
		t.Errorf("PassesHeld = %v, want %v", p.PassesHeld, d.PassesHeld)
	}
	if p.SkillLevel != d.SkillLevel {
		t.Errorf("SkillLevel = %q, want %q", p.SkillLevel, d.SkillLevel)
	}
	if p.Preferences != d.Preferences {
		t.Errorf("Preferences mismatch")
	}
	if p.RemoteWorkCapable != d.RemoteWorkCapable {
		t.Errorf("RemoteWorkCapable = %v, want %v", p.RemoteWorkCapable, d.RemoteWorkCapable)
	}
	if p.TypicalPTODays != d.TypicalPTODays {
		t.Errorf("TypicalPTODays = %d, want %d", p.TypicalPTODays, d.TypicalPTODays)
	}
}

func TestProfileFromEnv_AllOverrides(t *testing.T) {
	env := map[string]string{
		"HOME_BASE":      "Salt Lake City, UT",
		"HOME_LATITUDE":  "40.7608",
		"HOME_LONGITUDE": "-111.8910",
		"PASSES":         "epic,ikon",
		"SKILL_LEVEL":    "intermediate",
		"PREFERENCES":    "Love groomers and gentle trees",
		"REMOTE_WORK":    "false",
		"PTO_DAYS":       "10",
	}
	p := ProfileFromEnv(mapLookup(env))

	if p.HomeBase != "Salt Lake City, UT" {
		t.Errorf("HomeBase = %q, want %q", p.HomeBase, "Salt Lake City, UT")
	}
	if p.HomeLatitude != 40.7608 {
		t.Errorf("HomeLatitude = %v, want %v", p.HomeLatitude, 40.7608)
	}
	if p.HomeLongitude != -111.8910 {
		t.Errorf("HomeLongitude = %v, want %v", p.HomeLongitude, -111.8910)
	}
	if len(p.PassesHeld) != 2 || p.PassesHeld[0] != "epic" || p.PassesHeld[1] != "ikon" {
		t.Errorf("PassesHeld = %v, want [epic ikon]", p.PassesHeld)
	}
	if p.SkillLevel != "intermediate" {
		t.Errorf("SkillLevel = %q, want %q", p.SkillLevel, "intermediate")
	}
	if p.Preferences != "Love groomers and gentle trees" {
		t.Errorf("Preferences = %q, want %q", p.Preferences, "Love groomers and gentle trees")
	}
	if p.RemoteWorkCapable != false {
		t.Errorf("RemoteWorkCapable = %v, want false", p.RemoteWorkCapable)
	}
	if p.TypicalPTODays != 10 {
		t.Errorf("TypicalPTODays = %d, want 10", p.TypicalPTODays)
	}
}

func TestProfileFromEnv_PartialOverride(t *testing.T) {
	env := map[string]string{
		"HOME_BASE": "Boulder, CO",
		"PTO_DAYS":  "20",
	}
	p := ProfileFromEnv(mapLookup(env))
	d := catalog.DefaultProfile()

	// Overridden fields
	if p.HomeBase != "Boulder, CO" {
		t.Errorf("HomeBase = %q, want %q", p.HomeBase, "Boulder, CO")
	}
	if p.TypicalPTODays != 20 {
		t.Errorf("TypicalPTODays = %d, want 20", p.TypicalPTODays)
	}

	// Non-overridden fields keep defaults
	if p.HomeLatitude != d.HomeLatitude {
		t.Errorf("HomeLatitude = %v, want default %v", p.HomeLatitude, d.HomeLatitude)
	}
	if p.HomeLongitude != d.HomeLongitude {
		t.Errorf("HomeLongitude = %v, want default %v", p.HomeLongitude, d.HomeLongitude)
	}
	if p.SkillLevel != d.SkillLevel {
		t.Errorf("SkillLevel = %q, want default %q", p.SkillLevel, d.SkillLevel)
	}
	if p.RemoteWorkCapable != d.RemoteWorkCapable {
		t.Errorf("RemoteWorkCapable = %v, want default %v", p.RemoteWorkCapable, d.RemoteWorkCapable)
	}
	if len(p.PassesHeld) != len(d.PassesHeld) || p.PassesHeld[0] != d.PassesHeld[0] {
		t.Errorf("PassesHeld = %v, want default %v", p.PassesHeld, d.PassesHeld)
	}
}
