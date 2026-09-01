package infrastructure_test

import (
	"context"
	"crypto/x509"
	"errors"
	"strings"
	"testing"
	"time"

	"muse-backend/internal/entitlement/domain"
	"muse-backend/internal/entitlement/infrastructure"
	"muse-backend/internal/entitlement/infrastructure/appstoretest"
)

func testTransaction() appstoretest.Transaction {
	return appstoretest.Transaction{
		TransactionID:         "2000000001",
		OriginalTransactionID: "2000000001",
		BundleID:              "com.muse.app",
		ProductID:             "dev.muse.placeholder.collection_capacity",
		Type:                  "Non-Consumable",
		AppAccountToken:       "6F9619FF-8B86-D011-B42D-00C04FC964FF",
		Environment:           "Sandbox",
		InAppOwnershipType:    "PURCHASED",
		PurchaseDate:          time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC).UnixMilli(),
		SignedDate:            time.Date(2026, 8, 27, 12, 0, 5, 0, time.UTC).UnixMilli(),
	}
}

func appleShaped(t *testing.T) (*appstoretest.Signer, *infrastructure.AppStoreJWSVerifier) {
	t.Helper()
	signer, err := appstoretest.NewSigner(appstoretest.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return signer, infrastructure.NewAppStoreJWSVerifierWithRoots(signer.RootPool(), nil, nil)
}

func TestVerifier_AcceptsAnAppleShapedChain_AndDecodesThePayload(t *testing.T) {
	signer, verifier := appleShaped(t)
	tx := testTransaction()
	revoked := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC).UnixMilli()
	reason := int64(1)
	tx.RevocationDate = &revoked
	tx.RevocationReason = &reason
	signed, err := signer.Sign(tx)
	if err != nil {
		t.Fatal(err)
	}

	verified, err := verifier.VerifyTransaction(context.Background(), signed)
	if err != nil {
		t.Fatal(err)
	}
	if verified.OriginalTransactionID != "2000000001" || verified.BundleID != "com.muse.app" ||
		verified.ProductID != tx.ProductID || verified.Type != "Non-Consumable" ||
		verified.InAppOwnershipType != "PURCHASED" || verified.Environment != "Sandbox" {
		t.Fatalf("decoded: %+v", verified)
	}
	if verified.AppAccountToken != strings.ToLower(tx.AppAccountToken) {
		t.Fatalf("token must be normalised to lower case: %q", verified.AppAccountToken)
	}
	if !verified.PurchasedAt.Equal(time.UnixMilli(tx.PurchaseDate).UTC()) {
		t.Fatalf("purchase date: %v", verified.PurchasedAt)
	}
	if verified.RevokedAt == nil || !verified.RevokedAt.Equal(time.UnixMilli(revoked).UTC()) || verified.RevocationReason != "1" {
		t.Fatalf("revocation: %v %q", verified.RevokedAt, verified.RevocationReason)
	}
}

func TestVerifier_RefusesASignatureTheChainDoesNotVouchFor(t *testing.T) {
	_, verifier := appleShaped(t)
	other, _ := appstoretest.NewSigner(appstoretest.Options{})
	signed, _ := other.Sign(testTransaction())

	if _, err := verifier.VerifyTransaction(context.Background(), signed); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("an untrusted chain must fail closed, got %v", err)
	}
}

func TestVerifier_RefusesATrustedChainOverAForeignSignature(t *testing.T) {
	trusted, verifier := appleShaped(t)
	other, _ := appstoretest.NewSigner(appstoretest.Options{})
	signed, _ := other.SignWithChain(testTransaction(), trusted.X5C)

	if _, err := verifier.VerifyTransaction(context.Background(), signed); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("a signature that does not match the leaf must fail closed, got %v", err)
	}
}

func TestVerifier_RefusesATamperedPayload(t *testing.T) {
	signer, verifier := appleShaped(t)
	signed, _ := signer.Sign(testTransaction())
	parts := strings.Split(signed, ".")
	forged, _ := signer.Sign(func() appstoretest.Transaction {
		tx := testTransaction()
		tx.ProductID = "premium.everything"
		return tx
	}())
	tampered := parts[0] + "." + strings.Split(forged, ".")[1] + "." + parts[2]

	if _, err := verifier.VerifyTransaction(context.Background(), tampered); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("a tampered payload must fail closed, got %v", err)
	}
}

