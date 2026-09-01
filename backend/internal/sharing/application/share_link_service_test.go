package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"muse-backend/internal/sharing/application"
	"muse-backend/internal/sharing/domain"
)

const (
	codeA = "AAAAAAAAAAAAAAAAAAAAAA"
	codeB = "BBBBBBBBBBBBBBBBBBBBBB"
	codeC = "CCCCCCCCCCCCCCCCCCCCCC"
)

type fakeLinks struct {
	byCode  map[domain.Code]domain.ShareLink
	rotates int
	ensures int
}

func newFakeLinks() *fakeLinks { return &fakeLinks{byCode: map[domain.Code]domain.ShareLink{}} }

func (f *fakeLinks) active(museumID string) (domain.ShareLink, bool) {
	for _, l := range f.byCode {
		if l.MuseumID == museumID && l.IsActive() {
			return l, true
		}
	}
	return domain.ShareLink{}, false
}

func (f *fakeLinks) FindActiveByMuseum(_ context.Context, museumID string) (domain.ShareLink, error) {
	if l, ok := f.active(museumID); ok {
		return l, nil
	}
	return domain.ShareLink{}, domain.ErrNoActiveLink
}

func (f *fakeLinks) FindByCode(_ context.Context, code domain.Code) (domain.ShareLink, error) {
	if l, ok := f.byCode[code]; ok {
		return l, nil
	}
	return domain.ShareLink{}, domain.ErrLinkNotAvailable
}

func (f *fakeLinks) EnsureActive(_ context.Context, museumID string, code domain.Code, now time.Time) (domain.ShareLink, error) {
	f.ensures++
	if l, ok := f.active(museumID); ok {
		return l, nil
	}
	l := domain.ShareLink{ID: "l-" + string(code), MuseumID: museumID, Code: code, Status: domain.StatusActive, CreatedAt: now}
	f.byCode[code] = l
	return l, nil
}

func (f *fakeLinks) Rotate(_ context.Context, museumID string, code domain.Code, now time.Time) (domain.ShareLink, error) {
	f.rotates++
	for c, l := range f.byCode {
		if l.MuseumID == museumID && l.IsActive() {
			at := now
			l.Status, l.RevokedAt = domain.StatusRevoked, &at
			f.byCode[c] = l
		}
	}
	l := domain.ShareLink{ID: "l-" + string(code), MuseumID: museumID, Code: code, Status: domain.StatusActive, CreatedAt: now}
	f.byCode[code] = l
	return l, nil
}

type fakeCodes struct{ queue []domain.Code }

func (f *fakeCodes) NewCode() (domain.Code, error) {
	if len(f.queue) == 0 {
		return "", errors.New("out of codes")
	}
	c := f.queue[0]
	f.queue = f.queue[1:]
	return c, nil
}

type fakeMuseums struct {
	byOwner map[string]application.Museum
	byID    map[string]application.Museum
}

func newFakeMuseums(museums ...application.Museum) *fakeMuseums {
	f := &fakeMuseums{byOwner: map[string]application.Museum{}, byID: map[string]application.Museum{}}
	for _, m := range museums {
		f.byOwner[m.OwnerAccountID] = m
		f.byID[m.ID] = m
	}
	return f
}

func (f *fakeMuseums) OwnedMuseum(_ context.Context, accountID string) (application.Museum, error) {
	if m, ok := f.byOwner[accountID]; ok {
		return m, nil
	}
	return application.Museum{}, domain.ErrNoMuseum
}

func (f *fakeMuseums) MuseumByID(_ context.Context, id string) (application.Museum, error) {
	if m, ok := f.byID[id]; ok {
		return m, nil
	}
	return application.Museum{}, domain.ErrNoMuseum
}

func (f *fakeMuseums) setPublic(id string, public bool) {
	m := f.byID[id]
	m.Public = public
	f.byID[id] = m
	f.byOwner[m.OwnerAccountID] = m
}

type fakeContent struct {
	museums     map[string]application.MuseumContent
	rooms       map[string]application.RoomContent
	tickets     map[string][]application.PhotoTicket
	reads       int
	ticketReads int
	photosDown  bool
}

