package domain_test

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"muse-backend/internal/catalog/domain"
)

func committedFixtureLayout(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "assets", "dev_fixtures", "bundles",
		"dev_fixture_collection_design", "v1", "layout.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read committed fixture layout: %v", err)
	}
	return raw
}

func TestParseCollectionDesignTierCapacities_DerivesTheCommittedFixture(t *testing.T) {
	designID, table, err := domain.ParseCollectionDesignTierCapacities(committedFixtureLayout(t))
	if err != nil {
		t.Fatalf("the committed fixture layout was refused: %v", err)
	}
	if designID != "dev-fixture:collection-design" {
		t.Fatalf("design_id = %q", designID)
	}
	want := domain.TierCapacities{{Tier: 1, Cumulative: 4}, {Tier: 2, Cumulative: 10}, {Tier: 3, Cumulative: 18}}
	if !table.Equal(want) {
		t.Fatalf("derived %v, want %v", table, want)
	}
	for tier, capacity := range map[int]int{1: 4, 2: 10, 3: 18} {
		got, ok := table.SlotCapacityAt(tier)
		if !ok || got != capacity {
			t.Fatalf("SlotCapacityAt(%d) = %d, %v; want %d", tier, got, ok, capacity)
		}
	}
	if _, ok := table.SlotCapacityAt(4); ok {
		t.Fatal("a tier the layout does not author reported a capacity")
	}
	if _, ok := table.SlotCapacityAt(0); ok {
		t.Fatal("tier 0 reported a capacity")
	}
}

func layoutJSON(tiers string) string {
	return `{"format_version":1,"design_id":"d","entry":{"position":[0,0,1]},"tiers":[` + tiers + `]}`
}

func tier(ordinal, cumulative int, slots ...int) string {
	var transforms []string
	for _, slot := range slots {
		transforms = append(transforms, `{"slot_index":`+itoa(slot)+`,"position":[0,0,0]}`)
	}
	return `{"tier":` + itoa(ordinal) + `,"cumulative_capacity":` + itoa(cumulative) +
		`,"item_transforms":[` + strings.Join(transforms, ",") + `]}`
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestParseCollectionDesignTierCapacities_AcceptsACoherentTable(t *testing.T) {
	_, table, err := domain.ParseCollectionDesignTierCapacities([]byte(layoutJSON(
		tier(1, 2, 0, 1) + "," + tier(2, 5, 2, 3, 4),
	)))
	if err != nil {
		t.Fatal(err)
	}
	if !table.Equal(domain.TierCapacities{{Tier: 1, Cumulative: 2}, {Tier: 2, Cumulative: 5}}) {
		t.Fatalf("derived %v", table)
	}
}

func TestParseCollectionDesignTierCapacities_RefusesMalformedMetadata(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		layout string
	}{
		{"not JSON", `{not json`},
		{"wrong format_version", `{"format_version":2,"design_id":"d","tiers":[` + tier(1, 1, 0) + `]}`},
		{"no design_id", `{"format_version":1,"tiers":[` + tier(1, 1, 0) + `]}`},
		{"no tiers key", `{"format_version":1,"design_id":"d"}`},
		{"empty tiers", layoutJSON("")},
		{"tier missing its ordinal", layoutJSON(`{"cumulative_capacity":1,"item_transforms":[{"slot_index":0}]}`)},
		{"tier missing its capacity", layoutJSON(`{"tier":1,"item_transforms":[{"slot_index":0}]}`)},
		{"zero capacity", layoutJSON(tier(1, 0))},
		{"negative capacity", layoutJSON(`{"tier":1,"cumulative_capacity":-3,"item_transforms":[]}`)},
		{"non-monotonic capacities", layoutJSON(tier(1, 4, 0, 1, 2, 3) + "," + tier(2, 4))},
		{"decreasing capacities", layoutJSON(tier(1, 4, 0, 1, 2, 3) + "," + tier(2, 3))},
		{"duplicate tier ordinal", layoutJSON(tier(1, 1, 0) + "," + tier(1, 2, 1))},
		{"gap in tier ordinals", layoutJSON(tier(1, 1, 0) + "," + tier(3, 2, 1))},
		{"tiers not starting at 1", layoutJSON(tier(2, 1, 0))},
		{"slot count does not match added capacity", layoutJSON(tier(1, 3, 0, 1))},
		{"slot indices not contiguous", layoutJSON(tier(1, 2, 0, 2))},
		{"slot indices not starting at 0", layoutJSON(tier(1, 1, 1))},
		{"slot missing its index", layoutJSON(`{"tier":1,"cumulative_capacity":1,"item_transforms":[{"position":[0,0,0]}]}`)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, err := domain.ParseCollectionDesignTierCapacities([]byte(testCase.layout))
			if !errors.Is(err, domain.ErrBundleInvalid) {
				t.Fatalf("got %v, want ErrBundleInvalid", err)
			}
		})
	}
}

func TestParseCollectionDesignTierCapacities_RefusesAnOversizedLayout(t *testing.T) {
	huge := make([]byte, domain.MaxDesignLayoutBytes+1)
	_, _, err := domain.ParseCollectionDesignTierCapacities(huge)
	if !errors.Is(err, domain.ErrBundleInvalid) {
		t.Fatalf("got %v, want ErrBundleInvalid", err)
	}
}

func TestAssetBundleValidate_TierCapacitiesOnlyOnACollectionDesign(t *testing.T) {
	bundle := domain.AssetBundle{
		BundleID: "b", Version: 1, Kind: domain.BundleKindRoomVariant, Format: "usda", MinAppVersion: 1,
		Files: []domain.BundleFile{{
			AssetID: "geometry", Role: domain.RoleGeometry, StorageKey: "bundles/b/v1/geometry",
			ContentType: "model/vnd.usda+ascii", ByteSize: 1, ChecksumSHA256: strings.Repeat("a", 64),
		}},
		TierCapacities: domain.TierCapacities{{Tier: 1, Cumulative: 4}},
	}
	if err := bundle.Validate(); !errors.Is(err, domain.ErrBundleInvalid) {
		t.Fatalf("a room_variant with tier capacities validated: %v", err)
	}

	bundle.Kind = domain.BundleKindCollectionDesign
	if err := bundle.Validate(); err != nil {
		t.Fatalf("a collection_design with tier capacities was refused: %v", err)
	}

	bundle.TierCapacities = domain.TierCapacities{{Tier: 1, Cumulative: 4}, {Tier: 2, Cumulative: 4}}
	if err := bundle.Validate(); !errors.Is(err, domain.ErrBundleInvalid) {
		t.Fatalf("a non-monotonic table validated: %v", err)
	}
}
