package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	entitlementdomain "muse-backend/internal/entitlement/domain"
	"muse-backend/internal/entitlement/infrastructure/appstoretest"
)

var smallCaps = entitlementdomain.ItemCapacities{Free: 3, Paid: 6, Source: "test"}

type entitlementStatusJSON struct {
	State        string `json:"state"`
	ItemCapacity int    `json:"item_capacity"`
	ItemCount    int    `json:"item_count"`
}

func (s *stack) entitlementStatus(token string) entitlementStatusJSON {
	s.t.Helper()
	r := s.get("/entitlements/me", token)
	if r.status != http.StatusOK {
		s.t.Fatalf("GET /entitlements/me: %d %s", r.status, r.body)
	}
	var status entitlementStatusJSON
	if err := json.Unmarshal([]byte(r.body), &status); err != nil {
		s.t.Fatal(err)
	}
	return status
}

func (s *stack) appAccountToken(token string) string {
	s.t.Helper()
	resp, raw := s.do(http.MethodPost, "/entitlements/app-account-token", nil, token)
	if resp.StatusCode != http.StatusOK {
		s.t.Fatalf("mint app account token: %d %s", resp.StatusCode, raw)
	}
	var body struct {
		AppAccountToken string `json:"app_account_token"`
	}
	_ = json.Unmarshal(raw, &body)
	return body.AppAccountToken
}

func purchasePayload(original, appAccountToken string) appstoretest.Transaction {
	return appstoretest.Transaction{
		TransactionID:         original,
		OriginalTransactionID: original,
		BundleID:              testAppleBundleID,
		ProductID:             testCapacityProductID,
		Type:                  "Non-Consumable",
		AppAccountToken:       appAccountToken,
		Environment:           "Sandbox",
		InAppOwnershipType:    "PURCHASED",
		PurchaseDate:          time.Now().Add(-time.Minute).UnixMilli(),
		SignedDate:            time.Now().UnixMilli(),
	}
}

func (s *stack) signed(payload any) string {
	s.t.Helper()
	jws, err := s.appStore.Sign(payload)
	if err != nil {
		s.t.Fatal(err)
	}
	return jws
}

func (s *stack) redeem(token, jws string) (int, string) {
	s.t.Helper()
	resp, raw := s.do(http.MethodPost, "/entitlements/app-store/transactions", map[string]string{"signed_transaction": jws}, token)
	return resp.StatusCode, string(raw)
}

func (s *stack) notify(notificationType string, inner appstoretest.Transaction) reply {
	s.t.Helper()
	var n appstoretest.Notification
	n.NotificationType = notificationType
	n.NotificationUUID = "uuid-" + notificationType + "-" + inner.OriginalTransactionID
	n.SignedDate = time.Now().UnixMilli()
	n.Version = "2.0"
	n.Data.BundleID = testAppleBundleID
	n.Data.Environment = "Sandbox"
	n.Data.SignedTransactionInfo = s.signed(inner)
	return s.doAnonymous(http.MethodPost, "/app-store/notifications", map[string]string{"signedPayload": s.signed(n)}, nil)
}

func (s *stack) addItemAs(roomID, token string) (int, string) {
	s.t.Helper()
	resp, raw := s.do(http.MethodPost, "/collection-rooms/"+roomID+"/items", map[string]string{"catalog_model_id": syntheticModel}, token)
	return resp.StatusCode, string(raw)
}

func (s *stack) accountItemCount(token string) int {
	s.t.Helper()
	return s.entitlementStatus(token).ItemCount
}

func mustBeCapacityRefusal(t *testing.T, status int, body string) {
	t.Helper()
	if status != http.StatusPaymentRequired || !strings.Contains(body, `"code":"item_capacity_reached"`) {
		t.Fatalf("expected 402 item_capacity_reached, got %d %s", status, body)
	}
}