func (f *fakeContent) VisitorMuseum(_ context.Context, museumID string) (application.MuseumContent, error) {
	f.reads++
	if c, ok := f.museums[museumID]; ok {
		return c, nil
	}
	return application.MuseumContent{}, application.ErrContentNotVisible
}

func (f *fakeContent) VisitorRoom(_ context.Context, museumID, roomID string) (application.RoomContent, error) {
	f.reads++
	if r, ok := f.rooms[museumID+"/"+roomID]; ok {
		return r, nil
	}
	return application.RoomContent{}, application.ErrContentNotVisible
}

func (f *fakeContent) VisitorRoomPhotoTickets(_ context.Context, museumID, roomID string) ([]application.PhotoTicket, error) {
	f.ticketReads++
	if f.photosDown {
		return nil, application.ErrPhotosUnavailable
	}
	if t, ok := f.tickets[museumID+"/"+roomID]; ok {
		return t, nil
	}
	return nil, application.ErrContentNotVisible
}

type fakeProfiles struct {
	byAccount map[string]application.OwnerProfile
	reads     int
}

func (f *fakeProfiles) PublicProfile(_ context.Context, accountID string) (application.OwnerProfile, error) {
	f.reads++
	if p, ok := f.byAccount[accountID]; ok {
		return p, nil
	}
	return application.OwnerProfile{}, application.ErrOwnerUnavailable
}

type harness struct {
	service  *application.ShareLinkService
	links    *fakeLinks
	codes    *fakeCodes
	museums  *fakeMuseums
	content  *fakeContent
	profiles *fakeProfiles
	now      time.Time
}

func newHarness(museumPublic bool) *harness {
	h := &harness{
		links: newFakeLinks(),
		codes: &fakeCodes{queue: []domain.Code{codeA, codeB, codeC}},
		museums: newFakeMuseums(
			application.Museum{ID: "m1", OwnerAccountID: "owner", StyleID: "style_modern", Public: museumPublic},
			application.Museum{ID: "m2", OwnerAccountID: "other", StyleID: "style_gothic", Public: true},
		),
		content: &fakeContent{
			museums: map[string]application.MuseumContent{
				"m1": {ID: "m1", StyleID: "style_modern", Rooms: []application.RoomSummary{{ID: "r1", Name: "Hall", VariantID: "v1"}}},
				"m2": {ID: "m2", StyleID: "style_gothic"},
			},
			rooms: map[string]application.RoomContent{
				"m1/r1": {ID: "r1", Name: "Hall", VariantID: "v1"},
			},
			tickets: map[string][]application.PhotoTicket{
				"m1/r1": {{PhotoAssetID: "a1", URL: "https://cdn.example/a1", PixelWidth: 640, PixelHeight: 480}},
			},
		},
		profiles: &fakeProfiles{byAccount: map[string]application.OwnerProfile{
			"owner": {AvatarID: "avatar_2"},
		}},
		now: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC),
	}
	h.service = application.NewShareLinkService(h.links, h.codes, h.museums, h.content, h.profiles, func() time.Time { return h.now })
	return h
}

func TestEnsureLink_CreatesOnFirstUse_ThenReusesTheSameLink(t *testing.T) {
	h := newHarness(true)
	ctx := context.Background()

	first, err := h.service.EnsureLink(ctx, "owner")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := h.service.EnsureLink(ctx, "owner")
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if first.Code != codeA || !first.IsActive() || first.MuseumID != "m1" {
		t.Fatalf("unexpected first link %+v", first)
	}
	if second != first {
		t.Fatalf("Share Museum must reuse the active link, got %+v then %+v", first, second)
	}
	if h.links.ensures != 2 {
		t.Fatalf("both calls go through EnsureActive (the repository decides), got %d", h.links.ensures)
	}
}

func TestEnsureLink_WithoutAMuseum_IsRefused(t *testing.T) {
	h := newHarness(true)

	_, err := h.service.EnsureLink(context.Background(), "nobody")

	if !errors.Is(err, domain.ErrNoMuseum) {
		t.Fatalf("got %v, want ErrNoMuseum", err)
	}
	if len(h.links.byCode) != 0 {
		t.Fatal("no link may be minted for an account with no Museum")
	}
}

