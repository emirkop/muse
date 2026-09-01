package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	analyticsapp "muse-backend/internal/analytics/application"
	analyticsdomain "muse-backend/internal/analytics/domain"
	analyticsinfra "muse-backend/internal/analytics/infrastructure"
	analyticsiface "muse-backend/internal/analytics/interfaces"
	catalogapp "muse-backend/internal/catalog/application"
	catalogdomain "muse-backend/internal/catalog/domain"
	catalinfra "muse-backend/internal/catalog/infrastructure"
	cataliface "muse-backend/internal/catalog/interfaces"
	collectionapp "muse-backend/internal/collection/application"
	collectioninfra "muse-backend/internal/collection/infrastructure"
	collectioniface "muse-backend/internal/collection/interfaces"
	entitlementapp "muse-backend/internal/entitlement/application"
	entitlementdomain "muse-backend/internal/entitlement/domain"
	entitlementinfra "muse-backend/internal/entitlement/infrastructure"
	"muse-backend/internal/entitlement/infrastructure/appstoretest"
	entitlementiface "muse-backend/internal/entitlement/interfaces"
	identitydomain "muse-backend/internal/identity/domain"
	identityinfra "muse-backend/internal/identity/infrastructure"
	identityiface "muse-backend/internal/identity/interfaces"
	mediaapp "muse-backend/internal/media/application"
	mediainfra "muse-backend/internal/media/infrastructure"
	mediaiface "muse-backend/internal/media/interfaces"
	museumapp "muse-backend/internal/museum/application"
	museumdomain "muse-backend/internal/museum/domain"
	museuminfra "muse-backend/internal/museum/infrastructure"
	museumiface "muse-backend/internal/museum/interfaces"
	"muse-backend/internal/platform/config"
	"muse-backend/internal/platform/database"
	platformhttp "muse-backend/internal/platform/http"
	"muse-backend/internal/platform/observability"
	sharingapp "muse-backend/internal/sharing/application"
	sharinginfra "muse-backend/internal/sharing/infrastructure"
	sharingiface "muse-backend/internal/sharing/interfaces"
)

type stack struct {
	t            *testing.T
	server       *httptest.Server
	pool         *database.Pool
	signer       *identityinfra.AccessTokenSigner
	media        *mediaapp.MediaService
	museums      *museumapp.MuseumService
	storage      *mediainfra.FilesystemObjectStorage
	accountID    string
	token        string
	logs         *logCapture
	metrics      *observability.Registry
	publisher    *catalogapp.BundlePublisher
	bundleStore  *catalinfra.BundleObjectStore
	entitlements *entitlementapp.EntitlementService
	appStore     *appstoretest.Signer
	capacities   entitlementdomain.ItemCapacities

	collection    *collectionapp.CollectionRoomService
	authenticator *identityiface.BearerAuthenticator
	logger        *slog.Logger
}

const (
	testAppleBundleID     = "com.muse.app"
	testCapacityProductID = "dev.muse.placeholder.collection_capacity"
)

var testDefaultCapacities = entitlementdomain.ItemCapacities{Free: 1000, Paid: 2000, Source: "test default"}

func newStack(t *testing.T) *stack {
	t.Helper()
	return newStackWithCapacities(t, testDefaultCapacities)
}

