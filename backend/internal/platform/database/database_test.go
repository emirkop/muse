package database

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestConnect_InvalidConnectionString_ReturnsError(t *testing.T) {
	ctx := context.Background()

	_, err := Connect(ctx, "not a valid postgres connection string")

	if err == nil {
		t.Fatal("expected an error for a malformed connection string, got nil")
	}
}

func TestConnect_UnreachableDatabase_ReturnsErrorNotPanic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := Connect(ctx, "postgres://muse:muse@127.0.0.1:1/muse_dev?connect_timeout=1")

	if err == nil {
		t.Fatal("expected an error connecting to an unreachable database, got nil")
	}
}

func TestApplyMigrations_IsRealAndIdempotent(t *testing.T) {
	connStr := os.Getenv("TEST_DATABASE_URL")
	if connStr == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping real-database migration test")
	}

	ctx := context.Background()
	pool, err := Connect(ctx, connStr)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.ApplyMigrations(ctx); err != nil {
		t.Fatalf("first ApplyMigrations: %v", err)
	}

	var accountsTableExists bool
	err = pool.Pool().QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.tables WHERE table_name = 'accounts'
	)`).Scan(&accountsTableExists)
	if err != nil {
		t.Fatalf("check accounts table exists: %v", err)
	}
	if !accountsTableExists {
		t.Fatal("expected the accounts table to exist after ApplyMigrations")
	}

	if err := pool.ApplyMigrations(ctx); err != nil {
		t.Fatalf("second (idempotent) ApplyMigrations: %v", err)
	}
}
