package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	analyticsdomain "muse-backend/internal/analytics/domain"
)

func (s *stack) postEvents(body any, token string) (*http.Response, map[string]any) {
	s.t.Helper()
	resp, raw := s.do(http.MethodPost, "/analytics/events", body, token)
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	return resp, decoded
}

func oneEvent(fields map[string]any) map[string]any {
	event := map[string]any{"event_uuid": analyticsdomain.NewEventUUID()}
	for key, value := range fields {
		event[key] = value
	}
	return map[string]any{"events": []any{event}}
}

func (s *stack) analyticsRowCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM analytics_events WHERE account_id = $1`, s.accountID).Scan(&count); err != nil {
		t.Fatalf("count analytics rows: %v", err)
	}
	return count
}

func TestAcceptsADocumentedEvent(t *testing.T) {
	s := newStack(t)
	before := s.analyticsRowCount(t)

	resp, body := s.postEvents(oneEvent(map[string]any{
		"name": "museum_creation_step", "step": "style_previewed",
	}), s.token)

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body %v", resp.StatusCode, body)
	}
	if body["stored"] != float64(1) {
		t.Fatalf("stored = %v, want 1", body["stored"])
	}
	if got := s.analyticsRowCount(t); got != before+1 {
		t.Fatalf("row count %d → %d, want +1", before, got)
	}
}

func TestUnknownEventOrPropertyIsRejected(t *testing.T) {
	s := newStack(t)
	before := s.analyticsRowCount(t)

	cases := []struct {
		name string
		body any
	}{
		{"unknown event name", oneEvent(map[string]any{"name": "exploration_started", "step": "x"})},
		{"the dropped onboarding event", oneEvent(map[string]any{"name": "onboarding_step_reached", "step": "signed_in"})},
		{"unknown JSON key", oneEvent(map[string]any{"name": "museum_creation_step", "step": "style_previewed", "dwell_ms": 1200})},
		{"a property bag", map[string]any{"events": []any{map[string]any{
			"event_uuid": analyticsdomain.NewEventUUID(), "name": "museum_creation_step",
			"properties": map[string]any{"step": "style_previewed"},
		}}}},
		{"property not on this event", oneEvent(map[string]any{
			"name": "museum_creation_step", "step": "style_previewed", "surface": "room_entry"})},
		{"unknown enum value", oneEvent(map[string]any{"name": "museum_creation_step", "step": "style_wondered_about"})},
		{"missing required property", oneEvent(map[string]any{"name": "museum_creation_step"})},
		{"malformed event_uuid", map[string]any{"events": []any{map[string]any{
			"event_uuid": "not-a-uuid", "name": "museum_creation_step", "step": "style_previewed"}}}},
		{"empty batch", map[string]any{"events": []any{}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			resp, _ := s.postEvents(testCase.body, s.token)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
		})
	}

	if got := s.analyticsRowCount(t); got != before {
		t.Fatalf("a rejected payload stored %d rows", got-before)
	}
}

func TestNoFreeTextPayloadIsAccepted(t *testing.T) {
	s := newStack(t)
	before := s.analyticsRowCount(t)

	attempts := []any{
		oneEvent(map[string]any{"name": "catalog_search_outcome", "outcome": "selected",
			"category_id": "category_watches", "query": "omega speedmaster"}),
		oneEvent(map[string]any{"name": "room_creation_step", "step": "name_entered",
			"room_name": "Grandad's watches"}),
		oneEvent(map[string]any{"name": "collection_room_creation_step", "step": "name_entered",
			"category_id": "my dad's collection"}),
		oneEvent(map[string]any{"name": "failure_shown", "surface": "catalog_search",
			"classification": "server", "retried": true, "retry_succeeded": false,
			"message": "500 internal server error: pq: duplicate key"}),
	}
	for _, attempt := range attempts {
		resp, _ := s.postEvents(attempt, s.token)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("free text was accepted with status %d", resp.StatusCode)
		}
	}
	if got := s.analyticsRowCount(t); got != before {
		t.Fatal("a payload carrying free text stored a row")
	}
}

func TestServerOnlyEventsAreRefusedFromAClient(t *testing.T) {
	s := newStack(t)
	for _, body := range []any{
		oneEvent(map[string]any{"name": "catalog_search_performed",
			"category_id": "category_watches", "result_bucket": "few"}),
		oneEvent(map[string]any{"name": "item_add_refused", "reason": "item_capacity_reached"}),
	} {
		resp, _ := s.postEvents(body, s.token)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("a server-only event was accepted from a client: %d", resp.StatusCode)
		}
	}
}

func TestDuplicateEventUUIDIsCountedOnce(t *testing.T) {
	s := newStack(t)
	before := s.analyticsRowCount(t)

	uuid := analyticsdomain.NewEventUUID()
	payload := map[string]any{"events": []any{map[string]any{
		"event_uuid": uuid, "name": "capacity_upgrade_step", "step": "capacity_screen_shown",
	}}}

	first, firstBody := s.postEvents(payload, s.token)
	second, secondBody := s.postEvents(payload, s.token)

	if first.StatusCode != http.StatusAccepted || second.StatusCode != http.StatusAccepted {
		t.Fatalf("statuses = %d and %d, want 202 twice — a duplicate is not an error", first.StatusCode, second.StatusCode)
	}
	if firstBody["stored"] != float64(1) || firstBody["duplicates"] != float64(0) {
		t.Fatalf("first submission: %v", firstBody)
	}
	if secondBody["stored"] != float64(0) || secondBody["duplicates"] != float64(1) {
		t.Fatalf("second submission must store nothing and report a duplicate: %v", secondBody)
	}
	if got := s.analyticsRowCount(t); got != before+1 {
		t.Fatalf("row count moved by %d; a duplicate must not double-count", got-before)
	}
}

func TestNoUnauthenticatedAnalyticsPathExists(t *testing.T) {
	s := newStack(t)
	before := s.analyticsRowCount(t)

	for _, token := range []string{"", "garbage"} {
		resp, _ := s.postEvents(oneEvent(map[string]any{
			"name": "museum_creation_step", "step": "style_list_shown",
		}), token)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("token %q → %d, want 401", token, resp.StatusCode)
		}
	}
	if got := s.analyticsRowCount(t); got != before {
		t.Fatal("an unauthenticated request stored a row")
	}
}

func TestProductActionsSucceedWhenAnalyticsFails(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()

	if _, err := s.pool.Pool().Exec(ctx, `ALTER TABLE analytics_events RENAME TO analytics_events_hidden`); err != nil {
		t.Fatalf("hide the analytics table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Pool().Exec(context.Background(),
			`ALTER TABLE analytics_events_hidden RENAME TO analytics_events`)
	})

	resp, body := s.do(http.MethodGet,
		"/catalog/collection-models?category_id="+seededCategory, nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search with analytics broken → %d %s", resp.StatusCode, body)
	}

	roomID := s.createCollectionRoomInCategory(s.token, "Analytics", seededCategory)
	refusal, refusalBody := s.addItem(roomID, "dev-fixture:model-does-not-exist")
	if refusal.StatusCode != http.StatusBadRequest {
		t.Fatalf("item add with analytics broken → %d %s", refusal.StatusCode, refusalBody)
	}
	var decoded map[string]any
	_ = json.Unmarshal(refusalBody, &decoded)
	if decoded["code"] != "model_not_available" {
		t.Fatalf("the refusal code changed because analytics failed: %v", decoded)
	}

	var rooms int
	if err := s.pool.Pool().QueryRow(ctx,
		`SELECT count(*) FROM collection_rooms WHERE id = $1`, roomID).Scan(&rooms); err != nil {
		t.Fatalf("count rooms: %v", err)
	}
	if rooms != 1 {
		t.Fatal("the Collection Room did not survive a broken analytics table")
	}
}

func TestServerSideEmittersRecordSearchAndRefusal(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()

	countOf := func(name string) int {
		var count int
		if err := s.pool.Pool().QueryRow(ctx,
			`SELECT count(*) FROM analytics_events WHERE account_id = $1 AND name = $2`,
			s.accountID, name).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", name, err)
		}
		return count
	}
	searchesBefore, refusalsBefore := countOf("catalog_search_performed"), countOf("item_add_refused")

	resp, body := s.do(http.MethodGet,
		"/catalog/collection-models?category_id="+seededCategory+"&q=nothingmatchesthis", nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("search → %d %s", resp.StatusCode, body)
	}
	roomID := s.createCollectionRoomInCategory(s.token, "Analytics emit", seededCategory)
	if refusal, refusalBody := s.addItem(roomID, "dev-fixture:model-does-not-exist"); refusal.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected a refusal, got %d %s", refusal.StatusCode, refusalBody)
	}

	if got := countOf("catalog_search_performed"); got != searchesBefore+1 {
		t.Fatalf("search events moved by %d, want 1", got-searchesBefore)
	}
	if got := countOf("item_add_refused"); got != refusalsBefore+1 {
		t.Fatalf("refusal events moved by %d, want 1", got-refusalsBefore)
	}

	var bucket, category string
	if err := s.pool.Pool().QueryRow(ctx, `
		SELECT result_bucket, category_id FROM analytics_events
		WHERE account_id = $1 AND name = 'catalog_search_performed'
		ORDER BY received_at DESC LIMIT 1`, s.accountID).Scan(&bucket, &category); err != nil {
		t.Fatalf("read the search event: %v", err)
	}
	if bucket != "none" {
		t.Fatalf("result_bucket = %q, want \"none\" for a query that matched nothing", bucket)
	}
	if category != seededCategory {
		t.Fatalf("category_id = %q", category)
	}
}

func TestSchemaHasNoPropertyBagAndNoDeviceOrClientFields(t *testing.T) {
	s := newStack(t)
	rows, err := s.pool.Pool().Query(context.Background(), `
		SELECT column_name, data_type FROM information_schema.columns
		WHERE table_name = 'analytics_events'`)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	defer rows.Close()

	found := map[string]string{}
	for rows.Next() {
		var column, dataType string
		if err := rows.Scan(&column, &dataType); err != nil {
			t.Fatalf("scan: %v", err)
		}
		found[column] = dataType
	}
	if len(found) == 0 {
		t.Fatal("found no columns — the introspection is not testing anything")
	}

	for column, dataType := range found {
		if dataType == "jsonb" || dataType == "json" {
			t.Errorf("column %q is %s: a property bag is exactly what the contract forbids", column, dataType)
		}
	}
	for _, forbidden := range []string{
		"device_id", "idfv", "idfa", "advertising_id", "session_id",
		"ip_address", "ip", "user_agent", "client_timestamp", "occurred_at",
		"message", "error", "query", "room_name", "email", "display_name", "share_code",
	} {
		if _, present := found[forbidden]; present {
			t.Errorf("analytics_events must not carry %q", forbidden)
		}
	}
	for _, required := range []string{"event_uuid", "name", "account_id", "received_at"} {
		if _, present := found[required]; !present {
			t.Errorf("analytics_events is missing %q", required)
		}
	}
}
