package domain

import "time"

// StormState is the lifecycle stage of a detected storm opportunity.
type StormState string

const (
	StormDetected  StormState = "detected"  // window found above threshold
	StormEvaluated StormState = "evaluated" // LLM has scored it
	StormBriefed   StormState = "briefed"   // initial Discord post sent
	StormUpdated   StormState = "updated"   // follow-up Discord post sent
	StormExpired   StormState = "expired"   // window has passed
)

// validTransitions encodes the allowed state machine edges. Storms move forward
// through the lifecycle; backward transitions indicate a bug in the pipeline.
var validTransitions = map[StormState][]StormState{
	StormDetected:  {StormEvaluated, StormExpired},
	StormEvaluated: {StormBriefed, StormExpired},
	StormBriefed:   {StormUpdated, StormExpired},
	StormUpdated:   {StormUpdated, StormExpired},
	StormExpired:   {},
}

// Storm represents a detected snowfall opportunity in a region over a time window.
type Storm struct {
	ID              int64
	RegionID        string
	WindowStart     time.Time
	WindowEnd       time.Time
	State           StormState
	CurrentTier     Tier   // empty until first evaluation
	DiscordThreadID string // set when state transitions to briefed
	DetectedAt      time.Time
	LastEvaluatedAt time.Time // zero if not yet evaluated
	LastPostedAt    time.Time // zero if not yet posted
}

// WindowOverlaps returns true if this storm's date window overlaps [start, end].
// Used to merge duplicate detections of the same storm event across pipeline runs.
func (s Storm) WindowOverlaps(start, end time.Time) bool {
	return !s.WindowEnd.Before(start) && !end.Before(s.WindowStart)
}

// ExpandWindow returns a new Storm whose window is the union of the existing window
// and [start, end]. The storm value itself is not mutated.
func (s Storm) ExpandWindow(start, end time.Time) Storm {
	if start.Before(s.WindowStart) {
		s.WindowStart = start
	}
	if end.After(s.WindowEnd) {
		s.WindowEnd = end
	}
	return s
}

// ValidTransition reports whether the from→to state edge is allowed by the lifecycle.
func ValidTransition(from, to StormState) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == to {
			return true
		}
	}
	return false
}
