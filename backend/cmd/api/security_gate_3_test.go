package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	mediaapp "muse-backend/internal/media/application"
	mediainfra "muse-backend/internal/media/infrastructure"
)

const forbiddenBody = "signature invalid or expired\n"

func (s *stack) ticketFor(t *testing.T, token, roomID, assetID string) string {
	t.Helper()
	r := s.get("/museum/me/rooms/"+roomID+"/photo-urls", token)
	if r.status != http.StatusOK {
		t.Fatalf("owner tickets: %d %s", r.status, r.body)
	}
	var body struct {
		Tickets []struct {
			PhotoAssetID string `json:"photo_asset_id"`
			URL          string `json:"url"`
		} `json:"tickets"`
	}
	if err := json.Unmarshal([]byte(r.body), &body); err != nil {
		t.Fatal(err)
	}
	for _, ticket := range body.Tickets {
		if ticket.PhotoAssetID == assetID {
			return ticket.URL
		}
	}
	t.Fatalf("no ticket for %s in %s", assetID, r.body)
	return ""
}

func redeem(t *testing.T, rawURL string) reply {
	t.Helper()
	resp, err := testGet(rawURL) //nolint:gosec // a test URL minted by the test server
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return reply{status: resp.StatusCode, body: string(body)}
}

func withQuery(t *testing.T, rawURL, name, value string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set(name, value)
	u.RawQuery = q.Encode()
	return u.String()
}

// MARK: - 1. Unforgeable, non-durable

func TestGate3_TicketURLs_AreUnforgeableAndNonDurable_AndFailGenerically(t *testing.T) {
	s := newStack(t)
	f := newVisitorFixture(t, s)

	valid := s.ticketFor(t, s.token, f.privateRoom, f.privateAsset)
	if r := redeem(t, valid); r.status != http.StatusOK {
		t.Fatalf("a valid ticket must redeem for the owner, got %d %s", r.status, r.body)
	}
	if resp, err := testGet(valid); err == nil { //nolint:gosec // test URL
		resp.Body.Close()
		if cc := resp.Header.Get("Cache-Control"); cc != "private, no-store" {
			t.Errorf("bytes must never be cached by an intermediary: Cache-Control %q", cc)
		}
	}

	parsed, err := url.Parse(valid)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := parsed.Scheme + "://" + parsed.Host + parsed.Path
	exp := parsed.Query().Get("exp")
	sig := parsed.Query().Get("sig")
	if exp == "" || sig == "" {
		t.Fatalf("a ticket must carry an expiry and a signature: %s", valid)
	}

	repointed := strings.Replace(valid, "/"+f.privateAsset+"?", "/"+f.publicAsset+"?", 1)
	if repointed == valid {
		t.Fatal("test setup: could not re-point the ticket")
	}

	failures := map[string]reply{
		"signature stripped":        redeem(t, unsigned),
		"expiry extended":           redeem(t, withQuery(t, valid, "exp", "9999999999")),
		"signature tampered":        redeem(t, withQuery(t, valid, "sig", strings.Repeat("0", len(sig)))),
		"signature truncated":       redeem(t, withQuery(t, valid, "sig", sig[:len(sig)-2])),
		"re-pointed at another key": redeem(t, repointed),
		"expired":                   redeem(t, s.expiredTicketFor(t, f.privateAsset)),
	}
	for name, r := range failures {
		if r.status != http.StatusForbidden {
			t.Errorf("%s: expected 403, got %d %s", name, r.status, r.body)
		}
		if r.body != forbiddenBody {
			t.Errorf("%s: body %q leaks which check failed", name, r.body)
		}
	}
	mustBeIndistinguishable(t, failures)
}

func (s *stack) expiredTicketFor(t *testing.T, assetID string) string {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	assets := mediainfra.NewPostgresAssetRepository(s.pool.Pool())
	alreadyExpired := mediaapp.NewMediaService(assets, s.storage, 5*time.Minute, -time.Second, logger)
	tickets, err := alreadyExpired.IssuePhotoDownloadTickets(context.Background(), s.accountID, []string{assetID})
	if err != nil || len(tickets) != 1 {
		t.Fatalf("expired ticket: %v (%d)", err, len(tickets))
	}
	return tickets[0].URL
}

// MARK: - 2. A storage key is not authority