func (s *stack) twoRooms(t *testing.T) (string, string) {
	t.Helper()
	return s.roomWithPublishedDesign(t, "Watches One"), s.roomWithPublishedDesign(t, "Watches Two")
}

func TestFreeCapIsAccountWide_AndAnotherRoomCannotBypassIt(t *testing.T) {
	s := newStackWithCapacities(t, smallCaps)
	roomA, roomB := s.twoRooms(t)

	if status := s.entitlementStatus(s.token); status.State != "free" || status.ItemCapacity != 3 || status.ItemCount != 0 {
		t.Fatalf("initial: %+v", status)
	}
	s.mustAddItem(roomA, syntheticModel)
	s.mustAddItem(roomA, syntheticModel)
	s.mustAddItem(roomB, syntheticModel)
	if got := s.accountItemCount(s.token); got != 3 {
		t.Fatalf("count: %d", got)
	}
	beforeA, beforeB := s.roomState(t, roomA), s.roomState(t, roomB)

	s.mustRefuseAdd(t, roomA, s.token)
	s.mustRefuseAdd(t, roomB, s.token)

	roomC := s.roomWithPublishedDesign(t, "Watches Three")
	s.mustRefuseAdd(t, roomC, s.token)

	if s.roomState(t, roomA) != beforeA || s.roomState(t, roomB) != beforeB {
		t.Fatal("refused adds must change nothing")
	}
	if got := s.accountItemCount(s.token); got != 3 {
		t.Fatalf("count after refusals: %d", got)
	}
}

func (s *stack) mustRefuseAdd(t *testing.T, roomID, token string) {
	t.Helper()
	status, body := s.addItemAs(roomID, token)
	mustBeCapacityRefusal(t, status, body)
}

func TestPaidEntitlementAppliesTheHigherCap_NotUnlimited(t *testing.T) {
	s := newStackWithCapacities(t, smallCaps)
	roomA, roomB := s.twoRooms(t)
	for i := 0; i < 3; i++ {
		s.mustAddItem(roomA, syntheticModel)
	}
	s.mustRefuseAdd(t, roomB, s.token)

	appAccountToken := s.appAccountToken(s.token)
	if again := s.appAccountToken(s.token); again != appAccountToken {
		t.Fatal("the token must be stable per account")
	}
	status, body := s.redeem(s.token, s.signed(purchasePayload("2000000001", appAccountToken)))
	if status != http.StatusOK {
		t.Fatalf("redeem: %d %s", status, body)
	}
	if got := s.entitlementStatus(s.token); got.State != "paid" || got.ItemCapacity != 6 || got.ItemCount != 3 {
		t.Fatalf("after purchase: %+v", got)
	}

	s.mustAddItem(roomB, syntheticModel)
	s.mustAddItem(roomB, syntheticModel)
	s.mustAddItem(roomB, syntheticModel)
	s.mustRefuseAdd(t, roomB, s.token)
	if got := s.entitlementStatus(s.token); got.State != "paid" || got.ItemCount != 6 {
		t.Fatalf("at the paid cap: %+v", got)
	}
}

func TestLimitsAreConfiguration_NotCompiledIn(t *testing.T) {
	s := newStackWithCapacities(t, entitlementdomain.ItemCapacities{Free: 1, Paid: 2, Source: "test"})
	room := s.roomWithPublishedDesign(t, "Tiny")
	if status := s.entitlementStatus(s.token); status.ItemCapacity != 1 {
		t.Fatalf("configured capacity must be reported: %+v", status)
	}
	s.mustAddItem(room, syntheticModel)
	s.mustRefuseAdd(t, room, s.token)
	if testDefaultCapacities.Free == 1 || smallCaps.Free == 1 {
		t.Fatal("test precondition: capacities must differ across stacks")
	}
}