func TestCurrentLink_ReportsAbsence_WithoutCreating(t *testing.T) {
	h := newHarness(true)
	ctx := context.Background()

	if _, err := h.service.CurrentLink(ctx, "owner"); !errors.Is(err, domain.ErrNoActiveLink) {
		t.Fatalf("got %v, want ErrNoActiveLink", err)
	}
	if len(h.links.byCode) != 0 {
		t.Fatal("CurrentLink must not create")
	}

	created, _ := h.service.EnsureLink(ctx, "owner")
	current, err := h.service.CurrentLink(ctx, "owner")
	if err != nil || current != created {
		t.Fatalf("CurrentLink after EnsureLink: %+v, %v", current, err)
	}
}

func TestRegenerateLink_RevokesTheOld_AndIssuesANew(t *testing.T) {
	h := newHarness(true)
	ctx := context.Background()
	old, _ := h.service.EnsureLink(ctx, "owner")

	fresh, err := h.service.RegenerateLink(ctx, "owner")
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}

	if fresh.Code == old.Code {
		t.Fatal("a new link must have a new code")
	}
	revoked, _ := h.links.FindByCode(ctx, old.Code)
	if revoked.IsActive() || revoked.RevokedAt == nil || !revoked.RevokedAt.Equal(h.now) {
		t.Fatalf("the old link must be revoked at the rotation instant: %+v", revoked)
	}
	current, _ := h.service.CurrentLink(ctx, "owner")
	if current.Code != fresh.Code {
		t.Fatal("exactly one active link, the new one")
	}
	if h.links.rotates != 1 {
		t.Fatalf("one atomic rotation, got %d", h.links.rotates)
	}
}

func TestRegenerateLink_WithNoPriorLink_StillIssuesOne(t *testing.T) {
	h := newHarness(true)

	fresh, err := h.service.RegenerateLink(context.Background(), "owner")

	if err != nil || !fresh.IsActive() {
		t.Fatalf("got %+v, %v", fresh, err)
	}
}

func TestOwnerOperations_NeverTouchAnotherOwnersMuseum(t *testing.T) {
	h := newHarness(true)
	ctx := context.Background()

	mine, _ := h.service.EnsureLink(ctx, "owner")
	theirs, _ := h.service.EnsureLink(ctx, "other")
	_, _ = h.service.RegenerateLink(ctx, "other")

	still, _ := h.links.FindByCode(ctx, mine.Code)
	if !still.IsActive() {
		t.Fatal("regenerating THEIR link must not revoke MINE")
	}
	if theirs.MuseumID != "m2" || mine.MuseumID != "m1" {
		t.Fatalf("links must bind to the caller's own Museum: %+v / %+v", mine, theirs)
	}
}

func TestPreview_ForAnActiveLinkToAPublicMuseum_CarriesOnlySafeFields(t *testing.T) {
	h := newHarness(true)
	ctx := context.Background()
	link, _ := h.service.EnsureLink(ctx, "owner")

	preview, err := h.service.Preview(ctx, link.Code)

	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	want := application.Preview{Code: link.Code, StyleID: "style_modern", Owner: application.OwnerProfile{AvatarID: "avatar_2"}}
	if preview != want {
		t.Fatalf("got %+v, want %+v", preview, want)
	}
}

func TestVisitorGate_EveryRefusalIsErrLinkNotAvailable(t *testing.T) {
	h := newHarness(true)
	ctx := context.Background()
	active, _ := h.service.EnsureLink(ctx, "owner")
	_, _ = h.service.RegenerateLink(ctx, "owner")
	live, _ := h.service.CurrentLink(ctx, "owner")

	privateOwnerLink, _ := h.service.EnsureLink(ctx, "other")
	h.museums.setPublic("m2", false)

	cases := map[string]domain.Code{
		"unknown but well-formed":   "ZZZZZZZZZZZZZZZZZZZZZZ",
		"malformed (short)":         "abc",
		"malformed (charset)":       "AAAAAAAAAAAAAAAAAAAA!!",
		"empty":                     "",
		"revoked":                   active.Code,
		"active but Museum Private": privateOwnerLink.Code,
	}
	for name, code := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := h.service.Preview(ctx, code); !errors.Is(err, domain.ErrLinkNotAvailable) {
				t.Fatalf("Preview: got %v, want ErrLinkNotAvailable", err)
			}
			if _, err := h.service.VisitorMuseum(ctx, code); !errors.Is(err, domain.ErrLinkNotAvailable) {
				t.Fatalf("VisitorMuseum: got %v, want ErrLinkNotAvailable", err)
			}
			if _, err := h.service.VisitorRoom(ctx, code, "r1"); !errors.Is(err, domain.ErrLinkNotAvailable) {
				t.Fatalf("VisitorRoom: got %v, want ErrLinkNotAvailable", err)
			}
			if _, err := h.service.VisitorRoomPhotoTickets(ctx, code, "r1"); !errors.Is(err, domain.ErrLinkNotAvailable) {
				t.Fatalf("VisitorRoomPhotoTickets: got %v, want ErrLinkNotAvailable", err)
			}
		})
	}

	if _, err := h.service.VisitorMuseum(ctx, live.Code); err != nil {
		t.Fatalf("the live link must resolve: %v", err)
	}
}

