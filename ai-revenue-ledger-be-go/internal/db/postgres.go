package db

import (
"context"
"fmt"
"time"

"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, databaseURL string, timeout time.Duration) (*pgxpool.Pool, error) {
if databaseURL == "" {
return nil, nil
}

cfg, err := pgxpool.ParseConfig(databaseURL)
if err != nil {
return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
}

connectCtx, cancel := context.WithTimeout(ctx, timeout)
defer cancel()

pool, err := pgxpool.NewWithConfig(connectCtx, cfg)
if err != nil {
return nil, fmt.Errorf("create pgx pool: %w", err)
}

pingCtx, pingCancel := context.WithTimeout(ctx, timeout)
defer pingCancel()

if err := pool.Ping(pingCtx); err != nil {
pool.Close()
return nil, fmt.Errorf("ping postgres: %w", err)
}

return pool, nil
}
