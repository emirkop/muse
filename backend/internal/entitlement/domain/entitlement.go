package domain

import (
	"errors"
	"time"
)

type State string

const (
	StateFree        State = "free"
	StatePaid        State = "paid"
	StateRevoked     State = "revoked"
	StateUnavailable State = "unavailable"
)

type ItemCapacities struct {
	Free   int
	Paid   int
	Source string
}

func (c ItemCapacities) Validate() error {
	if c.Free < 0 {
		return ErrInvalidCapacities
	}
	if c.Paid <= c.Free {
		return ErrInvalidCapacities
	}
	return nil
}

func (c ItemCapacities) CapacityFor(state State) int {
	switch state {
	case StatePaid:
		return c.Paid
	case StateFree, StateRevoked:
		return c.Free
	default:
		return 0
	}
}

type AppStoreTransaction struct {
	OriginalTransactionID string
	TransactionID         string
	AccountID             string
	ProductID             string
	BundleID              string
	Environment           string
	AppAccountToken       string
	PurchasedAt           time.Time
	RevokedAt             *time.Time
	RevocationReason      string
	FirstVerifiedAt       time.Time
	LastVerifiedAt        time.Time
}

func (t AppStoreTransaction) IsActive() bool { return t.RevokedAt == nil }

type VerifiedTransaction struct {
	TransactionID         string
	OriginalTransactionID string
	BundleID              string
	ProductID             string
	Type                  string
	AppAccountToken       string
	Environment           string
	InAppOwnershipType    string
	PurchasedAt           time.Time
	SignedAt              time.Time
	RevokedAt             *time.Time
	RevocationReason      string
}

type Notification struct {
	Type        string
	Subtype     string
	UUID        string
	BundleID    string
	AppAppleID  string
	Environment string
	Transaction *VerifiedTransaction
	SignedAt    time.Time
}

const (
	NotificationRefund         = "REFUND"
	NotificationRevoke         = "REVOKE"
	NotificationRefundReversed = "REFUND_REVERSED"
)

const ProductTypeNonConsumable = "Non-Consumable"

const OwnershipPurchased = "PURCHASED"

type AccountEntitlement struct {
	State        State
	ItemCapacity int
}

func Resolve(transactions []AppStoreTransaction, capacities ItemCapacities) AccountEntitlement {
	state := StateFree
	for _, t := range transactions {
		if t.IsActive() {
			state = StatePaid
			break
		}
		state = StateRevoked
	}
	return AccountEntitlement{State: state, ItemCapacity: capacities.CapacityFor(state)}
}

var (
	ErrInvalidCapacities = errors.New("entitlement: capacities must be non-negative with paid > free")

	ErrInvalidSignedTransaction = errors.New("entitlement: signed transaction did not verify")
	ErrVerificationUnavailable  = errors.New("entitlement: verification unavailable")

	ErrPolicyIncomplete = errors.New("entitlement: app store policy is incomplete for this environment")

	ErrWrongBundle       = errors.New("entitlement: transaction is for another app")
	ErrWrongAppAppleID   = errors.New("entitlement: notification is for another App Apple ID")
	ErrWrongProduct      = errors.New("entitlement: transaction is for another product")
	ErrWrongEnvironment  = errors.New("entitlement: transaction environment is not accepted by this deployment")
	ErrNotNonConsumable  = errors.New("entitlement: transaction is not a non-consumable purchase")
	ErrFamilyShared      = errors.New("entitlement: family-shared transactions are not accepted")
	ErrNoAppAccountToken = errors.New("entitlement: transaction carries no app account token")

	ErrAppAccountTokenMismatch          = errors.New("entitlement: app account token does not belong to this account")
	ErrTransactionBoundToAnotherAccount = errors.New("entitlement: transaction is already bound to another account")

	ErrTransactionNotFound    = errors.New("entitlement: transaction not found")
	ErrUnknownAppAccountToken = errors.New("entitlement: unknown app account token")
)
