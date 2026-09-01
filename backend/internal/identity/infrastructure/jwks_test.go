package infrastructure

import (
	"context"
	"crypto/rsa"
	"net/http"
	"testing"
	"time"
)

func TestJWKSClient_KeyForID_FetchesAndParsesRSAKey(t *testing.T) {
	priv := generateRSAKeyPair(t)
	server := serveRSAJWKS(t, "test-kid", &priv.PublicKey)
	defer server.Close()

	client := NewJWKSClient(server.URL, nil, time.Hour)

	key, err := client.KeyForID(context.Background(), "test-kid")
	if err != nil {
		t.Fatalf("KeyForID: %v", err)
	}

	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("expected *rsa.PublicKey, got %T", key)
	}
	if rsaKey.N.Cmp(priv.PublicKey.N) != 0 || rsaKey.E != priv.PublicKey.E {
		t.Fatal("parsed RSA public key does not match the original")
	}
}

func TestJWKSClient_KeyForID_UnknownKidReturnsError(t *testing.T) {
	priv := generateRSAKeyPair(t)
	server := serveRSAJWKS(t, "test-kid", &priv.PublicKey)
	defer server.Close()

	client := NewJWKSClient(server.URL, nil, time.Hour)

	_, err := client.KeyForID(context.Background(), "does-not-exist")
	if err == nil {
		t.Fatal("expected an error for an unknown kid, got nil")
	}
}

func TestJWKSClient_KeyForID_CachesWithinTTL(t *testing.T) {
	priv := generateRSAKeyPair(t)
	fetchCount := 0

	server := serveRSAJWKSCounting(t, "test-kid", &priv.PublicKey, &fetchCount)
	defer server.Close()

	client := NewJWKSClient(server.URL, nil, time.Hour)

	if _, err := client.KeyForID(context.Background(), "test-kid"); err != nil {
		t.Fatalf("first KeyForID: %v", err)
	}
	if _, err := client.KeyForID(context.Background(), "test-kid"); err != nil {
		t.Fatalf("second KeyForID: %v", err)
	}

	if fetchCount != 1 {
		t.Fatalf("expected exactly 1 fetch within the cache TTL, got %d", fetchCount)
	}
}

func TestNewJWKSClient_DefaultClientHasATimeout(t *testing.T) {
	client := NewJWKSClient("https://example.invalid/keys", nil, time.Hour)
	if client.httpClient == nil {
		t.Fatal("no http client")
	}
	if client.httpClient.Timeout <= 0 {
		t.Fatal("the default JWKS client must have a timeout; http.DefaultClient has none")
	}
	if client.httpClient == http.DefaultClient {
		t.Fatal("the default JWKS client must not be http.DefaultClient")
	}
}
