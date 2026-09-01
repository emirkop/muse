package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"muse-backend/internal/entitlement/application"
	"muse-backend/internal/entitlement/domain"
)

type fakeTransactions struct {
	byOriginal map[string]domain.AppStoreTransaction
	listErr    error
}

func newFakeTransactions() *fakeTransactions {
	return &fakeTransactions{byOriginal: map[string]domain.AppStoreTransaction{}}
}

func (f *fakeTransactions) Bind(_ context.Context, t domain.AppStoreTransaction) (domain.AppStoreTransaction, error) {
	if existing, ok := f.byOriginal[t.OriginalTransactionID]; ok {
		if existing.AccountID != t.AccountID {
			return domain.AppStoreTransaction{}, domain.ErrTransactionBoundToAnotherAccount
		}
		existing.TransactionID, existing.RevokedAt, existing.RevocationReason, existing.LastVerifiedAt = t.TransactionID, t.RevokedAt, t.RevocationReason, t.LastVerifiedAt
		f.byOriginal[t.OriginalTransactionID] = existing
		return existing, nil
	}
	f.byOriginal[t.OriginalTransactionID] = t
	return t, nil
}

func (f *fakeTransactions) ListForAccount(_ context.Context, accountID string) ([]domain.AppStoreTransaction, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []domain.AppStoreTransaction
	for _, t := range f.byOriginal {
		if t.AccountID == accountID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeTransactions) SetRevocation(_ context.Context, original string, revokedAt *time.Time, reason string) (bool, error) {
	t, ok := f.byOriginal[original]
	if !ok {
		return false, nil
	}
	t.RevokedAt, t.RevocationReason = revokedAt, reason
	f.byOriginal[original] = t
	return true, nil
}

type fakeTokens struct {
	byAccount map[string]string
}

func (f *fakeTokens) EnsureToken(_ context.Context, accountID string) (string, error) {
	if t, ok := f.byAccount[accountID]; ok {
		return t, nil
	}
	t := "token-for-" + accountID
	f.byAccount[accountID] = t
	return t, nil
}

func (f *fakeTokens) AccountForToken(_ context.Context, token string) (string, error) {
	for account, t := range f.byAccount {
		if t == token {
			return account, nil
		}
	}
	return "", domain.ErrUnknownAppAccountToken
}

type fakeCounts struct {
	byAccount map[string]int
	err       error
}

func (f *fakeCounts) CountItemsForAccount(_ context.Context, accountID string) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.byAccount[accountID], nil
}

type fakeVerifier struct {
	transactions  map[string]domain.VerifiedTransaction
	notifications map[string]domain.Notification
	unavailable   bool
}

func (f *fakeVerifier) VerifyTransaction(_ context.Context, signed string) (domain.VerifiedTransaction, error) {
	if f.unavailable {
		return domain.VerifiedTransaction{}, domain.ErrVerificationUnavailable
	}
	t, ok := f.transactions[signed]
	if !ok {
		return domain.VerifiedTransaction{}, domain.ErrInvalidSignedTransaction
	}
	return t, nil
}

func (f *fakeVerifier) VerifyNotification(_ context.Context, signed string) (domain.Notification, error) {
	n, ok := f.notifications[signed]
	if !ok {
		return domain.Notification{}, domain.ErrInvalidSignedTransaction
	}
	return n, nil
}

const (
	accountA = "acct-a"
	accountB = "acct-b"
	bundle   = "com.muse.app"
	product  = "dev.muse.placeholder.collection_capacity"
)

var caps = domain.ItemCapacities{Free: 3, Paid: 6, Source: "test"}

func purchase(token string) domain.VerifiedTransaction {
	return domain.VerifiedTransaction{
		TransactionID: "tx-1", OriginalTransactionID: "orig-1",
		BundleID: bundle, ProductID: product, Type: domain.ProductTypeNonConsumable,
		AppAccountToken: token, Environment: "Sandbox", InAppOwnershipType: domain.OwnershipPurchased,
		PurchasedAt: time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC),
	}
}

func notificationFor(notificationType string, inner domain.VerifiedTransaction) domain.Notification {
	return domain.Notification{
		Type: notificationType, BundleID: bundle, Environment: "Sandbox",
		Transaction: &inner,
	}
}

