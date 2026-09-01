package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	analyticsapp "muse-backend/internal/analytics/application"
	analyticsdomain "muse-backend/internal/analytics/domain"
	analyticsinfra "muse-backend/internal/analytics/infrastructure"
	analyticsiface "muse-backend/internal/analytics/interfaces"
	catalogapp "muse-backend/internal/catalog/application"
	catalinfra "muse-backend/internal/catalog/infrastructure"
	cataliface "muse-backend/internal/catalog/interfaces"
	collectionapp "muse-backend/internal/collection/application"
	collectioninfra "muse-backend/internal/collection/infrastructure"
	collectioniface "muse-backend/internal/collection/interfaces"
	entitlementapp "muse-backend/internal/entitlement/application"
	entitlementinfra "muse-backend/internal/entitlement/infrastructure"
	entitlementiface "muse-backend/internal/entitlement/interfaces"
	identityapp "muse-backend/internal/identity/application"
	identityinfra "muse-backend/internal/identity/infrastructure"
	identityiface "muse-backend/internal/identity/interfaces"
	mediaapp "muse-backend/internal/media/application"
	mediainfra "muse-backend/internal/media/infrastructure"
	mediaiface "muse-backend/internal/media/interfaces"
	museumapp "muse-backend/internal/museum/application"
	museuminfra "muse-backend/internal/museum/infrastructure"
	museumiface "muse-backend/internal/museum/interfaces"
	"muse-backend/internal/platform/config"
	"muse-backend/internal/platform/database"
	"muse-backend/internal/platform/featureflag"
	platformhttp "muse-backend/internal/platform/http"
	"muse-backend/internal/platform/objectstore"
	"muse-backend/internal/platform/observability"
	sharingapp "muse-backend/internal/sharing/application"
	sharinginfra "muse-backend/internal/sharing/infrastructure"
	sharingiface "muse-backend/internal/sharing/interfaces"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour
	sessionTTL      = 180 * 24 * time.Hour

	jwksCacheTTL = 24 * time.Hour
)

const (
	uploadURLTTL         = 5 * time.Minute
	downloadURLTTL       = 5 * time.Minute
	reclaimAfter         = 24 * time.Hour
	reclaimBatch         = 200
	releasedReclaimAfter = 1 * time.Hour
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg := config.Load()
	logger.Info("configuration loaded", "environment", cfg.Environment, "port", cfg.Port)

	if errs := productionConfigurationErrors(cfg); len(errs) > 0 {
		for _, err := range errs {
			logger.Error("production configuration refused", "error", err)
		}
		os.Exit(1)
	}

	flags, err := featureflag.NewProvider(string(cfg.Environment), os.Environ())
	if err != nil {
		logger.Error("feature flag configuration refused", "error", err)
		os.Exit(1)
	}
	logFeatureFlags(logger, flags)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dbPool := connectDatabaseIfConfigured(ctx, logger, cfg)
	if dbPool != nil {
		defer dbPool.Close()
	}

	router := platformhttp.NewRouter()
	router.Handle("GET /health", platformhttp.HealthHandler)

	metrics := observability.NewRegistry()
	metrics.MarkUp()
	observability.UseRegistry(metrics)
	router.Handle("GET /health/ready", observability.ReadinessHandler(metrics, readinessProbe(dbPool)))
	router.Handle("GET /metrics", observability.MetricsHandler(metrics, cfg.MetricsToken))

	signingKey := sessionSigningKey(cfg, logger)

	identityHandlers, passwordService := buildIdentityHandlers(cfg, logger, dbPool, signingKey)
	startEmailOutboxDrainer(ctx, cfg, logger, passwordService)
	identityHandlers.RegisterRoutes(router)

	registerMuseumAndCatalogRoutes(ctx, cfg, flags, logger, dbPool, signingKey, router)

	server := platformhttp.NewServer(":"+cfg.Port, observability.Instrument(metrics, router, router))

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "port", cfg.Port)
		serverErrors <- server.Start(ctx)
	}()

	select {
	case err := <-serverErrors:
		if err != nil {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		logger.Info("shutdown signal received, stopping gracefully")

		shutdownCtx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
			os.Exit(1)
		}
		logger.Info("shutdown complete")
	}
}