func TestVerifier_RefusesTheWrongAlgorithm_AndMalformedInput(t *testing.T) {
	signer, verifier := appleShaped(t)
	signed, _ := signer.Sign(testTransaction())
	parts := strings.Split(signed, ".")
	noneHeader := `{"alg":"none","x5c":[]}`
	none := b64(noneHeader) + "." + parts[1] + "."
	for name, input := range map[string]string{
		"alg none":     none,
		"two parts":    parts[0] + "." + parts[1],
		"empty":        "",
		"not base64":   "!!!.???.***",
		"json boolean": b64(`{"alg":"ES256","x5c":[]}`) + "." + b64(`true`) + "." + parts[2],
	} {
		if _, err := verifier.VerifyTransaction(context.Background(), input); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestVerifier_RequiresAppleMarkerOIDs_OnAnAppleRootedChain(t *testing.T) {
	plain, _ := appstoretest.NewSigner(appstoretest.Options{OmitAppleOIDs: true})
	verifier := infrastructure.NewAppStoreJWSVerifierWithRoots(plain.RootPool(), nil, nil)
	signed, _ := plain.Sign(testTransaction())

	if _, err := verifier.VerifyTransaction(context.Background(), signed); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("a chain without Apple's marker OIDs must not pass as Apple's, got %v", err)
	}
}

func TestVerifier_ExtraRoot_VerifiesChainAndSignature_WithoutAppleOIDs(t *testing.T) {
	dev, _ := appstoretest.NewSigner(appstoretest.Options{OmitAppleOIDs: true})
	unrelatedApple, _ := appstoretest.NewSigner(appstoretest.Options{})
	verifier := infrastructure.NewAppStoreJWSVerifierWithRoots(unrelatedApple.RootPool(), dev.RootPool(), nil)

	signed, _ := dev.Sign(testTransaction())
	if _, err := verifier.VerifyTransaction(context.Background(), signed); err != nil {
		t.Fatalf("a chain to a configured extra root must verify: %v", err)
	}
	stranger, _ := appstoretest.NewSigner(appstoretest.Options{OmitAppleOIDs: true})
	signed, _ = stranger.Sign(testTransaction())
	if _, err := verifier.VerifyTransaction(context.Background(), signed); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("an unknown root must still fail closed, got %v", err)
	}
}

func TestVerifier_RefusesAnExpiredChain(t *testing.T) {
	expired, _ := appstoretest.NewSigner(appstoretest.Options{NotAfter: time.Now().Add(-time.Minute)})
	verifier := infrastructure.NewAppStoreJWSVerifierWithRoots(expired.RootPool(), nil, nil)
	signed, _ := expired.Sign(testTransaction())
	if _, err := verifier.VerifyTransaction(context.Background(), signed); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("an expired chain must fail closed, got %v", err)
	}
}

func TestVerifier_EmbeddedAppleRootParses(t *testing.T) {
	verifier, err := infrastructure.NewDevelopmentVerifier("", false, nil)
	if err != nil || verifier == nil {
		t.Fatalf("embedded root: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(infrastructure.AppleRootCAG3PEM)) {
		t.Fatal("Apple Root CA - G3 PEM did not parse")
	}
	signer, _ := appstoretest.NewSigner(appstoretest.Options{})
	signed, _ := signer.Sign(testTransaction())
	if _, err := verifier.VerifyTransaction(context.Background(), signed); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("the production verifier must refuse a chain that is not Apple's, got %v", err)
	}
	if _, err := infrastructure.NewDevelopmentVerifier("not a pem", false, nil); err == nil {
		t.Fatal("unparseable extra roots must be refused at construction")
	}
}

func TestVerifier_Notification_VerifiesOuterAndInner(t *testing.T) {
	signer, verifier := appleShaped(t)
	inner, _ := signer.Sign(testTransaction())
	var n appstoretest.Notification
	n.NotificationType = "REFUND"
	n.NotificationUUID = "uuid-1"
	n.SignedDate = time.Now().UnixMilli()
	n.Version = "2.0"
	n.Data.BundleID = "com.muse.app"
	n.Data.Environment = "Sandbox"
	n.Data.SignedTransactionInfo = inner
	signed, _ := signer.Sign(n)

	got, err := verifier.VerifyNotification(context.Background(), signed)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "REFUND" || got.Transaction == nil || got.Transaction.OriginalTransactionID != "2000000001" {
		t.Fatalf("notification: %+v", got)
	}

	other, _ := appstoretest.NewSigner(appstoretest.Options{})
	forgedInner, _ := other.Sign(testTransaction())
	n.Data.SignedTransactionInfo = forgedInner
	signed, _ = signer.Sign(n)
	if _, err := verifier.VerifyNotification(context.Background(), signed); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("the inner transaction must be verified too, got %v", err)
	}
}

func b64(s string) string {
	return strings.TrimRight(base64url(s), "=")
}

func base64url(s string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var out strings.Builder
	data := []byte(s)
	for i := 0; i < len(data); i += 3 {
		var chunk [3]byte
		n := copy(chunk[:], data[i:])
		v := uint(chunk[0])<<16 | uint(chunk[1])<<8 | uint(chunk[2])
		out.WriteByte(alphabet[v>>18&63])
		out.WriteByte(alphabet[v>>12&63])
		if n > 1 {
			out.WriteByte(alphabet[v>>6&63])
		}
		if n > 2 {
			out.WriteByte(alphabet[v&63])
		}
	}
	return out.String()
}
