package main

import (
	"fmt"
	"net/url"

	"muse-backend/internal/platform/config"
)

const minimumMetricsTokenBytes = 32

func productionConfigurationErrors(cfg config.Config) []error {
	if cfg.Environment != config.Production {
		return nil
	}
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if cfg.MetricsToken == "" {
		fail("METRICS_TOKEN is required in production — without it /metrics answers 404 and no alert in observability.Rules can be evaluated")
	}
	if cfg.MetricsToken != "" && len(cfg.MetricsToken) < minimumMetricsTokenBytes {
		fail("METRICS_TOKEN must be at least %d bytes in production; got %d",
			minimumMetricsTokenBytes, len(cfg.MetricsToken))
	}

	switch {
	case cfg.SessionSigningKey == "":
		fail("SESSION_SIGNING_KEY is required in production — an ephemeral key is a development convenience, not a secret")
	case len(cfg.SessionSigningKey) < minimumSessionSigningKeyBytes:
		fail("SESSION_SIGNING_KEY must be at least %d bytes in production (HS256, RFC 7518 §3.2); got %d",
			minimumSessionSigningKeyBytes, len(cfg.SessionSigningKey))
	}

	if cfg.DatabaseURL == "" {
		fail("DATABASE_URL is required in production — the in-memory account resolver is a foundation-phase stand-in")
	}

	switch cfg.ObjectStorageDriver {
	case "", "r2":
	case "filesystem":
		fail("OBJECT_STORAGE_DRIVER=filesystem is not permitted in production — it is the explicitly non-production dev adapter")
	default:
		fail("OBJECT_STORAGE_DRIVER=%q is not a known driver", cfg.ObjectStorageDriver)
	}

	switch cfg.EmailSenderDriver {
	case "resend":
		if cfg.ResendAPIKey == "" || cfg.EmailFromAddress == "" {
			fail("EMAIL_SENDER_DRIVER=resend requires RESEND_API_KEY and EMAIL_FROM_ADDRESS in production — falling back to the log sender is not permitted")
		}
	default:
		fail("EMAIL_SENDER_DRIVER must be \"resend\" in production — the log sender is the explicitly non-production adapter and delivers no verification or reset email")
	}

	if cfg.PublicBaseURL == "" {
		fail("PUBLIC_BASE_URL is required in production")
	} else if err := requireHTTPS("PUBLIC_BASE_URL", cfg.PublicBaseURL); err != nil {
		errs = append(errs, err)
	}
	for name, value := range map[string]string{
		"SHARE_LINK_BASE_URL":          cfg.ShareLinkBaseURL,
		"ASSET_BUNDLE_PUBLIC_BASE_URL": cfg.AssetBundlePublicBaseURL,
		"APP_STORE_URL":                cfg.AppStoreURL,
		"R2_ENDPOINT":                  cfg.R2Endpoint,
	} {
		if value == "" {
			continue
		}
		if err := requireHTTPS(name, value); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

const minimumSessionSigningKeyBytes = 32

func requireHTTPS(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s must be an absolute URL in production; got %q", name, raw)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%s must use https in production; got scheme %q", name, u.Scheme)
	}
	return nil
}
