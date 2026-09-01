package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func requireLoadTest(t *testing.T) {
	t.Helper()
	if os.Getenv("MUSE_LOAD_TEST") == "" {
		t.Skip("MUSE_LOAD_TEST not set — skipping load runs (they seed up to 100k rows and fire thousands of requests)")
	}
}

// MARK: - Measurement

type latencies struct {
	samples []time.Duration
}

func (l *latencies) add(d time.Duration) { l.samples = append(l.samples, d) }

func (l *latencies) sortNow() {
	sort.Slice(l.samples, func(i, j int) bool { return l.samples[i] < l.samples[j] })
}

func (l *latencies) quantile(q float64) time.Duration {
	if len(l.samples) == 0 {
		return 0
	}
	index := int(q * float64(len(l.samples)))
	if index >= len(l.samples) {
		index = len(l.samples) - 1
	}
	return l.samples[index]
}

func (l *latencies) mean() time.Duration {
	if len(l.samples) == 0 {
		return 0
	}
	var total time.Duration
	for _, d := range l.samples {
		total += d
	}
	return total / time.Duration(len(l.samples))
}

func (l *latencies) String() string {
	l.sortNow()
	return fmt.Sprintf("n=%d mean=%s p50=%s p95=%s p99=%s max=%s",
		len(l.samples),
		l.mean().Round(time.Microsecond),
		l.quantile(0.50).Round(time.Microsecond),
		l.quantile(0.95).Round(time.Microsecond),
		l.quantile(0.99).Round(time.Microsecond),
		l.samples[len(l.samples)-1].Round(time.Microsecond))
}

type poolDelta struct {
	maxConns        int32
	totalConns      int32
	acquires        int64
	emptyAcquires   int64
	acquireWaitTime time.Duration
	newConns        int64
}

func (d poolDelta) String() string {
	return fmt.Sprintf("max_conns=%d total_conns=%d acquires=%d empty_acquires=%d acquire_wait=%s new_conns=%d",
		d.maxConns, d.totalConns, d.acquires, d.emptyAcquires,
		d.acquireWaitTime.Round(time.Microsecond), d.newConns)
}

func (s *stack) poolStats() poolDelta {
	stat := s.pool.Pool().Stat()
	return poolDelta{
		maxConns:        stat.MaxConns(),
		totalConns:      stat.TotalConns(),
		acquires:        stat.AcquireCount(),
		emptyAcquires:   stat.EmptyAcquireCount(),
		acquireWaitTime: stat.AcquireDuration(),
		newConns:        stat.NewConnsCount(),
	}
}

func poolSince(before, after poolDelta) poolDelta {
	return poolDelta{
		maxConns:        after.maxConns,
		totalConns:      after.totalConns,
		acquires:        after.acquires - before.acquires,
		emptyAcquires:   after.emptyAcquires - before.emptyAcquires,
		acquireWaitTime: after.acquireWaitTime - before.acquireWaitTime,
		newConns:        after.newConns - before.newConns,
	}
}

type loadResult struct {
	lat        latencies
	requests   int64
	errors     int64
	unexpected map[int]int64
	elapsed    time.Duration
	pool       poolDelta
	heapDelta  int64
}

func (r loadResult) throughput() float64 {
	if r.elapsed == 0 {
		return 0
	}
	return float64(r.requests) / r.elapsed.Seconds()
}

func (r loadResult) errorRate() float64 {
	if r.requests == 0 {
		return 0
	}
	return float64(r.errors) / float64(r.requests) * 100
}

func (r loadResult) report(t *testing.T, name string) {
	t.Helper()
	t.Logf("MEASURED %s\n    latency   %s\n    throughput %.0f req/s over %s (%d requests)\n    errors    %d (%.2f%%) %v\n    pool      %s\n    heap      %+d KiB",
		name, r.lat.String(), r.throughput(), r.elapsed.Round(time.Millisecond), r.requests,
		r.errors, r.errorRate(), r.unexpected, r.pool.String(), r.heapDelta/1024)
}

