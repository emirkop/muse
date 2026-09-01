package infrastructure

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/ocsp"

	"muse-backend/internal/entitlement/domain"
)

type AppStoreJWSVerifier struct {
	apple                *x509.CertPool
	extra                *x509.CertPool
	now                  func() time.Time
	online               bool
	ocspClient           *http.Client
	ocspEndpointOverride string
}

var (
	oidAppleAppStoreLeaf     = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 11, 1}
	oidAppleWWDRIntermediate = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 2, 1}
)

const ocspMaxSkew = 60 * time.Second

const ocspTimeout = 5 * time.Second

func appleRootPool() (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	for _, root := range AppleTrustedRoots {
		block, _ := decodePEMCertificate(root.PEM)
		if block == nil {
			return nil, fmt.Errorf("appstore verifier: embedded root %q did not parse", root.Name)
		}
		cert, err := x509.ParseCertificate(block)
		if err != nil {
			return nil, fmt.Errorf("appstore verifier: embedded root %q: %w", root.Name, err)
		}
		sum := sha256.Sum256(cert.Raw)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), root.SHA256Hex) {
			return nil, fmt.Errorf("appstore verifier: embedded root %q does not match its recorded SHA-256 — refusing to trust it", root.Name)
		}
		if !cert.IsCA || cert.Subject.String() != root.Subject {
			return nil, fmt.Errorf("appstore verifier: embedded root %q is not the recorded CA certificate", root.Name)
		}
		pool.AddCert(cert)
	}
	return pool, nil
}

func NewProductionVerifier(now func() time.Time) (*AppStoreJWSVerifier, error) {
	apple, err := appleRootPool()
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	return &AppStoreJWSVerifier{
		apple:      apple,
		now:        now,
		online:     true,
		ocspClient: &http.Client{Timeout: ocspTimeout},
	}, nil
}

func NewDevelopmentVerifier(extraRootsPEM string, onlineChecks bool, now func() time.Time) (*AppStoreJWSVerifier, error) {
	apple, err := appleRootPool()
	if err != nil {
		return nil, err
	}
	var extra *x509.CertPool
	if strings.TrimSpace(extraRootsPEM) != "" {
		extra = x509.NewCertPool()
		if !extra.AppendCertsFromPEM([]byte(extraRootsPEM)) {
			return nil, errors.New("appstore verifier: extra trust roots did not parse")
		}
	}
	if now == nil {
		now = time.Now
	}
	return &AppStoreJWSVerifier{
		apple:      apple,
		extra:      extra,
		now:        now,
		online:     onlineChecks,
		ocspClient: &http.Client{Timeout: ocspTimeout},
	}, nil
}

func NewAppStoreJWSVerifierWithRoots(applePosition *x509.CertPool, extra *x509.CertPool, now func() time.Time) *AppStoreJWSVerifier {
	if now == nil {
		now = time.Now
	}
	return &AppStoreJWSVerifier{apple: applePosition, extra: extra, now: now, ocspClient: &http.Client{Timeout: ocspTimeout}}
}

func (v *AppStoreJWSVerifier) WithOnlineChecks(client *http.Client, endpoint string) *AppStoreJWSVerifier {
	v.online = true
	if client != nil {
		v.ocspClient = client
	}
	v.ocspEndpointOverride = endpoint
	return v
}

func (v *AppStoreJWSVerifier) OnlineChecksEnabled() bool { return v.online }

type jwsHeader struct {
	Alg string   `json:"alg"`
	X5C []string `json:"x5c"`
}

type transactionPayload struct {
	TransactionID         string `json:"transactionId"`
	OriginalTransactionID string `json:"originalTransactionId"`
	BundleID              string `json:"bundleId"`
	ProductID             string `json:"productId"`
	Type                  string `json:"type"`
	AppAccountToken       string `json:"appAccountToken"`
	Environment           string `json:"environment"`
	InAppOwnershipType    string `json:"inAppOwnershipType"`
	PurchaseDate          int64  `json:"purchaseDate"`
	SignedDate            int64  `json:"signedDate"`
	RevocationDate        *int64 `json:"revocationDate"`
	RevocationReason      *int64 `json:"revocationReason"`
}

