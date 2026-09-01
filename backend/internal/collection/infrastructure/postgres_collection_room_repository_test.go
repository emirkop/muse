package infrastructure_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	catalogdomain "muse-backend/internal/catalog/domain"
	catalinfra "muse-backend/internal/catalog/infrastructure"
	"muse-backend/internal/collection/domain"
	"muse-backend/internal/collection/infrastructure"
	"muse-backend/internal/platform/database"
)

func testPool(t *testing.T) *database.Pool {
	t.Helper()

	connStr := os.Getenv("TEST_DATABASE_URL")
	if connStr == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping PostgreSQL integration tests")
	}

	ctx := context.Background()
	pool, err := database.Connect(ctx, connStr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.ApplyMigrations(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if _, err := pool.Pool().Exec(ctx, `TRUNCATE collection_items, collection_rooms, accounts CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := catalinfra.NewPostgresCatalogRepository(pool.Pool()).EnsureSeeded(ctx); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
	canonical := make([]string, 0, 4)
	for _, category := range catalogdomain.SeedCollectionCategories() {
		canonical = append(canonical, string(category.ID))
	}
	if _, err := pool.Pool().Exec(ctx,
		`DELETE FROM collection_rooms WHERE category_id IS NOT NULL AND category_id <> ALL($1)`, canonical); err != nil {
		t.Fatalf("clear non-canonical category references: %v", err)
	}
	if _, err := pool.Pool().Exec(ctx,
		`DELETE FROM collection_categories WHERE id <> ALL($1)`, canonical); err != nil {
		t.Fatalf("clear non-canonical categories: %v", err)
	}
	canonicalDesigns := make([]string, 0, 1)
	for _, design := range catalogdomain.SeedCollectionDesigns() {
		canonicalDesigns = append(canonicalDesigns, design.ID)
	}
	if _, err := pool.Pool().Exec(ctx,
		`UPDATE collection_rooms SET design_id = NULL WHERE design_id IS NOT NULL AND design_id <> ALL($1)`,
		canonicalDesigns); err != nil {
		t.Fatalf("clear non-canonical design references: %v", err)
	}
	if _, err := pool.Pool().Exec(ctx,
		`DELETE FROM collection_designs WHERE id <> ALL($1)`, canonicalDesigns); err != nil {
		t.Fatalf("clear non-canonical designs: %v", err)
	}
	return pool
}

const seededCategory = "category_watches"

const seededDesign = "dev-fixture:collection-design"

const seededModel = "dev-fixture:model-chrono-one"

func newAccount(t *testing.T, pool *database.Pool, name string) string {
	t.Helper()
	var id string
	err := pool.Pool().QueryRow(context.Background(),
		`INSERT INTO accounts (display_name) VALUES ($1) RETURNING id`, name).Scan(&id)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	return id
}

func insertItem(t *testing.T, pool *database.Pool, roomID string, slot int, model string) {
	t.Helper()
	_, err := pool.Pool().Exec(context.Background(),
		`INSERT INTO collection_items (collection_room_id, slot_index, catalog_model_id) VALUES ($1, $2, $3)`,
		roomID, slot, model)
	if err != nil {
		t.Fatalf("insert item (slot %d): %v", slot, err)
	}
}

func TestCreateAndFind_RoundTrip(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	account := newAccount(t, pool, "owner")

	created, err := repo.Create(ctx, domain.CollectionRoom{
		AccountID:   account,
		Name:        "Watches",
		CategoryID:  seededCategory,
		DesignID:    seededDesign,
		CurrentTier: domain.BaseTier,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("Create returned no id")
	}

	found, err := repo.Find(ctx, created.ID)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found.AccountID != account || found.Name != "Watches" ||
		found.CategoryID != seededCategory || found.DesignID != seededDesign {
		t.Fatalf("round trip lost data: %+v", found)
	}
	if found.CurrentTier != domain.BaseTier {
		t.Fatalf("CurrentTier = %d, want BaseTier", found.CurrentTier)
	}
	if found.Items == nil {
		t.Fatal("Items is nil — an empty Room must read back as an empty slice, not nil")
	}
}

func TestCreate_UnsetReferencesAreNull(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()

	created, err := repo.Create(ctx, domain.CollectionRoom{
		AccountID:   newAccount(t, pool, "owner"),
		Name:        "Untitled",
		CurrentTier: domain.BaseTier,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var categoryNull, designNull bool
	err = pool.Pool().QueryRow(ctx,
		`SELECT category_id IS NULL, design_id IS NULL FROM collection_rooms WHERE id = $1`,
		string(created.ID)).Scan(&categoryNull, &designNull)
	if err != nil {
		t.Fatal(err)
	}
	if !categoryNull || !designNull {
		t.Fatalf("unset references stored as empty strings, not NULL (category null: %v, design null: %v)", categoryNull, designNull)
	}

	found, err := repo.Find(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.CategoryID != "" || found.DesignID != "" {
		t.Fatalf("NULL read back as %q / %q, want empty strings", found.CategoryID, found.DesignID)
	}
}

func TestCreate_ManyRoomsForOneAccount_NoUniqueConstraint(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	account := newAccount(t, pool, "collector")

	for index := 0; index < 25; index++ {
		if _, err := repo.Create(ctx, domain.CollectionRoom{
			AccountID: account, Name: "Room", CurrentTier: domain.BaseTier,
		}); err != nil {
			t.Fatalf("Create #%d: %v — no per-account uniqueness may exist", index+1, err)
		}
	}

	rooms, err := repo.ListForAccount(ctx, account)
	if err != nil {
		t.Fatalf("ListForAccount: %v", err)
	}
	if len(rooms) != 25 {
		t.Fatalf("ListForAccount = %d, want 25", len(rooms))
	}

	var uniques int
	err = pool.Pool().QueryRow(ctx, `
		SELECT count(*)
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = ANY (i.indkey)
		WHERE c.relname = 'collection_rooms' AND i.indisunique AND a.attname = 'account_id'
	`).Scan(&uniques)
	if err != nil {
		t.Fatal(err)
	}
	if uniques != 0 {
		t.Fatalf("collection_rooms.account_id is covered by %d unique index(es) — unlimited rooms per account is confirmed (`01` §8.1)", uniques)
	}
}

func TestCollectionItems_HaveNoUpperSlotBound(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()

	room, err := repo.Create(ctx, domain.CollectionRoom{
		AccountID: newAccount(t, pool, "owner"), Name: "Big", CurrentTier: domain.BaseTier,
	})
	if err != nil {
		t.Fatal(err)
	}

	const count = 120
	for slot := 0; slot < count; slot++ {
		insertItem(t, pool, string(room.ID), slot, seededModel)
	}

	loaded, err := repo.Find(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Items) != count {
		t.Fatalf("loaded %d items, want %d", len(loaded.Items), count)
	}
	for position, item := range loaded.Items {
		if item.SlotIndex != position {
			t.Fatalf("item %d read back at slot %d — inserted 0..%d contiguously, so the order is wrong",
				position, item.SlotIndex, count-1)
		}
	}

	_, err = pool.Pool().Exec(ctx,
		`INSERT INTO collection_items (collection_room_id, slot_index, catalog_model_id) VALUES ($1, -1, 'm')`,
		string(room.ID))
	if err == nil {
		t.Fatal("a negative slot_index was accepted")
	}
}

func TestFind_ItemsAreOrderedBySlotNotInsertion(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()

	room, err := repo.Create(ctx, domain.CollectionRoom{
		AccountID: newAccount(t, pool, "owner"), Name: "Shuffled", CurrentTier: domain.BaseTier,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, slot := range []int{3, 0, 4, 1, 2} {
		insertItem(t, pool, string(room.ID), slot, seededModel)
	}

	loaded, err := repo.Find(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	for position, item := range loaded.Items {
		if item.SlotIndex != position {
			t.Fatalf("item %d has slot %d — items must be ordered by slot_index", position, item.SlotIndex)
		}
	}
}

func TestCollectionItems_DuplicateSlotIsRefused(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()

	room, err := repo.Create(ctx, domain.CollectionRoom{
		AccountID: newAccount(t, pool, "owner"), Name: "Room", CurrentTier: domain.BaseTier,
	})
	if err != nil {
		t.Fatal(err)
	}
	insertItem(t, pool, string(room.ID), 0, seededModel)

	_, err = pool.Pool().Exec(ctx,
		`INSERT INTO collection_items (collection_room_id, slot_index, catalog_model_id) VALUES ($1, 0, 'b')`,
		string(room.ID))
	if err == nil {
		t.Fatal("two items were allowed into the same slot")
	}
}

func TestCollectionItems_UniquenessTiming_AllFourCases(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()

	room, err := repo.Create(ctx, domain.CollectionRoom{
		AccountID: newAccount(t, pool, "owner"), Name: "Room", CurrentTier: domain.BaseTier,
	})
	if err != nil {
		t.Fatal(err)
	}
	insertItem(t, pool, string(room.ID), 0, seededModel)
	insertItem(t, pool, string(room.ID), 1, seededModel)

	const swap = `
		UPDATE collection_items SET slot_index = 1 - slot_index
		WHERE collection_room_id = $1 AND slot_index IN (0, 1)
	`

	initial, err := repo.Find(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	originalAtSlotZero, originalAtSlotOne := initial.Items[0].ID, initial.Items[1].ID

	if _, err := pool.Pool().Exec(ctx, swap, string(room.ID)); err != nil {
		t.Fatalf("single-statement swap failed: %v — DEFERRABLE should make the check end-of-statement", err)
	}
	loaded, err := repo.Find(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Items[0].ID != originalAtSlotOne || loaded.Items[1].ID != originalAtSlotZero {
		t.Fatalf("swap did not exchange the rows: slot 0 holds %q, slot 1 holds %q",
			loaded.Items[0].ID, loaded.Items[1].ID)
	}

	atSlotZero, atSlotOne := loaded.Items[0].ID, loaded.Items[1].ID
	const moveTo = `UPDATE collection_items SET slot_index = $2 WHERE id = $1`

	withTx(t, pool, func(tx pgx.Tx) {
		if _, err := tx.Exec(ctx, moveTo, string(atSlotZero), 1); err == nil {
			t.Fatal("the first half of a naive two-statement swap was accepted — the uniqueness check is no longer running at end of statement, and this schema's guarantee has changed")
		}
	})

	withTx(t, pool, func(tx pgx.Tx) {
		if _, err := tx.Exec(ctx, `SET CONSTRAINTS collection_items_unique_slot DEFERRED`); err != nil {
			t.Fatalf("SET CONSTRAINTS: %v", err)
		}
		if _, err := tx.Exec(ctx, moveTo, string(atSlotZero), 1); err != nil {
			t.Fatalf("deferred first half: %v", err)
		}
		if _, err := tx.Exec(ctx, moveTo, string(atSlotOne), 0); err != nil {
			t.Fatalf("deferred second half: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit of a valid deferred swap: %v", err)
		}
	})
	loaded, err = repo.Find(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Items[0].ID != originalAtSlotZero || loaded.Items[1].ID != originalAtSlotOne {
		t.Fatalf("deferred swap did not restore the original order: slot 0 holds %q, slot 1 holds %q",
			loaded.Items[0].ID, loaded.Items[1].ID)
	}

	withTx(t, pool, func(tx pgx.Tx) {
		if _, err := tx.Exec(ctx, `SET CONSTRAINTS collection_items_unique_slot DEFERRED`); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE collection_items SET slot_index = 0 WHERE collection_room_id = $1`,
			string(room.ID)); err != nil {
			t.Fatalf("the duplicating update should be accepted, deferring the check: %v", err)
		}
		if err := tx.Commit(ctx); err == nil {
			t.Fatal("a commit leaving two items on slot 0 succeeded — the invariant has been weakened")
		}
	})
	loaded, err = repo.Find(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Items) != 2 || loaded.Items[0].SlotIndex != 0 || loaded.Items[1].SlotIndex != 1 {
		t.Fatalf("the rolled-back transaction disturbed the order: %+v", loaded.Items)
	}
}