func runLoad(t *testing.T, s *stack, workers, perWorker, expected int, do func(worker, iteration int) int) loadResult {
	t.Helper()

	var memBefore, memAfter runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)
	poolBefore := s.poolStats()

	var (
		mu         sync.Mutex
		lat        latencies
		unexpected = map[int]int64{}
		requests   atomic.Int64
		errors     atomic.Int64
		ready      sync.WaitGroup
		start      = make(chan struct{})
		done       sync.WaitGroup
	)
	ready.Add(workers)
	done.Add(workers)

	for worker := 0; worker < workers; worker++ {
		go func(worker int) {
			defer done.Done()
			local := make([]time.Duration, 0, perWorker)
			localBad := map[int]int64{}
			ready.Done()
			<-start
			for iteration := 0; iteration < perWorker; iteration++ {
				began := time.Now()
				status := do(worker, iteration)
				local = append(local, time.Since(began))
				requests.Add(1)
				if status != expected {
					errors.Add(1)
					localBad[status]++
				}
			}
			mu.Lock()
			lat.samples = append(lat.samples, local...)
			for status, count := range localBad {
				unexpected[status] += count
			}
			mu.Unlock()
		}(worker)
	}

	ready.Wait()
	began := time.Now()
	close(start)
	done.Wait()
	elapsed := time.Since(began)

	poolAfter := s.poolStats()
	runtime.GC()
	runtime.ReadMemStats(&memAfter)

	return loadResult{
		lat:        lat,
		requests:   requests.Load(),
		errors:     errors.Load(),
		unexpected: unexpected,
		elapsed:    elapsed,
		pool:       poolSince(poolBefore, poolAfter),
		heapDelta:  int64(memAfter.HeapAlloc) - int64(memBefore.HeapAlloc),
	}
}

// MARK: - Scenario 1: concurrent visitor sessions / shared-content reads
func TestConcurrentVisitorReads(t *testing.T) {
	requireLoadTest(t)
	s := newStack(t)
	f := newVisitorFixture(t, s)

	const maxWorkers = 64
	tokens := make([]string, maxWorkers)
	for i := range tokens {
		tokens[i] = s.strangerToken()
	}

	routes := []struct {
		name string
		path string
	}{
		{"museum", "/share-links/" + f.code + "/museum"},
		{"room", "/share-links/" + f.code + "/rooms/" + f.publicRoom},
		{"photo-urls", "/share-links/" + f.code + "/rooms/" + f.publicRoom + "/photo-urls"},
	}

	for _, workers := range []int{1, 8, 32, 64} {
		for _, route := range routes {
			result := runLoad(t, s, workers, 25, http.StatusOK, func(worker, _ int) int {
				resp, _ := s.do(http.MethodGet, route.path, nil, tokens[worker%len(tokens)])
				return resp.StatusCode
			})
			result.report(t, fmt.Sprintf("visitor %s @ %d concurrent", route.name, workers))

			if result.errors != 0 {
				t.Errorf("visitor %s @ %d: %d errors %v — a stateless read must not fail under concurrency",
					route.name, workers, result.errors, result.unexpected)
			}
		}
	}

	mixed := runLoad(t, s, 48, 30, http.StatusOK, func(worker, iteration int) int {
		route := routes[iteration%len(routes)]
		resp, _ := s.do(http.MethodGet, route.path, nil, tokens[worker%len(tokens)])
		return resp.StatusCode
	})
	mixed.report(t, "visitor mixed (all three routes) @ 48 concurrent")
	if mixed.errors != 0 {
		t.Errorf("mixed visitor load: %d errors %v", mixed.errors, mixed.unexpected)
	}
}