func TestGate3_StorageKeys_AreNotAuthority_OnEitherByteSurface(t *testing.T) {
	s := newStack(t)
	f := newVisitorFixture(t, s)

	realKey := "photos/" + s.accountID + "/" + f.privateAsset
	fakeKey := "photos/" + s.accountID + "/" + s.randomID()

	signedSurface := map[string]reply{
		"real key, unsigned":        redeem(t, s.server.URL+mediainfra.DevStoragePathPrefix+realKey),
		"nonexistent key, unsigned": redeem(t, s.server.URL+mediainfra.DevStoragePathPrefix+fakeKey),
	}
	for name, r := range signedSurface {
		if r.status != http.StatusForbidden {
			t.Errorf("%s: expected 403, got %d", name, r.status)
		}
	}
	mustBeIndistinguishable(t, signedSurface)

	publicSurface := map[string]reply{
		"real photo key":        redeem(t, s.server.URL+mediainfra.DevPublicAssetPathPrefix+realKey),
		"nonexistent photo key": redeem(t, s.server.URL+mediainfra.DevPublicAssetPathPrefix+fakeKey),
	}
	for name, r := range publicSurface {
		if r.status != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", name, r.status)
		}
	}
	mustBeIndistinguishable(t, publicSurface)
}

// MARK: - 3. Issuance re-derives authority on every request

func TestGate3_TicketIssuance_RefusesPrivateRevokedAndNonexistentIdentically(t *testing.T) {
	s := newStack(t)
	f := newVisitorFixture(t, s)

	if r := s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom+"/photo-urls", f.stranger); r.status != http.StatusOK {
		t.Fatalf("visitor tickets for a Public Room: %d %s", r.status, r.body)
	}

	revoked := f.code
	fresh := s.regenerateShareLink(s.token)

	refusals := map[string]reply{
		"Private Room":           s.get("/share-links/"+fresh.code+"/rooms/"+f.privateRoom+"/photo-urls", f.stranger),
		"nonexistent Room":       s.get("/share-links/"+fresh.code+"/rooms/"+f.nonexistentRoom+"/photo-urls", f.stranger),
		"malformed Room id":      s.get("/share-links/"+fresh.code+"/rooms/not-a-uuid/photo-urls", f.stranger),
		"revoked link":           s.get("/share-links/"+revoked+"/rooms/"+f.publicRoom+"/photo-urls", f.stranger),
		"unknown link":           s.get("/share-links/"+unknownCode+"/rooms/"+f.publicRoom+"/photo-urls", f.stranger),
		"Room of another Museum": s.get("/share-links/"+fresh.code+"/rooms/"+s.strangersRoom(t, f)+"/photo-urls", f.stranger),
	}
	for name, r := range refusals {
		if r.status != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d %s", name, r.status, r.body)
		}
	}
	mustBeIndistinguishable(t, refusals)

	ownerRoute := map[string]reply{
		"stranger on owner route, real Room":        s.get("/museum/me/rooms/"+f.privateRoom+"/photo-urls", f.stranger),
		"stranger on owner route, nonexistent Room": s.get("/museum/me/rooms/"+f.nonexistentRoom+"/photo-urls", f.stranger),
	}
	for name, r := range ownerRoute {
		if r.status != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d %s", name, r.status, r.body)
		}
	}
	mustBeIndistinguishable(t, ownerRoute)
}

func (s *stack) strangersRoom(t *testing.T, f visitorFixture) string {
	t.Helper()
	resp, raw := s.do(http.MethodPost, "/museum/me/rooms",
		map[string]string{"name": "Elsewhere", "variant_id": "style_modern_variant_Hall"}, f.strangerWithMuseum)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("stranger's room: %d %s", resp.StatusCode, raw)
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &created)
	return created.ID
}