func buildIdentityHandlers(cfg config.Config, logger *slog.Logger, dbPool *database.Pool, signingKey []byte) (*identityiface.Handlers, *identityapp.PasswordService) {
	appleJWKS := identityinfra.NewJWKSClient(identityinfra.AppleJWKSURL, nil, jwksCacheTTL)
	googleJWKS := identityinfra.NewJWKSClient(identityinfra.GoogleJWKSURL, nil, jwksCacheTTL)

	appleVerifier := identityinfra.NewAppleVerifier(cfg.AppleBundleID, appleJWKS)
	googleVerifier := identityinfra.NewGoogleVerifier(cfg.GoogleClientID, googleJWKS)
	providerVerifier := identityinfra.NewProviderVerifier(appleVerifier, googleVerifier)

	if cfg.AppleBundleID == "" {
		logger.Warn("APPLE_BUNDLE_ID not configured — Apple sign-in will reject every token until it is set")
	}
	if cfg.GoogleClientID == "" {
		logger.Warn("GOOGLE_CLIENT_ID not configured — Google sign-in will reject every token until it is set")
	}

	accessTokens := identityinfra.NewAccessTokenSigner(signingKey, "muse-backend", accessTokenTTL)
	refreshGen := identityinfra.OpaqueRefreshTokenGenerator{}

	sessions := identityinfra.NewInMemorySessionStore()
	logger.Warn("session persistence is in-memory only (foundation phase) — sessions do not survive a restart")

	accountService := buildAccountService(dbPool)
	if accountService == nil {
		logger.Warn("account persistence is in-memory only (no DATABASE_URL configured) — accounts do not survive a restart, and the Profile endpoints will return 503")
	} else {
		logger.Info("account persistence: PostgreSQL")
	}
	accounts := buildAccountResolver(accountService)

	login := identityapp.NewLoginService(providerVerifier, accounts, sessions, accessTokens, refreshGen, sessionTTL, refreshTokenTTL)
	refresh := identityapp.NewRefreshService(sessions, accessTokens, refreshGen, refreshTokenTTL)
	logout := identityapp.NewLogoutService(sessions)

	passwords := buildPasswordService(cfg, logger, dbPool, sessions, login)

	return identityiface.NewHandlers(login, refresh, logout, accountService, passwords, accessTokens, logger), passwords
}

func buildPasswordService(
	cfg config.Config,
	logger *slog.Logger,
	dbPool *database.Pool,
	sessions identityapp.SessionRepository,
	login *identityapp.LoginService,
) *identityapp.PasswordService {
	if dbPool == nil {
		logger.Warn("email/password authentication disabled (no DATABASE_URL configured) — /auth/email/* will return 503; Apple and Google sign-in are unaffected")
		return nil
	}

	pool := dbPool.Pool()
	credentials := identityinfra.NewPostgresPasswordRepository(pool)
	pending := identityinfra.NewPostgresPendingSignupRepository(pool)
	resets := identityinfra.NewPostgresPasswordResetRepository(pool)
	accountRepo := identityinfra.NewPostgresAccountRepository(pool)
	limiter := identityinfra.NewPostgresAttemptLimiter(pool, identityinfra.DefaultAttemptPolicy)

	hasher := identityinfra.NewDefaultArgon2idHasher()

	emailSender := buildEmailSender(cfg, logger)

	return identityapp.NewPasswordService(
		credentials, pending, resets, accountRepo, sessions, login,
		hasher, identityinfra.OpaqueRefreshTokenGenerator{}, emailSender, limiter,
		identityinfra.NewPostgresEmailOutbox(pool),
	).WithLogger(logger)
}

func buildEmailSender(cfg config.Config, logger *slog.Logger) identityapp.TransactionalEmailSender {
	switch cfg.EmailSenderDriver {
	case "resend":
		if cfg.ResendAPIKey == "" || cfg.EmailFromAddress == "" {
			logger.Error("EMAIL_SENDER_DRIVER=resend but RESEND_API_KEY or EMAIL_FROM_ADDRESS is unset — falling back to the NON-PRODUCTION log sender; no email will be delivered")
			return identityinfra.NewLogEmailSender(logger, cfg.PublicBaseURL)
		}
		logger.Info("transactional email: Resend")
		return identityinfra.NewResendEmailSender(cfg.ResendAPIKey, cfg.EmailFromAddress, cfg.PublicBaseURL, nil)
	default:
		logger.Warn("transactional email: NON-PRODUCTION log sender — verification and password-reset emails are not delivered; set EMAIL_SENDER_DRIVER=resend for real delivery")
		return identityinfra.NewLogEmailSender(logger, cfg.PublicBaseURL)
	}
}

