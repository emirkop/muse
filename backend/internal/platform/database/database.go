package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Pool struct {
	pool *pgxpool.Pool
}

func Connect(ctx context.Context, connString string) (*Pool, error) {
	pgxCfg, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("database: parse connection config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, pgxCfg)
	if err != nil {
		return nil, fmt.Errorf("database: create connection pool: %w", err)
	}

	p := &Pool{pool: pool}

	if err := p.HealthCheck(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: initial health check failed: %w", err)
	}

	return p, nil
}

func (p *Pool) HealthCheck(ctx context.Context) error {
	if err := p.pool.Ping(ctx); err != nil {
		return fmt.Errorf("database: ping failed: %w", err)
	}
	return nil
}

func (p *Pool) Close() {
	p.pool.Close()
}

func (p *Pool) Pool() *pgxpool.Pool {
	return p.pool
}