type harness struct {
	service      *application.EntitlementService
	transactions *fakeTransactions
	tokens       *fakeTokens
	counts       *fakeCounts
	verifier     *fakeVerifier
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		transactions: newFakeTransactions(),
		tokens:       &fakeTokens{byAccount: map[string]string{}},
		counts:       &fakeCounts{byAccount: map[string]int{}},
		verifier:     &fakeVerifier{transactions: map[string]domain.VerifiedTransaction{}, notifications: map[string]domain.Notification{}},
	}
	service, err := application.NewEntitlementService(
		h.transactions, h.tokens, h.counts, h.verifier,
		application.AppStorePolicy{BundleID: bundle, ProductIDs: []string{product}, Environment: "Sandbox"},
		caps, func() time.Time { return time.Date(2026, 8, 27, 13, 0, 0, 0, time.UTC) },
	)
	if err != nil {
		t.Fatal(err)
	}
	h.service = service
	return h
}

func TestService_RefusesIncoherentCapacities(t *testing.T) {
	for name, c := range map[string]domain.ItemCapacities{
		"paid not above free": {Free: 5, Paid: 5},
		"paid below free":     {Free: 5, Paid: 2},
		"negative free":       {Free: -1, Paid: 5},
	} {
		if _, err := application.NewEntitlementService(nil, nil, nil, nil, application.AppStorePolicy{BundleID: bundle, ProductIDs: []string{product}, Environment: "Sandbox"}, c, nil); !errors.Is(err, domain.ErrInvalidCapacities) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestService_FreeAccount_HasTheFreeCapacity(t *testing.T) {
	h := newHarness(t)
	status, err := h.service.Status(context.Background(), accountA)
	if err != nil || status.Entitlement.State != domain.StateFree || status.Entitlement.ItemCapacity != 3 {
		t.Fatalf("%+v %v", status, err)
	}
}

func TestService_Redeem_BindsAVerifiedPurchase_AndGrantsThePaidCapacity(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	token, _ := h.service.AppAccountToken(ctx, accountA)
	h.verifier.transactions["jws-a"] = purchase(token)
	h.counts.byAccount[accountA] = 4

	status, err := h.service.RedeemSignedTransaction(ctx, accountA, "jws-a")
	if err != nil {
		t.Fatal(err)
	}
	if status.Entitlement.State != domain.StatePaid || status.Entitlement.ItemCapacity != 6 || status.ItemCount != 4 {
		t.Fatalf("%+v", status)
	}
	bound := h.transactions.byOriginal["orig-1"]
	if bound.AccountID != accountA || bound.AppAccountToken != token || bound.ProductID != product {
		t.Fatalf("bound: %+v", bound)
	}
	again, err := h.service.RedeemSignedTransaction(ctx, accountA, "jws-a")
	if err != nil || again.Entitlement.State != domain.StatePaid {
		t.Fatalf("restore: %+v %v", again, err)
	}
	if len(h.transactions.byOriginal) != 1 {
		t.Fatal("a restore must not create a second binding")
	}
}

func TestService_ATransactionForAccountA_CannotUnlockAccountB(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tokenA, _ := h.service.AppAccountToken(ctx, accountA)
	_, _ = h.service.AppAccountToken(ctx, accountB)
	h.verifier.transactions["jws-a"] = purchase(tokenA)
	if _, err := h.service.RedeemSignedTransaction(ctx, accountA, "jws-a"); err != nil {
		t.Fatal(err)
	}

	_, err := h.service.RedeemSignedTransaction(ctx, accountB, "jws-a")
	if !errors.Is(err, domain.ErrAppAccountTokenMismatch) {
		t.Fatalf("B must not inherit A's purchase: %v", err)
	}
	status, _ := h.service.Status(ctx, accountB)
	if status.Entitlement.State != domain.StateFree || status.Entitlement.ItemCapacity != 3 {
		t.Fatalf("B must remain free: %+v", status)
	}
	statusA, _ := h.service.Status(ctx, accountA)
	if statusA.Entitlement.State != domain.StatePaid {
		t.Fatalf("A must remain paid: %+v", statusA)
	}
}

func TestService_TheSameOriginalTransaction_CannotBindToTwoAccounts(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tokenA, _ := h.service.AppAccountToken(ctx, accountA)
	tokenB, _ := h.service.AppAccountToken(ctx, accountB)
	h.verifier.transactions["jws-a"] = purchase(tokenA)
	h.verifier.transactions["jws-b-forged"] = purchase(tokenB)
	if _, err := h.service.RedeemSignedTransaction(ctx, accountA, "jws-a"); err != nil {
		t.Fatal(err)
	}

	_, err := h.service.RedeemSignedTransaction(ctx, accountB, "jws-b-forged")
	if !errors.Is(err, domain.ErrTransactionBoundToAnotherAccount) {
		t.Fatalf("expected ErrTransactionBoundToAnotherAccount, got %v", err)
	}
	if h.transactions.byOriginal["orig-1"].AccountID != accountA {
		t.Fatal("the binding must not move")
	}
}

func TestService_ATransactionWithNoOrUnknownToken_IsRefused(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.verifier.transactions["no-token"] = purchase("")
	h.verifier.transactions["unknown-token"] = purchase("never-minted")

	if _, err := h.service.RedeemSignedTransaction(ctx, accountA, "no-token"); !errors.Is(err, domain.ErrNoAppAccountToken) {
		t.Fatalf("no token: %v", err)
	}
	if _, err := h.service.RedeemSignedTransaction(ctx, accountA, "unknown-token"); !errors.Is(err, domain.ErrAppAccountTokenMismatch) {
		t.Fatalf("unknown token: %v", err)
	}
	if len(h.transactions.byOriginal) != 0 {
		t.Fatal("nothing may be bound")
	}
}

func TestService_Policy_RefusesEverythingThatIsNotThisAppsOneNonConsumable(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	token, _ := h.service.AppAccountToken(ctx, accountA)
	cases := map[string]struct {
		mutate func(*domain.VerifiedTransaction)
		want   error
	}{
		"other bundle":      {func(t *domain.VerifiedTransaction) { t.BundleID = "com.other.app" }, domain.ErrWrongBundle},
		"other product":     {func(t *domain.VerifiedTransaction) { t.ProductID = "premium.subscription" }, domain.ErrWrongProduct},
		"subscription type": {func(t *domain.VerifiedTransaction) { t.Type = "Auto-Renewable Subscription" }, domain.ErrNotNonConsumable},
		"consumable type":   {func(t *domain.VerifiedTransaction) { t.Type = "Consumable" }, domain.ErrNotNonConsumable},
		"family shared":     {func(t *domain.VerifiedTransaction) { t.InAppOwnershipType = "FAMILY_SHARED" }, domain.ErrFamilyShared},
		"environment":       {func(t *domain.VerifiedTransaction) { t.Environment = "Xcode" }, domain.ErrWrongEnvironment},
	}
	for name, c := range cases {
		tx := purchase(token)
		c.mutate(&tx)
		h.verifier.transactions[name] = tx
		if _, err := h.service.RedeemSignedTransaction(ctx, accountA, name); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}
	if len(h.transactions.byOriginal) != 0 {
		t.Fatal("no refused transaction may be bound")
	}
	status, _ := h.service.Status(ctx, accountA)
	if status.Entitlement.State != domain.StateFree {
		t.Fatalf("still free: %+v", status)
	}
}

func TestService_VerificationFailureOrUnavailability_NeverCreatesAnEntitlement(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	if _, err := h.service.RedeemSignedTransaction(ctx, accountA, `{"isPremium":true}`); !errors.Is(err, domain.ErrInvalidSignedTransaction) {
		t.Fatalf("a client claim is not a signed transaction: %v", err)
	}
	h.verifier.unavailable = true
	if _, err := h.service.RedeemSignedTransaction(ctx, accountA, "anything"); !errors.Is(err, domain.ErrVerificationUnavailable) {
		t.Fatalf("unavailable: %v", err)
	}
	if len(h.transactions.byOriginal) != 0 {
		t.Fatal("nothing may be bound")
	}
	h.transactions.listErr = errors.New("store down")
	allowed, err := h.service.MayAddCollectionItem(ctx, accountA, "room")
	if err == nil || allowed {
		t.Fatalf("an unreadable entitlement must refuse, got allowed=%v err=%v", allowed, err)
	}
}

func TestService_MayAddCollectionItem_IsCountAgainstTheStatesCapacity(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	h.counts.byAccount[accountA] = 2
	if ok, _ := h.service.MayAddCollectionItem(ctx, accountA, "any-room"); !ok {
		t.Fatal("2 of 3: allowed")
	}
	h.counts.byAccount[accountA] = 3
	if ok, _ := h.service.MayAddCollectionItem(ctx, accountA, "any-room"); ok {
		t.Fatal("3 of 3: refused — the count is account-wide, the Room is irrelevant")
	}

	token, _ := h.service.AppAccountToken(ctx, accountA)
	h.verifier.transactions["jws"] = purchase(token)
	if _, err := h.service.RedeemSignedTransaction(ctx, accountA, "jws"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := h.service.MayAddCollectionItem(ctx, accountA, "any-room"); !ok {
		t.Fatal("3 of 6 after purchase: allowed")
	}
	h.counts.byAccount[accountA] = 6
	if ok, _ := h.service.MayAddCollectionItem(ctx, accountA, "any-room"); ok {
		t.Fatal("6 of 6: refused — paid is a higher ceiling, not unlimited")
	}
	h.counts.err = errors.New("count failed")
	if ok, err := h.service.MayAddCollectionItem(ctx, accountA, "any-room"); ok || err == nil {
		t.Fatal("an uncountable account fails closed")
	}
}

func TestService_Refund_RevokesTheEntitlement_AndReversalRestoresIt(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	token, _ := h.service.AppAccountToken(ctx, accountA)
	h.verifier.transactions["jws"] = purchase(token)
	if _, err := h.service.RedeemSignedTransaction(ctx, accountA, "jws"); err != nil {
		t.Fatal(err)
	}
	inner := purchase(token)
	refund := notificationFor(domain.NotificationRefund, inner)
	refund.SignedAt = time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)
	h.verifier.notifications["refund"] = refund
	h.verifier.notifications["reversed"] = notificationFor(domain.NotificationRefundReversed, inner)
	h.verifier.notifications["unrelated"] = notificationFor("DID_RENEW", inner)
	other := purchase(token)
	other.BundleID = "com.other.app"
	otherApp := notificationFor(domain.NotificationRefund, other)
	otherApp.BundleID = "com.other.app"
	h.verifier.notifications["other app"] = otherApp

	if err := h.service.ApplyNotification(ctx, "refund"); err != nil {
		t.Fatal(err)
	}
	status, _ := h.service.Status(ctx, accountA)
	if status.Entitlement.State != domain.StateRevoked || status.Entitlement.ItemCapacity != 3 {
		t.Fatalf("after refund: %+v", status)
	}
	if h.transactions.byOriginal["orig-1"].RevokedAt == nil {
		t.Fatal("revocation must be recorded, never the row deleted")
	}
	h.counts.byAccount[accountA] = 5
	if ok, _ := h.service.MayAddCollectionItem(ctx, accountA, "room"); ok {
		t.Fatal("5 items above a free cap of 3: new additions refused")
	}

	if err := h.service.ApplyNotification(ctx, "unrelated"); err != nil {
		t.Fatal(err)
	}
	if s, _ := h.service.Status(ctx, accountA); s.Entitlement.State != domain.StateRevoked {
		t.Fatal("an unrelated notification must change nothing")
	}
	if err := h.service.ApplyNotification(ctx, "other app"); !errors.Is(err, domain.ErrWrongBundle) {
		t.Fatalf("another app's notification: %v", err)
	}

	if err := h.service.ApplyNotification(ctx, "reversed"); err != nil {
		t.Fatal(err)
	}
	if s, _ := h.service.Status(ctx, accountA); s.Entitlement.State != domain.StatePaid {
		t.Fatalf("after reversal: %+v", s)
	}
	stranger := purchase("whatever")
	stranger.OriginalTransactionID = "orig-never-seen"
	h.verifier.notifications["stranger"] = notificationFor(domain.NotificationRefund, stranger)
	if err := h.service.ApplyNotification(ctx, "stranger"); err != nil {
		t.Fatal(err)
	}
	revokedAt := time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)
	tx := purchase(token)
	tx.OriginalTransactionID, tx.TransactionID, tx.RevokedAt = "orig-2", "tx-2", &revokedAt
	h.verifier.transactions["already-revoked"] = tx
	s, err := h.service.RedeemSignedTransaction(ctx, accountB, "already-revoked")
	if !errors.Is(err, domain.ErrAppAccountTokenMismatch) {
		t.Fatalf("B with A's token: %v", err)
	}
	s, err = h.service.RedeemSignedTransaction(ctx, accountA, "already-revoked")
	if err != nil || s.Entitlement.State != domain.StatePaid {
		t.Fatalf("%+v %v", s, err)
	}
}