func registerMuseumAndCatalogRoutes(
	ctx context.Context,
	cfg config.Config,
	flags featureflag.FeatureFlagProviding,
	logger *slog.Logger,
	dbPool *database.Pool,
	signingKey []byte,
	router *platformhttp.Router,
) {
	if dbPool == nil {
		logger.Warn("no database configured — Museum and catalog routes are not registered")
		return
	}

	catalogRepo := catalinfra.NewPostgresCatalogRepository(dbPool.Pool())
	if err := catalogRepo.EnsureSeeded(ctx); err != nil {
		logger.Error("presentation catalog seeding failed — Museum and catalog routes not registered", "error", err)
		return
	}

	accessTokens := identityinfra.NewAccessTokenSigner(signingKey, "muse-backend", accessTokenTTL)
	authenticator := identityiface.NewBearerAuthenticator(accessTokens)

	museumRepo := museuminfra.NewPostgresMuseumRepository(dbPool.Pool())
	museumService := museumapp.NewMuseumService(museumRepo, catalogRepo).WithUnitOfWork(dbPool)

	mediaService, storage := buildMediaService(ctx, cfg, logger, dbPool, router)
	if mediaService != nil {
		adapter := mediaForMuseum{media: mediaService}
		museumService.EnablePhotos(dbPool, adapter, adapter)
		mediaiface.NewHandlers(mediaService, authenticator, logger).RegisterRoutes(router)
		logger.Info("media routes registered; photo assignment enabled")
	} else {
		logger.Warn("no object storage configured — photo endpoints will answer 503; see backend/config/.env.example OBJECT_STORAGE_DRIVER")
	}

	museumiface.NewHandlers(museumService, authenticator, logger).RegisterRoutes(router)

	catalogHandlers := cataliface.NewHandlers(catalogRepo, authenticator, logger)
	if storage.media != nil {
		catalogHandlers = catalogHandlers.WithMusicDelivery(catalogapp.NewMusicDeliveryService(
			catalogRepo,
			catalogAudio{storage: storage.media},
			downloadURLTTL,
			cfg.Environment == config.Production,
		))
		logger.Info("music audio delivery enabled")
	} else {
		logger.Warn("no object storage configured — music audio URLs will answer 503")
	}
	bundleRepo := catalinfra.NewPostgresBundleRepository(dbPool.Pool())
	catalogHandlers = withAssetBundleDelivery(catalogHandlers, logger, bundleRepo, storage)
	collectionDesigns := catalogapp.NewCollectionDesignService(catalogRepo, cfg.Environment == config.Production).
		WithBundleRegistry(bundleRepo)
	catalogHandlers = catalogHandlers.WithCollectionDesigns(collectionDesigns)
	collectionCatalog := catalogapp.NewCollectionCatalogService(catalogRepo, cfg.Environment == config.Production)
	catalogHandlers = catalogHandlers.WithCollectionCatalog(collectionCatalog)

	analyticsService := analyticsapp.NewAnalyticsService(
		analyticsinfra.NewPostgresEventRepository(dbPool.Pool()), logger, nil,
	)
	recorder := analyticsRecorder{analytics: analyticsService, newUUID: analyticsdomain.NewEventUUID}
	catalogHandlers = catalogHandlers.WithSearchAnalytics(recorder)
	analyticsiface.NewHandlers(analyticsService, authenticator, logger).RegisterRoutes(router)
	startAnalyticsPruner(ctx, logger, analyticsService)

	catalogHandlers.RegisterRoutes(router)

	registerSharingRoutes(cfg, flags, logger, dbPool, museumService, authenticator, router)

	collectionRepo := collectioninfra.NewPostgresCollectionRoomRepository(dbPool.Pool())
	capacities, err := entitlementCapacities(cfg, logger)
	if err != nil {
		logger.Error("entitlement configuration invalid", "error", err)
		os.Exit(1)
	}
	verifier, err := appStoreVerifier(cfg, logger)
	if err != nil {
		logger.Error("app store verifier configuration invalid", "error", err)
		os.Exit(1)
	}
	policy, err := appStorePolicy(cfg)
	if err != nil {
		logger.Error("app store policy configuration invalid", "error", err)
		os.Exit(1)
	}
	logger.Info("app store verification policy",
		"production_rules", policy.Production, "bundle_id", policy.BundleID,
		"environment", policy.Environment, "local_testing_environments", policy.LocalTestingEnvironments,
		"app_apple_id_configured", policy.AppAppleID != "", "product_ids", policy.ProductIDs,
		"online_revocation_checks", verifier.OnlineChecksEnabled())
	entitlementRepo := entitlementinfra.NewPostgresEntitlementRepository(dbPool.Pool())
	entitlementService, err := entitlementapp.NewEntitlementService(
		entitlementRepo, entitlementRepo,
		collectionForEntitlement{rooms: collectionRepo},
		verifier, policy, capacities, nil,
	)
	if err != nil {
		logger.Error("entitlement service invalid", "error", err)
		os.Exit(1)
	}
	entitlementiface.NewHandlers(entitlementService, authenticator, logger).RegisterRoutes(router)

	collectionService := collectionapp.NewCollectionRoomService(
		collectionRepo,
		catalogRepo,
		collectionDesigns,
		collectionCatalog,
	).WithUnitOfWork(dbPool).
		WithMusicCatalog(catalogRepo).
		WithEntitlements(entitlementService)
	collectioniface.NewHandlers(collectionService, authenticator, logger).
		WithItemAnalytics(recorder).
		RegisterRoutes(router)

	registerCollectionSharingRoutes(cfg, flags, logger, dbPool, collectionService, authenticator, router)

	logger.Info("museum content, collection content and presentation catalog routes registered")
}

