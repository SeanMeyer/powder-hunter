package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/seanmeyer/powder-hunter/domain"
)

const profileID = 1

// GetProfile returns the single user profile, or nil if none has been saved yet.
func (d *DB) GetProfile(ctx context.Context) (*domain.UserProfile, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, home_base, home_lat, home_lon, passes_held,
		       skill_level, preferences, remote_work_capable,
		       typical_pto_days, blackout_dates
		FROM user_profiles
		WHERE id = ?`, profileID)

	p, err := scanProfile(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get profile: %w", err)
	}
	return &p, nil
}

// SaveProfile upserts the user profile. There is exactly one profile row (ID=1).
func (d *DB) SaveProfile(ctx context.Context, p domain.UserProfile) error {
	passes, err := json.Marshal(p.PassesHeld)
	if err != nil {
		return fmt.Errorf("marshal passes_held: %w", err)
	}
	blackout, err := json.Marshal(p.BlackoutDates)
	if err != nil {
		return fmt.Errorf("marshal blackout_dates: %w", err)
	}

	remoteWork := 0
	if p.RemoteWorkCapable {
		remoteWork = 1
	}

	_, err = d.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO user_profiles
			(id, home_base, home_lat, home_lon, passes_held,
			 skill_level, preferences, remote_work_capable,
			 typical_pto_days, blackout_dates)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		profileID,
		p.HomeBase, p.HomeLatitude, p.HomeLongitude,
		string(passes), p.SkillLevel, p.Preferences, remoteWork,
		p.TypicalPTODays, string(blackout),
	)
	if err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	return nil
}

func scanProfile(row *sql.Row) (domain.UserProfile, error) {
	var p domain.UserProfile
	var passesJSON, blackoutJSON string
	var remoteWork int

	err := row.Scan(
		&p.ID, &p.HomeBase, &p.HomeLatitude, &p.HomeLongitude,
		&passesJSON, &p.SkillLevel, &p.Preferences, &remoteWork,
		&p.TypicalPTODays, &blackoutJSON,
	)
	if err != nil {
		return domain.UserProfile{}, err
	}

	p.RemoteWorkCapable = remoteWork != 0

	if err := json.Unmarshal([]byte(passesJSON), &p.PassesHeld); err != nil {
		return domain.UserProfile{}, fmt.Errorf("unmarshal passes_held: %w", err)
	}
	if err := json.Unmarshal([]byte(blackoutJSON), &p.BlackoutDates); err != nil {
		return domain.UserProfile{}, fmt.Errorf("unmarshal blackout_dates: %w", err)
	}

	return p, nil
}
