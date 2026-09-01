package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"muse-backend/internal/entitlement/application"
	"muse-backend/internal/entitlement/domain"
)

const (
	productionProduct = "com.muse.collection_capacity"
	appAppleID        = "6740000001"
)

type countingTransactions struct {
	*fakeTransactions
	binds, revocations int
}

func (c *countingTransactions) Bind(ctx context.Context, t domain.AppStoreTransaction) (domain.AppStoreTransaction, error) {
	c.binds++
	return c.fakeTransactions.Bind(ctx, t)
}

func (c *countingTransactions) SetRevocation(ctx context.Context, original string, revokedAt *time.Time, reason string) (bool, error) {
	c.revocations++
	return c.fakeTransactions.SetRevocation(ctx, original, revokedAt, reason)
}

func productionPolicy() application.AppStorePolicy {
	return application.AppStorePolicy{
		Production:  true,
		BundleID:    bundle,
		AppAppleID:  appAppleID,
		ProductIDs:  []string{productionProduct},
		Environment: "Production",
	}
}

type identityHarness struct {
	service      *application.EntitlementService
	transactions *countingTransactions
	verifier     *fakeVerifier
	counts       *fakeCounts
}

func newIdentityHarness(t *testing.T, policy application.AppStorePolicy) *identityHarness {
	t.Helper()
	h := &identityHarness{
		transactions: &countingTransactions{fakeTransactions: newFakeTransactions()},
		verifier:     &fakeVerifier{transactions: map[string]domain.VerifiedTransaction{}, notifications: map[string]domain.Notification{}},
		counts:       &fakeCounts{byAccount: map[string]int{}},
	}
	service, err := application.NewEntitlementService(
		h.transactions, &fakeTokens{byAccount: map[string]string{}}, h.counts, h.verifier,
		policy, caps, func() time.Time { return time.Date(2026, 8, 27, 13, 0, 0, 0, time.UTC) },
	)
	if err != nil {
		t.Fatal(err)
	}
	h.service = service
	return h
}

func productionPurchase(token string) domain.VerifiedTransaction {
	tx := purchase(token)
	tx.ProductID = productionProduct
	tx.Environment = "Production"
	return tx
}

func (h *identityHarness) mustBeFree(t *testing.T) {
	t.Helper()
	status, err := h.service.Status(context.Background(), accountA)
	if err != nil {
		t.Fatal(err)
	}
	if status.Entitlement.State != domain.StateFree || status.Entitlement.ItemCapacity != caps.Free {
		t.Fatalf("state must be untouched (free): %+v", status.Entitlement)
	}
	if h.transactions.binds != 0 || h.transactions.revocations != 0 {
		t.Fatalf("a refused payload must write nothing: binds=%d revocations=%d", h.transactions.binds, h.transactions.revocations)
	}
}

func TestIdentity_ProductionPolicy_RefusesPlaceholderProduct_MissingAppAppleID_AndNonProductionEnvironment(t *testing.T) {
	cases := map[string]func(p *application.AppStorePolicy){
		"DEV placeholder product id": func(p *application.AppStorePolicy) {
			p.ProductIDs = []string{"dev.muse.placeholder.collection_capacity"}
		},
		"placeholder among real ids": func(p *application.AppStorePolicy) {
			p.ProductIDs = []string{productionProduct, "dev.muse.placeholder.other"}
		},
		"missing App Apple ID":         func(p *application.AppStorePolicy) { p.AppAppleID = "" },
		"Sandbox environment":          func(p *application.AppStorePolicy) { p.Environment = "Sandbox" },
		"Xcode environment":            func(p *application.AppStorePolicy) { p.Environment = "Xcode" },
		"local-testing environments":   func(p *application.AppStorePolicy) { p.LocalTestingEnvironments = []string{"Xcode"} },
		"missing bundle id":            func(p *application.AppStorePolicy) { p.BundleID = "" },
		"no product at all":            func(p *application.AppStorePolicy) { p.ProductIDs = nil },
		"empty product id":             func(p *application.AppStorePolicy) { p.ProductIDs = []string{""} },
		"missing expected environment": func(p *application.AppStorePolicy) { p.Environment = "" },
	}
	for name, mutate := range cases {
		policy := productionPolicy()
		mutate(&policy)
		if err := policy.Validate(); !errors.Is(err, domain.ErrPolicyIncomplete) {
			t.Errorf("%s: expected ErrPolicyIncomplete, got %v", name, err)
		}
		if _, err := application.NewEntitlementService(nil, nil, nil, nil, policy, caps, nil); !errors.Is(err, domain.ErrPolicyIncomplete) {
			t.Errorf("%s: the service must refuse construction, got %v", name, err)
		}
	}
	if err := productionPolicy().Validate(); err != nil {
		t.Fatalf("a complete production policy is valid: %v", err)
	}
	dev := application.AppStorePolicy{BundleID: bundle, ProductIDs: []string{"dev.muse.placeholder.collection_capacity"}, Environment: "Sandbox", LocalTestingEnvironments: []string{"Xcode", "LocalTesting"}}
	if err := dev.Validate(); err != nil {
		t.Fatalf("development may use the placeholder: %v", err)
	}
	if err := (application.AppStorePolicy{BundleID: bundle, Environment: "Sandbox"}).Validate(); !errors.Is(err, domain.ErrPolicyIncomplete) {
		t.Fatalf("development still needs a product id, got %v", err)
	}
}

