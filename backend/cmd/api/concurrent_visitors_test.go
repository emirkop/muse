package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	identitydomain "muse-backend/internal/identity/domain"
	identityinfra "muse-backend/internal/identity/infrastructure"
)

const (
	visitorCount = 16
	readRounds   = 8
)

type concurrentFixture struct {
	visitorFixture
	visitors []string
}

func newConcurrentFixture(t *testing.T, s *stack, visitors int) concurrentFixture {
	t.Helper()
	f := newVisitorFixture(t, s)
	tokens := make([]string, 0, visitors)
	for i := 0; i < visitors; i++ {
		tokens = append(tokens, s.strangerToken())
	}
	return concurrentFixture{visitorFixture: f, visitors: tokens}
}

type observation struct {
	visitor int
	round   int
	path    string
	status  int
	body    string
	sentAt  time.Time
}

func fanOut(s *stack, tokens []string, rounds int, fn func(visitor int, token string, round int) []observation) []observation {
	var (
		mu  sync.Mutex
		all []observation
		wg  sync.WaitGroup
	)
	for i, token := range tokens {
		wg.Add(1)
		go func(i int, token string) {
			defer wg.Done()
			var mine []observation
			for r := 0; r < rounds; r++ {
				mine = append(mine, fn(i, token, r)...)
			}
			mu.Lock()
			all = append(all, mine...)
			mu.Unlock()
		}(i, token)
	}
	wg.Wait()
	return all
}

func (s *stack) observe(visitor, round int, path, token string) observation {
	sentAt := time.Now()
	r := s.get(path, token)
	return observation{visitor: visitor, round: round, path: path, status: r.status, body: r.body, sentAt: sentAt}
}

func assetIDsOf(t *testing.T, ticketsBody string) []string {
	t.Helper()
	var decoded struct {
		Tickets []struct {
			PhotoAssetID string `json:"photo_asset_id"`
		} `json:"tickets"`
	}
	if err := json.Unmarshal([]byte(ticketsBody), &decoded); err != nil {
		t.Fatalf("decode tickets: %v (%s)", err, ticketsBody)
	}
	ids := make([]string, 0, len(decoded.Tickets))
	for _, tk := range decoded.Tickets {
		ids = append(ids, tk.PhotoAssetID)
	}
	sort.Strings(ids)
	return ids
}

func TestConcurrentVisitors_ReadTheSameContentIndependently(t *testing.T) {
	s := newStack(t)
	f := newConcurrentFixture(t, s, visitorCount)
	before := s.snapshotOwnerState()

	museumPath := "/share-links/" + f.code + "/museum"
	roomPath := "/share-links/" + f.code + "/rooms/" + f.publicRoom
	ticketsPath := roomPath + "/photo-urls"

	all := fanOut(s, f.visitors, readRounds, func(v int, token string, r int) []observation {
		return []observation{
			s.observe(v, r, museumPath, token),
			s.observe(v, r, roomPath, token),
			s.observe(v, r, ticketsPath, token),
		}
	})

	if want := visitorCount * readRounds * 3; len(all) != want {
		t.Fatalf("expected %d observations, got %d", want, len(all))
	}
	bodies := map[string]map[string]bool{}
	for _, o := range all {
		if o.status != http.StatusOK {
			t.Fatalf("visitor %d round %d %s → %d %s", o.visitor, o.round, o.path, o.status, o.body)
		}
		if bodies[o.path] == nil {
			bodies[o.path] = map[string]bool{}
		}
		key := o.body
		if o.path == ticketsPath {
			key = strings.Join(assetIDsOf(t, o.body), ",")
		}
		bodies[o.path][key] = true
	}
	for path, distinct := range bodies {
		if len(distinct) != 1 {
			t.Fatalf("%s: %d distinct answers across visitors — content must not depend on who else is reading", path, len(distinct))
		}
	}
	if !strings.Contains(firstKey(bodies[ticketsPath]), f.publicAsset) {
		t.Fatalf("every visitor must be ticketed the owner's photograph, got %s", firstKey(bodies[ticketsPath]))
	}
	for _, o := range all {
		if strings.Contains(o.body, f.privateRoom) || strings.Contains(o.body, f.privateAsset) {
			t.Fatalf("visitor %d saw the Private Room under load: %s", o.visitor, o.body)
		}
	}
	if after := s.snapshotOwnerState(); after != before {
		t.Fatalf("concurrent visitors must change nothing of the owner's:\nbefore %+v\nafter  %+v", before, after)
	}
}

