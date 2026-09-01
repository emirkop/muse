package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type collectionCategoryJSON struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	SortOrder   int    `json:"sort_order"`
}

type collectionCategoryListJSON struct {
	CollectionCategories []collectionCategoryJSON `json:"collection_categories"`
}

func (s *stack) fetchCategories(token string) collectionCategoryListJSON {
	s.t.Helper()
	resp, body := s.do(http.MethodGet, "/catalog/collection-categories", nil, token)
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("list categories: %d %s", resp.StatusCode, body)
	}
	var list collectionCategoryListJSON
	if err := json.Unmarshal(body, &list); err != nil {
		s.t.Fatalf("decode categories: %v (%s)", err, body)
	}
	return list
}

func TestCategoryRegistryServesTheFourInitialCategories(t *testing.T) {
	s := newStack(t)

	list := s.fetchCategories(s.token)

	want := []collectionCategoryJSON{
		{ID: "category_watches", DisplayName: "Watches", SortOrder: 10},
		{ID: "category_hot_wheels", DisplayName: "Hot Wheels", SortOrder: 20},
		{ID: "category_coins", DisplayName: "Coins", SortOrder: 30},
		{ID: "category_license_plates", DisplayName: "License Plates", SortOrder: 40},
	}
	if len(list.CollectionCategories) != len(want) {
		t.Fatalf("served %d categories, want exactly %d: %+v",
			len(list.CollectionCategories), len(want), list.CollectionCategories)
	}
	for index, expected := range want {
		if list.CollectionCategories[index] != expected {
			t.Fatalf("category %d is %+v, want %+v", index, list.CollectionCategories[index], expected)
		}
	}
}

func TestCategoryPayloadCarriesNothingFromTheCatalog(t *testing.T) {
	s := newStack(t)

	resp, body := s.do(http.MethodGet, "/catalog/collection-categories", nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", resp.StatusCode, body)
	}

	var raw struct {
		CollectionCategories []map[string]any `json:"collection_categories"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.CollectionCategories) == 0 {
		t.Fatal("no categories served")
	}
	for _, category := range raw.CollectionCategories {
		if len(category) != 3 {
			t.Fatalf("category payload has %d fields, want exactly 3 (id, display_name, sort_order): %+v",
				len(category), category)
		}
		for _, field := range []string{"id", "display_name", "sort_order"} {
			if _, ok := category[field]; !ok {
				t.Fatalf("category payload is missing %q: %+v", field, category)
			}
		}
	}
}

func TestCategoryRegistryIsAuthenticatedButNotOwnerScoped(t *testing.T) {
	s := newStack(t)

	if resp, _ := s.do(http.MethodGet, "/catalog/collection-categories", nil, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated → %d, want 401", resp.StatusCode)
	}

	mine := s.fetchCategories(s.token)
	theirs := s.fetchCategories(s.strangerToken())
	if len(mine.CollectionCategories) != len(theirs.CollectionCategories) {
		t.Fatalf("owner saw %d categories, a stranger saw %d — the catalog is Platform data and must be identical",
			len(mine.CollectionCategories), len(theirs.CollectionCategories))
	}
	for index := range mine.CollectionCategories {
		if mine.CollectionCategories[index] != theirs.CollectionCategories[index] {
			t.Fatalf("category %d differs between callers", index)
		}
	}
}

func TestEveryServedCategoryCanBeCreatedIn(t *testing.T) {
	s := newStack(t)

	for _, category := range s.fetchCategories(s.token).CollectionCategories {
		resp, body := s.do(http.MethodPost, "/collection-rooms",
			map[string]string{"name": "My " + category.DisplayName, "category_id": category.ID}, s.token)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create in %q → %d %s", category.ID, resp.StatusCode, body)
		}
		created := decodeCollectionRoom(t, body)
		if created.CategoryID != category.ID {
			t.Fatalf("created Room has category %q, want %q", created.CategoryID, category.ID)
		}
		if created.DesignID != "" {
			t.Fatalf("created Room has design %q; does not choose one", created.DesignID)
		}
	}
}

func TestManyRoomsAcrossEveryCategory_NoCountLimit(t *testing.T) {
	s := newStack(t)
	categories := s.fetchCategories(s.token).CollectionCategories

	const perCategory = 6
	for round := 0; round < perCategory; round++ {
		for _, category := range categories {
			resp, body := s.do(http.MethodPost, "/collection-rooms",
				map[string]string{"name": category.DisplayName, "category_id": category.ID}, s.token)
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("round %d, category %q → %d %s — no count limit may exist at this phase",
					round, category.ID, resp.StatusCode, body)
			}
		}
	}

	resp, body := s.do(http.MethodGet, "/collection-rooms", nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", resp.StatusCode, body)
	}
	var list collectionRoomListJSON
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	expected := perCategory * len(categories)
	if len(list.CollectionRooms) != expected {
		t.Fatalf("list = %d rooms, want %d", len(list.CollectionRooms), expected)
	}

	seen := map[string]int{}
	for _, room := range list.CollectionRooms {
		seen[room.CategoryID]++
	}
	for _, category := range categories {
		if seen[category.ID] != perCategory {
			t.Fatalf("category %q holds %d rooms, want %d", category.ID, seen[category.ID], perCategory)
		}
	}
}

func TestANewCategoryNeedsNoCodeChange(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()

	if _, err := s.pool.Pool().Exec(ctx,
		`INSERT INTO collection_categories (id, display_name, sort_order) VALUES ('category_vinyl', 'Vinyl Records', 60)
		 ON CONFLICT (id) DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := s.pool.Pool().Exec(context.Background(),
			`DELETE FROM collection_rooms WHERE category_id = 'category_vinyl'`); err != nil {
			t.Errorf("cleanup rooms: %v", err)
		}
		if _, err := s.pool.Pool().Exec(context.Background(),
			`DELETE FROM collection_categories WHERE id = 'category_vinyl'`); err != nil {
			t.Errorf("cleanup category: %v", err)
		}
	})

	var found bool
	for _, category := range s.fetchCategories(s.token).CollectionCategories {
		if category.ID == "category_vinyl" && category.DisplayName == "Vinyl Records" {
			found = true
		}
	}
	if !found {
		t.Fatal("a category inserted into the table was not served — the registry is not data-driven")
	}

	resp, body := s.do(http.MethodPost, "/collection-rooms",
		map[string]string{"name": "Records", "category_id": "category_vinyl"}, s.token)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create in the new category → %d %s", resp.StatusCode, body)
	}
}