func TestVisitorGate_RefusesBeforeTouchingContentOrProfile(t *testing.T) {
	h := newHarness(false)
	ctx := context.Background()
	link, _ := h.service.EnsureLink(ctx, "owner")

	_, _ = h.service.Preview(ctx, link.Code)
	_, _ = h.service.VisitorMuseum(ctx, link.Code)
	_, _ = h.service.VisitorRoom(ctx, link.Code, "r1")
	_, _ = h.service.VisitorRoomPhotoTickets(ctx, link.Code, "r1")
	_, _ = h.service.Preview(ctx, "ZZZZZZZZZZZZZZZZZZZZZZ")

	if h.content.reads != 0 || h.profiles.reads != 0 || h.content.ticketReads != 0 {
		t.Fatalf("no protected read may happen for a refused link: content %d, profile %d, tickets %d",
			h.content.reads, h.profiles.reads, h.content.ticketReads)
	}
}

func TestVisitorMuseum_ReturnsTheMuseumContextsVisitorView(t *testing.T) {
	h := newHarness(true)
	ctx := context.Background()
	link, _ := h.service.EnsureLink(ctx, "owner")

	content, err := h.service.VisitorMuseum(ctx, link.Code)

	if err != nil {
		t.Fatalf("visitor museum: %v", err)
	}
	if content.ID != "m1" || len(content.Rooms) != 1 || content.Rooms[0].ID != "r1" {
		t.Fatalf("unexpected content %+v", content)
	}
}

func TestVisitorRoom_UnderTheWrongLink_IsNotAvailable(t *testing.T) {
	h := newHarness(true)
	ctx := context.Background()
	otherLink, _ := h.service.EnsureLink(ctx, "other")

	_, err := h.service.VisitorRoom(ctx, otherLink.Code, "r1")

	if !errors.Is(err, domain.ErrLinkNotAvailable) {
		t.Fatalf("got %v, want ErrLinkNotAvailable", err)
	}
}

func TestRegenerate_TakesEffectOnTheNextVisitorRead(t *testing.T) {
	h := newHarness(true)
	ctx := context.Background()
	old, _ := h.service.EnsureLink(ctx, "owner")
	if _, err := h.service.VisitorMuseum(ctx, old.Code); err != nil {
		t.Fatalf("live before: %v", err)
	}

	fresh, _ := h.service.RegenerateLink(ctx, "owner")

	if _, err := h.service.VisitorMuseum(ctx, old.Code); !errors.Is(err, domain.ErrLinkNotAvailable) {
		t.Fatalf("old link must be dead immediately, got %v", err)
	}
	if _, err := h.service.VisitorMuseum(ctx, fresh.Code); err != nil {
		t.Fatalf("new link must be live immediately: %v", err)
	}
}

func TestMuseumPrivacy_GatesTheLinkOnEveryRequest(t *testing.T) {
	h := newHarness(true)
	ctx := context.Background()
	link, _ := h.service.EnsureLink(ctx, "owner")

	h.museums.setPublic("m1", false)
	if _, err := h.service.Preview(ctx, link.Code); !errors.Is(err, domain.ErrLinkNotAvailable) {
		t.Fatalf("Private Museum: got %v", err)
	}
	h.museums.setPublic("m1", true)
	if _, err := h.service.Preview(ctx, link.Code); err != nil {
		t.Fatalf("Public again: %v", err)
	}
	if still, _ := h.links.FindByCode(ctx, link.Code); !still.IsActive() {
		t.Fatal("privacy must not alter the link record")
	}
}

