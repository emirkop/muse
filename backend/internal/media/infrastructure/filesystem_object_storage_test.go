package infrastructure_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"muse-backend/internal/media/application"
	"muse-backend/internal/media/infrastructure"
)

func newTestFS(t *testing.T) (*infrastructure.FilesystemObjectStorage, *httptest.Server) {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	baseURL := "http://" + server.Listener.Addr().String()

	store, err := infrastructure.NewFilesystemObjectStorage(t.TempDir(), baseURL, []byte("test-secret-32-bytes-long-enough!"))
	if err != nil {
		t.Fatalf("NewFilesystemObjectStorage: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle(infrastructure.DevStoragePathPrefix, store.Handler())
	server.Config.Handler = mux
	server.Start()
	t.Cleanup(server.Close)
	return store, server
}

func putTo(t *testing.T, ticket application.UploadTicket, body []byte, contentType string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(ticket.Method, ticket.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func sum(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

func TestFS_UploadThenStatReadAndDownload_RoundTrips(t *testing.T) {
	store, _ := newTestFS(t)
	ctx := context.Background()
	body := []byte("not really a jpeg, but bytes are bytes here")

	ticket, err := store.PresignUpload(ctx, application.PresignUploadRequest{
		Key: "photos/acct/a1", ContentType: "image/jpeg", ByteSize: int64(len(body)), ChecksumSHA256: sum(body), TTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("PresignUpload: %v", err)
	}
	if resp := putTo(t, ticket, body, "image/jpeg"); resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d", resp.StatusCode)
	}

	stat, err := store.Stat(ctx, "photos/acct/a1")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stat.ByteSize != int64(len(body)) || stat.ContentType != "image/jpeg" || stat.ChecksumSHA256 != sum(body) {
		t.Errorf("Stat must report what was stored: %+v", stat)
	}

	head, err := store.ReadRange(ctx, "photos/acct/a1", 0, 10)
	if err != nil || string(head) != "not really" {
		t.Errorf("ReadRange = %q, %v", head, err)
	}

	rc, err := store.Open(ctx, "photos/acct/a1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	all, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(all, body) {
		t.Error("Open must stream the full object")
	}

	download, err := store.PresignDownload(ctx, "photos/acct/a1", time.Minute)
	if err != nil {
		t.Fatalf("PresignDownload: %v", err)
	}
	resp, err := http.Get(download.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, body) || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Errorf("download must serve the stored bytes with their content type; status=%d", resp.StatusCode)
	}
	if resp.Header.Get("Cache-Control") != "private, no-store" {
		t.Errorf("private photo bytes must not be cacheable; got %q", resp.Header.Get("Cache-Control"))
	}
}

func TestFS_StatOnMissingObject_IsErrObjectNotFound(t *testing.T) {
	store, _ := newTestFS(t)
	if _, err := store.Stat(context.Background(), "photos/nobody/none"); !errors.Is(err, application.ErrObjectNotFound) {
		t.Errorf("expected ErrObjectNotFound, got %v", err)
	}
}

func TestFS_RejectsUploadsThatBreakTheSignedBinding(t *testing.T) {
	store, _ := newTestFS(t)
	ctx := context.Background()
	body := []byte("the exact bytes that were declared")
	declared := application.PresignUploadRequest{
		Key: "photos/acct/a2", ContentType: "image/jpeg", ByteSize: int64(len(body)), ChecksumSHA256: sum(body), TTL: time.Minute,
	}

	cases := []struct {
		name        string
		body        []byte
		contentType string
		mutateURL   func(string) string
		wantStatus  int
	}{
		{"wrong content type", body, "image/png", nil, http.StatusForbidden},
		{"wrong length", append([]byte("x"), body...), "image/jpeg", nil, http.StatusForbidden},
		{"wrong checksum, same length", []byte("the exact bytes that were DECLARED"), "image/jpeg", nil, http.StatusBadRequest},
		{"tampered signature", body, "image/jpeg", func(u string) string { return u[:len(u)-4] + "beef" }, http.StatusForbidden},
		{"tampered key", body, "image/jpeg", func(u string) string { return strings.Replace(u, "/a2?", "/a3?", 1) }, http.StatusForbidden},
		{"tampered length parameter", body, "image/jpeg", func(u string) string {
			parsed, _ := url.Parse(u)
			q := parsed.Query()
			q.Set("len", "1")
			parsed.RawQuery = q.Encode()
			return parsed.String()
		}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ticket, err := store.PresignUpload(ctx, declared)
			if err != nil {
				t.Fatalf("PresignUpload: %v", err)
			}
			if tc.mutateURL != nil {
				ticket.URL = tc.mutateURL(ticket.URL)
			}
			resp := putTo(t, ticket, tc.body, tc.contentType)
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if _, err := store.Stat(ctx, "photos/acct/a2"); !errors.Is(err, application.ErrObjectNotFound) {
				t.Errorf("a rejected upload must store nothing; Stat err = %v", err)
			}
		})
	}
}

func TestFS_ExpiredUploadURL_IsRefused(t *testing.T) {
	store, _ := newTestFS(t)
	body := []byte("late")
	ticket, err := store.PresignUpload(context.Background(), application.PresignUploadRequest{
		Key: "photos/acct/late", ContentType: "image/jpeg", ByteSize: int64(len(body)), ChecksumSHA256: sum(body),
		TTL: -time.Second,
	})
	if err != nil {
		t.Fatalf("PresignUpload: %v", err)
	}
	if resp := putTo(t, ticket, body, "image/jpeg"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("expired URL must be refused, got %d", resp.StatusCode)
	}
}

func TestFS_PathTraversalInKey_IsRefused(t *testing.T) {
	_, server := newTestFS(t)
	req, _ := http.NewRequest(http.MethodGet, server.URL+infrastructure.DevStoragePathPrefix+"../../etc/passwd?exp=9999999999&sig=x", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("a traversal key must never be served")
	}
}

func TestFS_Delete_IsIdempotent(t *testing.T) {
	store, _ := newTestFS(t)
	ctx := context.Background()
	body := []byte("bye")
	ticket, _ := store.PresignUpload(ctx, application.PresignUploadRequest{
		Key: "photos/acct/d", ContentType: "image/jpeg", ByteSize: 3, ChecksumSHA256: sum(body), TTL: time.Minute,
	})
	putTo(t, ticket, body, "image/jpeg")

	if err := store.Delete(ctx, "photos/acct/d"); err != nil {
		t.Fatalf("first delete: %v", err)
	}
	if err := store.Delete(ctx, "photos/acct/d"); err != nil {
		t.Fatalf("second delete must be a no-op, got %v", err)
	}
	if _, err := store.Stat(ctx, "photos/acct/d"); !errors.Is(err, application.ErrObjectNotFound) {
		t.Errorf("object must be gone; Stat err = %v", err)
	}
}