func registerCollectionSharingRoutes(
	cfg config.Config,
	flags featureflag.FeatureFlagProviding,
	logger *slog.Logger,
	dbPool *database.Pool,
	collectionService *collectionapp.CollectionRoomService,
	authenticator sharingiface.AccountAuthenticating,
	router *platformhttp.Router,
) {
	service := sharingapp.NewCollectionShareLinkService(
		sharinginfra.NewPostgresCollectionShareLinkRepository(dbPool.Pool()),
		sharinginfra.RandomCodeGenerator{},
		collectionForSharing{rooms: collectionService},
		nil,
	).
		WithVisitorMusicPolicy(visitorMusicPolicy(flags))
	sharingiface.NewCollectionHandlers(service, authenticator, sharingiface.Config{
		ShareLinkBaseURL: shareLinkBaseURL(cfg, logger),
		AppStoreURL:      cfg.AppStoreURL,
	}, logger).RegisterRoutes(router)
	logger.Info("collection sharing routes registered")
}

func visitorMusicPolicy(flags featureflag.FeatureFlagProviding) sharingapp.VisitorMusicPolicy {
	return sharingapp.VisitorMusicPolicy{
		AudibleToVisitors: flags.IsEnabled(featureflag.VisitorAudibleRoomMusic),
	}
}

func logFeatureFlags(logger *slog.Logger, flags *featureflag.Provider) {
	for _, status := range flags.Snapshot() {
		switch {
		case status.Enabled && status.RequiresExternalClearance:
			logger.Warn("feature flag ENABLED and it requires external clearance",
				"flag", status.Name, "variable", status.EnvironmentVariable,
				"summary", status.Summary, "clearance_required", status.ClearanceRequired)
		case status.Enabled:
			logger.Info("feature flag enabled", "flag", status.Name, "summary", status.Summary)
		default:
			logger.Info("feature flag off (default)", "flag", status.Name,
				"summary", status.Summary, "overridden", status.Overridden)
		}
	}
}

func shareLinkBaseURL(cfg config.Config, logger *slog.Logger) string {
	shareBase := cfg.ShareLinkBaseURL
	if shareBase == "" {
		shareBase = cfg.PublicBaseURL
	}
	if shareBase == "" {
		shareBase = "http://localhost:" + cfg.Port
		logger.Warn("SHARE_LINK_BASE_URL and PUBLIC_BASE_URL unset — share links are minted on localhost and will not open outside this machine")
	}
	return shareBase
}

