package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"arl/internal/models"
)

type IntegrationRepo struct {
	pool *pgxpool.Pool
}

func NewIntegrationRepo(pool *pgxpool.Pool) *IntegrationRepo {
	return &IntegrationRepo{pool: pool}
}

func (r *IntegrationRepo) Create(ctx context.Context, i *models.Integration) error {
	query := `
		INSERT INTO integrations (tenant_id, provider, status, last_sync_at, error_message)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`

	err := r.pool.QueryRow(
		ctx,
		query,
		i.TenantID,
		i.Provider,
		i.Status,
		i.LastSyncAt,
		i.ErrorMessage,
	).Scan(&i.ID)

	if err != nil {
		return handleError("IntegrationRepo.Create", err)
	}

	return nil
}

func (r *IntegrationRepo) ListByTenant(ctx context.Context, tenantID string) ([]models.Integration, error) {
	query := `
		SELECT id, tenant_id, provider, status, last_sync_at, error_message
		FROM integrations
		WHERE tenant_id = $1
		ORDER BY id`

	rows, err := r.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, handleError("IntegrationRepo.ListByTenant.Query", err)
	}
	defer rows.Close()

	var result []models.Integration
	for rows.Next() {
		var i models.Integration
		err := rows.Scan(
			&i.ID,
			&i.TenantID,
			&i.Provider,
			&i.Status,
			&i.LastSyncAt,
			&i.ErrorMessage,
		)
		if err != nil {
			return nil, handleError("IntegrationRepo.ListByTenant.Scan", err)
		}
		result = append(result, i)
	}

	if err := rows.Err(); err != nil {
		return nil, handleError("IntegrationRepo.ListByTenant.Rows", err)
	}

	return result, nil
}