func withTx(t *testing.T, pool *database.Pool, body func(tx pgx.Tx)) {
	t.Helper()
	tx, err := pool.Pool().Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	body(tx)
}

func TestUpdate_WritesOnlyThePatchedColumns(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()

	room, err := repo.Create(ctx, domain.CollectionRoom{
		AccountID: newAccount(t, pool, "owner"), Name: "Original",
		CategoryID: seededCategory, DesignID: seededDesign, CurrentTier: domain.BaseTier,
	})
	if err != nil {
		t.Fatal(err)
	}

	name := "Renamed"
	if err := repo.Update(ctx, room.ID, domain.CollectionRoomPatch{Name: &name}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	after, err := repo.Find(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Name != "Renamed" {
		t.Fatalf("Name = %q, want Renamed", after.Name)
	}
	if after.CategoryID != seededCategory || after.DesignID != seededDesign {
		t.Fatalf("a name-only patch disturbed the references: %q / %q", after.CategoryID, after.DesignID)
	}
	if after.CurrentTier != room.CurrentTier {
		t.Fatalf("CurrentTier moved from %d to %d during a rename", room.CurrentTier, after.CurrentTier)
	}
	if !after.UpdatedAt.After(room.UpdatedAt) && !after.UpdatedAt.Equal(room.UpdatedAt) {
		t.Fatalf("updated_at went backwards")
	}

	empty := ""
	if err := repo.Update(ctx, room.ID, domain.CollectionRoomPatch{DesignID: &empty}); err != nil {
		t.Fatalf("clear design: %v", err)
	}
	var designNull bool
	if err := pool.Pool().QueryRow(ctx,
		`SELECT design_id IS NULL FROM collection_rooms WHERE id = $1`, string(room.ID)).Scan(&designNull); err != nil {
		t.Fatal(err)
	}
	if !designNull {
		t.Fatal("clearing a design reference stored an empty string, not NULL")
	}
}

func TestDelete_CascadesToItemsOnly(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	account := newAccount(t, pool, "owner")

	doomed, err := repo.Create(ctx, domain.CollectionRoom{AccountID: account, Name: "Doomed", CurrentTier: domain.BaseTier})
	if err != nil {
		t.Fatal(err)
	}
	survivor, err := repo.Create(ctx, domain.CollectionRoom{AccountID: account, Name: "Survivor", CurrentTier: domain.BaseTier})
	if err != nil {
		t.Fatal(err)
	}
	insertItem(t, pool, string(doomed.ID), 0, seededModel)
	insertItem(t, pool, string(survivor.ID), 0, seededModel)

	if err := repo.Delete(ctx, doomed.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	var orphans int
	if err := pool.Pool().QueryRow(ctx,
		`SELECT count(*) FROM collection_items WHERE collection_room_id = $1`, string(doomed.ID)).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Fatalf("%d item rows outlived their Collection Room", orphans)
	}

	still, err := repo.Find(ctx, survivor.ID)
	if err != nil {
		t.Fatalf("survivor: %v", err)
	}
	if len(still.Items) != 1 {
		t.Fatalf("survivor has %d items, want 1", len(still.Items))
	}
	var accounts int
	if err := pool.Pool().QueryRow(ctx, `SELECT count(*) FROM accounts WHERE id = $1`, account).Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if accounts != 1 {
		t.Fatal("deleting a Collection Room removed its account")
	}

	if err := repo.Delete(ctx, doomed.ID); !errors.Is(err, domain.ErrCollectionRoomNotFound) {
		t.Fatalf("repeat Delete = %v, want ErrCollectionRoomNotFound", err)
	}
}

func TestMalformedID_IsNotFound(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()

	name := "x"
	for _, id := range []domain.CollectionRoomID{"", "not-a-uuid", "../../etc/passwd", "'; DROP TABLE collection_rooms;--",
		domain.CollectionRoomID(strings.Repeat("a", 500))} {
		if _, err := repo.Find(ctx, id); !errors.Is(err, domain.ErrCollectionRoomNotFound) {
			t.Fatalf("Find(%q) = %v, want ErrCollectionRoomNotFound", id, err)
		}
		if err := repo.Update(ctx, id, domain.CollectionRoomPatch{Name: &name}); !errors.Is(err, domain.ErrCollectionRoomNotFound) {
			t.Fatalf("Update(%q) = %v, want ErrCollectionRoomNotFound", id, err)
		}
		if err := repo.Delete(ctx, id); !errors.Is(err, domain.ErrCollectionRoomNotFound) {
			t.Fatalf("Delete(%q) = %v, want ErrCollectionRoomNotFound", id, err)
		}
	}

	if _, err := repo.Create(ctx, domain.CollectionRoom{
		AccountID: newAccount(t, pool, "owner"), Name: "still works", CurrentTier: domain.BaseTier,
	}); err != nil {
		t.Fatalf("Create after malformed-id probes: %v", err)
	}
}

func TestCreate_ConcurrentForOneAccount_AllSucceed(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	account := newAccount(t, pool, "collector")

	const attempts = 8
	var wg sync.WaitGroup
	errs := make([]error, attempts)
	for index := 0; index < attempts; index++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			_, errs[slot] = repo.Create(ctx, domain.CollectionRoom{
				AccountID: account, Name: "Concurrent", CurrentTier: domain.BaseTier,
			})
		}(index)
	}
	wg.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("concurrent Create #%d failed: %v", index, err)
		}
	}
	rooms, err := repo.ListForAccount(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != attempts {
		t.Fatalf("%d rooms exist after %d concurrent creates", len(rooms), attempts)
	}
}