func TestGate3_PrivacyAndLinkChanges_TakeEffectOnTheNextTicketRequest(t *testing.T) {
	s := newStack(t)
	f := newVisitorFixture(t, s)
	visitorTickets := func(code string) reply {
		return s.get("/share-links/"+code+"/rooms/"+f.publicRoom+"/photo-urls", f.stranger)
	}
	nonexistent := s.get("/share-links/"+f.code+"/rooms/"+f.nonexistentRoom+"/photo-urls", f.stranger)

	if r := visitorTickets(f.code); r.status != http.StatusOK {
		t.Fatalf("baseline: %d", r.status)
	}

	s.setMuseumPrivacy(s.token, "private")
	mustBeIndistinguishable(t, map[string]reply{"after Museum Private": visitorTickets(f.code), "nonexistent": nonexistent})
	s.setMuseumPrivacy(s.token, "public")
	if r := visitorTickets(f.code); r.status != http.StatusOK {
		t.Fatalf("Museum Public again must restore delivery: %d", r.status)
	}

	s.setRoomPrivacy(s.token, f.publicRoom, "private")
	mustBeIndistinguishable(t, map[string]reply{"after Room Private": visitorTickets(f.code), "nonexistent": nonexistent})
	s.setRoomPrivacy(s.token, f.publicRoom, "public")
	if r := visitorTickets(f.code); r.status != http.StatusOK {
		t.Fatalf("Room Public again must restore delivery: %d", r.status)
	}

	fresh := s.regenerateShareLink(s.token)
	mustBeIndistinguishable(t, map[string]reply{"old link": visitorTickets(f.code), "nonexistent": nonexistent})
	if r := visitorTickets(fresh.code); r.status != http.StatusOK {
		t.Fatalf("the new link delivers: %d", r.status)
	}

	if r := s.get("/museum/me/rooms/"+f.privateRoom+"/photo-urls", s.token); r.status != http.StatusOK {
		t.Fatalf("owner must always see their own Private Room's tickets: %d", r.status)
	}
}

// MARK: - 4. The payload carries no authority beyond the URL

func TestGate3_TicketPayload_CarriesNoStorageKeyOrAccountIdentity(t *testing.T) {
	s := newStack(t)
	f := newVisitorFixture(t, s)

	for name, path := range map[string]string{
		"owner":   "/museum/me/rooms/" + f.publicRoom + "/photo-urls",
		"visitor": "/share-links/" + f.code + "/rooms/" + f.publicRoom + "/photo-urls",
	} {
		token := s.token
		if name == "visitor" {
			token = f.stranger
		}
		r := s.get(path, token)
		if r.status != http.StatusOK {
			t.Fatalf("%s tickets: %d %s", name, r.status, r.body)
		}
		var body struct {
			Tickets []map[string]json.RawMessage `json:"tickets"`
		}
		if err := json.Unmarshal([]byte(r.body), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Tickets) == 0 {
			t.Fatalf("%s: expected a ticket", name)
		}
		for _, ticket := range body.Tickets {
			for key := range ticket {
				switch key {
				case "photo_asset_id", "url", "expires_at", "pixel_width", "pixel_height":
				default:
					t.Errorf("%s ticket carries an unexpected field %q — the URL is the only authority", name, key)
				}
			}
		}
		if !strings.Contains(r.body, `"expires_at"`) {
			t.Errorf("%s: a ticket without an expiry would be a durable link", name)
		}
	}
}

// MARK: - 5. The accepted window, stated

func TestGate3_AnIssuedTicket_RedeemsUntilExpiry_TheDocumentedPD017Window(t *testing.T) {
	s := newStack(t)
	f := newVisitorFixture(t, s)

	r := s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom+"/photo-urls", f.stranger)
	if r.status != http.StatusOK {
		t.Fatalf("visitor tickets: %d", r.status)
	}
	var body struct {
		Tickets []struct {
			URL string `json:"url"`
		} `json:"tickets"`
	}
	_ = json.Unmarshal([]byte(r.body), &body)
	issuedBefore := body.Tickets[0].URL

	s.setMuseumPrivacy(s.token, "private")

	if r := s.get("/share-links/"+f.code+"/rooms/"+f.publicRoom+"/photo-urls", f.stranger); r.status != http.StatusNotFound {
		t.Fatalf("issuance after Private must be refused, got %d", r.status)
	}
	if r := redeem(t, issuedBefore); r.status != http.StatusOK {
		t.Fatalf("an unexpired ticket is a bearer credential for its TTL; got %d — if this changed, was decided and must be recorded", r.status)
	}
	if r := redeem(t, s.expiredTicketFor(t, f.publicAsset)); r.status != http.StatusForbidden || r.body != forbiddenBody {
		t.Fatalf("an expired ticket must be refused generically, got %d %q", r.status, r.body)
	}
}