func TestIdentity_Production_ValidExpectedAppTransaction_Succeeds(t *testing.T) {
	h := newIdentityHarness(t, productionPolicy())
	ctx := context.Background()
	token, _ := h.service.AppAccountToken(ctx, accountA)
	h.verifier.transactions["jws"] = productionPurchase(token)

	status, err := h.service.RedeemSignedTransaction(ctx, accountA, "jws")
	if err != nil {
		t.Fatal(err)
	}
	if status.Entitlement.State != domain.StatePaid || status.Entitlement.ItemCapacity != caps.Paid {
		t.Fatalf("%+v", status.Entitlement)
	}
	if h.transactions.binds != 1 {
		t.Fatalf("exactly one bind, got %d", h.transactions.binds)
	}
}

func TestIdentity_Production_TransactionRefusals_GrantNothing(t *testing.T) {
	cases := map[string]struct {
		mutate func(tx *domain.VerifiedTransaction)
		want   error
	}{
		"wrong bundleId (another app, genuinely Apple-signed)": {
			func(tx *domain.VerifiedTransaction) { tx.BundleID = "com.other.vendor.app" }, domain.ErrWrongBundle},
		"Sandbox environment against a Production deployment": {
			func(tx *domain.VerifiedTransaction) { tx.Environment = "Sandbox" }, domain.ErrWrongEnvironment},
		"Xcode environment against a Production deployment": {
			func(tx *domain.VerifiedTransaction) { tx.Environment = "Xcode" }, domain.ErrWrongEnvironment},
		"unknown product id": {
			func(tx *domain.VerifiedTransaction) { tx.ProductID = "com.muse.some_future_product" }, domain.ErrWrongProduct},
		"DEV placeholder product id in production": {
			func(tx *domain.VerifiedTransaction) { tx.ProductID = "dev.muse.placeholder.collection_capacity" }, domain.ErrWrongProduct},
		"case-different bundle id is another app": {
			func(tx *domain.VerifiedTransaction) { tx.BundleID = "COM.MUSE.APP" }, domain.ErrWrongBundle},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := newIdentityHarness(t, productionPolicy())
			ctx := context.Background()
			token, _ := h.service.AppAccountToken(ctx, accountA)
			tx := productionPurchase(token)
			tc.mutate(&tx)
			h.verifier.transactions["jws"] = tx

			_, err := h.service.RedeemSignedTransaction(ctx, accountA, "jws")
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			h.mustBeFree(t)
		})
	}
}

func TestIdentity_AnotherAppsValidPayload_CannotGrantMuseEntitlement(t *testing.T) {
	h := newIdentityHarness(t, productionPolicy())
	ctx := context.Background()
	token, _ := h.service.AppAccountToken(ctx, accountA)
	other := productionPurchase(token)
	other.BundleID = "com.example.othertestapp"
	other.ProductID = "com.example.othertestapp.premium"
	h.verifier.transactions["other-app-jws"] = other

	if _, err := h.service.RedeemSignedTransaction(ctx, accountA, "other-app-jws"); !errors.Is(err, domain.ErrWrongBundle) {
		t.Fatalf("another app's purchase must be refused as another app's, got %v", err)
	}
	h.mustBeFree(t)

	collide := productionPurchase(token)
	collide.BundleID = "com.example.othertestapp"
	h.verifier.notifications["other-notif"] = domain.Notification{
		Type: domain.NotificationRefund, BundleID: "com.example.othertestapp", AppAppleID: "6749999999", Environment: "Production", Transaction: &collide,
	}
	if err := h.service.ApplyNotification(ctx, "other-notif"); !errors.Is(err, domain.ErrWrongBundle) {
		t.Fatalf("another app's notification must be refused, got %v", err)
	}
	h.mustBeFree(t)
}

