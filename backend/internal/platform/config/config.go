package config

import "os"

type Environment string

const (
	Development Environment = "development"
	Staging     Environment = "staging"
	Production  Environment = "production"
)

const defaultPort = "8080"

type Config struct {
	Environment       Environment
	Port              string
	DatabaseURL       string
	AppleBundleID     string
	GoogleClientID    string
	SessionSigningKey string

	ObjectStorageDriver  string
	R2AccountID          string
	R2Endpoint           string
	R2Bucket             string
	R2AccessKeyID        string
	R2SecretAccessKey    string
	DevStorageRoot       string
	PublicBaseURL        string
	MediaReclaimInterval string

	AssetBundlePublicBaseURL string

	EmailSenderDriver   string
	ResendAPIKey        string
	EmailFromAddress    string
	EmailOutboxInterval string

	MetricsToken string

	ShareLinkBaseURL string
	AppStoreURL      string
	AppleTeamID      string

	EntitlementFreeItemCapacity    string
	EntitlementPaidItemCapacity    string
	AppStoreProductID              string
	AppStoreExtraTrustRootsPEMPath string
	AppStoreAppAppleID             string
	AppStoreEnvironment            string
	AppStoreOnlineChecks           string
}

func Load() Config {
	return Config{
		Environment:       loadEnvironment(),
		Port:              loadPort(),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		AppleBundleID:     os.Getenv("APPLE_BUNDLE_ID"),
		GoogleClientID:    os.Getenv("GOOGLE_CLIENT_ID"),
		SessionSigningKey: os.Getenv("SESSION_SIGNING_KEY"),

		ObjectStorageDriver:  os.Getenv("OBJECT_STORAGE_DRIVER"),
		R2AccountID:          os.Getenv("R2_ACCOUNT_ID"),
		R2Endpoint:           os.Getenv("R2_ENDPOINT"),
		R2Bucket:             os.Getenv("R2_BUCKET"),
		R2AccessKeyID:        os.Getenv("R2_ACCESS_KEY_ID"),
		R2SecretAccessKey:    os.Getenv("R2_SECRET_ACCESS_KEY"),
		DevStorageRoot:       os.Getenv("DEV_STORAGE_ROOT"),
		PublicBaseURL:        os.Getenv("PUBLIC_BASE_URL"),
		MediaReclaimInterval: os.Getenv("MEDIA_RECLAIM_INTERVAL"),

		AssetBundlePublicBaseURL: os.Getenv("ASSET_BUNDLE_PUBLIC_BASE_URL"),

		EmailSenderDriver:   os.Getenv("EMAIL_SENDER_DRIVER"),
		ResendAPIKey:        os.Getenv("RESEND_API_KEY"),
		EmailFromAddress:    os.Getenv("EMAIL_FROM_ADDRESS"),
		EmailOutboxInterval: os.Getenv("EMAIL_OUTBOX_INTERVAL"),
		MetricsToken:        os.Getenv("METRICS_TOKEN"),

		ShareLinkBaseURL: os.Getenv("SHARE_LINK_BASE_URL"),
		AppStoreURL:      os.Getenv("APP_STORE_URL"),
		AppleTeamID:      os.Getenv("APPLE_TEAM_ID"),

		EntitlementFreeItemCapacity:    os.Getenv("ENTITLEMENT_FREE_ITEM_CAPACITY"),
		EntitlementPaidItemCapacity:    os.Getenv("ENTITLEMENT_PAID_ITEM_CAPACITY"),
		AppStoreProductID:              os.Getenv("APP_STORE_CAPACITY_PRODUCT_ID"),
		AppStoreExtraTrustRootsPEMPath: os.Getenv("APP_STORE_EXTRA_TRUST_ROOTS_PEM"),
		AppStoreAppAppleID:             os.Getenv("APP_STORE_APP_APPLE_ID"),
		AppStoreEnvironment:            os.Getenv("APP_STORE_ENVIRONMENT"),
		AppStoreOnlineChecks:           os.Getenv("APP_STORE_ONLINE_CHECKS"),
	}
}

func loadEnvironment() Environment {
	switch Environment(os.Getenv("APP_ENV")) {
	case Staging:
		return Staging
	case Production:
		return Production
	default:
		return Development
	}
}

func loadPort() string {
	if port := os.Getenv("APP_PORT"); port != "" {
		return port
	}
	return defaultPort
}
