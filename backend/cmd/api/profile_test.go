package main

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	entitlementdomain "muse-backend/internal/entitlement/domain"
)

func requireProfiling2(t *testing.T) {
	t.Helper()
	if os.Getenv("MUSE_PROFILE") == "" {
		t.Skip("MUSE_PROFILE not set — skipping profiling runs")
	}
}

func measure(t *testing.T, label string, runs int, body func() reply) {
	t.Helper()
	first := body()
	if first.status != http.StatusOK {
		t.Fatalf("%s: %d %s", label, first.status, first.body)
	}
	start := time.Now()
	for i := 0; i < runs; i++ {
		if r := body(); r.status != http.StatusOK {
			t.Fatalf("%s: %d %s", label, r.status, r.body)
		}
	}
	per := time.Since(start) / time.Duration(runs)
	t.Logf("MEASURED %-34s %8s per request, %6.1f KiB payload",
		label, per.Round(time.Microsecond), float64(len(first.body))/1024)
}

func TestFullMuseumRoomAndVisitorPayloads(t *testing.T) {
	requireProfiling2(t)
	s := newStack(t)
	f := newSweepFixture(t, s)
	visitor := s.strangerToken()

	assetIDs := make([]string, 0, 28)
	for i := 0; i < 28; i++ {
		assetIDs = append(assetIDs, s.uploaded(newPhoto(t, 640, 480, fmt.Sprintf("p%02d", i))).asset)
	}
	if resp, _, body := s.assign(f.publicRoom, assetIDs[:27]); resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("assign 27: %d %v", resp.StatusCode, body)
	}
	for _, assetID := range assetIDs[:27] {
		if resp, _, _ := s.setCaption(f.publicRoom, assetID, "A caption of an ordinary, realistic length for a photograph.", s.token); resp.StatusCode != http.StatusOK {
			t.Fatalf("caption: %d", resp.StatusCode)
		}
	}
	for _, id := range s.seedSculptureCatalog("p1", "p2", "p3") {
		if resp, _, _ := s.addSculpture(f.publicRoom, id, s.token); resp.StatusCode != http.StatusCreated {
			t.Fatalf("sculpture: %d", resp.StatusCode)
		}
	}
	if n := s.slotCount(f.publicRoom); n != 28 {
		t.Fatalf("expected a full Room of 28, got %d", n)
	}

	measure(t, "owner: full Room (28+3)", 30, func() reply {
		return s.get("/museum/me/rooms/"+f.publicRoom, s.token)
	})
	measure(t, "owner: photo tickets (28)", 30, func() reply {
		return s.get("/museum/me/rooms/"+f.publicRoom+"/photo-urls", s.token)
	})
	measure(t, "visitor: Museum through link", 30, func() reply {
		return s.get("/share-links/"+f.code+"/museum", visitor)
	})
	measure(t, "visitor: full Room through link", 30, func() reply {
		return s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom, visitor)
	})
	measure(t, "visitor: photo tickets (28)", 30, func() reply {
		return s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom+"/photo-urls", visitor)
	})
	measure(t, "pre-auth preview", 30, func() reply {
		return s.get("/share-links/"+f.code, "")
	})
}

func TestEntitlementEnforcedAddPath(t *testing.T) {
	requireProfiling2(t)
	s := newStackWithCapacities(t, entitlementdomain.ItemCapacities{Free: 400, Paid: 800, Source: "phase 78 profile"})
	s.publishCommittedDesignFixture(t)

	const perRoom = 18
	var rooms []string
	newRoom := func() string {
		id := s.roomWithPublishedDesign(t, fmt.Sprintf("Profile Watches %d", len(rooms)))
		rooms = append(rooms, id)
		return id
	}
	current := newRoom()
	inCurrent := 0

	addOne := func() {
		if inCurrent == perRoom {
			current = newRoom()
			inCurrent = 0
		}
		if tier := requiredTierFor(t, inCurrent+1); tier > s.currentTier(current) {
			if resp, body := s.ratchet(current, tier); resp.StatusCode != http.StatusOK {
				t.Fatalf("ratchet to %d: %d %s", tier, resp.StatusCode, body)
			}
		}
		if status, body := s.addItemAs(current, s.token); status != http.StatusCreated {
			t.Fatalf("add at %d account items: %d %s", s.accountItemCount(s.token), status, body)
		}
		inCurrent++
	}

	for _, milestone := range []int{18, 90, 180, 360} {
		for s.accountItemCount(s.token) < milestone-20 {
			addOne()
		}
		const runs = 20
		start := time.Now()
		for i := 0; i < runs; i++ {
			addOne()
		}
		per := time.Since(start) / runs
		t.Logf("MEASURED entitlement-enforced add at ~%3d account items across %d rooms: %8s per add",
			s.accountItemCount(s.token), len(rooms), per.Round(time.Microsecond))
	}
	t.Logf("NOTE: each add is ONE transaction taking a per-account advisory lock, then the "+
		"Room lock, then the Design tier check, then the account-wide count. "+
		"Total now %d items across %d rooms.", s.accountItemCount(s.token), len(rooms))
}
