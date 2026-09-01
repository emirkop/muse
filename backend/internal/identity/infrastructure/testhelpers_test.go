package infrastructure

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func generateRSAKeyPair(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return key
}

func generateECKeyPair(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate EC key: %v", err)
	}
	return key
}

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func serveRSAJWKS(t *testing.T, kid string, pub *rsa.PublicKey) *httptest.Server {
	t.Helper()

	eBytes := big.NewInt(int64(pub.E)).Bytes()
	body := jwkSet{Keys: []jwk{{
		Kty: "RSA",
		Kid: kid,
		Alg: "RS256",
		Use: "sig",
		N:   b64url(pub.N.Bytes()),
		E:   b64url(eBytes),
	}}}

	return httptest.NewServer(jsonHandler(t, body))
}

func serveECJWKS(t *testing.T, kid string, pub *ecdsa.PublicKey) *httptest.Server {
	t.Helper()

	body := jwkSet{Keys: []jwk{{
		Kty: "EC",
		Kid: kid,
		Alg: "ES256",
		Use: "sig",
		Crv: "P-256",
		X:   b64url(pub.X.Bytes()),
		Y:   b64url(pub.Y.Bytes()),
	}}}

	return httptest.NewServer(jsonHandler(t, body))
}

func serveRSAJWKSCounting(t *testing.T, kid string, pub *rsa.PublicKey, fetchCount *int) *httptest.Server {
	t.Helper()

	eBytes := big.NewInt(int64(pub.E)).Bytes()
	body := jwkSet{Keys: []jwk{{
		Kty: "RSA",
		Kid: kid,
		Alg: "RS256",
		Use: "sig",
		N:   b64url(pub.N.Bytes()),
		E:   b64url(eBytes),
	}}}

	handler := jsonHandler(t, body)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*fetchCount++
		handler(w, r)
	}))
}

func jsonHandler(t *testing.T, body interface{}) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(body); err != nil {
			panic(fmt.Sprintf("encode test JWKS response: %v", err))
		}
	}
}

func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign RS256 test token: %v", err)
	}
	return signed
}

func signES256(t *testing.T, key *ecdsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign ES256 test token: %v", err)
	}
	return signed
}
