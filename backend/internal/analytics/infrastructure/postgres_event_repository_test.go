package infrastructure

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"muse-backend/internal/analytics/domain"
	"muse-backend/internal/platform/database"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	connStr := os.Getenv("TEST_DATABASE_URL")
	if connStr == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping PostgreSQL analytics tests")
	}
	pool, err := database.Connect(context.Background(), connStr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.ApplyMigrations(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool.Pool()
}

func seedAccount(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(),
		`INSERT INTO accounts (display_name) VALUES ('analytics test') RETURNING id`).Scan(&id)
	if err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM accounts WHERE id = $1`, id)
	})
	return id
}

func step(value string) *string { return &value }

func TestRecord_DuplicateEventUUIDIsCountedOnce(t *testing.T) {
	pool := testPool(t)
	account := seedAccount(t, pool)
	repository := NewPostgresEventRepository(pool)
	ctx := context.Background()

	uuid := domain.NewEventUUID()
	event, err := domain.Validate(domain.Draft{
		UUID: uuid, Name: domain.EventMuseumCreationStep, Step: step("style_list_shown"),
	}, true)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	first, err := repository.Record(ctx, account, []domain.Event{event})
	if err != nil {
		t.Fatalf("first record: %v", err)
	}
	if first != 1 {
		t.Fatalf("expected 1 row inserted, got %d", first)
	}

	second, err := repository.Record(ctx, account, []domain.Event{event})
	if err != nil {
		t.Fatalf("second record: %v", err)
	}
	if second != 0 {
		t.Fatalf("a duplicate event_uuid inserted %d rows; must insert 0", second)
	}

	var stored int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM analytics_events WHERE event_uuid = $1`, uuid).Scan(&stored); err != nil {
		t.Fatalf("count: %v", err)
	}
	if stored != 1 {
		t.Fatalf("expected exactly 1 stored row, got %d", stored)
	}
}

func TestRecord_ABatchWithARepeatStoresOnlyTheNewEvents(t *testing.T) {
	pool := testPool(t)
	account := seedAccount(t, pool)
	repository := NewPostgresEventRepository(pool)
	ctx := context.Background()

	makeEvent := func() domain.Event {
		event, err := domain.Validate(domain.Draft{
			UUID: domain.NewEventUUID(), Name: domain.EventRoomCreationStep, Step: step("name_entered"),
		}, true)
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		return event
	}
	first, second := makeEvent(), makeEvent()
	if _, err := repository.Record(ctx, account, []domain.Event{first}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	inserted, err := repository.Record(ctx, account, []domain.Event{first, second})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("expected 1 new row from a 2-event batch with 1 repeat, got %d", inserted)
	}
}

