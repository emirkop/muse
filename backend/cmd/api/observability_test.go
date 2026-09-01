package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	identitydomain "muse-backend/internal/identity/domain"
	"muse-backend/internal/platform/observability"
)

func TestAuthorizationFailureIsTraceableEndToEnd(t *testing.T) {
	s := newStack(t)
	owner := s.createRoom()
	strangerID, strangerToken := s.stranger82(t)

	s.logs.reset()
	resp, _ := s.do(http.MethodPatch, "/museum/me/rooms/"+owner,
		map[string]any{"name": "not yours"}, strangerToken)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	requestID := resp.Header.Get(observability.ResponseHeaderName)
	if requestID == "" {
		t.Fatal("no request id on the response; a user cannot quote what they were not given")
	}

	var refusal map[string]any
	for _, record := range s.logs.records(t) {
		if record[observability.FieldEvent] == observability.EventAuthorizationRefused {
			refusal = record
		}
	}
	if refusal == nil {
		t.Fatalf("no authz.refused line was emitted; records: %v", s.logs.records(t))
	}

	if refusal[observability.FieldCategory] != string(observability.CategoryAuthz) {
		t.Errorf("category = %v", refusal[observability.FieldCategory])
	}
	if refusal[observability.FieldOutcome] != string(observability.OutcomeRefused) {
		t.Errorf("outcome = %v", refusal[observability.FieldOutcome])
	}
	if refusal[observability.FieldReason] != observability.ReasonNoMuseumForCaller {
		t.Errorf("reason = %v, want %q — the rule that refused is the diagnostic value",
			refusal[observability.FieldReason], observability.ReasonNoMuseumForCaller)
	}
	if refusal[observability.FieldAccountID] != strangerID {
		t.Errorf("account_id = %v, want the caller's own account", refusal[observability.FieldAccountID])
	}
	if refusal[observability.FieldRequestID] != requestID {
		t.Errorf("request_id = %v, want %q — the header and the log must agree",
			refusal[observability.FieldRequestID], requestID)
	}

	if strings.Contains(s.logs.raw(), owner) {
		t.Error("the log names the Room that was refused; a log line proving a Room exists is the same disclosure refuses")
	}
}

func TestRequestIDSurvivesTheLayers(t *testing.T) {
	s := newStack(t)
	s.logs.reset()

	first, _ := s.do(http.MethodGet, "/catalog/bundles/no-such-bundle/manifest", nil, s.token)
	firstID := first.Header.Get(observability.ResponseHeaderName)
	second, _ := s.do(http.MethodGet, "/museum/me/rooms/"+bogusUUID, nil, s.token)
	secondID := second.Header.Get(observability.ResponseHeaderName)

	if firstID == "" || secondID == "" || firstID == secondID {
		t.Fatalf("request ids must exist and differ per request: %q, %q", firstID, secondID)
	}

	byID := map[string][]string{}
	for _, record := range s.logs.records(t) {
		id, _ := record[observability.FieldRequestID].(string)
		event, _ := record[observability.FieldEvent].(string)
		if id != "" && event != "" {
			byID[id] = append(byID[id], event)
		}
	}
	if len(byID[firstID]) == 0 {
		t.Errorf("no line carried the first request's id")
	}
	if len(byID[secondID]) == 0 {
		t.Errorf("no line carried the second request's id")
	}
	for _, event := range byID[firstID] {
		if event == observability.EventAuthorizationRefused {
			t.Error("the manifest request's id appears on an authz line from the other request")
		}
	}
}

