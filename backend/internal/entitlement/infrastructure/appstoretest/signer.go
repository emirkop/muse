package appstoretest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	OIDAppStoreLeaf     = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 11, 1}
	OIDWWDRIntermediate = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 6, 2, 1}
)

type Signer struct {
	rootCert, intermediateCert, leafCert *x509.Certificate
	rootKey, intermediateKey, leafKey    *ecdsa.PrivateKey
	X5C                                  []string
}

type Options struct {
	OmitAppleOIDs bool
	NotAfter      time.Time
	OCSPServerURL string
}

func NewSigner(opts Options) (*Signer, error) {
	notAfter := opts.NotAfter
	if notAfter.IsZero() {
		notAfter = time.Now().Add(24 * time.Hour)
	}
	notBefore := time.Now().Add(-time.Hour)

	rootKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, err
	}
	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Root CA (appstoretest)"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		return nil, err
	}
	rootCert, _ := x509.ParseCertificate(rootDER)

	intermediateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, err
	}
	intermediateTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "Test WWDR Intermediate (appstoretest)"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	if !opts.OmitAppleOIDs {
		intermediateTemplate.ExtraExtensions = []pkix.Extension{{Id: OIDWWDRIntermediate, Value: []byte{0x05, 0x00}}}
	}
	if opts.OCSPServerURL != "" {
		intermediateTemplate.OCSPServer = []string{opts.OCSPServerURL}
	}
	intermediateDER, err := x509.CreateCertificate(rand.Reader, intermediateTemplate, rootCert, &intermediateKey.PublicKey, rootKey)
	if err != nil {
		return nil, err
	}
	intermediateCert, _ := x509.ParseCertificate(intermediateDER)

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "Test App Store Signer (appstoretest)"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if !opts.OmitAppleOIDs {
		leafTemplate.ExtraExtensions = []pkix.Extension{{Id: OIDAppStoreLeaf, Value: []byte{0x05, 0x00}}}
	}
	if opts.OCSPServerURL != "" {
		leafTemplate.OCSPServer = []string{opts.OCSPServerURL}
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, intermediateCert, &leafKey.PublicKey, intermediateKey)
	if err != nil {
		return nil, err
	}
	leafCert, _ := x509.ParseCertificate(leafDER)

	return &Signer{
		rootCert: rootCert, intermediateCert: intermediateCert, leafCert: leafCert,
		rootKey: rootKey, intermediateKey: intermediateKey, leafKey: leafKey,
		X5C: []string{
			base64.StdEncoding.EncodeToString(leafDER),
			base64.StdEncoding.EncodeToString(intermediateDER),
			base64.StdEncoding.EncodeToString(rootDER),
		},
	}, nil
}

func (s *Signer) RootPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(s.rootCert)
	return pool
}

func (s *Signer) RootPEM() string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.rootCert.Raw}))
}

func (s *Signer) Sign(payload any) (string, error) {
	return s.SignWithChain(payload, s.X5C)
}

func (s *Signer) SignWithChain(payload any, x5c []string) (string, error) {
	header := map[string]any{"alg": "ES256", "x5c": x5c}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
	signature, err := jwt.SigningMethodES256.Sign(signingInput, s.leafKey)
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

type Transaction struct {
	TransactionID         string `json:"transactionId"`
	OriginalTransactionID string `json:"originalTransactionId"`
	BundleID              string `json:"bundleId"`
	ProductID             string `json:"productId"`
	Type                  string `json:"type"`
	AppAccountToken       string `json:"appAccountToken,omitempty"`
	Environment           string `json:"environment"`
	InAppOwnershipType    string `json:"inAppOwnershipType"`
	PurchaseDate          int64  `json:"purchaseDate"`
	SignedDate            int64  `json:"signedDate"`
	RevocationDate        *int64 `json:"revocationDate,omitempty"`
	RevocationReason      *int64 `json:"revocationReason,omitempty"`
}

type Notification struct {
	NotificationType string `json:"notificationType"`
	Subtype          string `json:"subtype,omitempty"`
	NotificationUUID string `json:"notificationUUID"`
	SignedDate       int64  `json:"signedDate"`
	Version          string `json:"version"`
	Data             struct {
		AppAppleID            *int64 `json:"appAppleId,omitempty"`
		BundleID              string `json:"bundleId"`
		Environment           string `json:"environment"`
		SignedTransactionInfo string `json:"signedTransactionInfo"`
	} `json:"data"`
}
