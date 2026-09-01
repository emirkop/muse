package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"muse-backend/internal/entitlement/domain"
)

type AppStorePolicy struct {
	Production               bool
	BundleID                 string
	AppAppleID               string
	ProductIDs               []string
	Environment              string
	LocalTestingEnvironments []string
}

const DevPlaceholderProductPrefix = "dev.muse.placeholder."

func (p AppStorePolicy) Validate() error {
	if strings.TrimSpace(p.BundleID) == "" || len(p.ProductIDs) == 0 || strings.TrimSpace(p.Environment) == "" {
		return domain.ErrPolicyIncomplete
	}
	for _, id := range p.ProductIDs {
		if strings.TrimSpace(id) == "" {
			return domain.ErrPolicyIncomplete
		}
		if p.Production && strings.HasPrefix(id, DevPlaceholderProductPrefix) {
			return fmt.Errorf("%w: development placeholder product id %q is not permitted in production", domain.ErrPolicyIncomplete, id)
		}
	}
	if p.Production {
		if !strings.EqualFold(p.Environment, "Production") {
			return fmt.Errorf("%w: production must expect the Production environment, not %q", domain.ErrPolicyIncomplete, p.Environment)
		}
		if strings.TrimSpace(p.AppAppleID) == "" {
			return fmt.Errorf("%w: APP_STORE_APP_APPLE_ID is required in production", domain.ErrPolicyIncomplete)
		}
		if len(p.LocalTestingEnvironments) != 0 {
			return fmt.Errorf("%w: local-testing environments are not permitted in production", domain.ErrPolicyIncomplete)
		}
	}
	return nil
}

func (p AppStorePolicy) allowsEnvironment(environment string) bool {
	if strings.EqualFold(p.Environment, environment) {
		return true
	}
	if p.Production {
		return false
	}
	for _, local := range p.LocalTestingEnvironments {
		if strings.EqualFold(local, environment) {
			return true
		}
	}
	return false
}

func (p AppStorePolicy) grantsProduct(productID string) bool {
	for _, id := range p.ProductIDs {
		if id == productID {
			return true
		}
	}
	return false
}

type EntitlementService struct {
	transactions TransactionRepository
	tokens       AppAccountTokenRepository
	items        CollectionItemCounting
	verifier     SignedTransactionVerifying
	policy       AppStorePolicy
	capacities   domain.ItemCapacities
	clock        Clock
}

var _ EntitlementProviding = (*EntitlementService)(nil)

