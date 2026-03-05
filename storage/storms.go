package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/seanmeyer/powder-hunter/domain"
)

// GetActiveStorms returns storms in a region that have not yet expired.
func (d *DB) GetActiveStorms(ctx context.Context, regionID string) ([]domain.Storm, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, region_id, window_start, window_end, state, current_tier,
		       discord_thread_id, detected_at, last_evaluated_at, last_posted_at
		FROM storms
		WHERE region_id = ? AND state != ?`,
		regionID, string(domain.StormExpired),
	)
	if err != nil {
		return nil, fmt.Errorf("get active storms for region %s: %w", regionID, err)
	}
	defer rows.Close()
	return scanStorms(rows)
}

// GetActiveStormsByRegion returns all non-expired storms grouped by region ID.
func (d *DB) GetActiveStormsByRegion(ctx context.Context) (map[string][]domain.Storm, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, region_id, window_start, window_end, state, current_tier,
		       discord_thread_id, detected_at, last_evaluated_at, last_posted_at
		FROM storms
		WHERE state != ?`,
		string(domain.StormExpired),
	)
	if err != nil {
		return nil, fmt.Errorf("get active storms by region: %w", err)
	}
	defer rows.Close()

	storms, err := scanStorms(rows)
	if err != nil {
		return nil, err
	}

	byRegion := make(map[string][]domain.Storm, len(storms))
	for _, s := range storms {
		byRegion[s.RegionID] = append(byRegion[s.RegionID], s)
	}
	return byRegion, nil
}

// CreateStorm inserts a new storm and returns its auto-assigned ID.
func (d *DB) CreateStorm(ctx context.Context, s domain.Storm) (int64, error) {
	result, err := d.db.ExecContext(ctx, `
		INSERT INTO storms
			(region_id, window_start, window_end, state, current_tier,
			 discord_thread_id, detected_at, last_evaluated_at, last_posted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.RegionID,
		s.WindowStart.UTC().Format(time.RFC3339),
		s.WindowEnd.UTC().Format(time.RFC3339),
		string(s.State),
		string(s.CurrentTier),
		s.DiscordThreadID,
		s.DetectedAt.UTC().Format(time.RFC3339),
		nullableTime(s.LastEvaluatedAt),
		nullableTime(s.LastPostedAt),
	)
	if err != nil {
		return 0, fmt.Errorf("create storm: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get storm insert id: %w", err)
	}
	return id, nil
}

// UpdateStorm writes all mutable fields back to the storms row.
func (d *DB) UpdateStorm(ctx context.Context, s domain.Storm) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE storms SET
			region_id         = ?,
			window_start      = ?,
			window_end        = ?,
			state             = ?,
			current_tier      = ?,
			discord_thread_id = ?,
			detected_at       = ?,
			last_evaluated_at = ?,
			last_posted_at    = ?
		WHERE id = ?`,
		s.RegionID,
		s.WindowStart.UTC().Format(time.RFC3339),
		s.WindowEnd.UTC().Format(time.RFC3339),
		string(s.State),
		string(s.CurrentTier),
		s.DiscordThreadID,
		s.DetectedAt.UTC().Format(time.RFC3339),
		nullableTime(s.LastEvaluatedAt),
		nullableTime(s.LastPostedAt),
		s.ID,
	)
	if err != nil {
		return fmt.Errorf("update storm %d: %w", s.ID, err)
	}
	return nil
}

// FindOverlappingStorm returns the first active storm in the region whose window
// overlaps [start, end], or nil if none exists. Overlap detection uses half-open
// interval logic consistent with domain.Storm.WindowOverlaps.
func (d *DB) FindOverlappingStorm(ctx context.Context, regionID string, start, end time.Time) (*domain.Storm, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, region_id, window_start, window_end, state, current_tier,
		       discord_thread_id, detected_at, last_evaluated_at, last_posted_at
		FROM storms
		WHERE region_id = ?
		  AND state != ?
		  AND window_end >= ?
		  AND window_start <= ?
		LIMIT 1`,
		regionID,
		string(domain.StormExpired),
		start.UTC().Format(time.RFC3339),
		end.UTC().Format(time.RFC3339),
	)

	s, err := scanStormRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find overlapping storm: %w", err)
	}
	return &s, nil
}

// nullableTime returns an empty string for zero times so that optional timestamp
// columns store '' rather than the zero RFC3339 value, which would sort incorrectly.
func nullableTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseOptionalTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func scanStorms(rows *sql.Rows) ([]domain.Storm, error) {
	var storms []domain.Storm
	for rows.Next() {
		s, err := scanStorm(rows)
		if err != nil {
			return nil, fmt.Errorf("scan storm: %w", err)
		}
		storms = append(storms, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storm rows: %w", err)
	}
	return storms, nil
}

func scanStorm(s scanner) (domain.Storm, error) {
	var st domain.Storm
	var state, tier, windowStart, windowEnd, detectedAt, lastEval, lastPosted string
	err := s.Scan(
		&st.ID, &st.RegionID,
		&windowStart, &windowEnd,
		&state, &tier,
		&st.DiscordThreadID,
		&detectedAt, &lastEval, &lastPosted,
	)
	if err != nil {
		return domain.Storm{}, err
	}
	st.State, err = domain.ParseStormState(state)
	if err != nil {
		return domain.Storm{}, fmt.Errorf("scan storm: %w", err)
	}
	if tier != "" {
		st.CurrentTier, err = domain.ParseTier(tier)
		if err != nil {
			return domain.Storm{}, fmt.Errorf("scan storm: %w", err)
		}
	}
	st.WindowStart, _ = time.Parse(time.RFC3339, windowStart)
	st.WindowEnd, _ = time.Parse(time.RFC3339, windowEnd)
	st.DetectedAt, _ = time.Parse(time.RFC3339, detectedAt)
	st.LastEvaluatedAt = parseOptionalTime(lastEval)
	st.LastPostedAt = parseOptionalTime(lastPosted)
	return st, nil
}

func scanStormRow(row *sql.Row) (domain.Storm, error) {
	var st domain.Storm
	var state, tier, windowStart, windowEnd, detectedAt, lastEval, lastPosted string
	err := row.Scan(
		&st.ID, &st.RegionID,
		&windowStart, &windowEnd,
		&state, &tier,
		&st.DiscordThreadID,
		&detectedAt, &lastEval, &lastPosted,
	)
	if err != nil {
		return domain.Storm{}, err
	}
	st.State, err = domain.ParseStormState(state)
	if err != nil {
		return domain.Storm{}, fmt.Errorf("scan storm: %w", err)
	}
	if tier != "" {
		st.CurrentTier, err = domain.ParseTier(tier)
		if err != nil {
			return domain.Storm{}, fmt.Errorf("scan storm: %w", err)
		}
	}
	st.WindowStart, _ = time.Parse(time.RFC3339, windowStart)
	st.WindowEnd, _ = time.Parse(time.RFC3339, windowEnd)
	st.DetectedAt, _ = time.Parse(time.RFC3339, detectedAt)
	st.LastEvaluatedAt = parseOptionalTime(lastEval)
	st.LastPostedAt = parseOptionalTime(lastPosted)
	return st, nil
}
