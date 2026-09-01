package infrastructure_test

import (
	"context"
	"errors"
	"os"
	"testing"

	catalinfra "muse-backend/internal/catalog/infrastructure"
	"muse-backend/internal/museum/domain"
	"muse-backend/internal/museum/infrastructure"
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

	catalog := catalinfra.NewPostgresCatalogRepository(pool.Pool())
	if err := catalog.EnsureSeeded(ctx); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}

	if _, err := pool.Pool().Exec(ctx, `TRUNCATE room_photo_slots, room_sculptures, sculptures, rooms, museums, assets, email_outbox, password_credentials, pending_signups, password_resets, auth_attempts, external_identities, accounts CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}

func createCommittedAsset(t *testing.T, pool *database.Pool, accountID string, tag string) string {
	t.Helper()
	var id string
	err := pool.Pool().QueryRow(context.Background(), `
		INSERT INTO assets (account_id, category, storage_key, content_type, byte_size,
			pixel_width, pixel_height, checksum_sha256, state, client_upload_id, committed_at)
		VALUES ($1, 'room_photo', 'photos/' || $3 || '/' || $2, 'image/jpeg', 1024,
			1024, 768, repeat('0', 64), 'committed', $2, now())
		RETURNING id`, accountID, tag, accountID).Scan(&id)
	if err != nil {
		t.Fatalf("create committed asset: %v", err)
	}
	return id
}

func createSculptureCatalogEntry(t *testing.T, pool *database.Pool, id string) string {
	t.Helper()
	_, err := pool.Pool().Exec(context.Background(), `
		INSERT INTO sculptures (id, display_name, asset_bundle_id, asset_bundle_version)
		VALUES ($1, $1, 'bundle_test_' || $1, 1)
		ON CONFLICT (id) DO NOTHING`, id)
	if err != nil {
		t.Fatalf("create sculpture catalog entry: %v", err)
	}
	return id
}

func createAccount(t *testing.T, pool *database.Pool) string {
	t.Helper()
	var id string
	err := pool.Pool().QueryRow(context.Background(), `INSERT INTO accounts (display_name) VALUES ('') RETURNING id`).Scan(&id)
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return id
}

func newMuseum(t *testing.T, repo *infrastructure.PostgresMuseumRepository, accountID string) domain.Museum {
	t.Helper()
	museum, err := repo.CreateMuseum(context.Background(), domain.Museum{
		AccountID: accountID,
		StyleID:   "style_modern",
		Privacy:   domain.PrivacyPrivate,
	})
	if err != nil {
		t.Fatalf("create museum: %v", err)
	}
	return museum
}

// MARK: - 1:1 Museum per account

func TestCreateMuseum_SecondMuseumForSameAccount_IsRejected(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	accountID := createAccount(t, pool)

	newMuseum(t, repo, accountID)

	_, err := repo.CreateMuseum(context.Background(), domain.Museum{
		AccountID: accountID,
		StyleID:   "style_gothic",
		Privacy:   domain.PrivacyPrivate,
	})
	if !errors.Is(err, domain.ErrMuseumAlreadyExists) {
		t.Fatalf("expected ErrMuseumAlreadyExists, got %v", err)
	}
}

func TestCreateMuseum_DifferentAccounts_EachGetTheirOwn(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())

	first := newMuseum(t, repo, createAccount(t, pool))
	second := newMuseum(t, repo, createAccount(t, pool))

	if first.ID == second.ID {
		t.Fatal("expected distinct museums for distinct accounts")
	}
}

// MARK: - Capacity caps as data-layer invariants

func TestPhotoSlotCap_IsEnforcedByTheDatabase(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()

	museum := newMuseum(t, repo, createAccount(t, pool))
	room, err := repo.CreateRoom(ctx, domain.Room{
		MuseumID: museum.ID, Name: "Room", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate,
	})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	for index := 0; index < domain.MaxPhotosPerRoom; index++ {
		_, err := pool.Pool().Exec(ctx,
			`INSERT INTO room_photo_slots (room_id, slot_index) VALUES ($1, $2)`,
			string(room.ID), index)
		if err != nil {
			t.Fatalf("slot %d should be within the cap: %v", index, err)
		}
	}

	_, err = pool.Pool().Exec(ctx,
		`INSERT INTO room_photo_slots (room_id, slot_index) VALUES ($1, $2)`,
		string(room.ID), domain.MaxPhotosPerRoom)
	if err == nil {
		t.Fatal("expected the database to reject a slot index beyond the 28-photo cap")
	}
}

func TestPhotoSlot_CannotBeDoubleOccupied(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()

	museum := newMuseum(t, repo, createAccount(t, pool))
	room, _ := repo.CreateRoom(ctx, domain.Room{
		MuseumID: museum.ID, Name: "Room", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate,
	})

	if _, err := pool.Pool().Exec(ctx, `INSERT INTO room_photo_slots (room_id, slot_index) VALUES ($1, 0)`, string(room.ID)); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if _, err := pool.Pool().Exec(ctx, `INSERT INTO room_photo_slots (room_id, slot_index) VALUES ($1, 0)`, string(room.ID)); err == nil {
		t.Fatal("expected the database to reject two photos in the same logical slot")
	}
}

func TestSculptureCap_IsEnforcedByTheDatabase(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()

	museum := newMuseum(t, repo, createAccount(t, pool))
	room, _ := repo.CreateRoom(ctx, domain.Room{
		MuseumID: museum.ID, Name: "Room", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate,
	})

	catalogID := createSculptureCatalogEntry(t, pool, "sculpture_a")
	for index := 0; index < domain.MaxSculpturesPerRoom; index++ {
		_, err := pool.Pool().Exec(ctx,
			`INSERT INTO room_sculptures (room_id, slot_index, catalog_id) VALUES ($1, $2, $3)`,
			string(room.ID), index, catalogID)
		if err != nil {
			t.Fatalf("sculpture slot %d should be within the cap: %v", index, err)
		}
	}

	_, err := pool.Pool().Exec(ctx,
		`INSERT INTO room_sculptures (room_id, slot_index, catalog_id) VALUES ($1, $2, $3)`,
		string(room.ID), domain.MaxSculpturesPerRoom, catalogID)
	if err == nil {
		t.Fatal("expected the database to reject a sculpture beyond the 3-sculpture cap")
	}
}

// MARK: - Style change is a pure reference swap

func TestChangeStyle_IsAPureReferenceSwap_WithNoContentMutation(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()

	museum := newMuseum(t, repo, createAccount(t, pool))
	room, err := repo.CreateRoom(ctx, domain.Room{
		MuseumID: museum.ID, Name: "The Long Hall", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPublic,
	})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	for index := 0; index < 5; index++ {
		assetID := createCommittedAsset(t, pool, museum.AccountID, "asset_"+string(rune('a'+index)))
		if _, err := pool.Pool().Exec(ctx,
			`INSERT INTO room_photo_slots (room_id, slot_index, photo_asset_id, caption) VALUES ($1, $2, $3, $4)`,
			string(room.ID), index, assetID, "caption "+string(rune('a'+index))); err != nil {
			t.Fatalf("insert photo slot: %v", err)
		}
	}
	if _, err := pool.Pool().Exec(ctx,
		`INSERT INTO room_sculptures (room_id, slot_index, catalog_id) VALUES ($1, 0, $2)`,
		string(room.ID), createSculptureCatalogEntry(t, pool, "sculpture_x")); err != nil {
		t.Fatalf("insert sculpture: %v", err)
	}

	before, err := repo.FindRoom(ctx, room.ID)
	if err != nil {
		t.Fatalf("find room before: %v", err)
	}

	if err := repo.UpdateMuseumStyle(ctx, museum.ID, "style_gothic"); err != nil {
		t.Fatalf("change style: %v", err)
	}

	updatedMuseum, err := repo.FindMuseumByAccount(ctx, museum.AccountID)
	if err != nil {
		t.Fatalf("find museum after: %v", err)
	}
	if updatedMuseum.StyleID != "style_gothic" {
		t.Fatalf("expected style to change, got %q", updatedMuseum.StyleID)
	}

	after, err := repo.FindRoom(ctx, room.ID)
	if err != nil {
		t.Fatalf("find room after: %v", err)
	}

	if after.Name != before.Name {
		t.Errorf("room name changed: %q -> %q", before.Name, after.Name)
	}
	if after.Privacy != before.Privacy {
		t.Errorf("room privacy changed: %q -> %q", before.Privacy, after.Privacy)
	}
	if after.VariantID != before.VariantID {
		t.Errorf("room variant reference changed: %q -> %q", before.VariantID, after.VariantID)
	}
	if len(after.PhotoSlots) != len(before.PhotoSlots) {
		t.Fatalf("photo count changed: %d -> %d", len(before.PhotoSlots), len(after.PhotoSlots))
	}
	for index := range before.PhotoSlots {
		if after.PhotoSlots[index] != before.PhotoSlots[index] {
			t.Errorf("photo slot %d mutated by a style change: %+v -> %+v", index, before.PhotoSlots[index], after.PhotoSlots[index])
		}
	}
	if len(after.Sculptures) != len(before.Sculptures) {
		t.Fatalf("sculpture count changed: %d -> %d", len(before.Sculptures), len(after.Sculptures))
	}
	for index := range before.Sculptures {
		if after.Sculptures[index] != before.Sculptures[index] {
			t.Errorf("sculpture %d mutated by a style change", index)
		}
	}
}

// MARK: - Room CRUD

func TestRoomLifecycle_CreateListFindUpdateDelete(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()

	museum := newMuseum(t, repo, createAccount(t, pool))

	created, err := repo.CreateRoom(ctx, domain.Room{
		MuseumID: museum.ID, Name: "First", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	rooms, err := repo.ListRooms(ctx, museum.ID)
	if err != nil || len(rooms) != 1 {
		t.Fatalf("expected 1 room, got %d (%v)", len(rooms), err)
	}

	renamed, gallery, public := "Renamed", "style_modern_variant_Gallery", domain.PrivacyPublic
	if err := repo.UpdateRoom(ctx, created.ID, domain.RoomPatch{Name: &renamed, VariantID: &gallery, Privacy: &public}); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, err := repo.FindRoom(ctx, created.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if updated.Name != "Renamed" || updated.VariantID != "style_modern_variant_Gallery" || updated.Privacy != domain.PrivacyPublic {
		t.Fatalf("update did not apply: %+v", updated)
	}

	if err := repo.DeleteRoom(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.FindRoom(ctx, created.ID); !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("expected ErrRoomNotFound after delete, got %v", err)
	}
}

func TestDeleteRoom_CascadesItsContent(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()

	museum := newMuseum(t, repo, createAccount(t, pool))
	room, _ := repo.CreateRoom(ctx, domain.Room{
		MuseumID: museum.ID, Name: "Room", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate,
	})
	if _, err := pool.Pool().Exec(ctx, `INSERT INTO room_photo_slots (room_id, slot_index) VALUES ($1, 0)`, string(room.ID)); err != nil {
		t.Fatalf("insert slot: %v", err)
	}

	if err := repo.DeleteRoom(ctx, room.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var remaining int
	if err := pool.Pool().QueryRow(ctx, `SELECT count(*) FROM room_photo_slots WHERE room_id = $1`, string(room.ID)).Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected slot rows to cascade away, %d remain", remaining)
	}
}

// MARK: - Content never carries presentation data

func TestContentTables_CarryNoPresentationColumns(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	forbidden := []string{"transform", "mesh", "material", "texture", "position", "rotation", "scale", "geometry"}
	for _, table := range []string{"museums", "rooms", "room_photo_slots", "room_sculptures"} {
		rows, err := pool.Pool().Query(ctx,
			`SELECT column_name FROM information_schema.columns WHERE table_name = $1`, table)
		if err != nil {
			t.Fatalf("introspect %s: %v", table, err)
		}
		for rows.Next() {
			var column string
			if err := rows.Scan(&column); err != nil {
				t.Fatalf("scan column: %v", err)
			}
			for _, word := range forbidden {
				if column == word {
					t.Errorf("content table %s has presentation column %q — Content Models must never carry geometry or transform data", table, column)
				}
			}
		}
		rows.Close()
	}
}
