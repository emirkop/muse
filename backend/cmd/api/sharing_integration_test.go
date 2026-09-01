package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const (
	testShareLinkBase = "https://muse.test"
	testAppStoreURL   = "https://apps.apple.com/app/id0000000000"
	testAppleAppID    = "TEAMID0000.com.muse.app"
	unknownCode       = "ZZZZZZZZZZZZZZZZZZZZZZ"
)

type linkJSON struct {
	code, url string
}

func (s *stack) ensureShareLink(token string) linkJSON {
	s.t.Helper()
	resp, raw := s.do(http.MethodPost, "/museum/me/share-link", nil, token)
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("ensure share link: %d %s", resp.StatusCode, raw)
	}
	return decodeLink(s.t, raw)
}

func (s *stack) regenerateShareLink(token string) linkJSON {
	s.t.Helper()
	resp, raw := s.do(http.MethodPost, "/museum/me/share-link/regenerate", nil, token)
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("regenerate share link: %d %s", resp.StatusCode, raw)
	}
	return decodeLink(s.t, raw)
}

func decodeLink(t *testing.T, raw []byte) linkJSON {
	t.Helper()
	var body struct {
		Code string `json:"code"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode link: %v (%s)", err, raw)
	}
	return linkJSON{code: body.Code, url: body.URL}
}

func newLinkedFixture(t *testing.T, s *stack) (gate2Fixture, linkJSON) {
	t.Helper()
	f := newGate2Fixture(t, s)
	s.setMuseumPrivacy(s.token, "public")
	return f, s.ensureShareLink(s.token)
}

func TestShareLink_ShareMuseumReusesTheActiveLink_NewLinkReplacesIt(t *testing.T) {
	s := newStack(t)
	_ = newGate2Fixture(t, s)

	if r := s.get("/museum/me/share-link", s.token); r.status != http.StatusNotFound {
		t.Fatalf("before sharing there is no link: %d %s", r.status, r.body)
	}
	first := s.ensureShareLink(s.token)
	if first.url != testShareLinkBase+"/m/"+first.code || len(first.code) != 22 {
		t.Fatalf("unexpected link %+v", first)
	}
	if again := s.ensureShareLink(s.token); again != first {
		t.Fatalf("Share Museum must reuse the active link: %+v then %+v", first, again)
	}
	if r := s.get("/museum/me/share-link", s.token); r.status != http.StatusOK || !strings.Contains(r.body, first.code) {
		t.Fatalf("GET must report the active link: %d %s", r.status, r.body)
	}

	fresh := s.regenerateShareLink(s.token)
	if fresh.code == first.code {
		t.Fatal("New Link must issue a new code")
	}
	if r := s.get("/museum/me/share-link", s.token); !strings.Contains(r.body, fresh.code) || strings.Contains(r.body, first.code) {
		t.Fatalf("exactly one active link, the new one: %s", r.body)
	}
	if again := s.ensureShareLink(s.token); again != fresh {
		t.Fatal("after regeneration, Share Museum reuses the NEW link")
	}
}

func TestShareLink_OwnerWithoutAMuseum_GetsTheCollapsedNotFound(t *testing.T) {
	s := newStack(t)
	notFound := s.get("/museum/me/rooms/00000000-0000-4000-8000-000000000000", s.token)

	for _, attempt := range []struct{ method, path string }{
		{http.MethodPost, "/museum/me/share-link"},
		{http.MethodGet, "/museum/me/share-link"},
		{http.MethodPost, "/museum/me/share-link/regenerate"},
	} {
		resp, raw := s.do(attempt.method, attempt.path, nil, s.token)
		if resp.StatusCode != http.StatusNotFound || string(raw) != notFound.body {
			t.Fatalf("%s %s → %d %s; want the same 404 as any missing resource (%s)", attempt.method, attempt.path, resp.StatusCode, raw, notFound.body)
		}
	}
}

func TestShareLink_LifecycleIsOwnerScoped(t *testing.T) {
	s := newStack(t)
	f, owners := newLinkedFixture(t, s)

	if resp, _ := s.do(http.MethodPost, "/museum/me/share-link", nil, f.stranger); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a stranger with no Museum has nothing to share, got %d", resp.StatusCode)
	}
	theirs := s.ensureShareLink(f.strangerWithMuseum)
	if theirs.code == owners.code {
		t.Fatal("two Museums, two links")
	}
	_ = s.regenerateShareLink(f.strangerWithMuseum)

	if still := s.ensureShareLink(s.token); still != owners {
		t.Fatalf("regenerating THEIR link must not touch the owner's: %+v vs %+v", still, owners)
	}
	if r := s.get("/share-links/"+owners.code+"/museum", f.stranger); r.status != http.StatusOK {
		t.Fatalf("the owner's link must still resolve: %d %s", r.status, r.body)
	}
}

func TestSecurityGate2_Links_EveryRefusalIsIndistinguishable(t *testing.T) {
	s := newStack(t)
	f := newGate2Fixture(t, s)
	privateLink := s.ensureShareLink(s.token)
	revoked := s.ensureShareLink(f.strangerWithMuseum)
	_ = s.regenerateShareLink(f.strangerWithMuseum)

	codes := map[string]string{
		"unknown":                   unknownCode,
		"malformed (short)":         "abc",
		"malformed (charset)":       "AAAAAAAAAAAAAAAAAAAA!!",
		"revoked":                   revoked.code,
		"active but Museum Private": privateLink.code,
	}
	for name, token := range map[string]string{"stranger without museum": f.stranger, "stranger with own museum": f.strangerWithMuseum} {
		t.Run(name, func(t *testing.T) {
			previews := map[string]reply{}
			museums := map[string]reply{}
			rooms := map[string]reply{}
			pages := map[string]reply{}
			for label, code := range codes {
				previews[label] = s.get("/share-links/"+code, "")
				museums[label] = s.get("/share-links/"+code+"/museum", token)
				rooms[label] = s.get("/share-links/"+code+"/rooms/"+f.publicRoom, token)
				pages[label] = s.get("/m/"+code, "")
			}
			mustBeIndistinguishable(t, previews)
			mustBeIndistinguishable(t, museums)
			mustBeIndistinguishable(t, rooms)
			mustBeIndistinguishable(t, pages)
			if previews["unknown"].status != http.StatusNotFound || museums["unknown"].status != http.StatusNotFound || pages["unknown"].status != http.StatusNotFound {
				t.Fatalf("every refusal is a 404: %d / %d / %d", previews["unknown"].status, museums["unknown"].status, pages["unknown"].status)
			}
			if !strings.Contains(pages["unknown"].body, "no longer available") {
				t.Fatalf("the landing page must say the link is no longer available: %s", pages["unknown"].body)
			}
		})
	}
}

func TestSecurityGate2_Links_RegenerationEndsAccessThroughTheOldLink(t *testing.T) {
	s := newStack(t)
	f, old := newLinkedFixture(t, s)
	if r := s.get("/share-links/"+old.code+"/museum", f.stranger); r.status != http.StatusOK {
		t.Fatalf("live before: %d %s", r.status, r.body)
	}

	fresh := s.regenerateShareLink(s.token)

	unknown := s.get("/share-links/"+unknownCode+"/museum", f.stranger)
	mustBeIndistinguishable(t, map[string]reply{
		"old link": s.get("/share-links/"+old.code+"/museum", f.stranger),
		"unknown":  unknown,
	})
	mustBeIndistinguishable(t, map[string]reply{
		"old link preview": s.get("/share-links/"+old.code, ""),
		"unknown preview":  s.get("/share-links/"+unknownCode, ""),
	})
	mustBeIndistinguishable(t, map[string]reply{
		"old landing":     s.get("/m/"+old.code, ""),
		"unknown landing": s.get("/m/"+unknownCode, ""),
	})
	if r := s.get("/share-links/"+fresh.code+"/museum", f.stranger); r.status != http.StatusOK {
		t.Fatalf("the new link must be live immediately: %d %s", r.status, r.body)
	}
}

func TestSecurityGate2_Links_PreviewCarriesOnlySafeFields(t *testing.T) {
	s := newStack(t)
	f, link := newLinkedFixture(t, s)

	r := s.get("/share-links/"+link.code, "")
	if r.status != http.StatusOK {
		t.Fatalf("preview of an active link to a Public Museum: %d %s", r.status, r.body)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(r.body), &body); err != nil {
		t.Fatal(err)
	}
	for key := range body {
		switch key {
		case "code", "style_id", "owner":
		default:
			t.Fatalf("unexpected preview field %q: %s", key, r.body)
		}
	}
	owner, _ := body["owner"].(map[string]any)
	for key := range owner {
		if key != "avatar_id" {
			t.Fatalf("unexpected owner field %q: %s", key, r.body)
		}
	}
	for _, forbidden := range []string{"display_name", "\"owner\":\"owner\"", f.museumID, f.publicRoom, f.privateRoom, "The Long Hall", "Private Study", "Trabzon", "rooms", "privacy", "photo"} {
		if strings.Contains(r.body, forbidden) {
			t.Fatalf("preview must not carry %q: %s", forbidden, r.body)
		}
	}
	mustBeIndistinguishable(t, map[string]reply{
		"anonymous": r,
		"stranger":  s.get("/share-links/"+link.code, f.stranger),
		"owner":     s.get("/share-links/"+link.code, s.token),
	})
}

func TestSecurityGate2_Links_ContentIsAuthenticatedAndVisitorFiltered(t *testing.T) {
	s := newStack(t)
	f, link := newLinkedFixture(t, s)

	mustBeIndistinguishable(t, map[string]reply{
		"real link, no token":    s.get("/share-links/"+link.code+"/museum", ""),
		"unknown link, no token": s.get("/share-links/"+unknownCode+"/museum", ""),
		"real room, no token":    s.get("/share-links/"+link.code+"/rooms/"+f.publicRoom, ""),
	})
	if r := s.get("/share-links/"+link.code+"/museum", ""); r.status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", r.status)
	}

	museum := s.get("/share-links/"+link.code+"/museum", f.stranger)
	if museum.status != http.StatusOK {
		t.Fatalf("authenticated visitor: %d %s", museum.status, museum.body)
	}
	if !strings.Contains(museum.body, f.publicRoom) || strings.Contains(museum.body, f.privateRoom) || strings.Contains(museum.body, `"privacy"`) {
		t.Fatalf("only the Public Room, no privacy field: %s", museum.body)
	}
	var decoded struct {
		MuseumID string           `json:"museum_id"`
		Rooms    []map[string]any `json:"rooms"`
	}
	_ = json.Unmarshal([]byte(museum.body), &decoded)
	if decoded.MuseumID != f.museumID || len(decoded.Rooms) != 1 {
		t.Fatalf("exactly one Room listed, no placeholder for the hidden one: %s", museum.body)
	}

	if r := s.get("/share-links/"+link.code+"/rooms/"+f.publicRoom, f.stranger); r.status != http.StatusOK || strings.Contains(r.body, `"privacy"`) {
		t.Fatalf("the Public Room is readable without a privacy field: %d %s", r.status, r.body)
	}
	mustBeIndistinguishable(t, map[string]reply{
		"private room":     s.get("/share-links/"+link.code+"/rooms/"+f.privateRoom, f.stranger),
		"nonexistent room": s.get("/share-links/"+link.code+"/rooms/"+f.nonexistentRoom, f.stranger),
		"malformed room":   s.get("/share-links/"+link.code+"/rooms/not-an-id", f.stranger),
	})

	theirs := s.ensureShareLink(f.strangerWithMuseum)
	mustBeIndistinguishable(t, map[string]reply{
		"owner's room under stranger's link": s.get("/share-links/"+theirs.code+"/rooms/"+f.publicRoom, f.stranger),
		"nonexistent room under that link":   s.get("/share-links/"+theirs.code+"/rooms/"+f.nonexistentRoom, f.stranger),
	})
}

func TestSecurityGate2_Links_OwnerThroughTheLinkSeesTheVisitorView(t *testing.T) {
	s := newStack(t)
	f, link := newLinkedFixture(t, s)

	viaLink := s.get("/share-links/"+link.code+"/museum", s.token)
	asStranger := s.get("/share-links/"+link.code+"/museum", f.stranger)
	if viaLink != asStranger {
		t.Fatalf("the link shows the owner exactly the visitor view:\n%+v\n%+v", viaLink, asStranger)
	}
	if strings.Contains(viaLink.body, f.privateRoom) {
		t.Fatal("the Private Room is hidden even from the owner through the link")
	}
	if r := s.get("/museums/"+f.museumID, s.token); r.status != http.StatusOK || !strings.Contains(r.body, f.privateRoom) {
		t.Fatalf("the owner's by-id read still shows everything: %d %s", r.status, r.body)
	}
}

func TestShareLink_LandingPage_ShowsOnlyThePreview_AndSelfContainedHTML(t *testing.T) {
	s := newStack(t)
	f, link := newLinkedFixture(t, s)

	resp, raw := s.do(http.MethodGet, "/m/"+link.code, nil, "")
	body := string(raw)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("landing: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(body, "You've been invited to a Museum on Muse.") {
		t.Fatalf("the page carries the fixed, nameless heading: %s", body)
	}
	if !strings.Contains(body, link.url) || !strings.Contains(body, testAppStoreURL) {
		t.Fatalf("the page carries the share URL and the App Store link: %s", body)
	}
	for _, forbidden := range []string{"<script", "owner", f.museumID, f.publicRoom, "The Long Hall", "Private Study", "Trabzon", "http://", "https://fonts", "https://cdn"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("landing page must not contain %q: %s", forbidden, body)
		}
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("hardening headers missing: %v", resp.Header)
	}
}

func TestShareLink_AppleAppSiteAssociation_IsServedWithApplesShape(t *testing.T) {
	s := newStack(t)

	resp, raw := s.do(http.MethodGet, "/.well-known/apple-app-site-association", nil, "")

	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("AASA: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var aasa struct {
		Applinks struct {
			Details []struct {
				AppIDs     []string            `json:"appIDs"`
				Components []map[string]string `json:"components"`
			} `json:"details"`
		} `json:"applinks"`
	}
	if err := json.Unmarshal(raw, &aasa); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	if len(aasa.Applinks.Details) != 1 || len(aasa.Applinks.Details[0].AppIDs) != 1 || aasa.Applinks.Details[0].AppIDs[0] != testAppleAppID {
		t.Fatalf("appIDs must be exactly the configured <TeamID>.<BundleID>: %s", raw)
	}
	c := aasa.Applinks.Details[0].Components
	if len(c) != 2 || c[0]["/"] != "/m/*" || c[1]["/"] != "/c/*" {
		t.Fatalf("components must claim exactly /m/* and /c/*: %s", raw)
	}
}
