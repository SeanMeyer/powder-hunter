package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/seanmeyer/powder-hunter/domain"
)

// SaveEvaluation persists a new evaluation row and returns its auto-assigned ID.
func (d *DB) SaveEvaluation(ctx context.Context, e domain.Evaluation) (int64, error) {
	dayByDay, err := json.Marshal(e.DayByDay)
	if err != nil {
		return 0, fmt.Errorf("marshal day_by_day: %w", err)
	}
	keyFactors, err := json.Marshal(e.KeyFactors)
	if err != nil {
		return 0, fmt.Errorf("marshal key_factors: %w", err)
	}
	logistics, err := json.Marshal(e.LogisticsSummary)
	if err != nil {
		return 0, fmt.Errorf("marshal logistics_summary: %w", err)
	}
	weatherSnap, err := json.Marshal(e.WeatherSnapshot)
	if err != nil {
		return 0, fmt.Errorf("marshal weather_snapshot: %w", err)
	}
	structured, err := json.Marshal(e.StructuredResponse)
	if err != nil {
		return 0, fmt.Errorf("marshal structured_response: %w", err)
	}
	grounding, err := json.Marshal(e.GroundingSources)
	if err != nil {
		return 0, fmt.Errorf("marshal grounding_sources: %w", err)
	}
	resortPicks, err := json.Marshal(e.ResortInsights)
	if err != nil {
		return 0, fmt.Errorf("marshal top_resort_picks: %w", err)
	}

	delivered := 0
	if e.Delivered {
		delivered = 1
	}

	result, err := d.db.ExecContext(ctx, `
		INSERT INTO evaluations
			(storm_id, evaluated_at, prompt_version, tier, recommendation,
			 day_by_day, key_factors, logistics_summary, strategy, snow_quality,
			 crowd_estimate, closure_risk, weather_snapshot, raw_llm_response,
			 structured_response, grounding_sources, change_class, delivered,
			 summary, top_resort_picks, information_edge)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.StormID,
		e.EvaluatedAt.UTC().Format(time.RFC3339),
		e.PromptVersion,
		string(e.Tier),
		e.Recommendation,
		string(dayByDay),
		string(keyFactors),
		string(logistics),
		e.Strategy,
		e.SnowQuality,
		e.CrowdEstimate,
		e.ClosureRisk,
		string(weatherSnap),
		e.RawLLMResponse,
		string(structured),
		string(grounding),
		string(e.ChangeClass),
		delivered,
		e.Summary,
		string(resortPicks),
		e.InformationEdge,
	)
	if err != nil {
		return 0, fmt.Errorf("save evaluation: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get evaluation insert id: %w", err)
	}
	return id, nil
}

// GetLatestEvaluation returns the most recently created evaluation for a storm.
func (d *DB) GetLatestEvaluation(ctx context.Context, stormID int64) (*domain.Evaluation, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, storm_id, evaluated_at, prompt_version, tier, recommendation,
		       day_by_day, key_factors, logistics_summary, strategy, snow_quality,
		       crowd_estimate, closure_risk, weather_snapshot, raw_llm_response,
		       structured_response, grounding_sources, change_class, delivered,
		       summary, top_resort_picks, information_edge
		FROM evaluations
		WHERE storm_id = ?
		ORDER BY evaluated_at DESC
		LIMIT 1`,
		stormID,
	)
	e, err := scanEvaluation(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest evaluation for storm %d: %w", stormID, err)
	}
	return &e, nil
}