func registerSharingRoutes(
	cfg config.Config,
	flags featureflag.FeatureFlagProviding,
	logger *slog.Logger,
	dbPool *database.Pool,
	museumService *museumapp.MuseumService,
	authenticator sharingiface.AccountAuthenticating,
	router *platformhttp.Router,
) {
	museums := museumForSharing{museums: museumService}
	service := sharingapp.NewShareLinkService(
		sharinginfra.NewPostgresShareLinkRepository(dbPool.Pool()),
		sharinginfra.RandomCodeGenerator{},
		museums,
		museums,
		identityForSharing{accounts: buildAccountService(dbPool)},
		nil,
	)
	service = service.WithVisitorMusicPolicy(visitorMusicPolicy(flags))

	shareBase := shareLinkBaseURL(cfg, logger)
	appleAppID := ""
	if cfg.AppleTeamID != "" && cfg.AppleBundleID != "" {
		appleAppID = cfg.AppleTeamID + "." + cfg.AppleBundleID
	} else {
		logger.Warn("APPLE_TEAM_ID/APPLE_BUNDLE_ID unset — apple-app-site-association is NOT served; Universal Links cannot open the app ( report, unmet verification)")
	}
	if cfg.AppStoreURL == "" {
		logger.Warn("APP_STORE_URL unset — the share landing page cannot send a browser to the App Store")
	}

	sharingiface.NewHandlers(service, authenticator, sharingiface.Config{
		ShareLinkBaseURL: shareBase,
		AppStoreURL:      cfg.AppStoreURL,
		AppleAppID:       appleAppID,
	}, logger).RegisterRoutes(router)
	logger.Info("sharing routes registered", "share_link_base_url", shareBase, "aasa_served", appleAppID != "")
}

type objectStorageWiring struct {
	media                    mediaapp.ObjectStorage
	writer                   objectstore.PublicWriter
	assetBundlePublicBaseURL string
}

func buildMediaService(
	ctx context.Context,
	cfg config.Config,
	logger *slog.Logger,
	dbPool *database.Pool,
	router *platformhttp.Router,
) (*mediaapp.MediaService, objectStorageWiring) {
	var wiring objectStorageWiring

	switch cfg.ObjectStorageDriver {
	case "":
		return nil, wiring

	case "r2":
		r2, err := mediainfra.NewR2ObjectStorage(mediainfra.R2Config{
			AccountID:       cfg.R2AccountID,
			Endpoint:        cfg.R2Endpoint,
			Bucket:          cfg.R2Bucket,
			AccessKeyID:     cfg.R2AccessKeyID,
			SecretAccessKey: cfg.R2SecretAccessKey,
		})
		if err != nil {
			logger.Error("R2 object storage misconfigured — photo storage disabled", "error", err)
			return nil, objectStorageWiring{}
		}
		wiring = objectStorageWiring{
			media:                    r2,
			writer:                   r2,
			assetBundlePublicBaseURL: cfg.AssetBundlePublicBaseURL,
		}
		logger.Info("object storage: Cloudflare R2", "bucket", cfg.R2Bucket)

	case "filesystem":
		if cfg.Environment == config.Production {
			logger.Error("OBJECT_STORAGE_DRIVER=filesystem is not permitted in production — photo storage disabled")
			return nil, objectStorageWiring{}
		}
		root := cfg.DevStorageRoot
		if root == "" {
			root = "./.dev-storage"
		}
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			logger.Error("could not generate dev storage signing secret", "error", err)
			return nil, objectStorageWiring{}
		}
		fs, err := mediainfra.NewFilesystemObjectStorage(root, cfg.PublicBaseURL, secret)
		if err != nil {
			logger.Error("filesystem object storage misconfigured — photo storage disabled", "error", err)
			return nil, objectStorageWiring{}
		}
		handler := fs.Handler()
		router.Handle("PUT "+mediainfra.DevStoragePathPrefix+"{key...}", handler.ServeHTTP)
		router.Handle("GET "+mediainfra.DevStoragePathPrefix+"{key...}", handler.ServeHTTP)
		publicAssets := fs.PublicAssetHandler()
		router.Handle("GET "+mediainfra.DevPublicAssetPathPrefix+"{key...}", publicAssets.ServeHTTP)
		assetBase := ""
		if cfg.AssetBundlePublicBaseURL != "" {
			assetBase = cfg.AssetBundlePublicBaseURL
		} else if cfg.PublicBaseURL != "" {
			assetBase = strings.TrimRight(cfg.PublicBaseURL, "/") + strings.TrimSuffix(mediainfra.DevPublicAssetPathPrefix, "/")
		}
		wiring = objectStorageWiring{media: fs, writer: fs, assetBundlePublicBaseURL: assetBase}
		logger.Warn("object storage: FILESYSTEM DEV ADAPTER — non-production; objects under "+root+"never use in a deployment",
			"public_base_url", cfg.PublicBaseURL)

	default:
		logger.Error("unknown OBJECT_STORAGE_DRIVER — photo storage disabled", "driver", cfg.ObjectStorageDriver)
		return nil, objectStorageWiring{}
	}

	assets := mediainfra.NewPostgresAssetRepository(dbPool.Pool())
	service := mediaapp.NewMediaService(assets, wiring.media, uploadURLTTL, downloadURLTTL, logger)

	startReclamationTicker(ctx, cfg, logger, service)
	return service, wiring
}