func TestIdentity_Production_Notification_AppliesBundle_AppAppleID_Environment_AndProduct(t *testing.T) {
	arrange := func(t *testing.T) (*identityHarness, string) {
		h := newIdentityHarness(t, productionPolicy())
		ctx := context.Background()
		token, _ := h.service.AppAccountToken(ctx, accountA)
		h.verifier.transactions["jws"] = productionPurchase(token)
		if _, err := h.service.RedeemSignedTransaction(ctx, accountA, "jws"); err != nil {
			t.Fatal(err)
		}
		return h, token
	}
	validEnvelope := func(inner domain.VerifiedTransaction) domain.Notification {
		return domain.Notification{
			Type: domain.NotificationRefund, BundleID: bundle, AppAppleID: appAppleID, Environment: "Production", Transaction: &inner,
		}
	}
	mustStillBePaid := func(t *testing.T, h *identityHarness) {
		t.Helper()
		status, _ := h.service.Status(context.Background(), accountA)
		if status.Entitlement.State != domain.StatePaid {
			t.Fatalf("a refused notification must not revoke: %+v", status.Entitlement)
		}
		if h.transactions.revocations != 0 {
			t.Fatalf("a refused notification must write nothing, revocations=%d", h.transactions.revocations)
		}
	}

	cases := map[string]struct {
		mutate func(n *domain.Notification)
		want   error
	}{
		"wrong bundleId on the envelope":            {func(n *domain.Notification) { n.BundleID = "com.other.app" }, domain.ErrWrongBundle},
		"wrong appAppleId (production)":             {func(n *domain.Notification) { n.AppAppleID = "6749999999" }, domain.ErrWrongAppAppleID},
		"missing appAppleId (production)":           {func(n *domain.Notification) { n.AppAppleID = "" }, domain.ErrWrongAppAppleID},
		"Sandbox envelope against Production":       {func(n *domain.Notification) { n.Environment = "Sandbox" }, domain.ErrWrongEnvironment},
		"inner transaction for another bundle":      {func(n *domain.Notification) { n.Transaction.BundleID = "com.other.app" }, domain.ErrWrongBundle},
		"inner transaction from Sandbox":            {func(n *domain.Notification) { n.Transaction.Environment = "Sandbox" }, domain.ErrWrongEnvironment},
		"inner transaction for an unknown product":  {func(n *domain.Notification) { n.Transaction.ProductID = "com.muse.other" }, domain.ErrWrongProduct},
		"inner transaction for the DEV placeholder": {func(n *domain.Notification) { n.Transaction.ProductID = "dev.muse.placeholder.collection_capacity" }, domain.ErrWrongProduct},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h, token := arrange(t)
			n := validEnvelope(productionPurchase(token))
			tc.mutate(&n)
			h.verifier.notifications["refund"] = n

			if err := h.service.ApplyNotification(context.Background(), "refund"); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			mustStillBePaid(t, h)
		})
	}

	t.Run("the expected app's refund revokes", func(t *testing.T) {
		h, token := arrange(t)
		h.verifier.notifications["refund"] = validEnvelope(productionPurchase(token))
		if err := h.service.ApplyNotification(context.Background(), "refund"); err != nil {
			t.Fatal(err)
		}
		status, _ := h.service.Status(context.Background(), accountA)
		if status.Entitlement.State != domain.StateRevoked || h.transactions.revocations != 1 {
			t.Fatalf("%+v revocations=%d", status.Entitlement, h.transactions.revocations)
		}
	})
}

func TestIdentity_Development_EnvironmentIsExactToo_AndAppAppleIDIsNotRequired(t *testing.T) {
	dev := application.AppStorePolicy{BundleID: bundle, ProductIDs: []string{product}, Environment: "Sandbox", LocalTestingEnvironments: []string{"Xcode", "LocalTesting"}}
	h := newIdentityHarness(t, dev)
	ctx := context.Background()
	token, _ := h.service.AppAccountToken(ctx, accountA)

	prod := purchase(token)
	prod.Environment = "Production"
	h.verifier.transactions["prod"] = prod
	if _, err := h.service.RedeemSignedTransaction(ctx, accountA, "prod"); !errors.Is(err, domain.ErrWrongEnvironment) {
		t.Fatalf("a Production purchase must not unlock a Sandbox deployment, got %v", err)
	}
	h.mustBeFree(t)

	xcode := purchase(token)
	xcode.Environment = "Xcode"
	h.verifier.transactions["xcode"] = xcode
	if _, err := h.service.RedeemSignedTransaction(ctx, accountA, "xcode"); err != nil {
		t.Fatalf("a local-testing environment is accepted in development: %v", err)
	}
	inner := purchase(token)
	h.verifier.notifications["no-app-apple-id"] = domain.Notification{Type: domain.NotificationRefund, BundleID: bundle, Environment: "Sandbox", Transaction: &inner}
	if err := h.service.ApplyNotification(ctx, "no-app-apple-id"); err != nil {
		t.Fatalf("a Sandbox notification without appAppleId is Apple's documented shape: %v", err)
	}
}

func TestIdentity_VerificationFailure_NeverMutatesEntitlementState(t *testing.T) {
	h := newIdentityHarness(t, productionPolicy())
	ctx := context.Background()
	if _, err := h.service.RedeemSignedTransaction(ctx, accountA, "not-a-known-jws"); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("got %v", err)
	}
	h.mustBeFree(t)

	h.verifier.unavailable = true
	if _, err := h.service.RedeemSignedTransaction(ctx, accountA, "anything"); !errors.Is(err, domain.ErrVerificationUnavailable) {
		t.Fatalf("got %v", err)
	}
	h.mustBeFree(t)
	h.verifier.unavailable = false

	if err := h.service.ApplyNotification(ctx, "not-a-known-notification"); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("got %v", err)
	}
	h.mustBeFree(t)
}