func firstKey(m map[string]bool) string {
	for k := range m {
		return k
	}
	return ""
}

func TestOneVisitorsBrokenSession_NeverAffectsAnother(t *testing.T) {
	s := newStack(t)
	f := newConcurrentFixture(t, s, 8)

	otherKey := identityinfra.NewAccessTokenSigner([]byte("a-different-signing-key-of-sufficient-length"), "muse-backend", time.Hour)
	forged, _, _ := otherKey.Sign(identitydomain.AccountID(s.accountID), identitydomain.SessionID("forged"))
	expiredSigner := identityinfra.NewAccessTokenSigner([]byte("integration-signing-key-that-is-long-enough"), "muse-backend", -time.Minute)
	expired, _, _ := expiredSigner.Sign(identitydomain.AccountID(s.accountID), identitydomain.SessionID("expired"))
	broken := []string{"not-a-token", forged, expired}

	tokens := append([]string{}, f.visitors...)
	tokens = append(tokens, broken...)
	path := "/share-links/" + f.code + "/rooms/" + f.publicRoom

	all := fanOut(s, tokens, readRounds, func(v int, token string, r int) []observation {
		return []observation{s.observe(v, r, path, token)}
	})

	for _, o := range all {
		isBroken := o.visitor >= len(f.visitors)
		switch {
		case isBroken && o.status != http.StatusUnauthorized:
			t.Fatalf("broken session %d round %d → %d; a neighbour's valid session must never lend it access", o.visitor, o.round, o.status)
		case !isBroken && o.status != http.StatusOK:
			t.Fatalf("valid visitor %d round %d → %d %s; a neighbour's broken session must never cost it access", o.visitor, o.round, o.status, o.body)
		}
	}
	if r := s.get(path, expired); r.status != http.StatusUnauthorized {
		t.Fatalf("expired token should be refused, got %d", r.status)
	}
}

func TestRegenerationUnderLoad_LeaksNoAuthorityAcrossVisitors(t *testing.T) {
	s := newStack(t)
	f := newConcurrentFixture(t, s, visitorCount)
	oldPath := "/share-links/" + f.code + "/museum"

	var (
		regenDone time.Time
		newCode   string
		once      sync.Once
	)
	all := fanOut(s, f.visitors, readRounds*2, func(v int, token string, r int) []observation {
		if r == 0 {
			if o := s.observe(v, r, oldPath, token); o.status != http.StatusOK {
				t.Errorf("visitor %d must be able to read before regeneration: %d", v, o.status)
			}
		}
		if v == 0 && r == readRounds {
			once.Do(func() {
				fresh := s.regenerateShareLink(s.token)
				newCode = fresh.code
				regenDone = time.Now()
			})
		}
		return []observation{s.observe(v, r, oldPath, token)}
	})

	if regenDone.IsZero() {
		t.Fatal("the regeneration never happened")
	}
	afterCount := 0
	for _, o := range all {
		if o.sentAt.After(regenDone) {
			afterCount++
			if o.status != http.StatusNotFound {
				t.Fatalf("visitor %d sent a request through the OLD link after regeneration and got %d — authority leaked", o.visitor, o.status)
			}
		} else if o.status != http.StatusOK && o.status != http.StatusNotFound {
			t.Fatalf("visitor %d round %d → %d %s before/around regeneration", o.visitor, o.round, o.status, o.body)
		}
	}
	if afterCount == 0 {
		t.Fatal("no request was sent after regeneration — the test did not exercise the property")
	}
	for i, token := range f.visitors {
		if r := s.get("/share-links/"+newCode+"/museum", token); r.status != http.StatusOK {
			t.Fatalf("visitor %d through the new link: %d %s", i, r.status, r.body)
		}
	}
	replies := map[string]reply{}
	for i, token := range f.visitors {
		replies[string(rune('a'+i))] = s.get(oldPath, token)
	}
	replies["unknown"] = s.get("/share-links/"+unknownCode+"/museum", f.visitors[0])
	mustBeIndistinguishable(t, replies)
}