func NewEntitlementService(
	transactions TransactionRepository,
	tokens AppAccountTokenRepository,
	items CollectionItemCounting,
	verifier SignedTransactionVerifying,
	policy AppStorePolicy,
	capacities domain.ItemCapacities,
	clock Clock,
) (*EntitlementService, error) {
	if err := capacities.Validate(); err != nil {
		return nil, err
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if clock == nil {
		clock = time.Now
	}
	return &EntitlementService{
		transactions: transactions,
		tokens:       tokens,
		items:        items,
		verifier:     verifier,
		policy:       policy,
		capacities:   capacities,
		clock:        clock,
	}, nil
}

func (s *EntitlementService) Capacities() domain.ItemCapacities { return s.capacities }

func (s *EntitlementService) AppAccountToken(ctx context.Context, accountID string) (string, error) {
	token, err := s.tokens.EnsureToken(ctx, accountID)
	if err != nil {
		return "", fmt.Errorf("ensure app account token: %w", err)
	}
	return token, nil
}

type AccountStatus struct {
	Entitlement domain.AccountEntitlement
	ItemCount   int
}

func (s *EntitlementService) Status(ctx context.Context, accountID string) (AccountStatus, error) {
	entitlement, err := s.entitlement(ctx, accountID)
	if err != nil {
		return AccountStatus{}, err
	}
	count, err := s.items.CountItemsForAccount(ctx, accountID)
	if err != nil {
		return AccountStatus{}, fmt.Errorf("count items: %w", err)
	}
	return AccountStatus{Entitlement: entitlement, ItemCount: count}, nil
}

func (s *EntitlementService) RedeemSignedTransaction(ctx context.Context, accountID, signedTransaction string) (AccountStatus, error) {
	verified, err := s.verifier.VerifyTransaction(ctx, signedTransaction)
	if err != nil {
		return AccountStatus{}, err
	}
	if err := s.applyPolicy(verified); err != nil {
		return AccountStatus{}, err
	}

	if verified.AppAccountToken == "" {
		return AccountStatus{}, domain.ErrNoAppAccountToken
	}
	owner, err := s.tokens.AccountForToken(ctx, verified.AppAccountToken)
	if err != nil {
		if errors.Is(err, domain.ErrUnknownAppAccountToken) {
			return AccountStatus{}, domain.ErrAppAccountTokenMismatch
		}
		return AccountStatus{}, fmt.Errorf("resolve app account token: %w", err)
	}
	if owner != accountID {
		return AccountStatus{}, domain.ErrAppAccountTokenMismatch
	}

	now := s.clock()
	if _, err := s.transactions.Bind(ctx, domain.AppStoreTransaction{
		OriginalTransactionID: verified.OriginalTransactionID,
		TransactionID:         verified.TransactionID,
		AccountID:             accountID,
		ProductID:             verified.ProductID,
		BundleID:              verified.BundleID,
		Environment:           verified.Environment,
		AppAccountToken:       verified.AppAccountToken,
		PurchasedAt:           verified.PurchasedAt,
		RevokedAt:             verified.RevokedAt,
		RevocationReason:      verified.RevocationReason,
		FirstVerifiedAt:       now,
		LastVerifiedAt:        now,
	}); err != nil {
		return AccountStatus{}, err
	}
	return s.Status(ctx, accountID)
}

func (s *EntitlementService) applyPolicy(t domain.VerifiedTransaction) error {
	switch {
	case t.BundleID != s.policy.BundleID:
		return domain.ErrWrongBundle
	case !s.policy.allowsEnvironment(t.Environment):
		return domain.ErrWrongEnvironment
	case !s.policy.grantsProduct(t.ProductID):
		return domain.ErrWrongProduct
	case t.Type != domain.ProductTypeNonConsumable:
		return domain.ErrNotNonConsumable
	case t.InAppOwnershipType != domain.OwnershipPurchased:
		return domain.ErrFamilyShared
	}
	return nil
}

func (s *EntitlementService) applyNotificationPolicy(n domain.Notification) error {
	switch {
	case n.BundleID != s.policy.BundleID:
		return domain.ErrWrongBundle
	case s.policy.Production && n.AppAppleID != s.policy.AppAppleID:
		return domain.ErrWrongAppAppleID
	case !s.policy.allowsEnvironment(n.Environment):
		return domain.ErrWrongEnvironment
	}
	return nil
}

func (s *EntitlementService) ApplyNotification(ctx context.Context, signedPayload string) error {
	notification, err := s.verifier.VerifyNotification(ctx, signedPayload)
	if err != nil {
		return err
	}
	if notification.Transaction == nil {
		return nil
	}
	if err := s.applyNotificationPolicy(notification); err != nil {
		return err
	}
	if err := s.applyPolicy(*notification.Transaction); err != nil {
		return err
	}
	switch notification.Type {
	case domain.NotificationRefund, domain.NotificationRevoke:
		revokedAt := notification.Transaction.RevokedAt
		if revokedAt == nil {
			at := notification.SignedAt
			if at.IsZero() {
				at = s.clock()
			}
			revokedAt = &at
		}
		_, err := s.transactions.SetRevocation(ctx, notification.Transaction.OriginalTransactionID, revokedAt, notification.Transaction.RevocationReason)
		return err
	case domain.NotificationRefundReversed:
		_, err := s.transactions.SetRevocation(ctx, notification.Transaction.OriginalTransactionID, nil, "")
		return err
	default:
		return nil
	}
}

func (s *EntitlementService) MayAddCollectionItem(ctx context.Context, accountID string, _ string) (bool, error) {
	entitlement, err := s.entitlement(ctx, accountID)
	if err != nil {
		return false, err
	}
	if entitlement.State == domain.StateUnavailable {
		return false, domain.ErrVerificationUnavailable
	}
	count, err := s.items.CountItemsForAccount(ctx, accountID)
	if err != nil {
		return false, fmt.Errorf("count items: %w", err)
	}
	return count < entitlement.ItemCapacity, nil
}

func (s *EntitlementService) entitlement(ctx context.Context, accountID string) (domain.AccountEntitlement, error) {
	transactions, err := s.transactions.ListForAccount(ctx, accountID)
	if err != nil {
		return domain.AccountEntitlement{State: domain.StateUnavailable}, fmt.Errorf("list transactions: %w", err)
	}
	return domain.Resolve(transactions, s.capacities), nil
}
