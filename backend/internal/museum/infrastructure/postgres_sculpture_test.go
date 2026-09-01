package infrastructure_test

import (
	"context"
	"errors"
	"testing"
	"time"

	catalinfra "muse-backend/internal/catalog/infrastructure"
	"muse-backend/internal/museum/domain"
	"muse-backend/internal/museum/infrastructure"
)

func seedRoomForSculptures(t *testing.T, repo *infrastructure.PostgresMuseumRepository, accountID string) domain.Room {
	t.Helper()
	museum := newMuseum(t, repo, accountID)
	room, err := repo.CreateRoom(context.Background(), domain.Room{
		MuseumID: museum.ID, Name: "R", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate,
	})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	return room
}

func sculptureRows(t *testing.T, repo *infrastructure.PostgresMuseumRepository, roomID domain.RoomID) map[int]string {
	t.Helper()
	room, err := repo.FindRoom(context.Background(), roomID)
	if err != nil {
		t.Fatalf("find room: %v", err)
	}
	out := map[int]string{}
	for _, sculpture := range room.Sculptures {
		out[sculpture.SlotIndex] = sculpture.CatalogID
	}
	return out
}

// MARK: - The production catalog is genuinely empty

func TestSculptureCatalog_IsEmptyAfterSeeding(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	if err := catalinfra.NewPostgresCatalogRepository(pool.Pool()).EnsureSeeded(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var count int
	if err := pool.Pool().QueryRow(ctx, `SELECT count(*) FROM sculptures`).Scan(&count); err != nil {
		t.Fatalf("count sculptures: %v", err)
	}
	if count != 0 {
		t.Errorf("the production sculpture catalog must be empty until real content exists; found %d", count)
	}

	var styles int
	_ = pool.Pool().QueryRow(ctx, `SELECT count(*) FROM museum_styles`).Scan(&styles)
	if styles == 0 {
		t.Error("styles must still be seeded")
	}
}

func TestInsertSculpture_UnknownCatalogEntry_IsRefusedByTheDatabase(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	room := seedRoomForSculptures(t, repo, createAccount(t, pool))

	err := repo.InsertSculpture(context.Background(), room.ID, domain.SculptureInstance{
		SlotIndex: 0, CatalogID: "sculpture_never_seeded", CreatedAt: time.Now(),
	})

	if !errors.Is(err, domain.ErrUnknownSculpture) {
		t.Fatalf("expected ErrUnknownSculpture from the catalog foreign key, got %v", err)
	}
	if len(sculptureRows(t, repo, room.ID)) != 0 {
		t.Error("nothing may be placed")
	}
}

func TestSculptureCatalogEntry_ReferencedByARoom_CannotBeDeleted(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	room := seedRoomForSculptures(t, repo, createAccount(t, pool))
	catalogID := createSculptureCatalogEntry(t, pool, "sculpture_kept")
	if err := repo.InsertSculpture(ctx, room.ID, domain.SculptureInstance{SlotIndex: 0, CatalogID: catalogID, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if _, err := pool.Pool().Exec(ctx, `DELETE FROM sculptures WHERE id = $1`, catalogID); err == nil {
		t.Error("deleting a catalog entry a Room references must be refused")
	}
}

// MARK: - Placement and the cap

func TestInsertSculpture_PlacesAtTheGivenSlot_AndTheCapIsADatabaseInvariant(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	room := seedRoomForSculptures(t, repo, createAccount(t, pool))
	catalogID := createSculptureCatalogEntry(t, pool, "sculpture_one")

	for index := 0; index < domain.MaxSculpturesPerRoom; index++ {
		if err := repo.InsertSculpture(ctx, room.ID, domain.SculptureInstance{SlotIndex: index, CatalogID: catalogID, CreatedAt: time.Now()}); err != nil {
			t.Fatalf("slot %d must be within the cap: %v", index, err)
		}
	}

	rows := sculptureRows(t, repo, room.ID)
	if len(rows) != domain.MaxSculpturesPerRoom {
		t.Fatalf("expected %d sculptures, got %d", domain.MaxSculpturesPerRoom, len(rows))
	}

	err := repo.InsertSculpture(ctx, room.ID, domain.SculptureInstance{
		SlotIndex: domain.MaxSculpturesPerRoom, CatalogID: catalogID, CreatedAt: time.Now(),
	})
	if !errors.Is(err, domain.ErrInvalidSculptureSlot) {
		t.Fatalf("expected ErrInvalidSculptureSlot from the CHECK constraint, got %v", err)
	}
}

func TestInsertSculpture_OccupiedSlot_IsRefused(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	room := seedRoomForSculptures(t, repo, createAccount(t, pool))
	a := createSculptureCatalogEntry(t, pool, "sculpture_a")
	b := createSculptureCatalogEntry(t, pool, "sculpture_b")
	if err := repo.InsertSculpture(ctx, room.ID, domain.SculptureInstance{SlotIndex: 1, CatalogID: a, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("first: %v", err)
	}

	err := repo.InsertSculpture(ctx, room.ID, domain.SculptureInstance{SlotIndex: 1, CatalogID: b, CreatedAt: time.Now()})

	if !errors.Is(err, domain.ErrSlotOccupied) {
		t.Fatalf("expected ErrSlotOccupied, got %v", err)
	}
	if rows := sculptureRows(t, repo, room.ID); rows[1] != a {
		t.Errorf("slot 1 holds %q, want %q", rows[1], a)
	}
}

func TestInsertSculpture_TheSameCatalogEntryTwice_IsAllowed(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	room := seedRoomForSculptures(t, repo, createAccount(t, pool))
	catalogID := createSculptureCatalogEntry(t, pool, "sculpture_twice")

	for _, slot := range []int{0, 2} {
		if err := repo.InsertSculpture(ctx, room.ID, domain.SculptureInstance{SlotIndex: slot, CatalogID: catalogID, CreatedAt: time.Now()}); err != nil {
			t.Fatalf("slot %d: %v", slot, err)
		}
	}
	rows := sculptureRows(t, repo, room.ID)
	if rows[0] != catalogID || rows[2] != catalogID {
		t.Errorf("both copies must stand: %v", rows)
	}
}

// MARK: - Removal leaves the slot empty

func TestDeleteSculpture_LeavesTheSlotEmpty_AndMovesNothingElse(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	room := seedRoomForSculptures(t, repo, createAccount(t, pool))
	ids := []string{
		createSculptureCatalogEntry(t, pool, "sculpture_a"),
		createSculptureCatalogEntry(t, pool, "sculpture_b"),
		createSculptureCatalogEntry(t, pool, "sculpture_c"),
	}
	for index, id := range ids {
		if err := repo.InsertSculpture(ctx, room.ID, domain.SculptureInstance{SlotIndex: index, CatalogID: id, CreatedAt: time.Now()}); err != nil {
			t.Fatalf("seed %d: %v", index, err)
		}
	}

	if err := repo.DeleteSculpture(ctx, room.ID, 1); err != nil {
		t.Fatalf("DeleteSculpture: %v", err)
	}

	rows := sculptureRows(t, repo, room.ID)
	if len(rows) != 2 {
		t.Fatalf("expected 2 sculptures, got %v", rows)
	}
	if rows[0] != ids[0] || rows[2] != ids[2] {
		t.Errorf("surviving sculptures must not move: %v", rows)
	}
	if _, occupied := rows[1]; occupied {
		t.Error("slot 1 must be empty")
	}

	reuse := createSculptureCatalogEntry(t, pool, "sculpture_d")
	if err := repo.InsertSculpture(ctx, room.ID, domain.SculptureInstance{SlotIndex: 1, CatalogID: reuse, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("reuse slot 1: %v", err)
	}
	if rows := sculptureRows(t, repo, room.ID); rows[1] != reuse {
		t.Errorf("slot 1 should hold the new sculpture: %v", rows)
	}
}

func TestDeleteSculpture_EmptySlot_IsRefused(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	room := seedRoomForSculptures(t, repo, createAccount(t, pool))

	err := repo.DeleteSculpture(context.Background(), room.ID, 0)

	if !errors.Is(err, domain.ErrSculptureNotInRoom) {
		t.Fatalf("expected ErrSculptureNotInRoom, got %v", err)
	}
}

func TestDeleteSculpture_CannotReachAnotherRoomsSculpture(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	account := createAccount(t, pool)
	roomA := seedRoomForSculptures(t, repo, account)
	museum, _ := repo.FindMuseumByAccount(ctx, account)
	roomB, err := repo.CreateRoom(ctx, domain.Room{MuseumID: museum.ID, Name: "B", VariantID: "style_modern_variant_Hall", Privacy: domain.PrivacyPrivate})
	if err != nil {
		t.Fatalf("create room B: %v", err)
	}
	catalogID := createSculptureCatalogEntry(t, pool, "sculpture_a")
	if err := repo.InsertSculpture(ctx, roomA.ID, domain.SculptureInstance{SlotIndex: 0, CatalogID: catalogID, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := repo.DeleteSculpture(ctx, roomB.ID, 0); !errors.Is(err, domain.ErrSculptureNotInRoom) {
		t.Fatalf("expected ErrSculptureNotInRoom, got %v", err)
	}
	if rows := sculptureRows(t, repo, roomA.ID); rows[0] != catalogID {
		t.Error("a sculpture was removed across a Room boundary")
	}
}

func TestDeleteRoom_CascadesSculptures_ButNotTheCatalog(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresMuseumRepository(pool.Pool())
	ctx := context.Background()
	room := seedRoomForSculptures(t, repo, createAccount(t, pool))
	catalogID := createSculptureCatalogEntry(t, pool, "sculpture_a")
	if err := repo.InsertSculpture(ctx, room.ID, domain.SculptureInstance{SlotIndex: 0, CatalogID: catalogID, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := repo.DeleteRoom(ctx, room.ID); err != nil {
		t.Fatalf("delete room: %v", err)
	}

	var remaining int
	_ = pool.Pool().QueryRow(ctx, `SELECT count(*) FROM room_sculptures WHERE room_id = $1`, string(room.ID)).Scan(&remaining)
	if remaining != 0 {
		t.Errorf("sculpture rows must cascade away, %d remain", remaining)
	}
	var catalogRows int
	_ = pool.Pool().QueryRow(ctx, `SELECT count(*) FROM sculptures WHERE id = $1`, catalogID).Scan(&catalogRows)
	if catalogRows != 1 {
		t.Error("the catalog entry is Platform-owned and must survive a Room deletion")
	}
}
