package domain

import (
	"fmt"
	"reflect"
	"testing"
)

func TestPhotoSlotLayout_EveryCountFrom1To28(t *testing.T) {
	for count := 1; count <= MaxPhotosPerRoom; count++ {
		t.Run(fmt.Sprintf("count=%d", count), func(t *testing.T) {
			slots := PhotoSlotLayout(count)

			if len(slots) != count {
				t.Fatalf("got %d slots, want %d", len(slots), count)
			}

			for i, s := range slots {
				if s.Index != i {
					t.Errorf("slot at position %d has Index %d", i, s.Index)
				}
			}

			if slots[0].Wall != WallFocal {
				t.Errorf("slot 0 wall = %q, want focal", slots[0].Wall)
			}

			byWall := map[RoomWall]int{}
			anchors := map[[2]any]bool{}
			for _, s := range slots {
				byWall[s.Wall]++
				key := [2]any{s.Wall, s.PositionOnWall}
				if anchors[key] {
					t.Errorf("duplicate anchor %v/%d", s.Wall, s.PositionOnWall)
				}
				anchors[key] = true
			}

			if byWall[WallFocal] != 1 {
				t.Errorf("focal count = %d, want 1", byWall[WallFocal])
			}

			wantRear := 0
			if (count-1)%2 == 1 {
				wantRear = 1
			}
			if byWall[WallRear] != wantRear {
				t.Errorf("rear count = %d, want %d", byWall[WallRear], wantRear)
			}
			if wantRear == 1 && slots[len(slots)-1].Wall != WallRear {
				t.Errorf("rear must be the final slot, got %q", slots[len(slots)-1].Wall)
			}

			if byWall[WallLeft] != byWall[WallRight] {
				t.Errorf("side walls unbalanced: left=%d right=%d", byWall[WallLeft], byWall[WallRight])
			}
		})
	}
}

func TestPhotoSlotLayout_SideWallsStrictlyAlternateStartingLeft(t *testing.T) {
	if FirstAlternatingWall != WallLeft {
		t.Fatalf("FirstAlternatingWall = %q, want left", FirstAlternatingWall)
	}

	for count := 1; count <= MaxPhotosPerRoom; count++ {
		order := 0
		for _, s := range PhotoSlotLayout(count) {
			if s.Wall != WallLeft && s.Wall != WallRight {
				continue
			}
			want := WallLeft
			if order%2 == 1 {
				want = WallRight
			}
			if s.Wall != want {
				t.Errorf("count=%d side photo %d: wall = %q, want %q", count, order, s.Wall, want)
			}
			if s.PositionOnWall != order/2 {
				t.Errorf("count=%d side photo %d: position = %d, want %d", count, order, s.PositionOnWall, order/2)
			}
			order++
		}
	}
}

func TestPhotoSlotLayout_LowCountsMatchTheConfirmedTable(t *testing.T) {
	want := map[int][]LogicalPhotoSlot{
		1: {{0, WallFocal, 0}},
		2: {{0, WallFocal, 0}, {1, WallRear, 0}},
		3: {{0, WallFocal, 0}, {1, WallLeft, 0}, {2, WallRight, 0}},
		4: {{0, WallFocal, 0}, {1, WallLeft, 0}, {2, WallRight, 0}, {3, WallRear, 0}},
		5: {{0, WallFocal, 0}, {1, WallLeft, 0}, {2, WallRight, 0}, {3, WallLeft, 1}, {4, WallRight, 1}},
		6: {{0, WallFocal, 0}, {1, WallLeft, 0}, {2, WallRight, 0}, {3, WallLeft, 1}, {4, WallRight, 1}, {5, WallRear, 0}},
	}

	for count, expected := range want {
		if got := PhotoSlotLayout(count); !reflect.DeepEqual(got, expected) {
			t.Errorf("count=%d:\n got %+v\nwant %+v", count, got, expected)
		}
	}
}

func TestPhotoSlotLayout_FullRoomUses13PerSideWall(t *testing.T) {
	byWall := map[RoomWall]int{}
	for _, s := range PhotoSlotLayout(MaxPhotosPerRoom) {
		byWall[s.Wall]++
	}

	for wall, want := range map[RoomWall]int{WallFocal: 1, WallLeft: 13, WallRight: 13, WallRear: 1} {
		if byWall[wall] != want {
			t.Errorf("%s = %d, want %d", wall, byWall[wall], want)
		}
	}
}

func TestPhotoSlotLayout_27PhotosNeedNoRearWall(t *testing.T) {
	for _, s := range PhotoSlotLayout(27) {
		if s.Wall == WallRear {
			t.Fatalf("27 photos must not use the rear wall")
		}
	}
}

func TestPhotoSlotLayout_IsDeterministic(t *testing.T) {
	for count := 1; count <= MaxPhotosPerRoom; count++ {
		if !reflect.DeepEqual(PhotoSlotLayout(count), PhotoSlotLayout(count)) {
			t.Errorf("count=%d is not reproducible", count)
		}
	}
}

func TestPhotoSlotLayout_RejectsUnsupportedCounts(t *testing.T) {
	if SupportsPhotoCount(MaxPhotosPerRoom + 1) {
		t.Error("29 photos must be unsupported")
	}
	if SupportsPhotoCount(-1) {
		t.Error("a negative count must be unsupported")
	}
	if PhotoSlotLayout(MaxPhotosPerRoom+1) != nil {
		t.Error("an unsupported count must yield no layout")
	}
	if PhotoSlotLayout(0) != nil {
		t.Error("zero photos must yield no layout")
	}
}

func TestLowestFreeSculptureSlot(t *testing.T) {
	occupying := func(indices ...int) []SculptureInstance {
		out := make([]SculptureInstance, 0, len(indices))
		for _, index := range indices {
			out = append(out, SculptureInstance{SlotIndex: index, CatalogID: "sculpture_test"})
		}
		return out
	}

	cases := []struct {
		name     string
		occupied []SculptureInstance
		want     int
		wantFree bool
	}{
		{"empty Room takes slot 0", nil, 0, true},
		{"contiguous 0 takes 1", occupying(0), 1, true},
		{"contiguous 0,1 takes 2", occupying(0, 1), 2, true},
		{"gap at 0 is reused before 2", occupying(1), 0, true},
		{"gap at 1 is reused", occupying(0, 2), 1, true},
		{"gap at 0 with 1,2 taken", occupying(1, 2), 0, true},
		{"full Room has no free slot", occupying(0, 1, 2), 0, false},
		{"unordered occupancy", occupying(2, 0), 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, free := LowestFreeSculptureSlot(tc.occupied)
			if free != tc.wantFree {
				t.Fatalf("free = %v, want %v", free, tc.wantFree)
			}
			if free && got != tc.want {
				t.Errorf("slot = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestLowestFreeSculptureSlot_NeverExceedsTheCap(t *testing.T) {
	var occupied []SculptureInstance
	for range MaxSculpturesPerRoom {
		index, free := LowestFreeSculptureSlot(occupied)
		if !free {
			t.Fatalf("expected a free slot below the cap, occupied=%v", occupied)
		}
		if !IsValidSculptureSlotIndex(index) {
			t.Fatalf("slot %d is outside the confirmed cap", index)
		}
		occupied = append(occupied, SculptureInstance{SlotIndex: index, CatalogID: "sculpture_test"})
	}
	if _, free := LowestFreeSculptureSlot(occupied); free {
		t.Errorf("a Room at the cap must report no free slot; occupied=%v", occupied)
	}
}