func TestCategoryRefusalsAreSpecificAndCreateNothing(t *testing.T) {
	s := newStack(t)

	cases := []struct {
		name     string
		body     map[string]string
		wantCode string
	}{
		{"absent", map[string]string{"name": "X"}, "category_required"},
		{"empty", map[string]string{"name": "X", "category_id": ""}, "category_required"},
		{"unknown", map[string]string{"name": "X", "category_id": "category_stamps"}, "unknown_category"},
		{"wrong case", map[string]string{"name": "X", "category_id": "CATEGORY_WATCHES"}, "unknown_category"},
		{"untrimmed", map[string]string{"name": "X", "category_id": " category_watches "}, "unknown_category"},
		{"display name as id", map[string]string{"name": "X", "category_id": "Watches"}, "unknown_category"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := s.do(http.MethodPost, "/collection-rooms", tc.body, s.token)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("→ %d %s; want 400", resp.StatusCode, body)
			}
			if !strings.Contains(string(body), tc.wantCode) {
				t.Fatalf("body %s does not carry code %q", body, tc.wantCode)
			}
		})
	}

	var count int
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM collection_rooms WHERE account_id = $1`, s.accountID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("%d Collection Rooms exist after only refused creations", count)
	}
}

func TestChangedNothingAboutTheMuseumSurface(t *testing.T) {
	s := newStack(t)

	before := s.snapshotOwnerState()
	room := s.createCollectionRoom(s.token, "Watches")
	if room == "" {
		t.Fatal("no room created")
	}

	after := s.snapshotOwnerState()
	if after.Museum != before.Museum || after.Rooms != before.Rooms ||
		after.Slots != before.Slots || after.Assets != before.Assets {
		t.Fatalf("creating a Collection Room touched the Museum tree:\nbefore %+v\nafter  %+v", before, after)
	}

	for _, path := range []string{"/catalog/styles", "/catalog/sculptures", "/catalog/music"} {
		if resp, body := s.do(http.MethodGet, path, nil, s.token); resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s → %d %s", path, resp.StatusCode, body)
		}
	}
}
