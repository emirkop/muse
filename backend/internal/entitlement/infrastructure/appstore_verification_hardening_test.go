package infrastructure_test

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ocsp"

	"muse-backend/internal/entitlement/domain"
	"muse-backend/internal/entitlement/infrastructure"
	"muse-backend/internal/entitlement/infrastructure/appstoretest"
)

func TestAppleRoot_EmbeddedCertificateMatchesItsRecordedIdentity(t *testing.T) {
	if len(infrastructure.AppleTrustedRoots) != 1 {
		t.Fatalf("exactly one Apple root is trusted today, got %d", len(infrastructure.AppleTrustedRoots))
	}
	root := infrastructure.AppleTrustedRoots[0]
	block, _ := pem.Decode([]byte(root.PEM))
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatal("embedded PEM did not decode as a CERTIFICATE")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(cert.Raw)
	if got := hex.EncodeToString(sum[:]); got != root.SHA256Hex {
		t.Fatalf("SHA-256 drift: embedded %s, recorded %s", got, root.SHA256Hex)
	}
	if root.SHA256Hex != "63343abfb89a6a03ebb57e9b3f5fa7be7c4f5c756f3017b3a8c488c3653e9179" {
		t.Fatalf("recorded fingerprint is not Apple Root CA - G3's: %s", root.SHA256Hex)
	}
	if cert.Subject.String() != root.Subject || cert.Subject.CommonName != "Apple Root CA - G3" {
		t.Fatalf("subject: %s", cert.Subject)
	}
	if !strings.EqualFold(cert.SerialNumber.Text(16), root.SerialHex) {
		t.Fatalf("serial: %s", cert.SerialNumber.Text(16))
	}
	if !cert.IsCA || cert.Issuer.String() != cert.Subject.String() {
		t.Fatal("must be a self-signed CA")
	}
	if cert.NotBefore.Year() != 2014 || cert.NotAfter.Year() != 2039 {
		t.Fatalf("validity: %s → %s", cert.NotBefore, cert.NotAfter)
	}
	if cert.PublicKeyAlgorithm != x509.ECDSA {
		t.Fatalf("key algorithm: %s", cert.PublicKeyAlgorithm)
	}
	if _, err := infrastructure.NewProductionVerifier(nil); err != nil {
		t.Fatalf("the production verifier constructs from the pinned root: %v", err)
	}
}

func TestProductionVerifier_TrustsOnlyApplesRoots_AndAlwaysChecksRevocation(t *testing.T) {
	verifier, err := infrastructure.NewProductionVerifier(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !verifier.OnlineChecksEnabled() {
		t.Fatal("production must perform online revocation checks — not configurable")
	}
	appleShapedStranger, _ := appstoretest.NewSigner(appstoretest.Options{})
	devShaped, _ := appstoretest.NewSigner(appstoretest.Options{OmitAppleOIDs: true})
	for name, signer := range map[string]*appstoretest.Signer{
		"Apple-shaped chain from a non-Apple root": appleShapedStranger,
		"StoreKit-Test-shaped (dev) chain":         devShaped,
	} {
		signed, _ := signer.Sign(testTransaction())
		if _, err := verifier.VerifyTransaction(context.Background(), signed); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
			t.Errorf("%s: production must refuse, got %v", name, err)
		}
		var n appstoretest.Notification
		n.NotificationType = "REFUND"
		n.Data.BundleID = "com.muse.app"
		n.Data.Environment = "Production"
		n.Data.SignedTransactionInfo = signed
		signedN, _ := signer.Sign(n)
		if _, err := verifier.VerifyNotification(context.Background(), signedN); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
			t.Errorf("%s (notification): production must refuse, got %v", name, err)
		}
	}
}

