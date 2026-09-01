package main

import (
	"strings"
	"testing"

	"muse-backend/internal/platform/config"
)

func fullyConfiguredProduction() config.Config {
	return config.Config{
		Environment:         config.Production,
		Port:                "8080",
		DatabaseURL:         "postgres://user:pass@db.internal:5432/muse",
		SessionSigningKey:   strings.Repeat("k", 48),
		MetricsToken:        strings.Repeat("m", 48),
		ObjectStorageDriver: "r2",
		EmailSenderDriver:   "resend",
		ResendAPIKey:        "re_live_placeholder_for_test",
		EmailFromAddress:    "no-reply@muse.example",
		PublicBaseURL:       "https://api.muse.example",
		ShareLinkBaseURL:    "https://muse.example",
	}
}

func TestProductionGuard_FullyConfiguredProduction_StartsCleanly(t *testing.T) {
	if errs := productionConfigurationErrors(fullyConfiguredProduction()); len(errs) != 0 {
		t.Fatalf("a fully configured production config must pass, got %v", errs)
	}
}

func TestProductionGuard_DoesNotApplyOutsideProduction(t *testing.T) {
	for _, env := range []config.Environment{config.Development, config.Staging} {
		cfg := config.Config{Environment: env}
		if errs := productionConfigurationErrors(cfg); len(errs) != 0 {
			t.Fatalf("%s must not be guarded, got %v", env, errs)
		}
	}
}

func TestProductionGuard_EachSecurityValueIsRequiredOrRefused(t *testing.T) {
	cases := map[string]struct {
		mutate func(*config.Config)
		want   string
	}{
		"no signing key":        {func(c *config.Config) { c.SessionSigningKey = "" }, "SESSION_SIGNING_KEY is required"},
		"short signing key":     {func(c *config.Config) { c.SessionSigningKey = "too-short" }, "at least 32 bytes"},
		"no database":           {func(c *config.Config) { c.DatabaseURL = "" }, "DATABASE_URL is required"},
		"filesystem storage":    {func(c *config.Config) { c.ObjectStorageDriver = "filesystem" }, "not permitted in production"},
		"unknown storage":       {func(c *config.Config) { c.ObjectStorageDriver = "s3-ish" }, "not a known driver"},
		"resend without key":    {func(c *config.Config) { c.ResendAPIKey = "" }, "requires RESEND_API_KEY"},
		"resend without from":   {func(c *config.Config) { c.EmailFromAddress = "" }, "requires RESEND_API_KEY and EMAIL_FROM_ADDRESS"},
		"log email sender":      {func(c *config.Config) { c.EmailSenderDriver = "" }, "EMAIL_SENDER_DRIVER must be \"resend\""},
		"no public base url":    {func(c *config.Config) { c.PublicBaseURL = "" }, "PUBLIC_BASE_URL is required"},
		"http public base url":  {func(c *config.Config) { c.PublicBaseURL = "http://api.muse.example" }, "PUBLIC_BASE_URL must use https"},
		"relative public url":   {func(c *config.Config) { c.PublicBaseURL = "api.muse.example" }, "must be an absolute URL"},
		"http share link base":  {func(c *config.Config) { c.ShareLinkBaseURL = "http://muse.example" }, "SHARE_LINK_BASE_URL must use https"},
		"http asset bundle url": {func(c *config.Config) { c.AssetBundlePublicBaseURL = "http://cdn.muse.example" }, "ASSET_BUNDLE_PUBLIC_BASE_URL must use https"},
		"http app store url":    {func(c *config.Config) { c.AppStoreURL = "http://apps.apple.com/x" }, "APP_STORE_URL must use https"},
		"http r2 endpoint":      {func(c *config.Config) { c.R2Endpoint = "http://emulator:9000" }, "R2_ENDPOINT must use https"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := fullyConfiguredProduction()
			tc.mutate(&cfg)
			errs := productionConfigurationErrors(cfg)
			if len(errs) == 0 {
				t.Fatalf("expected a refusal mentioning %q, got none", tc.want)
			}
			var joined []string
			for _, err := range errs {
				joined = append(joined, err.Error())
			}
			if !strings.Contains(strings.Join(joined, "\n"), tc.want) {
				t.Fatalf("refusal must name the problem %q; got %v", tc.want, joined)
			}
		})
	}
}

func TestProductionGuard_ReportsEveryViolationAtOnce(t *testing.T) {
	cfg := config.Config{Environment: config.Production}
	errs := productionConfigurationErrors(cfg)
	if len(errs) < 4 {
		t.Fatalf("an empty production config has at least four independent problems, got %d: %v", len(errs), errs)
	}
}

func TestProductionGuard_SigningKeyFloorIsExactlyHS256HashSize(t *testing.T) {
	cfg := fullyConfiguredProduction()
	cfg.SessionSigningKey = strings.Repeat("k", 32)
	if errs := productionConfigurationErrors(cfg); len(errs) != 0 {
		t.Fatalf("32 bytes must pass, got %v", errs)
	}
	cfg.SessionSigningKey = strings.Repeat("k", 31)
	if errs := productionConfigurationErrors(cfg); len(errs) == 0 {
		t.Fatal("31 bytes must be refused")
	}
}

func TestProductionGuard_NoObjectStorageIsDegradedNotRefused(t *testing.T) {
	cfg := fullyConfiguredProduction()
	cfg.ObjectStorageDriver = ""
	if errs := productionConfigurationErrors(cfg); len(errs) != 0 {
		t.Fatalf("no object storage answers 503 on photo routes and is not a security failure, got %v", errs)
	}
}

func TestProductionGuard_MetricsTokenIsRequiredInProduction(t *testing.T) {
	cfg := fullyConfiguredProduction()
	cfg.MetricsToken = ""
	if errs := productionConfigurationErrors(cfg); len(errs) == 0 {
		t.Fatal("production without METRICS_TOKEN must be refused")
	}

	cfg.MetricsToken = "short"
	errs := productionConfigurationErrors(cfg)
	if len(errs) == 0 {
		t.Fatal("a short METRICS_TOKEN must be refused")
	}

	for _, environment := range []config.Environment{config.Development, config.Staging} {
		relaxed := fullyConfiguredProduction()
		relaxed.Environment = environment
		relaxed.MetricsToken = ""
		if errs := productionConfigurationErrors(relaxed); len(errs) != 0 {
			t.Errorf("%s must not require a metrics token: %v", environment, errs)
		}
	}
}