// MARK: - Scenario 2: unlimited Collection Rooms / large account content
func TestLargeAccountCollectionRoomList(t *testing.T) {
	requireLoadTest(t)
	s := newStack(t)
	ctx := context.Background()

	const itemsPerRoom = 8
	seed := func(rooms int) {
		if _, err := s.pool.Pool().Exec(ctx, `
			WITH inserted AS (
				INSERT INTO collection_rooms (account_id, name, category_id, design_id, current_tier)
				SELECT $1, 'Synthetic Room ' || g, $2, NULL, 1
				  FROM generate_series(1, $3) AS g
				RETURNING id
			)
			INSERT INTO collection_items (collection_room_id, slot_index, catalog_model_id)
			SELECT inserted.id, slot.i, 'dev-fixture:model-chrono-one'
			  FROM inserted, generate_series(0, $4 - 1) AS slot(i)`,
			s.accountID, seededCategory, rooms, itemsPerRoom); err != nil {
			t.Fatalf("seed %d rooms: %v", rooms, err)
		}
	}

	var previous time.Duration
	for _, target := range []int{25, 100, 400} {
		var current int
		if err := s.pool.Pool().QueryRow(ctx,
			`SELECT count(*) FROM collection_rooms WHERE account_id = $1`, s.accountID).Scan(&current); err != nil {
			t.Fatal(err)
		}
		if target > current {
			seed(target - current)
		}

		var bytes int
		serial := runLoad(t, s, 1, 15, http.StatusOK, func(_, _ int) int {
			resp, body := s.do(http.MethodGet, "/collection-rooms", nil, s.token)
			bytes = len(body)
			return resp.StatusCode
		})
		serial.report(t, fmt.Sprintf("collection room list — %d rooms x %d items, serial (payload %d KiB)",
			target, itemsPerRoom, bytes/1024))

		concurrent := runLoad(t, s, 8, 10, http.StatusOK, func(_, _ int) int {
			resp, _ := s.do(http.MethodGet, "/collection-rooms", nil, s.token)
			return resp.StatusCode
		})
		concurrent.report(t, fmt.Sprintf("collection room list — %d rooms, 8 concurrent", target))

		if serial.errors != 0 || concurrent.errors != 0 {
			t.Errorf("%d rooms: %d serial + %d concurrent errors %v %v",
				target, serial.errors, concurrent.errors, serial.unexpected, concurrent.unexpected)
		}

		serial.lat.sortNow()
		median := serial.lat.quantile(0.50)
		t.Logf("    per-room cost at %d rooms: %s/room (p50 %s)", target,
			(median / time.Duration(target)).Round(time.Microsecond), median.Round(time.Microsecond))
		if previous > 0 {
			t.Logf("    growth vs previous size: %.2fx latency for %.2fx rooms",
				float64(median)/float64(previous), float64(target)/float64(target/4))
		}
		previous = median
	}

	var rooms int
	if err := s.pool.Pool().QueryRow(ctx,
		`SELECT count(*) FROM collection_rooms WHERE account_id = $1`, s.accountID).Scan(&rooms); err != nil {
		t.Fatal(err)
	}
	perRoomQueries := func() time.Duration {
		began := time.Now()
		ids := []string{}
		rows, err := s.pool.Pool().Query(ctx, `SELECT id FROM collection_rooms WHERE account_id = $1`, s.accountID)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			itemRows, err := s.pool.Pool().Query(ctx,
				`SELECT id, slot_index, catalog_model_id FROM collection_items WHERE collection_room_id = $1 ORDER BY slot_index`, id)
			if err != nil {
				t.Fatal(err)
			}
			for itemRows.Next() {
			}
			itemRows.Close()
		}
		return time.Since(began)
	}
	batchedQuery := func() time.Duration {
		began := time.Now()
		rows, err := s.pool.Pool().Query(ctx, `
			SELECT i.collection_room_id, i.id, i.slot_index, i.catalog_model_id
			  FROM collection_items i
			  JOIN collection_rooms r ON r.id = i.collection_room_id
			 WHERE r.account_id = $1
			 ORDER BY i.collection_room_id, i.slot_index`, s.accountID)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
		}
		rows.Close()
		return time.Since(began)
	}
	const runs = 10
	var nPlusOne, batched time.Duration
	for i := 0; i < runs; i++ {
		nPlusOne += perRoomQueries()
		batched += batchedQuery()
	}
	t.Logf("MEASURED round-trip shape at %d rooms: %d queries (1+N) %s  vs  2 queries (batched) %s — %.1fx",
		rooms, rooms+1, (nPlusOne / runs).Round(time.Microsecond), (batched / runs).Round(time.Microsecond),
		float64(nPlusOne)/float64(batched))
}