func TestPreview_WhenTheOwnerIsUnavailable_IsNotAvailable(t *testing.T) {
	h := newHarness(true)
	ctx := context.Background()
	link, _ := h.service.EnsureLink(ctx, "owner")
	delete(h.profiles.byAccount, "owner")

	_, err := h.service.Preview(ctx, link.Code)

	if !errors.Is(err, domain.ErrLinkNotAvailable) {
		t.Fatalf("got %v, want ErrLinkNotAvailable", err)
	}
}

func TestIsPlausibleCode(t *testing.T) {
	good := []string{codeA, "abcdefghijklmnopqrstuv", "0123456789-_ABCDEFGHIJ"}
	bad := []string{"", "short", codeA + "A", "AAAAAAAAAAAAAAAAAAAAA=", "AAAAAAAAAAAAAAAAAAAAA/", "AAAAAAAAAAAAAAAAAAAAA "}
	for _, c := range good {
		if !domain.IsPlausibleCode(c) {
			t.Fatalf("%q should be plausible", c)
		}
	}
	for _, c := range bad {
		if domain.IsPlausibleCode(c) {
			t.Fatalf("%q should not be plausible", c)
		}
	}
}

func TestVisitorRoomPhotoTickets_ThroughAnActiveLink_ReturnsTheTickets(t *testing.T) {
	h := newHarness(true)
	ctx := context.Background()
	link, _ := h.service.EnsureLink(ctx, "owner")

	tickets, err := h.service.VisitorRoomPhotoTickets(ctx, link.Code, "r1")

	if err != nil {
		t.Fatalf("tickets: %v", err)
	}
	if len(tickets) != 1 || tickets[0].PhotoAssetID != "a1" || tickets[0].PixelWidth != 640 {
		t.Fatalf("unexpected tickets %+v", tickets)
	}
}

func TestVisitorRoomPhotoTickets_StopAtRegenerationAndAtPrivacy(t *testing.T) {
	h := newHarness(true)
	ctx := context.Background()
	old, _ := h.service.EnsureLink(ctx, "owner")

	fresh, _ := h.service.RegenerateLink(ctx, "owner")
	if _, err := h.service.VisitorRoomPhotoTickets(ctx, old.Code, "r1"); !errors.Is(err, domain.ErrLinkNotAvailable) {
		t.Fatalf("revoked link must not deliver bytes, got %v", err)
	}
	if _, err := h.service.VisitorRoomPhotoTickets(ctx, fresh.Code, "r1"); err != nil {
		t.Fatalf("the new link delivers: %v", err)
	}

	h.museums.setPublic("m1", false)
	if _, err := h.service.VisitorRoomPhotoTickets(ctx, fresh.Code, "r1"); !errors.Is(err, domain.ErrLinkNotAvailable) {
		t.Fatalf("a Private Museum must not deliver bytes, got %v", err)
	}
}

func TestVisitorRoomPhotoTickets_ForAnInvisibleRoom_IsNotAvailable(t *testing.T) {
	h := newHarness(true)
	ctx := context.Background()
	link, _ := h.service.EnsureLink(ctx, "owner")

	if _, err := h.service.VisitorRoomPhotoTickets(ctx, link.Code, "r2"); !errors.Is(err, domain.ErrLinkNotAvailable) {
		t.Fatalf("got %v, want ErrLinkNotAvailable", err)
	}
}

func TestVisitorRoomPhotoTickets_WithoutStorage_IsNotConfusedWithADeadLink(t *testing.T) {
	h := newHarness(true)
	ctx := context.Background()
	link, _ := h.service.EnsureLink(ctx, "owner")
	h.content.photosDown = true

	_, err := h.service.VisitorRoomPhotoTickets(ctx, link.Code, "r1")

	if !errors.Is(err, application.ErrPhotosUnavailable) {
		t.Fatalf("got %v, want ErrPhotosUnavailable", err)
	}
	if errors.Is(err, domain.ErrLinkNotAvailable) {
		t.Fatal("a deployment state must not masquerade as a dead link")
	}
}