func TestDevelopmentVerifier_IsTheOnlyPathToAnExtraRoot(t *testing.T) {
	var _ func(func() time.Time) (*infrastructure.AppStoreJWSVerifier, error) = infrastructure.NewProductionVerifier
	var _ func(string, bool, func() time.Time) (*infrastructure.AppStoreJWSVerifier, error) = infrastructure.NewDevelopmentVerifier

	dev, _ := appstoretest.NewSigner(appstoretest.Options{OmitAppleOIDs: true})
	verifier, err := infrastructure.NewDevelopmentVerifier(dev.RootPEM(), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if verifier.OnlineChecksEnabled() {
		t.Fatal("development online checks are opt-in")
	}
	signed, _ := dev.Sign(testTransaction())
	if _, err := verifier.VerifyTransaction(context.Background(), signed); err != nil {
		t.Fatalf("the configured dev root verifies in development: %v", err)
	}
	parts := strings.Split(signed, ".")
	tampered := parts[0] + "." + parts[1][:len(parts[1])-2] + "AA." + parts[2]
	if _, err := verifier.VerifyTransaction(context.Background(), tampered); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("a tampered payload must fail under a dev root too, got %v", err)
	}
}

type ocspFixture struct {
	responder *appstoretest.OCSPResponder
	signer    *appstoretest.Signer
	verifier  *infrastructure.AppStoreJWSVerifier
}

func onlineFixture(t *testing.T) *ocspFixture {
	t.Helper()
	responder := appstoretest.NewOCSPResponder()
	t.Cleanup(responder.Close)
	signer, err := appstoretest.NewSigner(appstoretest.Options{OCSPServerURL: responder.URL()})
	if err != nil {
		t.Fatal(err)
	}
	responder.Serve(signer)
	verifier := infrastructure.NewAppStoreJWSVerifierWithRoots(signer.RootPool(), nil, nil).
		WithOnlineChecks(&http.Client{Timeout: 2 * time.Second}, "")
	return &ocspFixture{responder: responder, signer: signer, verifier: verifier}
}

func (f *ocspFixture) verify(t *testing.T) error {
	t.Helper()
	signed, err := f.signer.Sign(testTransaction())
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.verifier.VerifyTransaction(context.Background(), signed)
	return err
}

func TestOCSP_GoodStatusForLeafAndIntermediate_IsAccepted_AndBothAreChecked(t *testing.T) {
	f := onlineFixture(t)
	if err := f.verify(t); err != nil {
		t.Fatalf("GOOD/GOOD must verify: %v", err)
	}
	leaf, intermediate := f.responder.RequestCount()
	if leaf != 1 || intermediate != 1 {
		t.Fatalf("expected one OCSP request per certificate, got leaf=%d intermediate=%d", leaf, intermediate)
	}
}

func TestOCSP_RevokedLeaf_IsRefused(t *testing.T) {
	f := onlineFixture(t)
	f.responder.SetLeafStatus(ocsp.Revoked)
	if err := f.verify(t); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("a revoked leaf must be ErrInvalidSignedTransaction, got %v", err)
	}
}

func TestOCSP_RevokedIntermediate_IsRefused_BeforeTheLeafIsAsked(t *testing.T) {
	f := onlineFixture(t)
	f.responder.SetIntermediateStatus(ocsp.Revoked)
	if err := f.verify(t); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("got %v", err)
	}
	if leaf, _ := f.responder.RequestCount(); leaf != 0 {
		t.Fatalf("the leaf must not be asked after its issuer failed, got %d leaf requests", leaf)
	}
}

func TestOCSP_UnknownStatus_IsRefused(t *testing.T) {
	f := onlineFixture(t)
	f.responder.SetLeafStatus(ocsp.Unknown)
	if err := f.verify(t); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("got %v", err)
	}
}