func TestSchema_NoForeignKeyBetweenTheCollectionAndMuseumTrees(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	collectionTables := []string{"collection_rooms", "collection_items"}
	museumTables := []string{"museums", "rooms", "room_photo_slots", "room_sculptures"}

	rows, err := pool.Pool().Query(ctx, `
		SELECT c.conrelid::regclass::text, c.confrelid::regclass::text
		FROM pg_constraint c
		WHERE c.contype = 'f'
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	contains := func(list []string, value string) bool {
		for _, entry := range list {
			if entry == value {
				return true
			}
		}
		return false
	}

	for rows.Next() {
		var from, to string
		if err := rows.Scan(&from, &to); err != nil {
			t.Fatal(err)
		}
		if contains(collectionTables, from) && contains(museumTables, to) {
			t.Fatalf("foreign key from %s to %s — the Collection tree must not reference the Museum tree (`01` §5.1)", from, to)
		}
		if contains(museumTables, from) && contains(collectionTables, to) {
			t.Fatalf("foreign key from %s to %s — the Museum tree must not reference the Collection tree (`01` §5.1)", from, to)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestSchema_OpenDecisionsHaveNoColumns(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	forbidden := map[string]map[string]string{
		"collection_rooms": {
			"privacy":   " CLOSED: no privacy state exists, by decision",
			"is_public": " CLOSED: no privacy state exists, by decision",
		},
		"collection_items": {
			"notes":       " is OPEN — owner-authored personal metadata is undecided",
			"note":        " is OPEN",
			"caption":     " is OPEN — and a caption is a Museum-photo concept, not a Collection one",
			"description": " is OPEN",
		},
	}

	for table, columns := range forbidden {
		for column, reason := range columns {
			var exists bool
			err := pool.Pool().QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_name = $1 AND column_name = $2
				)
			`, table, column).Scan(&exists)
			if err != nil {
				t.Fatal(err)
			}
			if exists {
				t.Fatalf("%s.%s exists — %s", table, column, reason)
			}
		}
	}
}