// MARK: - Scenario 3: entitlement / account-lock contention
func TestAccountLockContention(t *testing.T) {
	requireLoadTest(t)
	s := newStack(t)

	room := s.roomWithPublishedDesign(t, "Contention")
	if resp, body := s.ratchet(room, 3); resp.StatusCode != http.StatusOK {
		t.Fatalf("ratchet to tier 3: %d %s", resp.StatusCode, body)
	}
	const model = "dev-fixture:model-chrono-one"

	const concurrentAdds = 16
	sameAccount := runLoad(t, s, concurrentAdds, 1, http.StatusCreated, func(_, _ int) int {
		resp, _ := s.addItem(room, model)
		return resp.StatusCode
	})
	sameAccount.report(t, fmt.Sprintf("item add — %d concurrent, ONE account (advisory lock contended)", concurrentAdds))
	if sameAccount.errors != 0 {
		t.Errorf("same-account adds: %d errors %v — 16 adds into an 18-slot tier must all succeed",
			sameAccount.errors, sameAccount.unexpected)
	}

	var slots int
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT count(DISTINCT slot_index) FROM collection_items WHERE collection_room_id = $1`, room).Scan(&slots); err != nil {
		t.Fatal(err)
	}
	if slots != concurrentAdds {
		t.Errorf("expected %d distinct slots after %d concurrent adds, got %d — the lock did not serialise slot choice",
			concurrentAdds, concurrentAdds, slots)
	}

	type owned struct {
		token string
		room  string
	}
	others := make([]owned, 0, concurrentAdds)
	for i := 0; i < concurrentAdds; i++ {
		token := s.strangerToken()
		otherRoom := s.createCollectionRoomInCategory(token, fmt.Sprintf("Contention control %d", i), seededCategory)
		if resp, body := s.do(http.MethodPatch, "/collection-rooms/"+otherRoom,
			map[string]string{"design_id": devFixtureDesign}, token); resp.StatusCode != http.StatusOK {
			t.Fatalf("assign design to control room: %d %s", resp.StatusCode, body)
		}
		others = append(others, owned{token: token, room: otherRoom})
	}
	distinct := runLoad(t, s, concurrentAdds, 1, http.StatusCreated, func(worker, _ int) int {
		o := others[worker]
		resp, _ := s.do(http.MethodPost, "/collection-rooms/"+o.room+"/items",
			map[string]string{"catalog_model_id": model}, o.token)
		return resp.StatusCode
	})
	distinct.report(t, fmt.Sprintf("item add — %d concurrent, DISTINCT accounts (no shared lock)", concurrentAdds))
	if distinct.errors != 0 {
		t.Errorf("distinct-account adds: %d errors %v", distinct.errors, distinct.unexpected)
	}

	t.Logf("MEASURED account-lock serialisation: one account %.0f adds/s (p99 %s) vs distinct accounts %.0f adds/s (p99 %s) — %.2fx",
		sameAccount.throughput(), sameAccount.lat.quantile(0.99).Round(time.Microsecond),
		distinct.throughput(), distinct.lat.quantile(0.99).Round(time.Microsecond),
		distinct.throughput()/sameAccount.throughput())
	t.Log(" Serialisation on one account is DELIBERATE: it is what makes an " +
		"account-wide capacity enforceable. The number to watch is the distinct-account " +
		"throughput, since unrelated accounts must not queue behind each other.")
}

// MARK: - Scenario 4: Collection catalog search at larger synthetic sizes
func TestCatalogSearchAtScale(t *testing.T) {
	requireLoadTest(t)
	s := newStack(t)
	ctx := context.Background()

	var brandID string
	if err := s.pool.Pool().QueryRow(ctx, `SELECT id FROM collection_brands LIMIT 1`).Scan(&brandID); err != nil {
		t.Fatalf("no seeded brand: %v", err)
	}
	t.Cleanup(func() {
		if _, err := s.pool.Pool().Exec(context.Background(),
			`DELETE FROM collection_models WHERE id LIKE 'dev-synthetic:model-%'`); err != nil {
			t.Errorf("cleanup synthetic models: %v", err)
		}
	})

	seedTo := func(target int) {
		var have int
		if err := s.pool.Pool().QueryRow(ctx,
			`SELECT count(*) FROM collection_models WHERE id LIKE 'dev-synthetic:model-%'`).Scan(&have); err != nil {
			t.Fatal(err)
		}
		if have >= target {
			return
		}
		began := time.Now()
		if _, err := s.pool.Pool().Exec(ctx, `
			INSERT INTO collection_models (id, brand_id, category_id, display_name, search_text, classification)
			SELECT 'dev-synthetic:model-' || g, $1, $2,
			       'Synthetic Chronometer ' || g,
			       'Synthetic Chronometer ' || g || ' ref' || g,
			       'dev_fixture'
			  FROM generate_series($3::int + 1, $4::int) AS g
			ON CONFLICT (id) DO NOTHING`, brandID, seededCategory, have, target); err != nil {
			t.Fatalf("seed to %d models: %v", target, err)
		}
		if _, err := s.pool.Pool().Exec(ctx, `ANALYZE collection_models`); err != nil {
			t.Fatal(err)
		}
		t.Logf("seeded %d → %d synthetic Models in %s", have, target, time.Since(began).Round(time.Millisecond))
	}

	shapes := []struct {
		name  string
		query string
	}{
		{"browse (no terms)", ""},
		{"broad term", "synthetic"},
		{"selective term", "ref4242"},
		{"no match", "zzzznothing"},
	}

	for _, size := range []int{5_000, 50_000, 100_000} {
		seedTo(size)

		for _, shape := range shapes[1:] {
			plan := s.explainSearch(t, shape.query)
			t.Logf("plan @ %d Models — %s:\n%s", size, shape.name, plan)
		}

		for _, shape := range shapes {
			path := "/catalog/collection-models?category_id=" + seededCategory
			if shape.query != "" {
				path += "&q=" + shape.query
			}

			serial := runLoad(t, s, 1, 20, http.StatusOK, func(_, _ int) int {
				resp, _ := s.do(http.MethodGet, path, nil, s.token)
				return resp.StatusCode
			})
			serial.report(t, fmt.Sprintf("catalog search @ %d Models — %s, serial", size, shape.name))

			concurrent := runLoad(t, s, 32, 10, http.StatusOK, func(_, _ int) int {
				resp, _ := s.do(http.MethodGet, path, nil, s.token)
				return resp.StatusCode
			})
			concurrent.report(t, fmt.Sprintf("catalog search @ %d Models — %s, 32 concurrent", size, shape.name))

			if serial.errors != 0 || concurrent.errors != 0 {
				t.Errorf("@ %d Models, %s: %d serial + %d concurrent errors %v %v",
					size, shape.name, serial.errors, concurrent.errors, serial.unexpected, concurrent.unexpected)
			}
		}

		s.measureDeepPaging(t, size)
	}
}

func (s *stack) explainSearch(t *testing.T, term string) string {
	t.Helper()
	sql := `SELECT m.id FROM collection_models m JOIN collection_brands b ON b.id = m.brand_id
		WHERE m.category_id = $1`
	args := []any{seededCategory}
	if term != "" {
		args = append(args, term+":*")
		sql += ` AND m.search_document @@ to_tsquery('simple', $2)`
	}
	sql += ` ORDER BY m.display_name, m.id LIMIT 21`
	rows, err := s.pool.Pool().Query(context.Background(),
		"EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF, SUMMARY OFF) "+sql, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	plan := ""
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan += "        " + line + "\n"
	}
	return plan
}

func (s *stack) measureDeepPaging(t *testing.T, size int) {
	t.Helper()
	type page struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
		NextCursorName string `json:"next_cursor_name"`
		NextCursorID   string `json:"next_cursor_id"`
	}
	const (
		pageSize = 50
		maxPages = 20
	)
	base := fmt.Sprintf("/catalog/collection-models?category_id=%s&limit=%d", seededCategory, pageSize)
	var first, last time.Duration
	cursorName, cursorID := "", ""
	pagesWalked, rowsSeen := 0, 0

	for p := 0; p < maxPages; p++ {
		requestURL := base
		if cursorName != "" {
			requestURL += "&cursor_name=" + url.QueryEscape(cursorName) + "&cursor_id=" + url.QueryEscape(cursorID)
		}
		began := time.Now()
		resp, body := s.do(http.MethodGet, requestURL, nil, s.token)
		took := time.Since(began)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("deep paging page %d: %d %s", p+1, resp.StatusCode, body)
		}
		var decoded page
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatalf("deep paging page %d: %v", p+1, err)
		}
		pagesWalked++
		rowsSeen += len(decoded.Models)
		if p == 0 {
			first = took
		}
		last = took
		if decoded.NextCursorName == "" {
			break
		}
		cursorName, cursorID = decoded.NextCursorName, decoded.NextCursorID
	}

	if pagesWalked < 2 {
		t.Fatalf("deep paging walked only %d page(s) at %d Models — the cursor is not advancing, so this measures nothing",
			pagesWalked, size)
	}
	t.Logf("MEASURED catalog keyset paging @ %d Models: page 1 %s → page %d %s (%.2fx) over %d rows — flat with depth is the property keyset was chosen for",
		size, first.Round(time.Microsecond), pagesWalked, last.Round(time.Microsecond),
		float64(last)/float64(first), rowsSeen)
}