func TestOCSP_ResponderFailureOrOutage_IsUnavailable_NeverAccepted(t *testing.T) {
	f := onlineFixture(t)
	f.responder.FailHTTP(http.StatusInternalServerError)
	if err := f.verify(t); !errors.Is(err, domain.ErrVerificationUnavailable) {
		t.Fatalf("HTTP 500 from the responder: want ErrVerificationUnavailable, got %v", err)
	}
	f.responder.FailHTTP(0)
	if err := f.verify(t); err != nil {
		t.Fatalf("recovered responder: %v", err)
	}
	f.responder.Close()
	if err := f.verify(t); !errors.Is(err, domain.ErrVerificationUnavailable) {
		t.Fatalf("responder down: want ErrVerificationUnavailable, got %v", err)
	}
}

func TestOCSP_MalformedOrStaleResponse_IsRefused(t *testing.T) {
	f := onlineFixture(t)
	f.responder.AnswerGarbage(true)
	if err := f.verify(t); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("garbage response: got %v", err)
	}
	f.responder.AnswerGarbage(false)
	f.responder.Stale(2 * time.Hour)
	if err := f.verify(t); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("stale response: got %v", err)
	}
}

func TestOCSP_ResponseSignedByAnotherIssuer_IsRefused(t *testing.T) {
	responder := appstoretest.NewOCSPResponder()
	t.Cleanup(responder.Close)
	chainA, _ := appstoretest.NewSigner(appstoretest.Options{OCSPServerURL: responder.URL()})
	chainB, _ := appstoretest.NewSigner(appstoretest.Options{OCSPServerURL: responder.URL()})
	responder.Serve(chainB)
	verifier := infrastructure.NewAppStoreJWSVerifierWithRoots(chainA.RootPool(), nil, nil).
		WithOnlineChecks(&http.Client{Timeout: 2 * time.Second}, "")
	signed, _ := chainA.Sign(testTransaction())
	if _, err := verifier.VerifyTransaction(context.Background(), signed); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("a response signed by another issuer must be refused, got %v", err)
	}
}

func TestOCSP_ChainWithoutAResponder_IsRefused_WhenOnlineChecksAreOn(t *testing.T) {
	signer, _ := appstoretest.NewSigner(appstoretest.Options{})
	verifier := infrastructure.NewAppStoreJWSVerifierWithRoots(signer.RootPool(), nil, nil).
		WithOnlineChecks(&http.Client{Timeout: 2 * time.Second}, "")
	signed, _ := signer.Sign(testTransaction())
	if _, err := verifier.VerifyTransaction(context.Background(), signed); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("no responder to ask ⇒ refused, got %v", err)
	}
	offline := infrastructure.NewAppStoreJWSVerifierWithRoots(signer.RootPool(), nil, nil)
	if _, err := offline.VerifyTransaction(context.Background(), signed); err != nil {
		t.Fatalf("offline development verification: %v", err)
	}
}

func TestOCSP_Notification_ChecksOuterAndInnerChains(t *testing.T) {
	f := onlineFixture(t)
	inner, _ := f.signer.Sign(testTransaction())
	var n appstoretest.Notification
	n.NotificationType = "REFUND"
	n.NotificationUUID = "u-1"
	appAppleID := int64(6740000001)
	n.Data.AppAppleID = &appAppleID
	n.Data.BundleID = "com.muse.app"
	n.Data.Environment = "Sandbox"
	n.Data.SignedTransactionInfo = inner
	signed, _ := f.signer.Sign(n)

	verified, err := f.verifier.VerifyNotification(context.Background(), signed)
	if err != nil {
		t.Fatal(err)
	}
	if verified.AppAppleID != "6740000001" || verified.BundleID != "com.muse.app" || verified.Environment != "Sandbox" {
		t.Fatalf("envelope identity must be decoded for the policy: %+v", verified)
	}
	leaf, intermediate := f.responder.RequestCount()
	if leaf != 2 || intermediate != 2 {
		t.Fatalf("outer and inner chains each checked: leaf=%d intermediate=%d", leaf, intermediate)
	}
	f.responder.SetLeafStatus(ocsp.Revoked)
	if _, err := f.verifier.VerifyNotification(context.Background(), signed); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("a revoked signer invalidates the notification, got %v", err)
	}
}