type notificationPayload struct {
	NotificationType string `json:"notificationType"`
	Subtype          string `json:"subtype"`
	NotificationUUID string `json:"notificationUUID"`
	SignedDate       int64  `json:"signedDate"`
	Data             *struct {
		AppAppleID            *int64 `json:"appAppleId"`
		BundleID              string `json:"bundleId"`
		Environment           string `json:"environment"`
		SignedTransactionInfo string `json:"signedTransactionInfo"`
	} `json:"data"`
}

func (v *AppStoreJWSVerifier) VerifyTransaction(ctx context.Context, signed string) (domain.VerifiedTransaction, error) {
	var payload transactionPayload
	if err := v.verifyInto(ctx, signed, &payload); err != nil {
		return domain.VerifiedTransaction{}, err
	}
	if payload.TransactionID == "" || payload.OriginalTransactionID == "" || payload.BundleID == "" || payload.ProductID == "" || payload.Environment == "" {
		return domain.VerifiedTransaction{}, domain.ErrInvalidSignedTransaction
	}
	verified := domain.VerifiedTransaction{
		TransactionID:         payload.TransactionID,
		OriginalTransactionID: payload.OriginalTransactionID,
		BundleID:              payload.BundleID,
		ProductID:             payload.ProductID,
		Type:                  payload.Type,
		AppAccountToken:       strings.ToLower(payload.AppAccountToken),
		Environment:           payload.Environment,
		InAppOwnershipType:    payload.InAppOwnershipType,
		PurchasedAt:           millis(payload.PurchaseDate),
		SignedAt:              millis(payload.SignedDate),
	}
	if payload.RevocationDate != nil {
		at := millis(*payload.RevocationDate)
		verified.RevokedAt = &at
		if payload.RevocationReason != nil {
			verified.RevocationReason = fmt.Sprintf("%d", *payload.RevocationReason)
		}
	}
	return verified, nil
}

func (v *AppStoreJWSVerifier) VerifyNotification(ctx context.Context, signedPayload string) (domain.Notification, error) {
	var payload notificationPayload
	if err := v.verifyInto(ctx, signedPayload, &payload); err != nil {
		return domain.Notification{}, err
	}
	if payload.NotificationType == "" {
		return domain.Notification{}, domain.ErrInvalidSignedTransaction
	}
	notification := domain.Notification{
		Type:     payload.NotificationType,
		Subtype:  payload.Subtype,
		UUID:     payload.NotificationUUID,
		SignedAt: millis(payload.SignedDate),
	}
	if payload.Data != nil {
		notification.BundleID = payload.Data.BundleID
		notification.Environment = payload.Data.Environment
		if payload.Data.AppAppleID != nil {
			notification.AppAppleID = fmt.Sprintf("%d", *payload.Data.AppAppleID)
		}
		if payload.Data.SignedTransactionInfo != "" {
			inner, err := v.VerifyTransaction(ctx, payload.Data.SignedTransactionInfo)
			if err != nil {
				return domain.Notification{}, err
			}
			notification.Transaction = &inner
		}
	}
	return notification, nil
}