// GetEvaluationHistory returns all evaluations for a storm in chronological order.
func (d *DB) GetEvaluationHistory(ctx context.Context, stormID int64) ([]domain.Evaluation, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, storm_id, evaluated_at, prompt_version, tier, recommendation,
		       day_by_day, key_factors, logistics_summary, strategy, snow_quality,
		       crowd_estimate, closure_risk, weather_snapshot, raw_llm_response,
		       structured_response, grounding_sources, change_class, delivered,
		       summary, top_resort_picks, information_edge
		FROM evaluations
		WHERE storm_id = ?
		ORDER BY evaluated_at ASC`,
		stormID,
	)
	if err != nil {
		return nil, fmt.Errorf("get evaluation history for storm %d: %w", stormID, err)
	}
	defer rows.Close()

	var evals []domain.Evaluation
	for rows.Next() {
		e, err := scanEvaluation(rows)
		if err != nil {
			return nil, fmt.Errorf("scan evaluation: %w", err)
		}
		evals = append(evals, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("evaluation history rows: %w", err)
	}
	return evals, nil
}

// MarkEvaluationDelivered flips the delivered flag for an evaluation after a
// successful Discord post. A separate update avoids re-marshalling all JSON fields.
func (d *DB) MarkEvaluationDelivered(ctx context.Context, evalID int64, delivered bool) error {
	val := 0
	if delivered {
		val = 1
	}
	_, err := d.db.ExecContext(ctx,
		`UPDATE evaluations SET delivered = ? WHERE id = ?`,
		val, evalID,
	)
	if err != nil {
		return fmt.Errorf("mark evaluation %d delivered=%v: %w", evalID, delivered, err)
	}
	return nil
}

// GetEvaluation returns a single evaluation by ID.
func (d *DB) GetEvaluation(ctx context.Context, evalID int64) (*domain.Evaluation, error) {
	row := d.db.QueryRowContext(ctx, `
		SELECT id, storm_id, evaluated_at, prompt_version, tier, recommendation,
		       day_by_day, key_factors, logistics_summary, strategy, snow_quality,
		       crowd_estimate, closure_risk, weather_snapshot, raw_llm_response,
		       structured_response, grounding_sources, change_class, delivered,
		       summary, top_resort_picks, information_edge
		FROM evaluations
		WHERE id = ?`,
		evalID,
	)
	e, err := scanEvaluation(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get evaluation %d: %w", evalID, err)
	}
	return &e, nil
}

func scanEvaluation(s scanner) (domain.Evaluation, error) {
	var e domain.Evaluation
	var evaluatedAt, tier, changeClass string
	var dayByDay, keyFactors, logistics, weatherSnap, structured, grounding, resortPicks string
	var delivered int

	err := s.Scan(
		&e.ID, &e.StormID, &evaluatedAt, &e.PromptVersion, &tier, &e.Recommendation,
		&dayByDay, &keyFactors, &logistics, &e.Strategy, &e.SnowQuality,
		&e.CrowdEstimate, &e.ClosureRisk, &weatherSnap, &e.RawLLMResponse,
		&structured, &grounding, &changeClass, &delivered,
		&e.Summary, &resortPicks, &e.InformationEdge,
	)
	if err != nil {
		return domain.Evaluation{}, err
	}

	var parseErr error
	if e.EvaluatedAt, parseErr = time.Parse(time.RFC3339, evaluatedAt); parseErr != nil {
		return domain.Evaluation{}, fmt.Errorf("scan evaluation: parse evaluated_at: %w", parseErr)
	}
	if tier != "" {
		e.Tier, parseErr = domain.ParseTier(tier)
		if parseErr != nil {
			return domain.Evaluation{}, fmt.Errorf("scan evaluation: %w", parseErr)
		}
	}
	e.ChangeClass = domain.ChangeClass(changeClass)
	e.Delivered = delivered != 0

	if err := json.Unmarshal([]byte(dayByDay), &e.DayByDay); err != nil {
		return domain.Evaluation{}, fmt.Errorf("unmarshal day_by_day: %w", err)
	}
	if err := json.Unmarshal([]byte(keyFactors), &e.KeyFactors); err != nil {
		return domain.Evaluation{}, fmt.Errorf("unmarshal key_factors: %w", err)
	}
	if err := json.Unmarshal([]byte(logistics), &e.LogisticsSummary); err != nil {
		return domain.Evaluation{}, fmt.Errorf("unmarshal logistics_summary: %w", err)
	}
	if err := json.Unmarshal([]byte(weatherSnap), &e.WeatherSnapshot); err != nil {
		return domain.Evaluation{}, fmt.Errorf("unmarshal weather_snapshot: %w", err)
	}
	if err := json.Unmarshal([]byte(structured), &e.StructuredResponse); err != nil {
		return domain.Evaluation{}, fmt.Errorf("unmarshal structured_response: %w", err)
	}
	if err := json.Unmarshal([]byte(grounding), &e.GroundingSources); err != nil {
		return domain.Evaluation{}, fmt.Errorf("unmarshal grounding_sources: %w", err)
	}
	if err := json.Unmarshal([]byte(resortPicks), &e.ResortInsights); err != nil {
		return domain.Evaluation{}, fmt.Errorf("unmarshal top_resort_picks: %w", err)
	}

	return e, nil
}