func withAssetBundleDelivery(
	handlers *cataliface.Handlers,
	logger *slog.Logger,
	bundleRepo *catalinfra.PostgresBundleRepository,
	storage objectStorageWiring,
) *cataliface.Handlers {
	if storage.writer == nil {
		logger.Warn("no object storage configured — asset bundle manifests will answer 503")
		return handlers
	}
	if storage.assetBundlePublicBaseURL == "" {
		logger.Warn("ASSET_BUNDLE_PUBLIC_BASE_URL unset (and no PUBLIC_BASE_URL to derive it from) — " +
			"asset bundle manifests will answer 503; see backend/config/.env.example")
		return handlers
	}

	store, err := catalinfra.NewBundleObjectStore(storage.writer, storage.assetBundlePublicBaseURL)
	if err != nil {
		logger.Error("asset bundle store misconfigured — manifests will answer 503", "error", err)
		return handlers
	}
	bundles := catalogapp.NewBundleService(bundleRepo, store)
	logger.Info("asset bundle delivery enabled", "public_base_url", storage.assetBundlePublicBaseURL)
	return handlers.WithBundleDelivery(bundles)
}

func startReclamationTicker(ctx context.Context, cfg config.Config, logger *slog.Logger, service *mediaapp.MediaService) {
	if cfg.MediaReclaimInterval == "" {
		logger.Info("media reclamation ticker disabled (MEDIA_RECLAIM_INTERVAL unset)")
		return
	}
	interval, err := time.ParseDuration(cfg.MediaReclaimInterval)
	if err != nil || interval <= 0 {
		logger.Error("MEDIA_RECLAIM_INTERVAL is not a positive Go duration — reclamation ticker disabled", "value", cfg.MediaReclaimInterval)
		return
	}

	logger.Warn("media reclamation running as an in-process ticker (foundation phase) — schedule this externally in production",
		"interval", interval.String(), "reclaim_after", reclaimAfter.String())

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				abandoned, err := service.ReclaimAbandonedUploads(ctx, reclaimAfter, reclaimBatch)
				if err != nil {
					logger.Error("media reclamation sweep failed", "kind", "abandoned", "error", err)
				}
				released, err := service.ReclaimReleasedAssets(ctx, releasedReclaimAfter, reclaimBatch)
				if err != nil {
					logger.Error("media reclamation sweep failed", "kind", "released", "error", err)
				}
				if abandoned > 0 || released > 0 {
					logger.Info("media reclamation sweep", "discarded_abandoned", abandoned, "discarded_released", released)
				}
			}
		}
	}()
}