func TestPrivacyFlipsUnderLoad_AreAtomicPerRequest(t *testing.T) {
	s := newStack(t)
	f := newConcurrentFixture(t, s, 8)
	path := "/share-links/" + f.code + "/museum"

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 6; i++ {
			s.setMuseumPrivacy(s.token, "private")
			s.setMuseumPrivacy(s.token, "public")
		}
	}()
	all := fanOut(s, f.visitors, readRounds*2, func(v int, token string, r int) []observation {
		return []observation{s.observe(v, r, path, token)}
	})
	wg.Wait()

	okShape, refusal := 0, 0
	for _, o := range all {
		switch o.status {
		case http.StatusOK:
			var decoded struct {
				MuseumID string           `json:"museum_id"`
				StyleID  string           `json:"style_id"`
				Rooms    []map[string]any `json:"rooms"`
			}
			if err := json.Unmarshal([]byte(o.body), &decoded); err != nil || decoded.MuseumID != f.museumID || decoded.StyleID == "" || len(decoded.Rooms) != 1 {
				t.Fatalf("visitor %d got a 200 that is not the complete Public view: %s", o.visitor, o.body)
			}
			okShape++
		case http.StatusNotFound:
			if o.body != `{"error":"not found"}`+"\n" {
				t.Fatalf("visitor %d got a refusal that is not THE refusal: %q", o.visitor, o.body)
			}
			refusal++
		default:
			t.Fatalf("visitor %d → %d %s under a privacy flip; only 200 or 404 may ever be observed", o.visitor, o.status, o.body)
		}
	}
	t.Logf("observed %d complete views and %d refusals across %d reads", okShape, refusal, len(all))
	if r := s.get("/museum/me", s.token); !strings.Contains(r.body, `"privacy":"public"`) {
		t.Fatalf("owner's final privacy must be what the owner last set: %s", r.body)
	}
	if r := s.get(path, f.visitors[0]); r.status != http.StatusOK {
		t.Fatalf("visible again after the last flip: %d", r.status)
	}
}

