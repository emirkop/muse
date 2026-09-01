package appstoretest

import (
	"crypto"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"golang.org/x/crypto/ocsp"
)

type OCSPResponder struct {
	server *httptest.Server

	mu          sync.Mutex
	signer      *Signer
	statuses    map[string]int
	httpFailure int
	garbage     bool
	staleBy     time.Duration
	requests    map[string]int
}

func NewOCSPResponder() *OCSPResponder {
	r := &OCSPResponder{statuses: map[string]int{}, requests: map[string]int{}}
	r.server = httptest.NewServer(http.HandlerFunc(r.handle))
	return r
}

func (r *OCSPResponder) URL() string { return r.server.URL }

func (r *OCSPResponder) Serve(signer *Signer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.signer = signer
}

func (r *OCSPResponder) Close() { r.server.Close() }

func (r *OCSPResponder) SetLeafStatus(status int) { r.setStatus(r.signer.leafCert, status) }
func (r *OCSPResponder) SetIntermediateStatus(status int) {
	r.setStatus(r.signer.intermediateCert, status)
}

func (r *OCSPResponder) setStatus(cert *x509.Certificate, status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses[cert.SerialNumber.String()] = status
}

func (r *OCSPResponder) FailHTTP(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.httpFailure = status
}

func (r *OCSPResponder) AnswerGarbage(on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.garbage = on
}

func (r *OCSPResponder) Stale(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.staleBy = d
}

func (r *OCSPResponder) RequestCount() (leaf, intermediate int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests[r.signer.leafCert.SerialNumber.String()], r.requests[r.signer.intermediateCert.SerialNumber.String()]
}

func (r *OCSPResponder) handle(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	body, err := io.ReadAll(req.Body)
	if err != nil || r.signer == nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	parsed, err := ocsp.ParseRequest(body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	serial := parsed.SerialNumber.String()
	r.requests[serial]++
	if r.httpFailure != 0 {
		http.Error(w, "responder failure", r.httpFailure)
		return
	}
	if r.garbage {
		w.Header().Set("Content-Type", "application/ocsp-response")
		_, _ = w.Write([]byte("this is not an OCSP response"))
		return
	}
	var issuer *x509.Certificate
	var issuerKey crypto.Signer
	var subject *x509.Certificate
	switch serial {
	case r.signer.leafCert.SerialNumber.String():
		subject, issuer, issuerKey = r.signer.leafCert, r.signer.intermediateCert, r.signer.intermediateKey
	case r.signer.intermediateCert.SerialNumber.String():
		subject, issuer, issuerKey = r.signer.intermediateCert, r.signer.rootCert, r.signer.rootKey
	default:
		http.Error(w, "unknown certificate", http.StatusNotFound)
		return
	}
	status, ok := r.statuses[serial]
	if !ok {
		status = ocsp.Good
	}
	now := time.Now()
	template := ocsp.Response{
		Status:       status,
		SerialNumber: subject.SerialNumber,
		ThisUpdate:   now.Add(-time.Minute),
		NextUpdate:   now.Add(time.Hour).Add(-r.staleBy),
		IssuerHash:   crypto.SHA256,
	}
	if status == ocsp.Revoked {
		template.RevokedAt = now.Add(-30 * time.Second)
		template.RevocationReason = ocsp.KeyCompromise
	}
	der, err := ocsp.CreateResponse(issuer, issuer, template, issuerKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/ocsp-response")
	_, _ = w.Write(der)
}
