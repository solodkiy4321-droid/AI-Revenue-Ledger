package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"arl/internal/models"
)

type UserRepo struct {
	pool *pgxpool.Pool
}

func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

func (r *UserRepo) Create(ctx context.Context, u *models.User) error {
	query := `
		INSERT INTO users (tenant_id, email, password_hash, role, created_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`

	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}

	err := r.pool.QueryRow(
		ctx,
		query,
		u.TenantID,
		u.Email,
		u.PasswordHash,
		u.Role,
		u.CreatedAt,
	).Scan(&u.ID)

	if err != nil {
		return handleError("UserRepo.Create", err)
	}

	return nil
}

func (r *UserRepo) GetByEmail(ctx context.Context, tenantID, email string) (models.User, error) {
	query := `
		SELECT id, tenant_id, email, password_hash, role, created_at
		FROM users
		WHERE tenant_id = $1 AND email = $2`

	var u models.User
	err := r.pool.QueryRow(ctx, query, tenantID, email).Scan(
		&u.ID,
		&u.TenantID,
		&u.Email,
		&u.PasswordHash,
		&u.Role,
		&u.CreatedAt,
	)

	if err != nil {
		return models.User{}, handleError("UserRepo.GetByEmail", err)
	}

	return u, nil
}
func (r *UserRepo) Update(ctx context.Context, u *models.User) error {
	query := `
		UPDATE users
		SET email = $1,
		    password_hash = $2,
		    role = $3
		WHERE id = $4 AND tenant_id = $5`

	cmdTag, err := r.pool.Exec(
		ctx,
		query,
		u.Email,
		u.PasswordHash,
		u.Role,
		u.ID,
		u.TenantID,
	)

	if err != nil {
		return handleError("UserRepo.Update", err)
	}
	
	if cmdTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}