func TestConcurrentAddsCannotExceedTheCap(t *testing.T) {
	s := newStackWithCapacities(t, smallCaps)
	roomA, roomB := s.twoRooms(t)
	s.mustAddItem(roomA, syntheticModel)
	s.mustAddItem(roomB, syntheticModel)

	const racers = 8
	statuses := make([]int, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			room := roomA
			if i%2 == 1 {
				room = roomB
			}
			statuses[i], _ = s.addItemAs(room, s.token)
		}(i)
	}
	wg.Wait()

	created, refused := 0, 0
	for _, status := range statuses {
		switch status {
		case http.StatusCreated:
			created++
		case http.StatusPaymentRequired:
			refused++
		default:
			t.Fatalf("unexpected status %d", status)
		}
	}
	if created != 1 || refused != racers-1 {
		t.Fatalf("exactly one racer may take the last slot: created=%d refused=%d", created, refused)
	}
	var dbCount int
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM collection_items i JOIN collection_rooms r ON r.id = i.collection_room_id WHERE r.account_id = $1`,
		s.accountID).Scan(&dbCount); err != nil {
		t.Fatal(err)
	}
	if dbCount != 3 {
		t.Fatalf("the database holds %d items for a cap of 3", dbCount)
	}
}

func TestTierAndEntitlementCeilingsStayDistinct(t *testing.T) {
	s := newStackWithCapacities(t, smallCaps)
	roomA, roomB := s.twoRooms(t)

	for i := 0; i < 3; i++ {
		s.mustAddItem(roomA, syntheticModel)
	}
	status, body := s.addItemAs(roomA, s.token)
	mustBeCapacityRefusal(t, status, body)
	if strings.Contains(body, "tier_capacity_reached") {
		t.Fatal("an entitlement refusal must never read as a tier refusal")
	}

	s.redeemOK(s.token, "2000000001")
	s.mustAddItem(roomA, syntheticModel)
	status, body = s.addItemAs(roomA, s.token)
	if status != http.StatusBadRequest || !strings.Contains(body, `"code":"tier_capacity_reached"`) {
		t.Fatalf("a full tier with entitlement to spare must be a tier refusal: %d %s", status, body)
	}
	if strings.Contains(body, "item_capacity_reached") || strings.Contains(body, "402") {
		t.Fatal("a tier refusal must never tell the owner to purchase")
	}
	if resp, raw := s.do(http.MethodPost, "/collection-rooms/"+roomA+"/tier", map[string]int{"tier": 2}, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("ratchet: %d %s", resp.StatusCode, raw)
	}
	s.mustAddItem(roomA, syntheticModel)
	s.mustAddItem(roomB, syntheticModel)
	status, body = s.addItemAs(roomA, s.token)
	mustBeCapacityRefusal(t, status, body)
}

func (s *stack) redeemOK(token, original string) {
	s.t.Helper()
	status, body := s.redeem(token, s.signed(purchasePayload(original, s.appAccountToken(token))))
	if status != http.StatusOK {
		s.t.Fatalf("redeem: %d %s", status, body)
	}
}

func TestClientDeclaredPremiumStateCannotUnlockCapacity(t *testing.T) {
	s := newStackWithCapacities(t, smallCaps)
	room := s.roomWithPublishedDesign(t, "Watches")
	for i := 0; i < 3; i++ {
		s.mustAddItem(room, syntheticModel)
	}
	appAccountToken := s.appAccountToken(s.token)
	genuine := s.signed(purchasePayload("2000000001", appAccountToken))
	parts := strings.Split(genuine, ".")
	stranger, _ := appstoretest.NewSigner(appstoretest.Options{})
	forgedByStranger, _ := stranger.Sign(purchasePayload("2000000001", appAccountToken))
	strangerChainGenuineSig, _ := stranger.SignWithChain(purchasePayload("2000000001", appAccountToken), s.appStore.X5C)
	tampered := parts[0] + "." + strings.Split(s.signed(func() appstoretest.Transaction {
		p := purchasePayload("2000000001", appAccountToken)
		p.ProductID = "premium.everything"
		return p
	}()), ".")[1] + "." + parts[2]
	wrongProduct := s.signed(func() appstoretest.Transaction {
		p := purchasePayload("2000000002", appAccountToken)
		p.ProductID = "other.product"
		return p
	}())
	wrongBundle := s.signed(func() appstoretest.Transaction {
		p := purchasePayload("2000000003", appAccountToken)
		p.BundleID = "com.other.app"
		return p
	}())
	subscription := s.signed(func() appstoretest.Transaction {
		p := purchasePayload("2000000004", appAccountToken)
		p.Type = "Auto-Renewable Subscription"
		return p
	}())
	familyShared := s.signed(func() appstoretest.Transaction {
		p := purchasePayload("2000000005", appAccountToken)
		p.InAppOwnershipType = "FAMILY_SHARED"
		return p
	}())
	noToken := s.signed(purchasePayload("2000000006", ""))

	for name, attempt := range map[string]struct {
		body any
		want int
		code string
	}{
		"client boolean":                {map[string]any{"is_premium": true}, http.StatusBadRequest, "invalid_signed_transaction"},
		"word":                          {map[string]string{"signed_transaction": "premium"}, http.StatusBadRequest, "invalid_signed_transaction"},
		"forged by a stranger":          {map[string]string{"signed_transaction": forgedByStranger}, http.StatusBadRequest, "invalid_signed_transaction"},
		"genuine chain, foreign sig":    {map[string]string{"signed_transaction": strangerChainGenuineSig}, http.StatusBadRequest, "invalid_signed_transaction"},
		"tampered payload":              {map[string]string{"signed_transaction": tampered}, http.StatusBadRequest, "invalid_signed_transaction"},
		"genuine, wrong product":        {map[string]string{"signed_transaction": wrongProduct}, http.StatusBadRequest, "transaction_not_applicable"},
		"genuine, wrong bundle":         {map[string]string{"signed_transaction": wrongBundle}, http.StatusBadRequest, "transaction_not_applicable"},
		"genuine, a subscription":       {map[string]string{"signed_transaction": subscription}, http.StatusBadRequest, "transaction_not_applicable"},
		"genuine, family shared":        {map[string]string{"signed_transaction": familyShared}, http.StatusBadRequest, "transaction_not_applicable"},
		"genuine, no app account token": {map[string]string{"signed_transaction": noToken}, http.StatusBadRequest, "transaction_not_applicable"},
	} {
		resp, raw := s.do(http.MethodPost, "/entitlements/app-store/transactions", attempt.body, s.token)
		if resp.StatusCode != attempt.want || !strings.Contains(string(raw), `"code":"`+attempt.code+`"`) {
			t.Errorf("%s: %d %s (want %d %s)", name, resp.StatusCode, raw, attempt.want, attempt.code)
		}
	}
	if got := s.entitlementStatus(s.token); got.State != "free" || got.ItemCapacity != 3 {
		t.Fatalf("every refusal must leave the account free: %+v", got)
	}
	resp, raw := s.do(http.MethodPost, "/collection-rooms/"+room+"/items", map[string]any{"catalog_model_id": syntheticModel, "is_premium": true, "entitlement": "paid"}, s.token)
	mustBeCapacityRefusal(t, resp.StatusCode, string(raw))
	var rows int
	_ = s.pool.Pool().QueryRow(context.Background(), `SELECT count(*) FROM app_store_transactions`).Scan(&rows)
	if rows != 0 {
		t.Fatalf("nothing may be bound: %d rows", rows)
	}
}

func TestOneTransactionUnlocksOneAccount_AndRestoreRespectsIt(t *testing.T) {
	s := newStackWithCapacities(t, smallCaps)
	accountA := s.token
	accountB := s.strangerToken()
	tokenA := s.appAccountToken(accountA)
	tokenB := s.appAccountToken(accountB)
	if tokenA == tokenB {
		t.Fatal("two accounts must have two tokens")
	}
	purchase := s.signed(purchasePayload("2000000001", tokenA))

	if status, body := s.redeem(accountA, purchase); status != http.StatusOK {
		t.Fatalf("A redeem: %d %s", status, body)
	}
	if got := s.entitlementStatus(accountA); got.State != "paid" || got.ItemCapacity != 6 {
		t.Fatalf("A: %+v", got)
	}

	status, body := s.redeem(accountB, purchase)
	if status != http.StatusConflict || !strings.Contains(body, `"code":"app_account_token_mismatch"`) {
		t.Fatalf("B with A's transaction: %d %s", status, body)
	}
	status, body = s.redeem(accountB, s.signed(purchasePayload("2000000001", tokenB)))
	if status != http.StatusConflict || !strings.Contains(body, `"code":"transaction_bound_to_another_account"`) {
		t.Fatalf("B re-binding A's original transaction: %d %s", status, body)
	}
	if got := s.entitlementStatus(accountB); got.State != "free" || got.ItemCapacity != 3 {
		t.Fatalf("B must remain free: %+v", got)
	}
	roomB := s.createCollectionRoomAs(t, accountB)
	for i := 0; i < 3; i++ {
		if status, body := s.addItemAs(roomB, accountB); status != http.StatusCreated {
			t.Fatalf("B add %d: %d %s", i, status, body)
		}
	}
	s.mustRefuseAdd(t, roomB, accountB)

	if status, body := s.redeem(accountA, purchase); status != http.StatusOK {
		t.Fatalf("A restore: %d %s", status, body)
	}
	var rows int
	var owner string
	if err := s.pool.Pool().QueryRow(context.Background(),
		`SELECT count(*), min(account_id::text) FROM app_store_transactions`).Scan(&rows, &owner); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || owner != s.accountID {
		t.Fatalf("bindings: %d rows, owner %s", rows, owner)
	}
	if got := s.entitlementStatus(accountA); got.State != "paid" {
		t.Fatalf("A after restore: %+v", got)
	}
}

func (s *stack) createCollectionRoomAs(t *testing.T, token string) string {
	t.Helper()
	s.publishCommittedDesignFixture(t)
	resp, body := s.do(http.MethodPost, "/collection-rooms",
		map[string]string{"name": "Theirs", "category_id": seededCategory, "design_id": devFixtureDesign}, token)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create room as other: %d %s", resp.StatusCode, body)
	}
	return decodeCollectionRoom(t, body).ID
}

func TestRefundRevokesCapacity_RetainsItems_RefusesNewOnes(t *testing.T) {
	s := newStackWithCapacities(t, smallCaps)
	roomA, roomB := s.twoRooms(t)
	tokenA := s.appAccountToken(s.token)
	purchase := purchasePayload("2000000001", tokenA)
	if status, body := s.redeem(s.token, s.signed(purchase)); status != http.StatusOK {
		t.Fatalf("redeem: %d %s", status, body)
	}
	for i := 0; i < 3; i++ {
		s.mustAddItem(roomA, syntheticModel)
	}
	s.mustAddItem(roomB, syntheticModel)
	first := s.mustAddItem(roomB, syntheticModel)
	beforeA, beforeB := s.roomState(t, roomA), s.roomState(t, roomB)

	revoked := time.Now().UnixMilli()
	reason := int64(1)
	refunded := purchase
	refunded.RevocationDate = &revoked
	refunded.RevocationReason = &reason
	if r := s.notify("REFUND", refunded); r.status != http.StatusOK {
		t.Fatalf("refund notification: %d %s", r.status, r.body)
	}

	got := s.entitlementStatus(s.token)
	if got.State != "revoked" || got.ItemCapacity != 3 || got.ItemCount != 5 {
		t.Fatalf("after refund: %+v", got)
	}
	if s.roomState(t, roomA) != beforeA || s.roomState(t, roomB) != beforeB {
		t.Fatal("a refund must never delete or move items")
	}
	s.mustRefuseAdd(t, roomA, s.token)
	s.mustRefuseAdd(t, roomB, s.token)
	if r := s.get("/collection-rooms/"+roomB, s.token); r.status != http.StatusOK || !strings.Contains(r.body, first.Items[0].ID) {
		t.Fatalf("read after refund: %d %s", r.status, r.body)
	}
	if resp, raw := s.do(http.MethodPatch, "/collection-rooms/"+roomB, map[string]string{"name": "Still Mine"}, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("rename after refund: %d %s", resp.StatusCode, raw)
	}
	itemInB := first.Items[len(first.Items)-1].ID
	if resp, raw := s.do(http.MethodPut, "/collection-rooms/"+roomB+"/items/"+itemInB+"/slot?app_asset_version=1", map[string]int{"slot_index": 3}, s.token); resp.StatusCode != http.StatusOK {
		t.Fatalf("rearrange after refund: %d %s", resp.StatusCode, raw)
	}
	var revokedAt *time.Time
	if err := s.pool.Pool().QueryRow(context.Background(), `SELECT revoked_at FROM app_store_transactions WHERE original_transaction_id = '2000000001'`).Scan(&revokedAt); err != nil || revokedAt == nil {
		t.Fatalf("revocation must be recorded on the kept row: %v %v", revokedAt, err)
	}

	if r := s.notify("REFUND_REVERSED", purchase); r.status != http.StatusOK {
		t.Fatalf("reversal: %d %s", r.status, r.body)
	}
	if got := s.entitlementStatus(s.token); got.State != "paid" || got.ItemCapacity != 6 {
		t.Fatalf("after reversal: %+v", got)
	}
	s.mustAddItem(roomA, syntheticModel)
	s.mustRefuseAdd(t, roomA, s.token)

	if r := s.notify("REFUND", purchasePayload("9999999999", tokenA)); r.status != http.StatusOK {
		t.Fatalf("unknown transaction notification must be acknowledged: %d", r.status)
	}
	other := purchase
	other.BundleID = "com.other.app"
	if r := s.notify("REFUND", other); r.status != http.StatusBadRequest {
		t.Fatalf("another app's notification: %d %s", r.status, r.body)
	}
	if got := s.entitlementStatus(s.token); got.State != "paid" {
		t.Fatalf("still paid: %+v", got)
	}
}

func TestMuseumRemainsOutsideTheEntitlement(t *testing.T) {
	s := newStackWithCapacities(t, smallCaps)
	museumRoom := s.createRoom()
	roomA := s.roomWithPublishedDesign(t, "Watches")
	for i := 0; i < 3; i++ {
		s.mustAddItem(roomA, syntheticModel)
	}
	s.mustRefuseAdd(t, roomA, s.token)
	before := s.snapshotOwnerState()

	photo := s.uploaded(newPhoto(t, 640, 480, "entitlement-museum-1"))
	if resp, _, body := s.assign(museumRoom, []string{photo.asset}); resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("museum photo assignment must not be gated by the Collection entitlement: %d %v", resp.StatusCode, body)
	}
	s.redeemOK(s.token, "2000000001")
	if r := s.notify("REFUND", purchasePayload("2000000001", s.appAccountToken(s.token))); r.status != http.StatusOK {
		t.Fatal("refund")
	}
	after := s.snapshotOwnerState()
	_ = before
	if again := s.snapshotOwnerState(); again != after {
		t.Fatal("entitlement changes must not touch the Museum")
	}
	r := s.get("/entitlements/me", s.token)
	for _, forbidden := range []string{"museum", "room", "photo", "style", "sculpture"} {
		if strings.Contains(strings.ToLower(r.body), forbidden) {
			t.Fatalf("the entitlement payload must not mention the Museum tree: %s", r.body)
		}
	}
}
