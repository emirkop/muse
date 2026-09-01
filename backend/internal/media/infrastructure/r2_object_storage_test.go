package infrastructure_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"muse-backend/internal/media/application"
	"muse-backend/internal/media/infrastructure"
)

func newTestR2(t *testing.T) *infrastructure.R2ObjectStorage {
	t.Helper()
	store, err := infrastructure.NewR2ObjectStorage(infrastructure.R2Config{
		AccountID:       "0123456789abcdef0123456789abcdef",
		Bucket:          "muse-photos-test",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	})
	if err != nil {
		t.Fatalf("NewR2ObjectStorage: %v", err)
	}
	return store
}

func TestR2_PresignUpload_TargetsTheR2EndpointAndBindsTheObject(t *testing.T) {
	store := newTestR2(t)

	ticket, err := store.PresignUpload(context.Background(), application.PresignUploadRequest{
		Key:            "photos/acct/asset-1",
		ContentType:    "image/jpeg",
		ByteSize:       123456,
		ChecksumSHA256: strings.Repeat("ab", 32),
		TTL:            5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("PresignUpload: %v", err)
	}

	u, err := url.Parse(ticket.URL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	if ticket.Method != "PUT" {
		t.Errorf("method = %s, want PUT", ticket.Method)
	}
	if u.Scheme != "https" || !strings.HasSuffix(u.Host, ".r2.cloudflarestorage.com") {
		t.Errorf("URL must target R2 over HTTPS, got %s://%s", u.Scheme, u.Host)
	}
	if !strings.Contains(u.Path, "muse-photos-test") || !strings.HasSuffix(u.Path, "/photos/acct/asset-1") {
		t.Errorf("URL path must address the bucket and exact key, got %s", u.Path)
	}

	q := u.Query()
	if q.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" {
		t.Errorf("expected SigV4, got %q", q.Get("X-Amz-Algorithm"))
	}
	if q.Get("X-Amz-Expires") != "300" {
		t.Errorf("expiry = %q, want 300", q.Get("X-Amz-Expires"))
	}
	if q.Get("X-Amz-Signature") == "" {
		t.Error("URL must be signed")
	}
	if !strings.Contains(q.Get("X-Amz-Credential"), "AKIAIOSFODNN7EXAMPLE/") {
		t.Errorf("credential scope must name the access key, got %q", q.Get("X-Amz-Credential"))
	}

	signed := strings.ToLower(q.Get("X-Amz-SignedHeaders"))
	for _, want := range []string{"host", "content-type", "x-amz-checksum-sha256"} {
		if !strings.Contains(signed, want) {
			t.Errorf("SignedHeaders %q must include %s", signed, want)
		}
	}
	if got := ticket.Headers["Content-Type"]; got != "image/jpeg" {
		t.Errorf("client must be told to send Content-Type: image/jpeg, got %q", got)
	}
	if got := ticket.Headers["X-Amz-Checksum-Sha256"]; got == "" {
		t.Error("client must be told the checksum header the signature binds")
	}
	if time.Until(ticket.ExpiresAt) > 5*time.Minute || time.Until(ticket.ExpiresAt) < 4*time.Minute {
		t.Errorf("ExpiresAt must reflect the TTL, got %v from now", time.Until(ticket.ExpiresAt))
	}
}

func TestR2_PresignDownload_IsAShortLivedSignedGET(t *testing.T) {
	store := newTestR2(t)

	ticket, err := store.PresignDownload(context.Background(), "photos/acct/asset-1", 2*time.Minute)
	if err != nil {
		t.Fatalf("PresignDownload: %v", err)
	}
	u, _ := url.Parse(ticket.URL)
	if u.Query().Get("X-Amz-Expires") != "120" || u.Query().Get("X-Amz-Signature") == "" {
		t.Errorf("download URL must be signed with the requested expiry, got %s", ticket.URL)
	}
	if !strings.HasSuffix(u.Path, "/photos/acct/asset-1") {
		t.Errorf("download URL must address exactly one object, got %s", u.Path)
	}
}

func TestR2_ExplicitEndpointOverridesTheAccountDerivedOne(t *testing.T) {
	store, err := infrastructure.NewR2ObjectStorage(infrastructure.R2Config{
		Endpoint:        "http://127.0.0.1:9000",
		Bucket:          "b",
		AccessKeyID:     "k",
		SecretAccessKey: "s",
	})
	if err != nil {
		t.Fatalf("NewR2ObjectStorage: %v", err)
	}
	ticket, err := store.PresignDownload(context.Background(), "k", time.Minute)
	if err != nil {
		t.Fatalf("PresignDownload: %v", err)
	}
	if !strings.HasPrefix(ticket.URL, "http://127.0.0.1:9000/") {
		t.Errorf("expected the explicit endpoint, got %s", ticket.URL)
	}
}

func TestR2_RequiresCredentialsAndBucket(t *testing.T) {
	if _, err := infrastructure.NewR2ObjectStorage(infrastructure.R2Config{AccountID: "a"}); err == nil {
		t.Error("missing bucket and credentials must be refused at construction, not at first use")
	}
	if _, err := infrastructure.NewR2ObjectStorage(infrastructure.R2Config{Bucket: "b", AccessKeyID: "k", SecretAccessKey: "s"}); err == nil {
		t.Error("missing account id and endpoint must be refused")
	}
}