func TestConcurrentVisitorsOfDifferentMuseums_NeverCross(t *testing.T) {
	s := newStack(t)
	f := newConcurrentFixture(t, s, 6)

	resp, raw := s.do(http.MethodPost, "/museum/me/rooms", map[string]string{"name": "B Hall", "variant_id": "style_modern_variant_Hall"}, f.strangerWithMuseum)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("B room: %d %s", resp.StatusCode, raw)
	}
	var bRoom struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &bRoom)
	s.setRoomPrivacy(f.strangerWithMuseum, bRoom.ID, "public")
	bPhoto := newPhoto(t, 640, 480, "b-photo")
	if resp, raw := s.do(http.MethodPost, "/media/photo-uploads", map[string]any{
		"client_upload_id": bPhoto.cuid, "content_type": "image/jpeg", "byte_size": len(bPhoto.data),
		"pixel_width": bPhoto.w, "pixel_height": bPhoto.h, "checksum_sha256": bPhoto.sha,
	}, f.strangerWithMuseum); resp.StatusCode != http.StatusCreated {
		t.Fatalf("B initiate: %d %s", resp.StatusCode, raw)
	} else {
		var body struct {
			AssetID string `json:"asset_id"`
			Upload  struct {
				URL     string            `json:"url"`
				Method  string            `json:"method"`
				Headers map[string]string `json:"headers"`
			} `json:"upload"`
		}
		_ = json.Unmarshal(raw, &body)
		bPhoto.asset = body.AssetID
		bPhoto.upload.URL, bPhoto.upload.Method, bPhoto.upload.Headers = body.Upload.URL, body.Upload.Method, body.Upload.Headers
	}
	if code := s.put(bPhoto); code != http.StatusOK {
		t.Fatalf("B PUT: %d", code)
	}
	if resp, raw := s.do(http.MethodPost, "/museum/me/rooms/"+bRoom.ID+"/photos", map[string]any{"asset_ids": []string{bPhoto.asset}}, f.strangerWithMuseum); resp.StatusCode != http.StatusCreated {
		t.Fatalf("B assign: %d %s", resp.StatusCode, raw)
	}
	bCode := s.ensureShareLink(f.strangerWithMuseum).code

	half := len(f.visitors) / 2
	aVisitors, bVisitors := f.visitors[:half], f.visitors[half:]
	var wg sync.WaitGroup
	var mu sync.Mutex
	var aObs, bObs []observation
	wg.Add(2)
	go func() {
		defer wg.Done()
		obs := fanOut(s, aVisitors, readRounds, func(v int, token string, r int) []observation {
			return []observation{
				s.observe(v, r, "/share-links/"+f.code+"/museum", token),
				s.observe(v, r, "/share-links/"+f.code+"/rooms/"+f.publicRoom+"/photo-urls", token),
			}
		})
		mu.Lock()
		aObs = obs
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		obs := fanOut(s, bVisitors, readRounds, func(v int, token string, r int) []observation {
			return []observation{
				s.observe(v, r, "/share-links/"+bCode+"/museum", token),
				s.observe(v, r, "/share-links/"+bCode+"/rooms/"+bRoom.ID+"/photo-urls", token),
			}
		})
		mu.Lock()
		bObs = obs
		mu.Unlock()
	}()
	wg.Wait()

	for _, o := range aObs {
		if o.status != http.StatusOK {
			t.Fatalf("A visitor %d → %d %s", o.visitor, o.status, o.body)
		}
		if strings.Contains(o.body, f.strangerMuseumID) || strings.Contains(o.body, bRoom.ID) || strings.Contains(o.body, bPhoto.asset) {
			t.Fatalf("Museum A's visitor received Museum B's content: %s", o.body)
		}
	}
	for _, o := range bObs {
		if o.status != http.StatusOK {
			t.Fatalf("B visitor %d → %d %s", o.visitor, o.status, o.body)
		}
		if strings.Contains(o.body, f.museumID) || strings.Contains(o.body, f.publicRoom) || strings.Contains(o.body, f.publicAsset) {
			t.Fatalf("Museum B's visitor received Museum A's content: %s", o.body)
		}
	}
	if r := s.get("/share-links/"+f.code+"/rooms/"+bRoom.ID+"/photo-urls", aVisitors[0]); r.status != http.StatusNotFound {
		t.Fatalf("A's link must not ticket B's Room: %d", r.status)
	}
	if r := s.get("/share-links/"+bCode+"/rooms/"+f.publicRoom+"/photo-urls", bVisitors[0]); r.status != http.StatusNotFound {
		t.Fatalf("B's link must not ticket A's Room: %d", r.status)
	}
}

func TestAPreviouslyAdmittedVisitor_HoldsNoAuthorityAfterRegeneration(t *testing.T) {
	s := newStack(t)
	f := newConcurrentFixture(t, s, 2)
	admitted, never := f.visitors[0], f.visitors[1]
	oldPath := "/share-links/" + f.code + "/museum"

	if r := s.get(oldPath, admitted); r.status != http.StatusOK {
		t.Fatalf("admitted visitor must read first: %d", r.status)
	}
	_ = s.regenerateShareLink(s.token)

	mustBeIndistinguishable(t, map[string]reply{
		"previously admitted": s.get(oldPath, admitted),
		"never used the link": s.get(oldPath, never),
		"unknown code":        s.get("/share-links/"+unknownCode+"/museum", never),
	})
}
