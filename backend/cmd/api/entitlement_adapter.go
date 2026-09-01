package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	collectioninfra "muse-backend/internal/collection/infrastructure"
	entitlementapp "muse-backend/internal/entitlement/application"
	entitlementdomain "muse-backend/internal/entitlement/domain"
	entitlementinfra "muse-backend/internal/entitlement/infrastructure"
	"muse-backend/internal/platform/config"
)

type collectionForEntitlement struct {
	rooms *collectioninfra.PostgresCollectionRoomRepository
}

var _ entitlementapp.CollectionItemCounting = collectionForEntitlement{}

func (a collectionForEntitlement) CountItemsForAccount(ctx context.Context, accountID string) (int, error) {
	return a.rooms.CountItemsForAccount(ctx, accountID)
}

const (
	devPlaceholderFreeItemCapacity = 5
	devPlaceholderPaidItemCapacity = 12
)

func entitlementCapacities(cfg config.Config, logger *slog.Logger) (entitlementdomain.ItemCapacities, error) {
	freeSet := strings.TrimSpace(cfg.EntitlementFreeItemCapacity) != ""
	paidSet := strings.TrimSpace(cfg.EntitlementPaidItemCapacity) != ""

	if !freeSet && !paidSet {
		if cfg.Environment == config.Production {
			return entitlementdomain.ItemCapacities{}, fmt.Errorf(
				"ENTITLEMENT_FREE_ITEM_CAPACITY and ENTITLEMENT_PAID_ITEM_CAPACITY are required in production — no product number is decided, and a placeholder may not ship")
		}
		caps := entitlementdomain.ItemCapacities{
			Free:   devPlaceholderFreeItemCapacity,
			Paid:   devPlaceholderPaidItemCapacity,
			Source: "DEV PLACEHOLDER (not a product decision)",
		}
		logger.Warn("entitlement capacities are the DEV PLACEHOLDER — not product numbers",
			"free_item_capacity", caps.Free, "paid_item_capacity", caps.Paid)
		return caps, nil
	}
	if !freeSet || !paidSet {
		return entitlementdomain.ItemCapacities{}, fmt.Errorf("ENTITLEMENT_FREE_ITEM_CAPACITY and ENTITLEMENT_PAID_ITEM_CAPACITY must be set together")
	}
	free, err := strconv.Atoi(strings.TrimSpace(cfg.EntitlementFreeItemCapacity))
	if err != nil {
		return entitlementdomain.ItemCapacities{}, fmt.Errorf("ENTITLEMENT_FREE_ITEM_CAPACITY: %w", err)
	}
	paid, err := strconv.Atoi(strings.TrimSpace(cfg.EntitlementPaidItemCapacity))
	if err != nil {
		return entitlementdomain.ItemCapacities{}, fmt.Errorf("ENTITLEMENT_PAID_ITEM_CAPACITY: %w", err)
	}
	caps := entitlementdomain.ItemCapacities{Free: free, Paid: paid, Source: "configuration"}
	if err := caps.Validate(); err != nil {
		return entitlementdomain.ItemCapacities{}, err
	}
	logger.Info("entitlement capacities configured", "free_item_capacity", free, "paid_item_capacity", paid)
	return caps, nil
}

func appStorePolicy(cfg config.Config) (entitlementapp.AppStorePolicy, error) {
	production := cfg.Environment == config.Production
	environment := strings.TrimSpace(cfg.AppStoreEnvironment)
	policy := entitlementapp.AppStorePolicy{
		Production: production,
		BundleID:   strings.TrimSpace(cfg.AppleBundleID),
		AppAppleID: strings.TrimSpace(cfg.AppStoreAppAppleID),
		ProductIDs: []string{strings.TrimSpace(cfg.AppStoreProductID)},
	}
	if production {
		if environment == "" {
			return entitlementapp.AppStorePolicy{}, fmt.Errorf("APP_STORE_ENVIRONMENT=Production is required in production")
		}
		policy.Environment = environment
	} else {
		if environment == "" {
			environment = "Sandbox"
		}
		policy.Environment = environment
		policy.LocalTestingEnvironments = []string{"Xcode", "LocalTesting"}
	}
	if policy.BundleID == "" {
		return entitlementapp.AppStorePolicy{}, fmt.Errorf("APPLE_BUNDLE_ID is required to verify App Store purchases")
	}
	if policy.ProductIDs[0] == "" {
		return entitlementapp.AppStorePolicy{}, fmt.Errorf("APP_STORE_CAPACITY_PRODUCT_ID is required to verify App Store purchases")
	}
	if err := policy.Validate(); err != nil {
		return entitlementapp.AppStorePolicy{}, fmt.Errorf("app store policy: %w", err)
	}
	return policy, nil
}

func appStoreVerifier(cfg config.Config, logger *slog.Logger) (*entitlementinfra.AppStoreJWSVerifier, error) {
	extraPath := strings.TrimSpace(cfg.AppStoreExtraTrustRootsPEMPath)
	if cfg.Environment == config.Production {
		if extraPath != "" {
			return nil, fmt.Errorf("APP_STORE_EXTRA_TRUST_ROOTS_PEM is not permitted in production — only Apple's pinned roots are trusted there")
		}
		if strings.TrimSpace(cfg.AppStoreOnlineChecks) != "" && !strings.EqualFold(strings.TrimSpace(cfg.AppStoreOnlineChecks), "enabled") {
			return nil, fmt.Errorf("APP_STORE_ONLINE_CHECKS cannot be disabled in production — revocation checking is always on")
		}
		return entitlementinfra.NewProductionVerifier(nil)
	}
	extra := ""
	if extraPath != "" {
		pem, err := os.ReadFile(extraPath)
		if err != nil {
			return nil, fmt.Errorf("read APP_STORE_EXTRA_TRUST_ROOTS_PEM: %w", err)
		}
		extra = string(pem)
		logger.Warn("extra App Store JWS trust roots loaded (development only)", "path", extraPath)
	}
	online := strings.EqualFold(strings.TrimSpace(cfg.AppStoreOnlineChecks), "enabled")
	if !online {
		logger.Warn("App Store JWS online revocation checks are OFF (development only; always on in production)")
	}
	return entitlementinfra.NewDevelopmentVerifier(extra, online, nil)
}
