package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"muse-backend/internal/platform/config"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(os.NewFile(0, os.DevNull), nil)) }

func productionConfig() config.Config {
	return config.Config{
		Environment:         config.Production,
		AppleBundleID:       "com.muse.app",
		AppStoreProductID:   "com.muse.collection_capacity",
		AppStoreAppAppleID:  "6740000001",
		AppStoreEnvironment: "Production",
	}
}

func TestEntitlementAdapter_ProductionPolicy_RequiresRealIdentity_AndRefusesPlaceholders(t *testing.T) {
	cases := map[string]struct {
		mutate func(c *config.Config)
		want   string
	}{
		"missing product id":           {func(c *config.Config) { c.AppStoreProductID = "" }, "APP_STORE_CAPACITY_PRODUCT_ID"},
		"DEV placeholder product id":   {func(c *config.Config) { c.AppStoreProductID = "dev.muse.placeholder.collection_capacity" }, "placeholder"},
		"missing App Apple ID":         {func(c *config.Config) { c.AppStoreAppAppleID = "" }, "APP_STORE_APP_APPLE_ID"},
		"missing environment":          {func(c *config.Config) { c.AppStoreEnvironment = "" }, "APP_STORE_ENVIRONMENT"},
		"Sandbox environment":          {func(c *config.Config) { c.AppStoreEnvironment = "Sandbox" }, "Production"},
		"Xcode environment":            {func(c *config.Config) { c.AppStoreEnvironment = "Xcode" }, "Production"},
		"missing bundle id":            {func(c *config.Config) { c.AppleBundleID = "" }, "APPLE_BUNDLE_ID"},
		"whitespace-only App Apple ID": {func(c *config.Config) { c.AppStoreAppAppleID = "   " }, "APP_STORE_APP_APPLE_ID"},
	}
	for name, tc := range cases {
		cfg := productionConfig()
		tc.mutate(&cfg)
		_, err := appStorePolicy(cfg)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: expected an error naming %q, got %v", name, tc.want, err)
		}
	}

	policy, err := appStorePolicy(productionConfig())
	if err != nil {
		t.Fatalf("a fully configured production policy is accepted: %v", err)
	}
	if !policy.Production || policy.Environment != "Production" || policy.AppAppleID != "6740000001" ||
		len(policy.ProductIDs) != 1 || policy.ProductIDs[0] != "com.muse.collection_capacity" || len(policy.LocalTestingEnvironments) != 0 {
		t.Fatalf("%+v", policy)
	}
}

func TestEntitlementAdapter_DevelopmentPolicy_DefaultsToSandbox_AndAcceptsThePlaceholder(t *testing.T) {
	cfg := config.Config{
		Environment:       config.Development,
		AppleBundleID:     "com.muse.app",
		AppStoreProductID: "dev.muse.placeholder.collection_capacity",
	}
	policy, err := appStorePolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if policy.Production || policy.Environment != "Sandbox" || policy.AppAppleID != "" ||
		strings.Join(policy.LocalTestingEnvironments, ",") != "Xcode,LocalTesting" {
		t.Fatalf("%+v", policy)
	}
	cfg.AppStoreProductID = ""
	if _, err := appStorePolicy(cfg); err == nil {
		t.Fatal("development still requires APP_STORE_CAPACITY_PRODUCT_ID")
	}
	cfg.AppStoreProductID, cfg.AppleBundleID = "dev.muse.placeholder.collection_capacity", ""
	if _, err := appStorePolicy(cfg); err == nil {
		t.Fatal("development still requires APPLE_BUNDLE_ID")
	}
}

func TestEntitlementAdapter_ProductionVerifier_RefusesExtraRoots_AndCannotDisableRevocationChecks(t *testing.T) {
	dir := t.TempDir()
	pemPath := filepath.Join(dir, "storekit-test.pem")
	if err := os.WriteFile(pemPath, []byte("-----BEGIN CERTIFICATE-----\nAA==\n-----END CERTIFICATE-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := productionConfig()
	cfg.AppStoreExtraTrustRootsPEMPath = pemPath
	if _, err := appStoreVerifier(cfg, quietLogger()); err == nil || !strings.Contains(err.Error(), "not permitted in production") {
		t.Fatalf("extra roots in production must refuse start-up, got %v", err)
	}

	cfg = productionConfig()
	cfg.AppStoreOnlineChecks = "disabled"
	if _, err := appStoreVerifier(cfg, quietLogger()); err == nil || !strings.Contains(err.Error(), "cannot be disabled") {
		t.Fatalf("disabling revocation checks in production must refuse start-up, got %v", err)
	}

	verifier, err := appStoreVerifier(productionConfig(), quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	if !verifier.OnlineChecksEnabled() {
		t.Fatal("production verifier must have online revocation checks on")
	}
}

func TestEntitlementAdapter_DevelopmentVerifier_OnlineChecksAreOptIn(t *testing.T) {
	cfg := config.Config{Environment: config.Development}
	verifier, err := appStoreVerifier(cfg, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	if verifier.OnlineChecksEnabled() {
		t.Fatal("development online checks default off (generated/StoreKit-Test chains have no responder)")
	}
	cfg.AppStoreOnlineChecks = "enabled"
	verifier, err = appStoreVerifier(cfg, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	if !verifier.OnlineChecksEnabled() {
		t.Fatal("APP_STORE_ONLINE_CHECKS=enabled turns them on")
	}
}
