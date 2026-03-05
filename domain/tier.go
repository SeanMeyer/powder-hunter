package domain

import "time"

// Tier represents the LLM's overall quality assessment of a storm opportunity.
type Tier string

const (
	TierDropEverything Tier = "DROP_EVERYTHING"
	TierWorthALook     Tier = "WORTH_A_LOOK"
	TierOnTheRadar     Tier = "ON_THE_RADAR"
)

// CooldownFor returns the minimum time between evaluations for a given tier.
// DROP_EVERYTHING has no cooldown (always re-evaluate when weather changes).
// Lower tiers have longer cooldowns to reduce API spend on less important storms.
func CooldownFor(tier Tier) time.Duration {
	switch tier {
	case TierDropEverything:
		return 0
	case TierWorthALook:
		return 12 * time.Hour
	case TierOnTheRadar:
		return 24 * time.Hour
	default:
		return 24 * time.Hour // conservative default for unknown tiers
	}
}

// NotificationLevel determines how a Discord alert is delivered.
type NotificationLevel string

const (
	NotifyPing       NotificationLevel = "ping"        // @here mention
	NotifySilentPost NotificationLevel = "silent_post"  // channel post, no mention
	NotifyThreadOnly NotificationLevel = "thread_only"  // thread update only, no new post
)

// NotificationFor returns the appropriate Discord delivery method for a new storm alert
// at the given tier. Higher tiers get louder delivery to cut through channel noise.
func NotificationFor(tier Tier) NotificationLevel {
	switch tier {
	case TierDropEverything:
		return NotifyPing
	case TierWorthALook:
		return NotifySilentPost
	default:
		return NotifyThreadOnly
	}
}
