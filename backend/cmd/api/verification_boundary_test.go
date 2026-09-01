package main

import (
	"net/http"
	"strings"
	"testing"

	"muse-backend/internal/entitlement/infrastructure/appstoretest"
)

func TestVerificationBoundary_TransactionForAnotherApp_Environment_OrProduct_IsRefused_AndStateUnchanged(t *testing.T) {
	s := newStackWithCapacities(t, smallCaps)
	token := s.appAccountToken(s.token)
	before := s.entitlementStatus(s.token)

	cases := map[string]func(tx *appstoretest.Transaction){
		"another app's bundle id":        func(tx *appstoretest.Transaction) { tx.BundleID = "com.example.othertestapp" },
		"Production environment payload": func(tx *appstoretest.Transaction) { tx.Environment = "Production" },
		"unknown product id":             func(tx *appstoretest.Transaction) { tx.ProductID = "com.muse.unrelated_future_product" },
	}
	i := 0
	for name, mutate := range cases {
		i++
		tx := purchasePayload("30000000"+string(rune('0'+i)), token)
		mutate(&tx)
		status, body := s.redeem(s.token, s.signed(tx))
		if status != http.StatusBadRequest || !strings.Contains(body, `"code":"transaction_not_applicable"`) {
			t.Fatalf("%s: expected 400 transaction_not_applicable, got %d %s", name, status, body)
		}
		if after := s.entitlementStatus(s.token); after != before {
			t.Fatalf("%s: entitlement changed: %+v → %+v", name, before, after)
		}
	}
}

func TestVerificationBoundary_NotificationForAnotherApp_Environment_OrProduct_IsRefused_AndDoesNotRevoke(t *testing.T) {
	s := newStackWithCapacities(t, smallCaps)
	token := s.appAccountToken(s.token)
	s.redeemOK(s.token, "4000000001")
	if status := s.entitlementStatus(s.token); status.State != "paid" {
		t.Fatalf("arrange: %+v", status)
	}

	cases := map[string]func(n *appstoretest.Notification, inner *appstoretest.Transaction){
		"envelope for another bundle": func(n *appstoretest.Notification, _ *appstoretest.Transaction) {
			n.Data.BundleID = "com.example.othertestapp"
		},
		"envelope from Production": func(n *appstoretest.Notification, _ *appstoretest.Transaction) { n.Data.Environment = "Production" },
		"inner transaction another bundle": func(_ *appstoretest.Notification, inner *appstoretest.Transaction) {
			inner.BundleID = "com.example.othertestapp"
		},
		"inner transaction other product": func(_ *appstoretest.Notification, inner *appstoretest.Transaction) {
			inner.ProductID = "com.muse.unrelated"
		},
	}
	for name, mutate := range cases {
		inner := purchasePayload("4000000001", token)
		var n appstoretest.Notification
		n.NotificationType = "REFUND"
		n.NotificationUUID = "uuid-boundary-" + name
		n.Version = "2.0"
		n.Data.BundleID = testAppleBundleID
		n.Data.Environment = "Sandbox"
		mutate(&n, &inner)
		n.Data.SignedTransactionInfo = s.signed(inner)
		r := s.doAnonymous(http.MethodPost, "/app-store/notifications", map[string]string{"signedPayload": s.signed(n)}, nil)
		if r.status != http.StatusBadRequest || !strings.Contains(r.body, `"code":"notification_not_applicable"`) {
			t.Fatalf("%s: expected 400 notification_not_applicable, got %d %s", name, r.status, r.body)
		}
		if status := s.entitlementStatus(s.token); status.State != "paid" {
			t.Fatalf("%s: a refused notification must not revoke: %+v", name, status)
		}
	}

	r := s.notify("REFUND", purchasePayload("4000000001", token))
	if r.status != http.StatusOK {
		t.Fatalf("genuine refund: %d %s", r.status, r.body)
	}
	if status := s.entitlementStatus(s.token); status.State != "revoked" {
		t.Fatalf("after genuine refund: %+v", status)
	}
}
