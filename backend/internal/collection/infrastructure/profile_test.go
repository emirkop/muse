package infrastructure_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"muse-backend/internal/collection/domain"
	"muse-backend/internal/collection/infrastructure"
	"muse-backend/internal/platform/database"
)

const (
	profileRooms        = 40
	profileItemsPerRoom = 250
	profileModels       = 5000
)

func requireProfiling(t *testing.T) {
	t.Helper()
	if os.Getenv("MUSE_PROFILE") == "" {
		t.Skip("MUSE_PROFILE not set — skipping profiling runs (they seed tens of thousands of rows)")
	}
}

func explain(t *testing.T, pool *database.Pool, sql string, args ...any) string {
	t.Helper()
	rows, err := pool.Pool().Query(context.Background(),
		"EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF, SUMMARY OFF) "+sql, args...)
	if err != nil {
		t.Fatalf("explain: %v\nsql: %s", err, sql)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return plan.String()
}

func seedLargeCollection(t *testing.T, pool *database.Pool) (accountID, biggestRoom string) {
	t.Helper()
	ctx := context.Background()
	accountID = newAccount(t, pool, "profile-owner")
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())

	start := time.Now()
	for roomIndex := 0; roomIndex < profileRooms; roomIndex++ {
		room, err := repo.Create(ctx, domain.CollectionRoom{
			AccountID:   accountID,
			Name:        fmt.Sprintf("Profile Room %d", roomIndex),
			CategoryID:  seededCategory,
			DesignID:    seededDesign,
			CurrentTier: domain.BaseTier,
		})
		if err != nil {
			t.Fatalf("create room %d: %v", roomIndex, err)
		}
		var values strings.Builder
		args := []any{string(room.ID), seededModel}
		for slot := 0; slot < profileItemsPerRoom; slot++ {
			if slot > 0 {
				values.WriteString(",")
			}
			fmt.Fprintf(&values, "($1, %d, $2)", slot)
		}
		if _, err := pool.Pool().Exec(ctx,
			`INSERT INTO collection_items (collection_room_id, slot_index, catalog_model_id) VALUES `+values.String(),
			args...); err != nil {
			t.Fatalf("seed items for room %d: %v", roomIndex, err)
		}
		if roomIndex == 0 {
			biggestRoom = string(room.ID)
		}
	}
	if _, err := pool.Pool().Exec(ctx, `ANALYZE collection_items, collection_rooms`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	t.Logf("seeded %d rooms × %d items = %d items in %s",
		profileRooms, profileItemsPerRoom, profileRooms*profileItemsPerRoom, time.Since(start).Round(time.Millisecond))
	return accountID, biggestRoom
}

func TestLargeCollectionRoomRead(t *testing.T) {
	requireProfiling(t)
	pool := testPool(t)
	_, biggestRoom := seedLargeCollection(t, pool)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()

	plan := explain(t, pool,
		`SELECT id, slot_index, catalog_model_id, created_at, updated_at
		   FROM collection_items WHERE collection_room_id = $1 ORDER BY slot_index`, biggestRoom)
	t.Logf("plan — large Room item read:\n%s", plan)

	if _, err := repo.Find(ctx, domain.CollectionRoomID(biggestRoom)); err != nil {
		t.Fatal(err)
	}
	const reads = 20
	start := time.Now()
	for i := 0; i < reads; i++ {
		room, err := repo.Find(ctx, domain.CollectionRoomID(biggestRoom))
		if err != nil {
			t.Fatal(err)
		}
		if len(room.Items) != profileItemsPerRoom {
			t.Fatalf("expected %d items, got %d", profileItemsPerRoom, len(room.Items))
		}
	}
	perRead := time.Since(start) / reads
	t.Logf("MEASURED: Find() on a %d-item Room: %s per read (mean of %d)", profileItemsPerRoom, perRead.Round(time.Microsecond), reads)

	if strings.Contains(plan, "Sort") {
		t.Logf("FINDING: the ordered read sorts rather than walking an ordered index:\n%s", plan)
	}
	if perRead > 200*time.Millisecond {
		t.Errorf("a %d-item Room read took %s — expected well under 200ms", profileItemsPerRoom, perRead)
	}
}

func TestAccountWideItemCount(t *testing.T) {
	requireProfiling(t)
	pool := testPool(t)
	accountID, _ := seedLargeCollection(t, pool)
	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	ctx := context.Background()

	plan := explain(t, pool,
		`SELECT count(*) FROM collection_items i
		   JOIN collection_rooms r ON r.id = i.collection_room_id
		  WHERE r.account_id = $1`, accountID)
	t.Logf("plan — account-wide item count:\n%s", plan)

	if _, err := repo.CountItemsForAccount(ctx, accountID); err != nil {
		t.Fatal(err)
	}
	const counts = 20
	start := time.Now()
	var total int
	for i := 0; i < counts; i++ {
		n, err := repo.CountItemsForAccount(ctx, accountID)
		if err != nil {
			t.Fatal(err)
		}
		total = n
	}
	perCount := time.Since(start) / counts
	if total != profileRooms*profileItemsPerRoom {
		t.Fatalf("expected %d items account-wide, got %d", profileRooms*profileItemsPerRoom, total)
	}
	t.Logf("MEASURED: CountItemsForAccount over %d rooms / %d items: %s per call (mean of %d)",
		profileRooms, total, perCount.Round(time.Microsecond), counts)

	if perCount > 100*time.Millisecond {
		t.Errorf("the account-wide count took %s over %d items — it runs under the account lock on every add", perCount, total)
	}
}

func TestCatalogSearch(t *testing.T) {
	requireProfiling(t)
	pool := testPool(t)
	ctx := context.Background()

	var brandID string
	if err := pool.Pool().QueryRow(ctx,
		`SELECT id FROM collection_brands LIMIT 1`).Scan(&brandID); err != nil {
		t.Fatalf("no seeded brand to attach synthetic Models to: %v", err)
	}
	start := time.Now()
	if _, err := pool.Pool().Exec(ctx, `
		INSERT INTO collection_models (id, brand_id, category_id, display_name, search_text, classification)
		SELECT 'dev-synthetic:model-' || g,
		       $1, $2,
		       'Synthetic Chronometer ' || g,
		       'Synthetic Chronometer ' || g || ' ref' || g,
		       'dev_fixture'
		  FROM generate_series(1, $3) AS g
		ON CONFLICT (id) DO NOTHING`, brandID, seededCategory, profileModels); err != nil {
		t.Fatalf("seed synthetic models: %v", err)
	}
	if _, err := pool.Pool().Exec(ctx, `ANALYZE collection_models`); err != nil {
		t.Fatal(err)
	}
	t.Logf("seeded %d synthetic catalog Models in %s", profileModels, time.Since(start).Round(time.Millisecond))

	repo := infrastructure.NewPostgresCollectionRoomRepository(pool.Pool())
	_ = repo

	browse := explain(t, pool, `
		SELECT m.id FROM collection_models m
		  JOIN collection_brands b ON b.id = m.brand_id
		 WHERE m.category_id = $1
		 ORDER BY m.display_name, m.id LIMIT 21`, seededCategory)
	t.Logf("plan — category browse (no terms):\n%s", browse)

	term := explain(t, pool, `
		SELECT m.id FROM collection_models m
		  JOIN collection_brands b ON b.id = m.brand_id
		 WHERE m.category_id = $1 AND m.search_document @@ to_tsquery('simple', $2)
		 ORDER BY m.display_name, m.id LIMIT 21`, seededCategory, "synthetic:*")
	t.Logf("plan — broad term match (matches nearly everything):\n%s", term)

	selective := explain(t, pool, `
		SELECT m.id FROM collection_models m
		  JOIN collection_brands b ON b.id = m.brand_id
		 WHERE m.category_id = $1 AND m.search_document @@ to_tsquery('simple', $2)
		 ORDER BY m.display_name, m.id LIMIT 21`, seededCategory, "ref4242:*")
	t.Logf("plan — selective term match (one row):\n%s", selective)

	for name, q := range map[string]struct {
		sql  string
		args []any
	}{
		"browse first page": {`SELECT m.id FROM collection_models m JOIN collection_brands b ON b.id = m.brand_id
			WHERE m.category_id = $1 ORDER BY m.display_name, m.id LIMIT 21`, []any{seededCategory}},
		"broad term": {`SELECT m.id FROM collection_models m JOIN collection_brands b ON b.id = m.brand_id
			WHERE m.category_id = $1 AND m.search_document @@ to_tsquery('simple', $2)
			ORDER BY m.display_name, m.id LIMIT 21`, []any{seededCategory, "synthetic:*"}},
		"selective term": {`SELECT m.id FROM collection_models m JOIN collection_brands b ON b.id = m.brand_id
			WHERE m.category_id = $1 AND m.search_document @@ to_tsquery('simple', $2)
			ORDER BY m.display_name, m.id LIMIT 21`, []any{seededCategory, "ref4242:*"}},
	} {
		const runs = 20
		s := time.Now()
		for i := 0; i < runs; i++ {
			rows, err := pool.Pool().Query(ctx, q.sql, q.args...)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			for rows.Next() {
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
		t.Logf("MEASURED: catalog search — %-18s %s per query (mean of %d, %d Models in category)",
			name, (time.Since(s) / runs).Round(time.Microsecond), runs, profileModels)
	}

	if _, err := pool.Pool().Exec(ctx,
		`DELETE FROM collection_models WHERE id LIKE 'dev-synthetic:model-%'`); err != nil {
		t.Fatalf("cleanup synthetic models: %v", err)
	}
}

func TestRedundantIndexAudit(t *testing.T) {
	requireProfiling(t)
	pool := testPool(t)

	rows, err := pool.Pool().Query(context.Background(), `
		WITH idx AS (
			SELECT c.relname   AS table_name,
			       i.relname   AS index_name,
			       ix.indisunique,
			       ix.indpred IS NOT NULL AS partial,
			       (SELECT string_agg(a.attname, ',' ORDER BY k.ord)
			          FROM unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord)
			          JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
			       ) AS cols
			  FROM pg_index ix
			  JOIN pg_class i ON i.oid = ix.indexrelid
			  JOIN pg_class c ON c.oid = ix.indrelid
			  JOIN pg_namespace n ON n.oid = c.relnamespace
			 WHERE n.nspname = 'public' AND i.relam = (SELECT oid FROM pg_am WHERE amname = 'btree')
		)
		SELECT a.table_name, a.index_name, a.cols, b.index_name, b.cols
		  FROM idx a JOIN idx b
		    ON a.table_name = b.table_name
		   AND a.index_name <> b.index_name
		   AND NOT a.partial AND NOT b.partial
		   AND b.cols LIKE a.cols || ',%'
		 ORDER BY a.table_name, a.index_name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	type redundancy struct{ table, shorter, shorterCols, longer, longerCols string }
	var found []redundancy
	for rows.Next() {
		var r redundancy
		if err := rows.Scan(&r.table, &r.shorter, &r.shorterCols, &r.longer, &r.longerCols); err != nil {
			t.Fatal(err)
		}
		found = append(found, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	for _, r := range found {
		t.Logf("REDUNDANT: %s.%s (%s) is a prefix of %s (%s)",
			r.table, r.shorter, r.shorterCols, r.longer, r.longerCols)
	}
	t.Logf("MEASURED: %d redundant btree index(es) found", len(found))
}
