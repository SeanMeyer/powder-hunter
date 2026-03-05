package domain

import "time"

// UserProfile captures the subscriber's preferences and constraints used to
// personalize storm evaluations and filter which alerts get pinged.
type UserProfile struct {
	ID                int64
	HomeBase          string
	HomeLatitude      float64
	HomeLongitude     float64
	PassesHeld        []string  // e.g. ["ikon", "epic"]
	SkillLevel        string    // e.g. "expert", "advanced", "intermediate"
	Preferences       string    // freeform skiing preferences for LLM context
	RemoteWorkCapable bool
	TypicalPTODays    int
	BlackoutDates     []DateRange
}

// DateRange is an inclusive date interval used for blackout periods.
type DateRange struct {
	Start time.Time
	End   time.Time
}
