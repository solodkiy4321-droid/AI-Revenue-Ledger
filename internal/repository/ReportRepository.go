package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"arl/internal/models"
)

type ReportRepo struct {
	pool *pgxpool.Pool
}

func NewReportRepo(pool *pgxpool.Pool) *ReportRepo {
	return &ReportRepo{pool: pool}
}

func (r *ReportRepo) Create(ctx context.Context, rep *models.Report) error {
	query := `
		INSERT INTO reports (
			tenant_id, confirmed_amount, declared_amount,
			modeled_low, modeled_high, methodology_version,
			calculated_at, stale
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`

	if rep.CalculatedAt.IsZero() {
		rep.CalculatedAt = time.Now().UTC()
	}

	err := r.pool.QueryRow(
		ctx,
		query,
		rep.TenantID,
		rep.ConfirmedAmount,
		rep.DeclaredAmount,
		rep.ModeledLow,
		rep.ModeledHigh,
		rep.MethodologyVersion,
		rep.CalculatedAt,
		rep.Stale,
	).Scan(&rep.ID)

	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}

	return nil
}

func (r *ReportRepo) GetLatestByTenant(ctx context.Context, tenantID string) (models.Report, error) {
	query := `
		SELECT id, tenant_id, confirmed_amount, declared_amount,
		       modeled_low, modeled_high, methodology_version,
		       calculated_at, stale
		FROM reports
		WHERE tenant_id = $1
		ORDER BY calculated_at DESC
		LIMIT 1`

	var rep models.Report
	err := r.pool.QueryRow(ctx, query, tenantID).Scan(
		&rep.ID,
		&rep.TenantID,
		&rep.ConfirmedAmount,
		&rep.DeclaredAmount,
		&rep.ModeledLow,
		&rep.ModeledHigh,
		&rep.MethodologyVersion,
		&rep.CalculatedAt,
		&rep.Stale,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return models.Report{}, ErrNotFound
	}
	if err != nil {
		return models.Report{}, fmt.Errorf("get latest report: %w", err)
	}

	return rep, nil
}
