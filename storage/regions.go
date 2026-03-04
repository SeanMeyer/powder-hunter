package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/seanmeyer/powder-hunter/domain"
)

// UpsertRegion inserts or replaces a region row. Used during seed and config reload.
func (d *DB) UpsertRegion(ctx context.Context, r domain.Region) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO regions
			(id, name, lat, lon, friction_tier, near_threshold_cm, extended_threshold_cm, country)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Name, r.Latitude, r.Longitude,
		string(r.FrictionTier), r.NearThresholdCM, r.ExtendedThresholdCM, r.Country,
	)
	if err != nil {
		return fmt.Errorf("upsert region %s: %w", r.ID, err)
	}
	return nil
}

// UpsertResort inserts or replaces a resort row. Metadata is stored as JSON.
func (d *DB) UpsertResort(ctx context.Context, r domain.Resort) error {
	meta, err := json.Marshal(r.Metadata)
	if err != nil {
		return fmt.Errorf("marshal resort metadata: %w", err)
	}

	_, err = d.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO resorts
			(id, region_id, name, lat, lon, elevation_m, pass_affiliation, vertical_drop_m, lift_count, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.RegionID, r.Name, r.Latitude, r.Longitude,
		r.ElevationM, r.PassAffiliation, r.VerticalDropM, r.LiftCount, string(meta),
	)
	if err != nil {
		return fmt.Errorf("upsert resort %s: %w", r.ID, err)
	}
	return nil
}

// ListRegions returns all regions in the database.
func (d *DB) ListRegions(ctx context.Context) ([]domain.Region, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, name, lat, lon, friction_tier, near_threshold_cm, extended_threshold_cm, country
		FROM regions`)
	if err != nil {
		return nil, fmt.Errorf("list regions: %w", err)
	}
	defer rows.Close()

	var regions []domain.Region
	for rows.Next() {
		r, err := scanRegion(rows)
		if err != nil {
			return nil, fmt.Errorf("scan region: %w", err)
		}
		regions = append(regions, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list regions rows: %w", err)
	}
	return regions, nil
}

// GetRegionWithResorts returns a region and all its resorts.
func (d *DB) GetRegionWithResorts(ctx context.Context, regionID string) (domain.Region, []domain.Resort, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, name, lat, lon, friction_tier, near_threshold_cm, extended_threshold_cm, country
		FROM regions WHERE id = ?`, regionID)

	region, err := scanRegionRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Region{}, nil, fmt.Errorf("region %s not found: %w", regionID, err)
		}
		return domain.Region{}, nil, fmt.Errorf("get region %s: %w", regionID, err)
	}

	rows, err := d.db.QueryContext(ctx, `
		SELECT id, region_id, name, lat, lon, elevation_m, pass_affiliation, vertical_drop_m, lift_count, metadata
		FROM resorts WHERE region_id = ?`, regionID)
	if err != nil {
		return domain.Region{}, nil, fmt.Errorf("list resorts for region %s: %w", regionID, err)
	}
	defer rows.Close()

	var resorts []domain.Resort
	for rows.Next() {
		r, err := scanResort(rows)
		if err != nil {
			return domain.Region{}, nil, fmt.Errorf("scan resort: %w", err)
		}
		resorts = append(resorts, r)
	}
	if err := rows.Err(); err != nil {
		return domain.Region{}, nil, fmt.Errorf("list resorts rows: %w", err)
	}

	return region, resorts, nil
}

// scanner is the common interface between *sql.Row and *sql.Rows so scanRegion
// can be called from both QueryRow and Query paths.
type scanner interface {
	Scan(dest ...any) error
}

func scanRegion(s scanner) (domain.Region, error) {
	var r domain.Region
	var ft string
	err := s.Scan(&r.ID, &r.Name, &r.Latitude, &r.Longitude,
		&ft, &r.NearThresholdCM, &r.ExtendedThresholdCM, &r.Country)
	if err != nil {
		return domain.Region{}, err
	}
	r.FrictionTier = domain.FrictionTier(ft)
	return r, nil
}

func scanRegionRow(row *sql.Row) (domain.Region, error) {
	var r domain.Region
	var ft string
	err := row.Scan(&r.ID, &r.Name, &r.Latitude, &r.Longitude,
		&ft, &r.NearThresholdCM, &r.ExtendedThresholdCM, &r.Country)
	if err != nil {
		return domain.Region{}, err
	}
	r.FrictionTier = domain.FrictionTier(ft)
	return r, nil
}

func scanResort(s scanner) (domain.Resort, error) {
	var r domain.Resort
	var metaJSON string
	err := s.Scan(&r.ID, &r.RegionID, &r.Name, &r.Latitude, &r.Longitude,
		&r.ElevationM, &r.PassAffiliation, &r.VerticalDropM, &r.LiftCount, &metaJSON)
	if err != nil {
		return domain.Resort{}, err
	}
	if err := json.Unmarshal([]byte(metaJSON), &r.Metadata); err != nil {
		return domain.Resort{}, fmt.Errorf("unmarshal resort metadata: %w", err)
	}
	return r, nil
}