func startEmailOutboxDrainer(ctx context.Context, cfg config.Config, logger *slog.Logger, passwords *identityapp.PasswordService) {
	if passwords == nil {
		logger.Info("email outbox drainer disabled (email/password authentication unavailable without DATABASE_URL)")
		return
	}

	const defaultInterval = 2 * time.Second
	interval := defaultInterval
	switch cfg.EmailOutboxInterval {
	case "":
	case "off":
		logger.Warn("email outbox drainer disabled by EMAIL_OUTBOX_INTERVAL=off — this process will not deliver verification or reset emails")
		return
	default:
		parsed, err := time.ParseDuration(cfg.EmailOutboxInterval)
		if err != nil || parsed <= 0 {
			logger.Error("EMAIL_OUTBOX_INTERVAL is neither a positive Go duration nor \"off\" — using the default",
				"value", cfg.EmailOutboxInterval, "default", defaultInterval.String())
		} else {
			interval = parsed
		}
	}
	logger.Info("email outbox drainer running in-process (foundation phase)", "interval", interval.String())

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				report, err := passwords.DrainEmailOutbox(ctx)
				if err != nil {
					logger.Error("email outbox drain failed", "error", err)
				}
				if report.Retried > 0 || report.Dead > 0 {
					logger.Warn("email outbox deliveries failed", "retried", report.Retried, "dead", report.Dead)
				}
				if report.Claimed > 0 {
					logger.Info("email outbox drain", "claimed", report.Claimed, "delivered", report.Delivered,
						"no_ops", report.NoOps, "retried", report.Retried, "dead", report.Dead)
				}
			}
		}
	}()
}

func buildAccountService(dbPool *database.Pool) *identityapp.AccountService {
	if dbPool == nil {
		return nil
	}
	return identityapp.NewAccountService(identityinfra.NewPostgresAccountRepository(dbPool.Pool()))
}

func buildAccountResolver(accountService *identityapp.AccountService) identityapp.AccountResolver {
	if accountService != nil {
		return accountService
	}
	return identityinfra.NewInMemoryAccountResolver()
}

func sessionSigningKey(cfg config.Config, logger *slog.Logger) []byte {
	if cfg.SessionSigningKey != "" {
		return []byte(cfg.SessionSigningKey)
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		logger.Error("failed to generate an ephemeral session signing key", "error", err)
		os.Exit(1)
	}

	logger.Warn("SESSION_SIGNING_KEY not configured — generated a random ephemeral key; sessions will not survive a restart")
	return []byte(hex.EncodeToString(buf))
}

func connectDatabaseIfConfigured(ctx context.Context, logger *slog.Logger, cfg config.Config) *database.Pool {
	if cfg.DatabaseURL == "" {
		logger.Info("no DATABASE_URL configured, starting without a database connection")
		return nil
	}

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		if cfg.Environment == config.Production {
			logger.Error("database connection failed in production — refusing to start without one", "error", err)
			os.Exit(1)
		}
		logger.Error("database connection failed, starting without one", "error", err)
		return nil
	}

	if err := pool.ApplyMigrations(ctx); err != nil {
		pool.Close()
		if cfg.Environment == config.Production {
			logger.Error("database migration failed in production — refusing to start", "error", err)
			os.Exit(1)
		}
		logger.Error("database migration failed, starting without a database connection", "error", err)
		return nil
	}

	logger.Info("database connection established, migrations applied")
	return pool
}

func startAnalyticsPruner(ctx context.Context, logger *slog.Logger, analytics *analyticsapp.AnalyticsService) {
	interval := strings.TrimSpace(os.Getenv("ANALYTICS_PRUNE_INTERVAL"))
	if interval == "" || interval == "off" {
		logger.Info("analytics pruner disabled (set ANALYTICS_PRUNE_INTERVAL to enable in-process pruning)",
			"raw_retention", analyticsapp.RawRetention.String())
		return
	}
	parsed, err := time.ParseDuration(interval)
	if err != nil || parsed <= 0 {
		logger.Error("ANALYTICS_PRUNE_INTERVAL is not a positive Go duration — pruner not started", "value", interval)
		return
	}
	logger.Info("analytics pruner running in-process",
		"interval", parsed.String(), "raw_retention", analyticsapp.RawRetention.String())

	go func() {
		ticker := time.NewTicker(parsed)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				removed, err := analytics.PruneNow(ctx)
				if err != nil {
					logger.Warn("analytics prune failed; will retry next tick", "error", err)
					continue
				}
				if removed > 0 {
					logger.Info("analytics raw events pruned", "removed", removed)
				}
			}
		}
	}()
}

func readinessProbe(pool *database.Pool) observability.DatabaseProbing {
	if pool == nil {
		return nil
	}
	return pool
}