// MARK: -: the Collection Category registry as a schema fact

func TestCollectionCategories_SeededSetIsExactlyTheFour(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	rows, err := pool.Pool().Query(ctx,
		`SELECT id, display_name FROM collection_categories ORDER BY sort_order, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	type entry struct{ id, name string }
	var got []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.name); err != nil {
			t.Fatal(err)
		}
		got = append(got, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	want := []entry{
		{"category_watches", "Watches"},
		{"category_hot_wheels", "Hot Wheels"},
		{"category_coins", "Coins"},
		{"category_license_plates", "License Plates"},
	}
	if len(got) != len(want) {
		t.Fatalf("registry holds %d categories, want exactly %d: %+v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("category %d is %+v, want %+v — sort_order fixes the presentation order `02`'s cards use",
				index, got[index], want[index])
		}
	}
}

func TestCollectionCategories_SeedingDoesNotPruneExtraRows(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	if _, err := pool.Pool().Exec(ctx,
		`INSERT INTO collection_categories (id, display_name, sort_order) VALUES ('category_stamps', 'Stamps', 50)
		 ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := pool.Pool().Exec(ctx, `DELETE FROM collection_rooms WHERE category_id = 'category_stamps'`); err != nil {
			t.Errorf("cleanup rooms: %v", err)
		}
		if _, err := pool.Pool().Exec(ctx, `DELETE FROM collection_categories WHERE id = 'category_stamps'`); err != nil {
			t.Errorf("cleanup category: %v", err)
		}
	})

	if err := catalinfra.NewPostgresCatalogRepository(pool.Pool()).EnsureSeeded(ctx); err != nil {
		t.Fatalf("re-seed: %v", err)
	}

	var exists bool
	if err := pool.Pool().QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM collection_categories WHERE id = 'category_stamps')`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("re-seeding removed a category that was added directly — the seed must be a starting set, not an authority that prunes")
	}

	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	if _, err := repo.Create(ctx, domain.CollectionRoom{
		AccountID: newAccount(t, pool, "owner"), Name: "Stamps",
		CategoryID: "category_stamps", CurrentTier: domain.BaseTier,
	}); err != nil {
		t.Fatalf("create against a directly-inserted category: %v", err)
	}
}

func TestCollectionRooms_UnknownCategoryIsRefusedByTheForeignKey(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	account := newAccount(t, pool, "owner")

	_, err := pool.Pool().Exec(ctx,
		`INSERT INTO collection_rooms (account_id, name, category_id, current_tier) VALUES ($1, 'Nope', 'category_no_such_vertical', 1)`,
		account)
	if err == nil {
		t.Fatal("a Collection Room referencing a nonexistent category was accepted — collection_rooms_category_fk is not doing its job")
	}

	if _, err := pool.Pool().Exec(ctx,
		`INSERT INTO collection_rooms (account_id, name, category_id, current_tier) VALUES ($1, 'Legacy', NULL, 1)`,
		account); err != nil {
		t.Fatalf("a NULL category was refused: %v", err)
	}
}

func TestCollectionCategories_CannotBeDeletedWhileReferenced(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()

	if _, err := repo.Create(ctx, domain.CollectionRoom{
		AccountID: newAccount(t, pool, "owner"), Name: "Watches",
		CategoryID: seededCategory, CurrentTier: domain.BaseTier,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Pool().Exec(ctx,
		`DELETE FROM collection_categories WHERE id = $1`, seededCategory); err == nil {
		t.Fatal("a referenced category was deleted — ON DELETE RESTRICT is missing")
	}
}

func TestMigration0014_ClearsUnvalidatedCategoriesWithoutFabricating(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	account := newAccount(t, pool, "owner")

	var legacyID, currentID string
	if err := pool.Pool().QueryRow(ctx,
		`INSERT INTO collection_rooms (account_id, name, category_id, current_tier) VALUES ($1, 'Legacy', NULL, 1) RETURNING id`,
		account).Scan(&legacyID); err != nil {
		t.Fatal(err)
	}
	if err := pool.Pool().QueryRow(ctx,
		`INSERT INTO collection_rooms (account_id, name, category_id, current_tier) VALUES ($1, 'Current', $2, 1) RETURNING id`,
		account, seededCategory).Scan(&currentID); err != nil {
		t.Fatal(err)
	}

	if _, err := pool.Pool().Exec(ctx, `
		UPDATE collection_rooms
		   SET category_id = NULL, updated_at = now()
		 WHERE category_id IS NOT NULL
		   AND NOT EXISTS (SELECT 1 FROM collection_categories c WHERE c.id = collection_rooms.category_id)
	`); err != nil {
		t.Fatal(err)
	}

	var current *string
	if err := pool.Pool().QueryRow(ctx,
		`SELECT category_id FROM collection_rooms WHERE id = $1`, currentID).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if current == nil || *current != seededCategory {
		t.Fatalf("a valid category was cleared: %v", current)
	}

	var legacy *string
	if err := pool.Pool().QueryRow(ctx,
		`SELECT category_id FROM collection_rooms WHERE id = $1`, legacyID).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	if legacy != nil {
		t.Fatalf("a category was fabricated for a legacy row: %q", *legacy)
	}
}

// MARK: -: the tier ratchet, at the database

func newRoomWithDesign(t *testing.T, pool *database.Pool, repo *infrastructure.PostgresCollectionRoomRepository) domain.CollectionRoom {
	t.Helper()
	room, err := repo.Create(context.Background(), domain.CollectionRoom{
		AccountID:   newAccount(t, pool, "owner"),
		Name:        "Watches",
		CategoryID:  seededCategory,
		DesignID:    seededDesign,
		CurrentTier: domain.BaseTier,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return room
}

func storedTier(t *testing.T, pool *database.Pool, id domain.CollectionRoomID) int {
	t.Helper()
	var tier int
	if err := pool.Pool().QueryRow(context.Background(),
		`SELECT current_tier FROM collection_rooms WHERE id = $1`, string(id)).Scan(&tier); err != nil {
		t.Fatal(err)
	}
	return tier
}

func TestRatchetTier_RaisesAndIsIdempotent(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	room := newRoomWithDesign(t, pool, repo)

	raised, err := repo.RatchetTier(ctx, room.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !raised {
		t.Fatal("the first raise should report that it moved")
	}
	if got := storedTier(t, pool, room.ID); got != 3 {
		t.Fatalf("stored tier = %d, want 3", got)
	}

	raised, err = repo.RatchetTier(ctx, room.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if raised {
		t.Fatal("a repeat request must report that nothing moved")
	}
	if got := storedTier(t, pool, room.ID); got != 3 {
		t.Fatalf("a repeat request changed the tier to %d", got)
	}
}

func TestRatchetTier_NeverLowers(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	room := newRoomWithDesign(t, pool, repo)

	if _, err := repo.RatchetTier(ctx, room.ID, 3); err != nil {
		t.Fatal(err)
	}
	for _, lower := range []domain.Tier{2, 1, 0, -5} {
		raised, err := repo.RatchetTier(ctx, room.ID, lower)
		if err != nil {
			t.Fatalf("request for tier %d errored: %v", lower, err)
		}
		if raised {
			t.Fatalf("a request for tier %d reported a change", lower)
		}
		if got := storedTier(t, pool, room.ID); got != 3 {
			t.Fatalf("the tier fell to %d after a request for %d — forbids retraction", got, lower)
		}
	}
}

func TestRatchetTier_TouchesNothingElse(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	room := newRoomWithDesign(t, pool, repo)
	insertItem(t, pool, string(room.ID), 0, seededModel)
	insertItem(t, pool, string(room.ID), 1, seededModel)

	before, err := repo.Find(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RatchetTier(ctx, room.ID, 2); err != nil {
		t.Fatal(err)
	}
	after, err := repo.Find(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}

	if after.CurrentTier != 2 {
		t.Fatalf("CurrentTier = %d, want 2", after.CurrentTier)
	}
	if after.Name != before.Name || after.CategoryID != before.CategoryID || after.DesignID != before.DesignID {
		t.Fatalf("a tier bump disturbed the Room: %+v vs %+v", before, after)
	}
	if len(after.Items) != len(before.Items) {
		t.Fatalf("a tier bump changed the item count: %d → %d", len(before.Items), len(after.Items))
	}
	for index := range after.Items {
		if after.Items[index] != before.Items[index] {
			t.Fatalf("a tier bump changed item %d", index)
		}
	}
}

func TestRatchetTier_ConcurrentRequestsLeaveTheHighest(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	room := newRoomWithDesign(t, pool, repo)

	requests := []domain.Tier{3, 1, 2, 3, 1, 2, 3, 1}
	var wg sync.WaitGroup
	errs := make([]error, len(requests))
	for index, tier := range requests {
		wg.Add(1)
		go func(slot int, requested domain.Tier) {
			defer wg.Done()
			_, errs[slot] = repo.RatchetTier(ctx, room.ID, requested)
		}(index, tier)
	}
	wg.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("concurrent request #%d failed: %v", index, err)
		}
	}
	if got := storedTier(t, pool, room.ID); got != 3 {
		t.Fatalf("stored tier = %d after interleaved concurrent requests, want 3", got)
	}
}

func TestRatchetTier_SurvivesAReconnect(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	room := newRoomWithDesign(t, pool, repo)

	if _, err := repo.RatchetTier(ctx, room.ID, 3); err != nil {
		t.Fatal(err)
	}

	fresh, err := database.Connect(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	reloaded, err := infrastructure.NewPostgresCollectionRoomRepository(fresh.Pool()).Find(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.CurrentTier != 3 {
		t.Fatalf("after a reconnect the tier is %d, want 3", reloaded.CurrentTier)
	}
}

func TestCollectionDesigns_FixtureAuthorsThreeTiers(t *testing.T) {
	pool := testPool(t)

	var tierCount int
	if err := pool.Pool().QueryRow(context.Background(),
		`SELECT tier_count FROM collection_designs WHERE id = $1`, seededDesign).Scan(&tierCount); err != nil {
		t.Fatal(err)
	}
	if tierCount != 3 {
		t.Fatalf("the fixture Design authors %d tiers, want 3 (matching its bundle's layout.json)", tierCount)
	}

	for _, column := range []string{"tier_capacities", "capacity", "item_capacity", "tier_capacity"} {
		var exists bool
		if err := pool.Pool().QueryRow(context.Background(), `
			SELECT EXISTS (SELECT 1 FROM information_schema.columns
			               WHERE table_name = 'collection_designs' AND column_name = $1)
		`, column).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Errorf("collection_designs.%s exists — capacities are authored in the Design's bundle, not the database", column)
		}
	}
}

// MARK: -: the item write path

func roomWithItems(t *testing.T, pool *database.Pool, slots ...int) (domain.CollectionRoomID, map[int]domain.CollectionItemID) {
	t.Helper()
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	room, err := repo.Create(ctx, domain.CollectionRoom{
		AccountID:   newAccount(t, pool, "owner"),
		Name:        "Room",
		CategoryID:  seededCategory,
		DesignID:    seededDesign,
		CurrentTier: domain.BaseTier,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, slot := range slots {
		insertItem(t, pool, string(room.ID), slot, seededModel)
	}
	loaded, err := repo.Find(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	bySlot := map[int]domain.CollectionItemID{}
	for _, item := range loaded.Items {
		bySlot[item.SlotIndex] = item.ID
	}
	return room.ID, bySlot
}

func readSlots(t *testing.T, pool *database.Pool, roomID domain.CollectionRoomID) map[int]domain.CollectionItemID {
	t.Helper()
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	loaded, err := repo.Find(context.Background(), roomID)
	if err != nil {
		t.Fatal(err)
	}
	bySlot := map[int]domain.CollectionItemID{}
	for _, item := range loaded.Items {
		bySlot[item.SlotIndex] = item.ID
	}
	return bySlot
}

func TestInsertItem_StoresARowAndRefusesAnUnknownModel(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	roomID, _ := roomWithItems(t, pool)

	item, err := repo.InsertItem(ctx, roomID, 0, seededModel)
	if err != nil {
		t.Fatal(err)
	}
	if item.ID == "" || item.SlotIndex != 0 || item.CatalogModelID != seededModel {
		t.Fatalf("unexpected item: %+v", item)
	}

	if _, err := repo.InsertItem(ctx, roomID, 1, "dev-fixture:model-nope"); !errors.Is(err, domain.ErrModelNotAvailable) {
		t.Fatalf("unknown model → %v, want ErrModelNotAvailable", err)
	}
	if _, err := repo.InsertItem(ctx, roomID, 0, seededModel); !errors.Is(err, domain.ErrItemSlotTaken) {
		t.Fatalf("occupied slot → %v, want ErrItemSlotTaken", err)
	}
}

func TestSwapItemSlots_ExchangesTwoRowsInOneStatement(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	roomID, bySlot := roomWithItems(t, pool, 0, 1, 2)

	if err := repo.SwapItemSlots(ctx, roomID, bySlot[0], bySlot[2]); err != nil {
		t.Fatalf("single-statement swap failed: %v", err)
	}
	after := readSlots(t, pool, roomID)
	if after[0] != bySlot[2] || after[2] != bySlot[0] {
		t.Fatalf("rows did not exchange: slot 0 holds %q, slot 2 holds %q", after[0], after[2])
	}
	if after[1] != bySlot[1] {
		t.Fatalf("the uninvolved item moved; slot 1 holds %q, want %q", after[1], bySlot[1])
	}
	if len(after) != 3 {
		t.Fatalf("%d items after a swap, want 3", len(after))
	}
}

func TestSwapItemSlots_TouchesOnlySlotIndexAndUpdatedAt(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	roomID, bySlot := roomWithItems(t, pool, 0, 1)

	type row struct {
		id      string
		slot    int
		model   string
		created string
	}
	read := func() []row {
		rows, err := pool.Pool().Query(ctx,
			`SELECT id::text, slot_index, catalog_model_id, created_at::text
			   FROM collection_items WHERE collection_room_id = $1 ORDER BY id`, string(roomID))
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.slot, &r.model, &r.created); err != nil {
				t.Fatal(err)
			}
			out = append(out, r)
		}
		return out
	}

	before := read()
	if err := repo.SwapItemSlots(ctx, roomID, bySlot[0], bySlot[1]); err != nil {
		t.Fatal(err)
	}
	after := read()

	if len(before) != len(after) {
		t.Fatalf("row count changed from %d to %d", len(before), len(after))
	}
	for index := range before {
		if before[index].id != after[index].id {
			t.Fatalf("row identity changed: %q → %q", before[index].id, after[index].id)
		}
		if before[index].model != after[index].model {
			t.Fatalf("catalog_model_id changed on %q", before[index].id)
		}
		if before[index].created != after[index].created {
			t.Fatalf("created_at changed on %q", before[index].id)
		}
		if before[index].slot == after[index].slot {
			t.Fatalf("slot_index did not change on %q — the swap did nothing", before[index].id)
		}
	}
}

func TestMoveItemToSlot_MovesOneRowAndLeavesTheHole(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	roomID, bySlot := roomWithItems(t, pool, 0, 1, 2)

	if err := repo.MoveItemToSlot(ctx, roomID, bySlot[1], 7); err != nil {
		t.Fatal(err)
	}
	after := readSlots(t, pool, roomID)
	if after[7] != bySlot[1] {
		t.Fatalf("slot 7 holds %q, want %q", after[7], bySlot[1])
	}
	if _, filled := after[1]; filled {
		t.Fatal("slot 1 was backfilled — a move must leave the hole it made")
	}
	if after[0] != bySlot[0] || after[2] != bySlot[2] {
		t.Fatal("an unrelated item moved")
	}

	if err := repo.MoveItemToSlot(ctx, roomID, bySlot[0], 2); !errors.Is(err, domain.ErrItemSlotTaken) {
		t.Fatalf("moving onto an occupied slot → %v, want ErrItemSlotTaken", err)
	}
}

func TestItemWrites_RefuseForeignAndMalformedIdentities(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	roomID, bySlot := roomWithItems(t, pool, 0, 1)
	otherRoomID, otherBySlot := roomWithItems(t, pool, 0)

	const bogus = "00000000-0000-4000-8000-000000000000"
	for name, id := range map[string]domain.CollectionItemID{
		"nonexistent":         bogus,
		"malformed":           "not-a-uuid",
		"another Room's item": otherBySlot[0],
	} {
		if err := repo.MoveItemToSlot(ctx, roomID, id, 5); !errors.Is(err, domain.ErrItemNotInRoom) {
			t.Fatalf("move (%s) → %v, want ErrItemNotInRoom", name, err)
		}
		if err := repo.SwapItemSlots(ctx, roomID, bySlot[0], id); !errors.Is(err, domain.ErrItemNotInRoom) {
			t.Fatalf("swap (%s) → %v, want ErrItemNotInRoom", name, err)
		}
	}
	if err := repo.SwapItemSlots(ctx, roomID, bySlot[0], bySlot[0]); !errors.Is(err, domain.ErrItemNotInRoom) {
		t.Fatalf("self-swap → %v, want ErrItemNotInRoom", err)
	}
	if got := readSlots(t, pool, roomID); got[0] != bySlot[0] || got[1] != bySlot[1] || len(got) != 2 {
		t.Fatalf("the Room changed: %v", got)
	}
	if got := readSlots(t, pool, otherRoomID); got[0] != otherBySlot[0] || len(got) != 1 {
		t.Fatalf("the other Room changed: %v", got)
	}
}

func TestLockRoomForUpdate_SerializesConcurrentPlacements(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	roomID, _ := roomWithItems(t, pool)

	const attempts = 8
	var wait sync.WaitGroup
	errs := make([]error, attempts)
	for attempt := 0; attempt < attempts; attempt++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			errs[index] = pool.Run(ctx, func(ctx context.Context) error {
				locked, err := repo.LockRoomForUpdate(ctx, roomID)
				if err != nil {
					return err
				}
				_, err = repo.InsertItem(ctx, roomID, domain.LowestFreeSlotIndex(locked.Items), seededModel)
				return err
			})
		}(attempt)
	}
	wait.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("concurrent placement %d failed: %v", index, err)
		}
	}
	after := readSlots(t, pool, roomID)
	if len(after) != attempts {
		t.Fatalf("%d distinct slots after %d concurrent placements", len(after), attempts)
	}
	for slot := 0; slot < attempts; slot++ {
		if after[slot] == "" {
			t.Fatalf("slot %d is empty — the lowest-free rule must produce 0..%d", slot, attempts-1)
		}
	}
}

func TestLockRoomForUpdate_SerializesConcurrentSwaps(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()
	roomID, bySlot := roomWithItems(t, pool, 0, 1, 2, 3)

	identities := map[domain.CollectionItemID]bool{}
	for _, id := range bySlot {
		identities[id] = true
	}

	const attempts = 12
	var wait sync.WaitGroup
	errs := make([]error, attempts)
	for attempt := 0; attempt < attempts; attempt++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			target := (index * 3) % 4
			errs[index] = pool.Run(ctx, func(ctx context.Context) error {
				locked, err := repo.LockRoomForUpdate(ctx, roomID)
				if err != nil {
					return err
				}
				subject := locked.Items[index%len(locked.Items)]
				change, err := domain.ResolveSlotChange(locked.Items, subject.ID, target, 4)
				if err != nil {
					return err
				}
				switch change.Kind {
				case domain.SlotChangeNone:
					return nil
				case domain.SlotChangeSwap:
					return repo.SwapItemSlots(ctx, roomID, change.Item.ID, change.Displaced.ID)
				default:
					return repo.MoveItemToSlot(ctx, roomID, change.Item.ID, change.TargetSlotIndex)
				}
			})
		}(attempt)
	}
	wait.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("concurrent swap %d failed: %v", index, err)
		}
	}

	after := readSlots(t, pool, roomID)
	if len(after) != 4 {
		t.Fatalf("%d distinct slots after concurrent swaps, want 4 — a slot was duplicated", len(after))
	}
	seen := map[domain.CollectionItemID]bool{}
	for _, id := range after {
		if seen[id] {
			t.Fatalf("item %q occupies two slots", id)
		}
		seen[id] = true
		if !identities[id] {
			t.Fatalf("item %q appeared out of nowhere", id)
		}
	}
	if len(seen) != len(identities) {
		t.Fatalf("%d of %d items survived", len(seen), len(identities))
	}
}
