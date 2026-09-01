package domain

import (
	"errors"
	"testing"
	"time"
)

func revoked(at time.Time) *time.Time { return &at }

func transaction(revokedAt *time.Time) AppStoreTransaction {
	return AppStoreTransaction{
		OriginalTransactionID: "1000000000000001",
		ProductID:             "dev.muse.placeholder.collection_capacity",
		PurchasedAt:           time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		RevokedAt:             revokedAt,
	}
}

func TestItemCapacities_ValidateRefusesShapesThatCannotBeAProductDecision(t *testing.T) {
	cases := []struct {
		name       string
		capacities ItemCapacities
		valid      bool
	}{
		{"ordinary", ItemCapacities{Free: 25, Paid: 500}, true},
		{"free may be zero", ItemCapacities{Free: 0, Paid: 1}, true},
		{"negative free", ItemCapacities{Free: -1, Paid: 500}, false},
		{"paid equals free — nothing is being sold", ItemCapacities{Free: 25, Paid: 25}, false},
		{"paid below free — the purchase would take capacity away", ItemCapacities{Free: 25, Paid: 10}, false},
		{"both zero", ItemCapacities{}, false},
	}
	for _, testCase := range cases {
		err := testCase.capacities.Validate()
		if testCase.valid && err != nil {
			t.Errorf("%s: %v", testCase.name, err)
		}
		if !testCase.valid {
			if err == nil {
				t.Errorf("%s: expected a refusal", testCase.name)
			} else if !errors.Is(err, ErrInvalidCapacities) {
				t.Errorf("%s: unexpected error %v", testCase.name, err)
			}
		}
	}
}

func TestItemCapacities_UnavailableGrantsNothing(t *testing.T) {
	capacities := ItemCapacities{Free: 25, Paid: 500}
	if got := capacities.CapacityFor(StateUnavailable); got != 0 {
		t.Fatalf("CapacityFor(unavailable) = %d; verification failure must never grant capacity", got)
	}
	if got := capacities.CapacityFor(State("something_new")); got != 0 {
		t.Fatalf("CapacityFor(unknown) = %d; the default must be nothing, not the paid tier", got)
	}
}

func TestItemCapacities_RevokedReturnsToTheFreeTierNotToZero(t *testing.T) {
	capacities := ItemCapacities{Free: 25, Paid: 500}
	if got := capacities.CapacityFor(StateRevoked); got != 25 {
		t.Fatalf("CapacityFor(revoked) = %d, want the free tier (25)", got)
	}
	if got := capacities.CapacityFor(StateFree); got != 25 {
		t.Fatalf("CapacityFor(free) = %d, want 25", got)
	}
	if got := capacities.CapacityFor(StatePaid); got != 500 {
		t.Fatalf("CapacityFor(paid) = %d, want 500", got)
	}
}

func TestAppStoreTransaction_IsActiveTracksRevocation(t *testing.T) {
	if !transaction(nil).IsActive() {
		t.Error("a transaction with no revocation date is active")
	}
	if transaction(revoked(time.Now())).IsActive() {
		t.Error("a revoked transaction is not active")
	}
}

func TestResolve_OneActiveTransactionAnywhereMakesTheAccountPaid(t *testing.T) {
	capacities := ItemCapacities{Free: 25, Paid: 500}
	active := transaction(nil)
	dead := transaction(revoked(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)))

	orders := map[string][]AppStoreTransaction{
		"active first":               {active, dead},
		"active last":                {dead, active},
		"active between two revoked": {dead, active, dead},
		"only active":                {active},
	}
	for name, transactions := range orders {
		got := Resolve(transactions, capacities)
		if got.State != StatePaid {
			t.Errorf("%s: state = %v, want paid", name, got.State)
		}
		if got.ItemCapacity != 500 {
			t.Errorf("%s: capacity = %d, want 500", name, got.ItemCapacity)
		}
	}
}

func TestResolve_NoTransactionsIsFreeAndAllRevokedIsRevoked(t *testing.T) {
	capacities := ItemCapacities{Free: 25, Paid: 500}
	dead := transaction(revoked(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)))

	if got := Resolve(nil, capacities); got.State != StateFree || got.ItemCapacity != 25 {
		t.Fatalf("no transactions → %+v, want free/25", got)
	}
	if got := Resolve([]AppStoreTransaction{}, capacities); got.State != StateFree {
		t.Fatalf("empty slice → %v, want free", got.State)
	}
	if got := Resolve([]AppStoreTransaction{dead, dead}, capacities); got.State != StateRevoked || got.ItemCapacity != 25 {
		t.Fatalf("all revoked → %+v, want revoked/25", got)
	}
}

func TestResolve_NeverProducesUnavailable(t *testing.T) {
	capacities := ItemCapacities{Free: 1, Paid: 2}
	inputs := [][]AppStoreTransaction{
		nil,
		{transaction(nil)},
		{transaction(revoked(time.Now()))},
		{transaction(nil), transaction(revoked(time.Now()))},
	}
	for _, transactions := range inputs {
		if got := Resolve(transactions, capacities); got.State == StateUnavailable {
			t.Fatalf("Resolve produced StateUnavailable for %v", transactions)
		}
	}
}

func TestResolve_IsPureAndDoesNotMutateItsInput(t *testing.T) {
	capacities := ItemCapacities{Free: 25, Paid: 500}
	transactions := []AppStoreTransaction{transaction(revoked(time.Now())), transaction(nil)}
	before := append([]AppStoreTransaction(nil), transactions...)

	first := Resolve(transactions, capacities)
	second := Resolve(transactions, capacities)
	if first != second {
		t.Fatalf("not deterministic: %+v then %+v", first, second)
	}
	for index := range before {
		if transactions[index] != before[index] {
			t.Fatalf("Resolve mutated its input at %d", index)
		}
	}
}