func TestSensitiveValuesNeverAppearInLogs(t *testing.T) {
	s := newStack(t)
	room := s.createRoom()
	photo := s.uploaded(newPhoto(t, 640, 480, "leak-check"))
	if resp, _, _ := s.assign(room, []string{photo.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatalf("assign: %d", resp.StatusCode)
	}
	linkResp, linkBody := s.do(http.MethodPost, "/museum/me/share-link", nil, s.token)
	if linkResp.StatusCode != http.StatusOK && linkResp.StatusCode != http.StatusCreated {
		t.Fatalf("share link: %d %s", linkResp.StatusCode, linkBody)
	}
	var link struct {
		Code string `json:"code"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(linkBody, &link); err != nil {
		t.Fatalf("decode link: %v", err)
	}

	s.logs.reset()

	cases := []struct {
		method, path string
		body         any
		token        string
	}{
		{http.MethodGet, "/share-links/aaaaaaaaaaaaaaaaaaaaaa/museum", nil, s.token},
		{http.MethodGet, "/share-links/" + link.Code + "/museum", nil, s.token},
		{http.MethodDelete, "/museum/me/rooms/" + bogusUUID + "/photos/" + photo.asset, nil, s.token},
		{http.MethodPost, "/media/photo-uploads", map[string]any{
			"client_upload_id": "leak-check", "content_type": "image/gif",
			"byte_size": 10, "pixel_width": 10, "pixel_height": 10,
			"checksum_sha256": strings.Repeat("a", 64),
		}, s.token},
		{http.MethodPost, "/auth/password/login", map[string]any{
			"email": "leak-check@example.com", "password": "a-very-secret-password",
		}, ""},
		{http.MethodGet, "/museum/me", nil, "eyJhbGciOiJIUzI1NiJ9.leaked-token-payload.signature"},
	}
	for _, testCase := range cases {
		s.do(testCase.method, testCase.path, testCase.body, testCase.token)
	}

	raw := s.logs.raw()
	forbidden := map[string]string{
		"share code (a capability, )":  link.Code,
		"share URL":                    link.URL,
		"photograph asset id":          photo.asset,
		"a Room id":                    room,
		"the caller's access token":    s.token,
		"an email address":             "leak-check@example.com",
		"a password":                   "a-very-secret-password",
		"a bearer token from a header": "leaked-token-payload",
	}
	for what, value := range forbidden {
		if value == "" {
			t.Fatalf("test setup produced no value for %s", what)
		}
		if strings.Contains(raw, value) {
			t.Errorf("logs contain %s (%q)", what, value)
		}
	}
	if strings.Contains(raw, "photos/") {
		t.Error("logs contain a storage key prefix")
	}
	if raw == "" {
		t.Fatal("no log output at all — the scan proved nothing")
	}
}

func TestEveryOperationalLineCarriesTheStableFields(t *testing.T) {
	s := newStack(t)
	s.logs.reset()

	s.do(http.MethodGet, "/museum/me/rooms/"+bogusUUID, nil, s.token)
	s.do(http.MethodGet, "/catalog/bundles/no-such-bundle/manifest", nil, s.token)
	s.do(http.MethodGet, "/share-links/aaaaaaaaaaaaaaaaaaaaaa/museum", nil, s.token)
	s.do(http.MethodPost, "/collection-rooms/"+bogusUUID+"/items",
		map[string]string{"catalog_model_id": "dev-fixture:model-does-not-exist"}, s.token)

	records := s.logs.records(t)
	if len(records) == 0 {
		t.Fatal("no log output — nothing was instrumented")
	}
	categories := map[string]bool{}
	for _, record := range records {
		event, ok := record[observability.FieldEvent].(string)
		if !ok || event == "" {
			t.Errorf("a log line has no %q field: %v", observability.FieldEvent, record)
			continue
		}
		if record["msg"] != event {
			t.Errorf("msg (%v) and event (%v) disagree — two representations of one fact drift", record["msg"], event)
		}
		category, ok := record[observability.FieldCategory].(string)
		if !ok || category == "" {
			t.Errorf("%s has no category", event)
			continue
		}
		categories[category] = true
		if _, ok := record[observability.FieldOutcome].(string); !ok {
			t.Errorf("%s has no outcome", event)
		}
	}
	for _, want := range []string{
		string(observability.CategoryAuthz),
		string(observability.CategoryAssetDelivery),
		string(observability.CategorySharing),
	} {
		if !categories[want] {
			t.Errorf("no line from category %q; instrumented contexts: %v", want, categories)
		}
	}
}

// MARK: - Harness

func (s *stack) stranger82(t *testing.T) (accountID string, token string) {
	t.Helper()
	if err := s.pool.Pool().QueryRow(context.Background(),
		`INSERT INTO accounts (display_name) VALUES ('observability stranger') RETURNING id`).Scan(&accountID); err != nil {
		t.Fatalf("seed stranger: %v", err)
	}
	signed, _, err := s.signer.Sign(identitydomain.AccountID(accountID), identitydomain.SessionID("observability-sess"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return accountID, signed
}

type logCapture struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (c *logCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buffer.Write(p)
}

func (c *logCapture) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buffer.Reset()
}

func (c *logCapture) raw() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buffer.String()
}

func (c *logCapture) records(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(c.raw()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		out = append(out, record)
	}
	return out
}