func TestPrune_RollsRawEventsIntoCountsAndDeletesThem(t *testing.T) {
	pool := testPool(t)
	account := seedAccount(t, pool)
	repository := NewPostgresEventRepository(pool)
	ctx := context.Background()

	var uuids []string
	for i := 0; i < 3; i++ {
		name, value := domain.EventMuseumCreationStep, "style_list_shown"
		if i == 2 {
			value = "style_confirmed"
		}
		event, err := domain.Validate(domain.Draft{UUID: domain.NewEventUUID(), Name: name, Step: step(value)}, true)
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		if _, err := repository.Record(ctx, account, []domain.Event{event}); err != nil {
			t.Fatalf("record: %v", err)
		}
		uuids = append(uuids, event.UUID)
	}
	fresh, err := domain.Validate(domain.Draft{
		UUID: domain.NewEventUUID(), Name: domain.EventMuseumCreationStep, Step: step("style_previewed"),
	}, true)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if _, err := repository.Record(ctx, account, []domain.Event{fresh}); err != nil {
		t.Fatalf("record fresh: %v", err)
	}

	aged := time.Now().Add(-30 * 24 * time.Hour)
	if _, err := pool.Exec(ctx,
		`UPDATE analytics_events SET received_at = $1 WHERE event_uuid = ANY($2)`, aged, uuids); err != nil {
		t.Fatalf("age events: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM analytics_daily_counts WHERE day = $1`,
			aged.UTC().Format("2006-01-02"))
	})

	removed, err := repository.Prune(ctx, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 3 {
		t.Fatalf("expected 3 raw rows removed, got %d", removed)
	}

	var remaining int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM analytics_events WHERE event_uuid = ANY($1)`, uuids).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected the aged raw rows to be deleted, %d remain", remaining)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM analytics_events WHERE event_uuid = $1`, fresh.UUID).Scan(&remaining); err != nil {
		t.Fatalf("count fresh: %v", err)
	}
	if remaining != 1 {
		t.Fatal("pruning must not touch events inside the retention window")
	}

	var listShown, confirmed int64
	if err := pool.QueryRow(ctx, `
		SELECT
			coalesce(sum(event_count) FILTER (WHERE step = 'style_list_shown'), 0),
			coalesce(sum(event_count) FILTER (WHERE step = 'style_confirmed'), 0)
		FROM analytics_daily_counts
		WHERE day = $1 AND name = $2`,
		aged.UTC().Format("2006-01-02"), domain.EventMuseumCreationStep,
	).Scan(&listShown, &confirmed); err != nil {
		t.Fatalf("read aggregates: %v", err)
	}
	if listShown != 2 || confirmed != 1 {
		t.Fatalf("aggregates = list_shown %d, confirmed %d; want 2 and 1", listShown, confirmed)
	}
}

func TestPrune_AggregatesCarryNoAccountIdentifier(t *testing.T) {
	pool := testPool(t)
	_ = seedAccount(t, pool)
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_name = 'analytics_daily_counts'`)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	defer rows.Close()

	found := 0
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scan: %v", err)
		}
		found++
		switch column {
		case "account_id", "event_uuid", "received_at", "user_id", "device_id":
			t.Errorf("analytics_daily_counts must carry no identifier; found %q", column)
		}
	}
	if found == 0 {
		t.Fatal("found no columns at all — the introspection is not testing anything")
	}
}

func TestPrune_IsIdempotent(t *testing.T) {
	pool := testPool(t)
	account := seedAccount(t, pool)
	repository := NewPostgresEventRepository(pool)
	ctx := context.Background()

	event, err := domain.Validate(domain.Draft{
		UUID: domain.NewEventUUID(), Name: domain.EventCapacityUpgradeStep, Step: step("purchase_started"),
	}, true)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if _, err := repository.Record(ctx, account, []domain.Event{event}); err != nil {
		t.Fatalf("record: %v", err)
	}
	aged := time.Now().Add(-30 * 24 * time.Hour)
	if _, err := pool.Exec(ctx,
		`UPDATE analytics_events SET received_at = $1 WHERE event_uuid = $2`, aged, event.UUID); err != nil {
		t.Fatalf("age: %v", err)
	}
	day := aged.UTC().Format("2006-01-02")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM analytics_daily_counts WHERE day = $1 AND name = $2`, day, domain.EventCapacityUpgradeStep)
	})

	before := time.Now().Add(-7 * 24 * time.Hour)
	if _, err := repository.Prune(ctx, before); err != nil {
		t.Fatalf("first prune: %v", err)
	}
	if removed, err := repository.Prune(ctx, before); err != nil {
		t.Fatalf("second prune: %v", err)
	} else if removed != 0 {
		t.Fatalf("the second prune removed %d rows; there was nothing left", removed)
	}

	var count int64
	if err := pool.QueryRow(ctx, `
		SELECT coalesce(sum(event_count), 0) FROM analytics_daily_counts
		WHERE day = $1 AND name = $2 AND step = 'purchase_started'`,
		day, domain.EventCapacityUpgradeStep).Scan(&count); err != nil {
		t.Fatalf("read aggregate: %v", err)
	}
	if count != 1 {
		t.Fatalf("aggregate = %d after two prunes; want 1", count)
	}
}
