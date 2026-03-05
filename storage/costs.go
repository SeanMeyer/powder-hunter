package storage

import (
	"context"
	"fmt"
	"time"
)

// RecordCost inserts a cost record for a Gemini API call.
func (d *DB) RecordCost(ctx context.Context, stormID int64, regionID string, estimatedCost float64, success bool) error {
	successInt := 0
	if success {
		successInt = 1
	}
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO eval_costs (storm_id, region_id, evaluated_at, estimated_cost_usd, success)
		VALUES (?, ?, ?, ?, ?)`,
		stormID, regionID, time.Now().UTC().Format(time.RFC3339), estimatedCost, successInt,
	)
	if err != nil {
		return fmt.Errorf("record eval cost: %w", err)
	}
	return nil
}

// MonthlySpend returns the total estimated spend and call count for successful
// evaluations in the current calendar month (UTC).
func (d *DB) MonthlySpend(ctx context.Context) (float64, int, error) {
	monthStart := time.Now().UTC().Format("2006-01") + "-01T00:00:00Z"

	var totalSpend float64
	var callCount int
	err := d.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(estimated_cost_usd), 0), COUNT(*)
		FROM eval_costs
		WHERE success = 1 AND evaluated_at >= ?`,
		monthStart,
	).Scan(&totalSpend, &callCount)
	if err != nil {
		return 0, 0, fmt.Errorf("query monthly spend: %w", err)
	}
	return totalSpend, callCount, nil
}
