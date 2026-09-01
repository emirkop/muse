package infrastructure_test

import (
	"context"
	"errors"
	"testing"

	"muse-backend/internal/collection/domain"
	"muse-backend/internal/collection/infrastructure"
	"muse-backend/internal/platform/database"
)

func seedDevTrack(t *testing.T, pool *database.Pool, id string) string {
	t.Helper()
	_, err := pool.Pool().Exec(context.Background(), `
		INSERT INTO music_tracks (id, display_name, attribution, licensing, storage_key, content_type, duration_seconds)
		VALUES ($1, 'DEV TEST TONE — not licensed content', 'Muse (test audio)', 'dev_test', $2, 'audio/mpeg', 12)
		ON CONFLICT (id) DO NOTHING`, id, "music/dev/"+id+".mp3")
	if err != nil {
		t.Fatalf("seed dev track: %v", err)
	}
	return id
}

func newMusicRoom(t *testing.T, pool *database.Pool) (*infrastructure.PostgresCollectionRoomRepository, domain.CollectionRoom) {
	t.Helper()
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	room, err := repo.Create(context.Background(), domain.CollectionRoom{
		AccountID:   newAccount(t, pool, "music owner"),
		Name:        "Watches",
		CategoryID:  "category_watches",
		CurrentTier: domain.BaseTier,
	})
	if err != nil {
		t.Fatal(err)
	}
	return repo, room
}

func otherColumns(t *testing.T, pool *database.Pool, id domain.CollectionRoomID) string {
	t.Helper()
	var state string
	err := pool.Pool().QueryRow(context.Background(), `
		SELECT account_id::text || '|' || name || '|' || coalesce(category_id, '') || '|' || coalesce(design_id, '') || '|' || current_tier::text
		FROM collection_rooms WHERE id = $1`, string(id)).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestSetMusic_WritesTheOneColumn_AndClearsToNull(t *testing.T) {
	pool := testPool(t)
	repo, room := newMusicRoom(t, pool)
	ctx := context.Background()
	track := seedDevTrack(t, pool, "track_dev_repo_a")
	before := otherColumns(t, pool, room.ID)

	if err := repo.SetMusic(ctx, room.ID, &track); err != nil {
		t.Fatal(err)
	}
	found, err := repo.Find(ctx, room.ID)
	if err != nil || found.MusicTrackID != track {
		t.Fatalf("after assign: %+v %v", found, err)
	}
	if after := otherColumns(t, pool, room.ID); after != before {
		t.Fatalf("a music write touched another column: %s → %s", before, after)
	}

	if err := repo.SetMusic(ctx, room.ID, nil); err != nil {
		t.Fatal(err)
	}
	found, _ = repo.Find(ctx, room.ID)
	if found.MusicTrackID != "" {
		t.Fatalf("clear must leave no music: %q", found.MusicTrackID)
	}
	var isNull bool
	if err := pool.Pool().QueryRow(ctx, `SELECT music_track_id IS NULL FROM collection_rooms WHERE id = $1`, string(room.ID)).Scan(&isNull); err != nil {
		t.Fatal(err)
	}
	if !isNull {
		t.Fatal("\"no music\" must be NULL in the database, one state rather than two")
	}
}

func TestSetMusic_UnknownRoomIsNotFound(t *testing.T) {
	pool := testPool(t)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	track := seedDevTrack(t, pool, "track_dev_repo_b")
	for _, id := range []domain.CollectionRoomID{"not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		if err := repo.SetMusic(context.Background(), id, &track); !errors.Is(err, domain.ErrCollectionRoomNotFound) {
			t.Errorf("%s: %v", id, err)
		}
	}
}

func TestSetMusic_TheCatalogIsARealForeignKey(t *testing.T) {
	pool := testPool(t)
	repo, room := newMusicRoom(t, pool)
	ctx := context.Background()

	bogus := "track_that_does_not_exist"
	err := repo.SetMusic(ctx, room.ID, &bogus)
	if err == nil || errors.Is(err, domain.ErrCollectionRoomNotFound) {
		t.Fatalf("a track outside the catalog must be refused by the foreign key, got %v", err)
	}
	found, _ := repo.Find(ctx, room.ID)
	if found.MusicTrackID != "" {
		t.Fatalf("nothing may be stored: %q", found.MusicTrackID)
	}

	track := seedDevTrack(t, pool, "track_dev_repo_c")
	if err := repo.SetMusic(ctx, room.ID, &track); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Pool().Exec(ctx, `DELETE FROM music_tracks WHERE id = $1`, track); err == nil {
		t.Fatal("deleting a referenced track must be refused")
	}
}

func TestSchema_CollectionRoomMusicIsARealCatalogReference(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	var nullable string
	err := pool.Pool().QueryRow(ctx, `
		SELECT is_nullable FROM information_schema.columns
		WHERE table_name = 'collection_rooms' AND column_name = 'music_track_id'`).Scan(&nullable)
	if err != nil {
		t.Fatalf("collection_rooms.music_track_id must exist (migration 0023): %v", err)
	}
	if nullable != "YES" {
		t.Fatal("music_track_id must be nullable — no music is a normal state (`01` §4.8)")
	}

	rows, err := pool.Pool().Query(ctx, `
		SELECT confrelid::regclass::text, confdeltype
		FROM pg_constraint WHERE conrelid = 'collection_rooms'::regclass AND contype = 'f' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	targets := map[string]string{}
	for rows.Next() {
		var target, onDelete string
		if err := rows.Scan(&target, &onDelete); err != nil {
			t.Fatal(err)
		}
		targets[target] = onDelete
	}
	if onDelete, ok := targets["music_tracks"]; !ok {
		t.Fatalf("collection_rooms must reference music_tracks; references %v", targets)
	} else if onDelete != "r" {
		t.Fatalf("music_tracks reference must be ON DELETE RESTRICT, got %q", onDelete)
	}
	for _, museumTable := range []string{"museums", "rooms", "room_photo_slots", "room_sculptures"} {
		if _, crosses := targets[museumTable]; crosses {
			t.Fatalf("collection_rooms must not reference the Museum tree (%s) — the trees share the catalog, not each other", museumTable)
		}
	}
}