func (v *AppStoreJWSVerifier) verifyInto(ctx context.Context, signed string, into any) error {
	parts := strings.Split(strings.TrimSpace(signed), ".")
	if len(parts) != 3 {
		return domain.ErrInvalidSignedTransaction
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return domain.ErrInvalidSignedTransaction
	}
	var header jwsHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return domain.ErrInvalidSignedTransaction
	}
	if header.Alg != "ES256" {
		return domain.ErrInvalidSignedTransaction
	}

	leaf, err := v.trustedLeaf(ctx, header.X5C)
	if err != nil {
		return err
	}
	publicKey, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || publicKey.Curve != elliptic.P256() {
		return domain.ErrInvalidSignedTransaction
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return domain.ErrInvalidSignedTransaction
	}
	if err := jwt.SigningMethodES256.Verify(parts[0]+"."+parts[1], signature, publicKey); err != nil {
		return domain.ErrInvalidSignedTransaction
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return domain.ErrInvalidSignedTransaction
	}
	if err := json.Unmarshal(payloadJSON, into); err != nil {
		return domain.ErrInvalidSignedTransaction
	}
	return nil
}

func (v *AppStoreJWSVerifier) trustedLeaf(ctx context.Context, x5c []string) (*x509.Certificate, error) {
	if len(x5c) == 0 {
		return nil, domain.ErrInvalidSignedTransaction
	}
	certs := make([]*x509.Certificate, 0, len(x5c))
	for _, encoded := range x5c {
		der, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, domain.ErrInvalidSignedTransaction
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, domain.ErrInvalidSignedTransaction
		}
		certs = append(certs, cert)
	}
	leaf := certs[0]
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}
	opts := x509.VerifyOptions{
		Intermediates: intermediates,
		CurrentTime:   v.now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}

	if len(certs) == 3 && v.apple != nil {
		opts.Roots = v.apple
		if chains, err := leaf.Verify(opts); err == nil {
			if !hasExtension(leaf, oidAppleAppStoreLeaf) || !hasExtension(certs[1], oidAppleWWDRIntermediate) {
				return nil, domain.ErrInvalidSignedTransaction
			}
			if v.online {
				root := chains[0][len(chains[0])-1]
				if err := v.checkRevocation(ctx, certs[1], root); err != nil {
					return nil, err
				}
				if err := v.checkRevocation(ctx, leaf, certs[1]); err != nil {
					return nil, err
				}
			}
			return leaf, nil
		}
	}
	if v.extra != nil {
		opts.Roots = v.extra
		if _, err := leaf.Verify(opts); err == nil {
			return leaf, nil
		}
	}
	return nil, domain.ErrInvalidSignedTransaction
}

func (v *AppStoreJWSVerifier) checkRevocation(ctx context.Context, cert, issuer *x509.Certificate) error {
	endpoint := v.ocspEndpointOverride
	if endpoint == "" {
		if len(cert.OCSPServer) == 0 {
			return domain.ErrInvalidSignedTransaction
		}
		endpoint = cert.OCSPServer[0]
	}
	request, err := ocsp.CreateRequest(cert, issuer, &ocsp.RequestOptions{Hash: crypto.SHA256})
	if err != nil {
		return domain.ErrInvalidSignedTransaction
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(request))
	if err != nil {
		return domain.ErrVerificationUnavailable
	}
	httpReq.Header.Set("Content-Type", "application/ocsp-request")
	httpReq.Header.Set("Accept", "application/ocsp-response")
	resp, err := v.ocspClient.Do(httpReq)
	if err != nil {
		return domain.ErrVerificationUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return domain.ErrVerificationUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return domain.ErrVerificationUnavailable
	}
	parsed, err := ocsp.ParseResponseForCert(body, cert, issuer)
	if err != nil {
		return domain.ErrInvalidSignedTransaction
	}
	if parsed.Status != ocsp.Good {
		return domain.ErrInvalidSignedTransaction
	}
	now := v.now()
	if parsed.ThisUpdate.After(now.Add(ocspMaxSkew)) {
		return domain.ErrInvalidSignedTransaction
	}
	if !parsed.NextUpdate.IsZero() && parsed.NextUpdate.Before(now.Add(-ocspMaxSkew)) {
		return domain.ErrInvalidSignedTransaction
	}
	return nil
}

func hasExtension(cert *x509.Certificate, oid asn1.ObjectIdentifier) bool {
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(oid) {
			return true
		}
	}
	return false
}

func millis(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