func newStackWithCapacities(t *testing.T, capacities entitlementdomain.ItemCapacities) *stack {
	t.Helper()
	connStr := testDatabaseURL(t, "whole-stack tests")
	ctx := context.Background()

	pool, err := database.Connect(ctx, connStr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.ApplyMigrations(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Pool().Exec(ctx, `TRUNCATE analytics_events, analytics_daily_counts, app_store_transactions, account_app_account_tokens, asset_bundle_dependencies, asset_bundle_files, asset_bundles, share_links, collection_share_links, collection_items, collection_rooms, room_photo_slots, room_sculptures, sculptures, rooms, museums, music_tracks, assets, email_outbox, password_credentials, pending_signups, password_resets, auth_attempts, external_identities, accounts CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	catalog := catalinfra.NewPostgresCatalogRepository(pool.Pool())
	if err := catalog.EnsureSeeded(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, model := range catalogdomain.SeedCollectionModels() {
		if _, err := pool.Pool().Exec(ctx,
			`UPDATE collection_models SET asset_bundle_id = $2, asset_bundle_version = $3 WHERE id = $1`,
			string(model.ID), model.AssetBundle.ID, model.AssetBundle.Version); err != nil {
			t.Fatalf("restore seeded model mappings: %v", err)
		}
	}

	server := httptest.NewUnstartedServer(nil)
	baseURL := "http://" + server.Listener.Addr().String()
	storage, err := mediainfra.NewFilesystemObjectStorage(t.TempDir(), baseURL, []byte("integration-test-secret-key-32b!"))
	if err != nil {
		t.Fatalf("storage: %v", err)
	}

	logs := &logCapture{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	signingKey := []byte("integration-signing-key-that-is-long-enough")
	signer := identityinfra.NewAccessTokenSigner(signingKey, "muse-backend", time.Hour)
	authenticator := identityiface.NewBearerAuthenticator(signer)

	assets := mediainfra.NewPostgresAssetRepository(pool.Pool())
	mediaService := mediaapp.NewMediaService(assets, storage, 5*time.Minute, 5*time.Minute, logger)
	adapter := mediaForMuseum{media: mediaService}

	museumRepo := museuminfra.NewPostgresMuseumRepository(pool.Pool())
	museumService := museumapp.NewMuseumService(museumRepo, catalog).EnablePhotos(pool, adapter, adapter)

	router := platformhttp.NewRouter()
	router.Handle("GET /health", platformhttp.HealthHandler)
	identityHandlers, _ := buildIdentityHandlers(config.Config{Environment: config.Development, Port: "0"}, logger, pool, signingKey)
	identityHandlers.RegisterRoutes(router)
	mediaiface.NewHandlers(mediaService, authenticator, logger).RegisterRoutes(router)
	museumiface.NewHandlers(museumService, authenticator, logger).RegisterRoutes(router)
	bundleStore, err := catalinfra.NewBundleObjectStore(storage, baseURL+"/dev-assets")
	if err != nil {
		t.Fatalf("bundle store: %v", err)
	}
	bundleRepo := catalinfra.NewPostgresBundleRepository(pool.Pool())
	collectionDesigns := catalogapp.NewCollectionDesignService(catalog, false).
		WithBundleRegistry(bundleRepo)
	analyticsService := analyticsapp.NewAnalyticsService(
		analyticsinfra.NewPostgresEventRepository(pool.Pool()), logger, nil,
	)
	analyticsRec := analyticsRecorder{analytics: analyticsService, newUUID: analyticsdomain.NewEventUUID}
	analyticsiface.NewHandlers(analyticsService, authenticator, logger).RegisterRoutes(router)

	collectionCatalog := catalogapp.NewCollectionCatalogService(catalog, false)
	cataliface.NewHandlers(catalog, authenticator, logger).
		WithMusicDelivery(catalogapp.NewMusicDeliveryService(catalog, catalogAudio{storage: storage}, 5*time.Minute, false)).
		WithBundleDelivery(catalogapp.NewBundleService(bundleRepo, bundleStore)).
		WithCollectionDesigns(collectionDesigns).
		WithCollectionCatalog(collectionCatalog).
		WithSearchAnalytics(analyticsRec).
		RegisterRoutes(router)
	sharingMuseums := museumForSharing{museums: museumService}
	sharingService := sharingapp.NewShareLinkService(
		sharinginfra.NewPostgresShareLinkRepository(pool.Pool()),
		sharinginfra.RandomCodeGenerator{},
		sharingMuseums, sharingMuseums,
		identityForSharing{accounts: buildAccountService(pool)},
		nil,
	)
	sharingiface.NewHandlers(sharingService, authenticator, sharingiface.Config{
		ShareLinkBaseURL: testShareLinkBase,
		AppStoreURL:      testAppStoreURL,
		AppleAppID:       testAppleAppID,
	}, logger).RegisterRoutes(router)
	collectionRepo := collectioninfra.NewPostgresCollectionRoomRepository(pool.Pool())
	appStore, err := appstoretest.NewSigner(appstoretest.Options{})
	if err != nil {
		t.Fatalf("app store test signer: %v", err)
	}
	verifier := entitlementinfra.NewAppStoreJWSVerifierWithRoots(appStore.RootPool(), nil, nil)
	entitlementRepo := entitlementinfra.NewPostgresEntitlementRepository(pool.Pool())
	entitlements, err := entitlementapp.NewEntitlementService(
		entitlementRepo, entitlementRepo,
		collectionForEntitlement{rooms: collectionRepo},
		verifier,
		entitlementapp.AppStorePolicy{
			Production:               false,
			BundleID:                 testAppleBundleID,
			ProductIDs:               []string{testCapacityProductID},
			Environment:              "Sandbox",
			LocalTestingEnvironments: []string{"Xcode", "LocalTesting"},
		},
		capacities, nil,
	)
	if err != nil {
		t.Fatalf("entitlement service: %v", err)
	}
	entitlementiface.NewHandlers(entitlements, authenticator, logger).RegisterRoutes(router)

	collectionService := collectionapp.NewCollectionRoomService(
		collectionRepo,
		catalog,
		collectionDesigns,
		collectionCatalog,
	).WithUnitOfWork(pool).
		WithMusicCatalog(catalog).
		WithEntitlements(entitlements)
	collectioniface.NewHandlers(collectionService, authenticator, logger).
		WithItemAnalytics(analyticsRec).
		RegisterRoutes(router)
	collectionSharing := sharingapp.NewCollectionShareLinkService(
		sharinginfra.NewPostgresCollectionShareLinkRepository(pool.Pool()),
		sharinginfra.RandomCodeGenerator{},
		collectionForSharing{rooms: collectionService},
		nil,
	)
	sharingiface.NewCollectionHandlers(collectionSharing, authenticator, sharingiface.Config{
		ShareLinkBaseURL: testShareLinkBase,
		AppStoreURL:      testAppStoreURL,
	}, logger).RegisterRoutes(router)
	devHandler := storage.Handler()
	router.Handle("PUT "+mediainfra.DevStoragePathPrefix+"{key...}", devHandler.ServeHTTP)
	router.Handle("GET "+mediainfra.DevStoragePathPrefix+"{key...}", devHandler.ServeHTTP)
	publicAssets := storage.PublicAssetHandler()
	router.Handle("GET "+mediainfra.DevPublicAssetPathPrefix+"{key...}", publicAssets.ServeHTTP)
	metrics := observability.NewRegistry()
	metrics.MarkUp()
	observability.UseRegistry(metrics)
	t.Cleanup(func() { observability.UseRegistry(nil) })
	router.Handle("GET /health/ready", observability.ReadinessHandler(metrics, pool))
	router.Handle("GET /metrics", observability.MetricsHandler(metrics, ""))
	server.Config.Handler = observability.Instrument(metrics, router, router)
	server.Start()
	t.Cleanup(server.Close)

	var accountID string
	if err := pool.Pool().QueryRow(ctx, `INSERT INTO accounts (display_name) VALUES ('owner') RETURNING id`).Scan(&accountID); err != nil {
		t.Fatalf("account: %v", err)
	}
	token, _, err := signer.Sign(identitydomain.AccountID(accountID), identitydomain.SessionID("sess"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	return &stack{
		entitlements: entitlements,
		appStore:     appStore,
		capacities:   capacities,
		t:            t, server: server, pool: pool, signer: signer,
		media: mediaService, museums: museumService, storage: storage,
		accountID: accountID, token: token, logs: logs, metrics: metrics,
		publisher:   catalogapp.NewBundlePublisher(bundleRepo, bundleStore),
		bundleStore: bundleStore,
		collection:  collectionService, authenticator: authenticator, logger: logger,
	}
}

// MARK: - HTTP helpers

func (s *stack) do(method, path string, body any, token string) (*http.Response, []byte) {
	s.t.Helper()
	return s.doAgainst(s.server.URL, method, path, body, token)
}

func (s *stack) doAgainst(baseURL, method, path string, body any, token string) (*http.Response, []byte) {
	s.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, baseURL+path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

func (s *stack) degradedMuseumServer() *httptest.Server {
	s.t.Helper()
	degraded := museumapp.NewMuseumService(
		museuminfra.NewPostgresMuseumRepository(s.pool.Pool()),
		catalinfra.NewPostgresCatalogRepository(s.pool.Pool()),
	).WithUnitOfWork(s.pool)
	router := platformhttp.NewRouter()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	museumiface.NewHandlers(degraded, identityiface.NewBearerAuthenticator(s.signer), logger).RegisterRoutes(router)
	return httptest.NewServer(router)
}

func (s *stack) createRoom() string {
	s.t.Helper()
	resp, _ := s.do(http.MethodPost, "/museum", map[string]string{"style_id": "style_modern"}, s.token)
	if resp.StatusCode != http.StatusCreated {
		s.t.Fatalf("create museum: %d", resp.StatusCode)
	}
	resp, raw := s.do(http.MethodPost, "/museum/me/rooms", map[string]string{"name": "Trabzon", "variant_id": "style_modern_variant_Hall"}, s.token)
	if resp.StatusCode != http.StatusCreated {
		s.t.Fatalf("create room: %d %s", resp.StatusCode, raw)
	}
	var room struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &room)
	return room.ID
}

type photoFixture struct {
	data   []byte
	w, h   int
	sha    string
	cuid   string
	asset  string
	upload struct {
		URL     string            `json:"url"`
		Method  string            `json:"method"`
		Headers map[string]string `json:"headers"`
	}
}

func newPhoto(t *testing.T, w, h int, cuid string) *photoFixture {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y += 9 {
		for x := 0; x < w; x += 9 {
			img.Set(x, y, color.RGBA{uint8(x * 3), uint8(y * 5), uint8(x ^ y), 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return &photoFixture{data: buf.Bytes(), w: w, h: h, sha: hex.EncodeToString(sum[:]), cuid: cuid}
}

func (s *stack) initiate(p *photoFixture) (*http.Response, []byte) {
	s.t.Helper()
	resp, raw := s.do(http.MethodPost, "/media/photo-uploads", map[string]any{
		"client_upload_id": p.cuid, "content_type": "image/jpeg", "byte_size": len(p.data),
		"pixel_width": p.w, "pixel_height": p.h, "checksum_sha256": p.sha,
	}, s.token)
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		var body struct {
			AssetID string `json:"asset_id"`
			Upload  *struct {
				URL     string            `json:"url"`
				Method  string            `json:"method"`
				Headers map[string]string `json:"headers"`
			} `json:"upload"`
		}
		_ = json.Unmarshal(raw, &body)
		p.asset = body.AssetID
		if body.Upload != nil {
			p.upload.URL, p.upload.Method, p.upload.Headers = body.Upload.URL, body.Upload.Method, body.Upload.Headers
		}
	}
	return resp, raw
}

func (s *stack) put(p *photoFixture) int {
	s.t.Helper()
	req, _ := http.NewRequest(p.upload.Method, p.upload.URL, bytes.NewReader(p.data))
	for k, v := range p.upload.Headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatalf("PUT: %v", err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func (s *stack) uploaded(p *photoFixture) *photoFixture {
	s.t.Helper()
	if resp, raw := s.initiate(p); resp.StatusCode != http.StatusCreated {
		s.t.Fatalf("initiate %s: %d %s", p.cuid, resp.StatusCode, raw)
	}
	if code := s.put(p); code != http.StatusOK {
		s.t.Fatalf("PUT %s: %d", p.cuid, code)
	}
	return p
}

type slotJSON struct {
	SlotIndex    int    `json:"slot_index"`
	PhotoAssetID string `json:"photo_asset_id"`
	Caption      string `json:"caption"`
}

func (s *stack) assign(roomID string, assetIDs []string) (*http.Response, []slotJSON, map[string]any) {
	s.t.Helper()
	resp, raw := s.do(http.MethodPost, "/museum/me/rooms/"+roomID+"/photos", map[string]any{"asset_ids": assetIDs}, s.token)
	var ok struct {
		PhotoSlots []slotJSON `json:"photo_slots"`
	}
	var errBody map[string]any
	if resp.StatusCode == http.StatusCreated {
		_ = json.Unmarshal(raw, &ok)
	} else {
		_ = json.Unmarshal(raw, &errBody)
	}
	return resp, ok.PhotoSlots, errBody
}

func (s *stack) assetState(assetID string) string {
	s.t.Helper()
	var state string
	if err := s.pool.Pool().QueryRow(context.Background(), `SELECT state FROM assets WHERE id = $1`, assetID).Scan(&state); err != nil {
		s.t.Fatalf("asset state: %v", err)
	}
	return state
}

func (s *stack) slotCount(roomID string) int {
	s.t.Helper()
	var n int
	_ = s.pool.Pool().QueryRow(context.Background(), `SELECT count(*) FROM room_photo_slots WHERE room_id = $1`, roomID).Scan(&n)
	return n
}

// MARK: - The happy path, end to end

func TestPhotoFlow_InitiateUploadAssignAndFetch(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	a := s.uploaded(newPhoto(t, 1600, 1200, "a"))
	b := s.uploaded(newPhoto(t, 900, 1350, "b"))

	if s.assetState(a.asset) != "pending" {
		t.Fatalf("an uploaded-but-unassigned asset must still be pending; got %s", s.assetState(a.asset))
	}

	resp, slots, errBody := s.assign(roomID, []string{a.asset, b.asset})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("assign: %d %v", resp.StatusCode, errBody)
	}
	if len(slots) != 2 || slots[0].SlotIndex != 0 || slots[0].PhotoAssetID != a.asset || slots[1].SlotIndex != 1 || slots[1].PhotoAssetID != b.asset {
		t.Fatalf("slots must be contiguous in request order: %+v", slots)
	}
	if s.assetState(a.asset) != "committed" || s.assetState(b.asset) != "committed" {
		t.Error("assigned assets must be committed in the same transaction")
	}

	resp, raw := s.do(http.MethodGet, "/museum/me/rooms/"+roomID, nil, s.token)
	if resp.StatusCode != http.StatusOK || bytes.Contains(raw, []byte("http")) {
		t.Errorf("room payload must hold asset references and no URL; got %s", raw)
	}

	resp, raw = s.do(http.MethodGet, "/museum/me/rooms/"+roomID+"/photo-urls", nil, s.token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("photo-urls: %d %s", resp.StatusCode, raw)
	}
	if resp.Header.Get("Cache-Control") != "private, no-store" {
		t.Errorf("tickets must be uncacheable; got %q", resp.Header.Get("Cache-Control"))
	}
	var tickets struct {
		Tickets []struct {
			PhotoAssetID string    `json:"photo_asset_id"`
			URL          string    `json:"url"`
			ExpiresAt    time.Time `json:"expires_at"`
			PixelWidth   int       `json:"pixel_width"`
			PixelHeight  int       `json:"pixel_height"`
		} `json:"tickets"`
	}
	_ = json.Unmarshal(raw, &tickets)
	if len(tickets.Tickets) != 2 {
		t.Fatalf("expected 2 tickets, got %d", len(tickets.Tickets))
	}
	if tickets.Tickets[1].PixelWidth != 900 || tickets.Tickets[1].PixelHeight != 1350 {
		t.Errorf("ticket must carry the stored dimensions; got %+v", tickets.Tickets[1])
	}
	if time.Until(tickets.Tickets[0].ExpiresAt) > 5*time.Minute {
		t.Error("tickets must be short-lived")
	}

	got, err := http.Get(tickets.Tickets[0].URL)
	if err != nil {
		t.Fatalf("GET ticket: %v", err)
	}
	defer got.Body.Close()
	body, _ := io.ReadAll(got.Body)
	if got.StatusCode != http.StatusOK || !bytes.Equal(body, a.data) {
		t.Errorf("downloaded bytes must equal the uploaded bytes (status %d, %d bytes)", got.StatusCode, len(body))
	}
}

// MARK: - Idempotency

func TestPhotoFlow_RetryingEveryStep_IsIdempotent(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	p := newPhoto(t, 1200, 800, "retry-me")

	first, _ := s.initiate(p)
	firstAsset := p.asset
	second, _ := s.initiate(p)
	if first.StatusCode != http.StatusCreated || second.StatusCode != http.StatusOK {
		t.Fatalf("initiate statuses = %d, %d; want 201 then 200", first.StatusCode, second.StatusCode)
	}
	if p.asset != firstAsset {
		t.Fatalf("retry must reuse the asset: %s vs %s", p.asset, firstAsset)
	}

	if s.put(p) != http.StatusOK || s.put(p) != http.StatusOK {
		t.Fatal("re-PUTting the same bytes must succeed")
	}

	r1, slots1, _ := s.assign(roomID, []string{p.asset})
	r2, slots2, _ := s.assign(roomID, []string{p.asset})
	if r1.StatusCode != http.StatusCreated || r2.StatusCode != http.StatusCreated {
		t.Fatalf("assign statuses = %d, %d", r1.StatusCode, r2.StatusCode)
	}
	if len(slots2) != 1 || slots2[0] != slots1[0] {
		t.Errorf("retried assign must converge, got %+v vs %+v", slots1, slots2)
	}
	if s.slotCount(roomID) != 1 {
		t.Errorf("exactly one slot row, got %d", s.slotCount(roomID))
	}

	resp, raw := s.initiate(p)
	var body struct {
		State  string          `json:"state"`
		Upload json.RawMessage `json:"upload"`
	}
	_ = json.Unmarshal(raw, &body)
	if resp.StatusCode != http.StatusOK || body.State != "committed" || string(body.Upload) != "null" {
		t.Errorf("committed asset must be reported without an upload URL; got %d %s", resp.StatusCode, raw)
	}
}

// MARK: - The 28-photo cap, server-side, under concurrency

func TestPhotoFlow_CapIsEnforcedUnderConcurrentAssigns(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()

	var seed []string
	for i := 0; i < 20; i++ {
		seed = append(seed, s.uploaded(newPhoto(t, 640, 480, fmt.Sprintf("seed-%d", i))).asset)
	}
	if resp, _, errBody := s.assign(roomID, seed); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed assign: %d %v", resp.StatusCode, errBody)
	}

	const racers = 12
	racing := make([]*photoFixture, racers)
	for i := range racing {
		racing[i] = s.uploaded(newPhoto(t, 640, 480, fmt.Sprintf("race-%d", i)))
	}

	var wg sync.WaitGroup
	statuses := make(chan int, racers)
	for _, p := range racing {
		wg.Add(1)
		go func(p *photoFixture) {
			defer wg.Done()
			resp, _, _ := s.assign(roomID, []string{p.asset})
			statuses <- resp.StatusCode
		}(p)
	}
	wg.Wait()
	close(statuses)

	won, refused := 0, 0
	for code := range statuses {
		switch code {
		case http.StatusCreated:
			won++
		case http.StatusConflict:
			refused++
		default:
			t.Errorf("unexpected status %d", code)
		}
	}
	if won != 8 || refused != 4 {
		t.Errorf("won=%d refused=%d; want 8 and 4", won, refused)
	}
	if n := s.slotCount(roomID); n != museumdomain.MaxPhotosPerRoom {
		t.Errorf("room holds %d slots, want exactly %d", n, museumdomain.MaxPhotosPerRoom)
	}

	committed := 0
	for _, p := range racing {
		if s.assetState(p.asset) == "committed" {
			committed++
		}
	}
	if committed != 8 {
		t.Errorf("%d racing assets committed, want 8", committed)
	}

	rows, _ := s.pool.Pool().Query(context.Background(), `SELECT slot_index FROM room_photo_slots WHERE room_id = $1 ORDER BY slot_index`, roomID)
	defer rows.Close()
	expect := 0
	for rows.Next() {
		var idx int
		_ = rows.Scan(&idx)
		if idx != expect {
			t.Errorf("slot index %d, want %d — layout not contiguous", idx, expect)
		}
		expect++
	}
}

func TestPhotoFlow_FullRoom_RefusesOneMore(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	var all []string
	for i := 0; i < museumdomain.MaxPhotosPerRoom; i++ {
		all = append(all, s.uploaded(newPhoto(t, 640, 480, fmt.Sprintf("p-%d", i))).asset)
	}
	if resp, slots, errBody := s.assign(roomID, all); resp.StatusCode != http.StatusCreated || len(slots) != 28 {
		t.Fatalf("filling to 28 must succeed: %d %v", resp.StatusCode, errBody)
	}

	extra := s.uploaded(newPhoto(t, 640, 480, "29th"))
	resp, _, errBody := s.assign(roomID, []string{extra.asset})

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("29th photo must be refused with 409, got %d %v", resp.StatusCode, errBody)
	}
	if s.assetState(extra.asset) != "pending" {
		t.Error("a refused asset must not commit")
	}
}

// MARK: - Partial batches and verification failures

func TestPhotoFlow_BatchWithAnUnuploadedAsset_IsRefusedNamingIt_AndAssignsNothing(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	good := s.uploaded(newPhoto(t, 1200, 800, "good"))
	ghost := newPhoto(t, 1200, 800, "ghost")
	s.initiate(ghost)

	resp, _, errBody := s.assign(roomID, []string{good.asset, ghost.asset})

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d %v", resp.StatusCode, errBody)
	}
	if errBody["code"] != "asset_not_uploaded" || errBody["asset_id"] != ghost.asset {
		t.Errorf("the response must name the unuploaded asset: %v", errBody)
	}
	if s.slotCount(roomID) != 0 || s.assetState(good.asset) != "pending" {
		t.Error("nothing may be assigned or committed when the batch is refused")
	}

	resp, slots, _ := s.assign(roomID, []string{good.asset})
	if resp.StatusCode != http.StatusCreated || len(slots) != 1 {
		t.Errorf("re-composed batch must succeed: %d", resp.StatusCode)
	}
}

func TestPhotoFlow_TamperedUpload_FailsVerification(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()

	p := newPhoto(t, 1200, 800, "liar")
	p.w, p.h = 640, 480
	if resp, raw := s.initiate(p); resp.StatusCode != http.StatusCreated {
		t.Fatalf("initiate: %d %s", resp.StatusCode, raw)
	}
	if code := s.put(p); code != http.StatusOK {
		t.Fatalf("PUT: %d", code)
	}

	resp, _, errBody := s.assign(roomID, []string{p.asset})

	if resp.StatusCode != http.StatusUnprocessableEntity || errBody["code"] != "asset_invalid" {
		t.Fatalf("lied dimensions must fail verification with 422 asset_invalid, got %d %v", resp.StatusCode, errBody)
	}
	if s.assetState(p.asset) != "pending" || s.slotCount(roomID) != 0 {
		t.Error("an invalid asset must neither commit nor be assigned")
	}
}

func TestPhotoFlow_BytesThatBreakTheSignedChecksum_AreNeverStored(t *testing.T) {
	s := newStack(t)
	p := newPhoto(t, 1200, 800, "swap")
	if resp, _ := s.initiate(p); resp.StatusCode != http.StatusCreated {
		t.Fatal("initiate")
	}
	other := newPhoto(t, 1200, 801, "other")
	p.data = other.data

	if code := s.put(p); code == http.StatusOK {
		t.Fatal("the store must refuse bytes that do not match the signed checksum")
	}
	if _, err := s.storage.Stat(context.Background(), "photos/"+s.accountID+"/"+p.asset); err == nil {
		t.Error("a refused upload must leave no object behind")
	}
}

// MARK: - Rollback

func TestPhotoFlow_CommitFailureInsideTheTransaction_RollsBackSlotInserts(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	p := s.uploaded(newPhoto(t, 1200, 800, "oob"))

	if _, err := s.pool.Pool().Exec(context.Background(),
		`UPDATE assets SET state = 'committed', committed_at = now() WHERE id = $1`, p.asset); err != nil {
		t.Fatalf("out-of-band commit: %v", err)
	}

	resp, _, errBody := s.assign(roomID, []string{p.asset})

	if resp.StatusCode == http.StatusCreated {
		t.Fatalf("expected the mismatched commit to fail the transaction, got 201")
	}
	if s.slotCount(roomID) != 0 {
		t.Errorf("slot insert must roll back with the failed commit; got %d slots (%v)", s.slotCount(roomID), errBody)
	}
}

// MARK: - Authorization and degraded mode

func TestPhotoFlow_AnotherAccountCannotAssignOrReadMyPhotos(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()
	p := s.uploaded(newPhoto(t, 1200, 800, "mine"))
	if resp, _, _ := s.assign(roomID, []string{p.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("assign")
	}

	var strangerID string
	_ = s.pool.Pool().QueryRow(context.Background(), `INSERT INTO accounts (display_name) VALUES ('stranger') RETURNING id`).Scan(&strangerID)
	strangerToken, _, _ := s.signer.Sign(identitydomain.AccountID(strangerID), identitydomain.SessionID("s2"))

	if resp, _ := s.do(http.MethodGet, "/museum/me/rooms/"+roomID+"/photo-urls", nil, strangerToken); resp.StatusCode == http.StatusOK {
		t.Error("a stranger must not receive download tickets for my Room")
	}
	if resp, _ := s.do(http.MethodPost, "/museum/me/rooms/"+roomID+"/photos", map[string]any{"asset_ids": []string{p.asset}}, strangerToken); resp.StatusCode == http.StatusCreated {
		t.Error("a stranger must not assign into my Room")
	}
	if resp, _ := s.do(http.MethodPost, "/media/photo-uploads", map[string]any{
		"client_upload_id": "x", "content_type": "image/jpeg", "byte_size": 10, "pixel_width": 640, "pixel_height": 480, "checksum_sha256": p.sha,
	}, "not-a-token"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated initiate must be 401, got %d", resp.StatusCode)
	}
}

func TestPhotoFlow_WithoutObjectStorage_PhotoEndpointsAnswer503(t *testing.T) {
	s := newStack(t)
	roomID := s.createRoom()

	degraded := museumapp.NewMuseumService(museuminfra.NewPostgresMuseumRepository(s.pool.Pool()), catalinfra.NewPostgresCatalogRepository(s.pool.Pool()))
	router := platformhttp.NewRouter()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	museumiface.NewHandlers(degraded, identityiface.NewBearerAuthenticator(s.signer), logger).RegisterRoutes(router)
	server := httptest.NewServer(router)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/museum/me/rooms/"+roomID+"/photos", bytes.NewReader([]byte(`{"asset_ids":["00000000-0000-4000-8000-000000000000"]}`)))
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 without object storage, got %d", resp.StatusCode)
	}
}

// MARK: - Reclamation, against the real store and database

func TestPhotoFlow_AbandonedUploads_AreReclaimedBytesFirst(t *testing.T) {
	s := newStack(t)
	abandoned := s.uploaded(newPhoto(t, 1200, 800, "abandoned"))
	kept := s.uploaded(newPhoto(t, 1200, 800, "kept"))
	roomID := s.createRoom()
	if resp, _, _ := s.assign(roomID, []string{kept.asset}); resp.StatusCode != http.StatusCreated {
		t.Fatal("assign kept")
	}
	if _, err := s.pool.Pool().Exec(context.Background(), `UPDATE assets SET created_at = now() - interval '2 days' WHERE id = $1`, abandoned.asset); err != nil {
		t.Fatal(err)
	}

	n, err := s.media.ReclaimAbandonedUploads(context.Background(), 24*time.Hour, 100)
	if err != nil || n != 1 {
		t.Fatalf("reclaim: n=%d err=%v", n, err)
	}
	if s.assetState(abandoned.asset) != "discarded" {
		t.Error("abandoned upload must be discarded")
	}
	if _, err := s.storage.Stat(context.Background(), "photos/"+s.accountID+"/"+abandoned.asset); err == nil {
		t.Error("abandoned bytes must be deleted")
	}
	if s.assetState(kept.asset) != "committed" {
		t.Error("committed assets are never reclaimed")
	}
}
