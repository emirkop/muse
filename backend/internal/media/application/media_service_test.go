package application_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"muse-backend/internal/media/application"
	"muse-backend/internal/media/domain"
)

// MARK: - Fakes

type fakeAssetRepo struct {
	mu     sync.Mutex
	assets map[domain.AssetID]domain.Asset
	seq    int
}

func newFakeAssetRepo() *fakeAssetRepo {
	return &fakeAssetRepo{assets: map[domain.AssetID]domain.Asset{}}
}

func (r *fakeAssetRepo) CreatePending(_ context.Context, asset domain.Asset) (domain.Asset, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.assets {
		if existing.AccountID == asset.AccountID && existing.ClientUploadID == asset.ClientUploadID && existing.State != domain.StateDiscarded {
			return existing, false, nil
		}
	}
	r.seq++
	asset.ID = domain.AssetID(fmt.Sprintf("asset_%d", r.seq))
	asset.StorageKey = domain.PhotoStorageKey(asset.AccountID, asset.ID)
	r.assets[asset.ID] = asset
	return asset, true, nil
}

func (r *fakeAssetRepo) FindOwnedByIDs(_ context.Context, accountID string, ids []domain.AssetID) ([]domain.Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.Asset
	for _, id := range ids {
		if a, ok := r.assets[id]; ok && a.AccountID == accountID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *fakeAssetRepo) MarkCommitted(_ context.Context, ids []domain.AssetID, at time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for _, id := range ids {
		if a, ok := r.assets[id]; ok && a.State == domain.StatePending {
			a.State = domain.StateCommitted
			a.CommittedAt = &at
			r.assets[id] = a
			n++
		}
	}
	return n, nil
}

func (r *fakeAssetRepo) MarkReleased(_ context.Context, ids []domain.AssetID, at time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for _, id := range ids {
		if a, ok := r.assets[id]; ok && a.State == domain.StateCommitted {
			a.State = domain.StateReleased
			a.ReleasedAt = &at
			r.assets[id] = a
			n++
		}
	}
	return n, nil
}

func (r *fakeAssetRepo) MarkDiscarded(_ context.Context, id domain.AssetID, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.assets[id]
	if !ok || (a.State != domain.StatePending && a.State != domain.StateReleased) {
		return domain.ErrAssetNotPending
	}
	a.State = domain.StateDiscarded
	a.DiscardedAt = &at
	r.assets[id] = a
	return nil
}

func (r *fakeAssetRepo) ListReleasedOlderThan(_ context.Context, cutoff time.Time, limit int) ([]domain.Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.Asset
	for _, a := range r.assets {
		if a.State == domain.StateReleased && a.ReleasedAt != nil && a.ReleasedAt.Before(cutoff) && len(out) < limit {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *fakeAssetRepo) ListPendingOlderThan(_ context.Context, cutoff time.Time, limit int) ([]domain.Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.Asset
	for _, a := range r.assets {
		if a.State == domain.StatePending && a.CreatedAt.Before(cutoff) && len(out) < limit {
			out = append(out, a)
		}
	}
	return out, nil
}

type fakeStorage struct {
	mu              sync.Mutex
	objects         map[string][]byte
	contentTypes    map[string]string
	reportChecksums bool
	deleted         []string
	presigned       []application.PresignUploadRequest
	failDelete      bool
}

func newFakeStorage(reportChecksums bool) *fakeStorage {
	return &fakeStorage{objects: map[string][]byte{}, contentTypes: map[string]string{}, reportChecksums: reportChecksums}
}

func (s *fakeStorage) put(key string, data []byte, contentType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = data
	s.contentTypes[key] = contentType
}

func (s *fakeStorage) PresignUpload(_ context.Context, req application.PresignUploadRequest) (application.UploadTicket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.presigned = append(s.presigned, req)
	return application.UploadTicket{
		URL: "https://fake/" + req.Key, Method: "PUT",
		Headers: map[string]string{"Content-Type": req.ContentType}, ExpiresAt: time.Now().Add(req.TTL),
	}, nil
}

func (s *fakeStorage) Stat(_ context.Context, key string) (application.ObjectStat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return application.ObjectStat{}, application.ErrObjectNotFound
	}
	stat := application.ObjectStat{ByteSize: int64(len(data)), ContentType: s.contentTypes[key]}
	if s.reportChecksums {
		sum := sha256.Sum256(data)
		stat.ChecksumSHA256 = hex.EncodeToString(sum[:])
	}
	return stat, nil
}

func (s *fakeStorage) ReadRange(_ context.Context, key string, offset, length int64) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, application.ErrObjectNotFound
	}
	end := offset + length
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	return data[offset:end], nil
}

func (s *fakeStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, application.ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *fakeStorage) PresignDownload(_ context.Context, key string, ttl time.Duration) (application.DownloadTicket, error) {
	return application.DownloadTicket{URL: "https://fake/get/" + key, ExpiresAt: time.Now().Add(ttl)}, nil
}

func (s *fakeStorage) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failDelete {
		return errors.New("simulated delete failure")
	}
	s.deleted = append(s.deleted, key)
	delete(s.objects, key)
	return nil
}

// MARK: - Fixtures

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y += 7 {
		for x := 0; x < w; x += 7 {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 60}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func declarationFor(data []byte, w, h int, clientUploadID string) domain.PhotoDeclaration {
	return domain.PhotoDeclaration{
		ClientUploadID: clientUploadID,
		ContentType:    domain.PhotoContentType,
		ByteSize:       int64(len(data)),
		PixelWidth:     w,
		PixelHeight:    h,
		ChecksumSHA256: sha256Hex(data),
	}
}

type harness struct {
	service *application.MediaService
	repo    *fakeAssetRepo
	storage *fakeStorage
}

func newHarness(reportChecksums bool) *harness {
	repo := newFakeAssetRepo()
	storage := newFakeStorage(reportChecksums)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &harness{
		service: application.NewMediaService(repo, storage, 5*time.Minute, 5*time.Minute, logger),
		repo:    repo,
		storage: storage,
	}
}

func (h *harness) uploaded(t *testing.T, account string, data []byte, w, hgt int, cuid string) string {
	t.Helper()
	ticket, err := h.service.InitiatePhotoUpload(context.Background(), account, declarationFor(data, w, hgt, cuid))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	h.storage.put(ticket.Asset.StorageKey, data, domain.PhotoContentType)
	return string(ticket.Asset.ID)
}

// MARK: - Initiation and idempotency

func TestInitiate_CreatesPendingAsset_AndBindsThePresignToTheDeclaration(t *testing.T) {
	h := newHarness(true)
	data := jpegBytes(t, 1200, 800)

	ticket, err := h.service.InitiatePhotoUpload(context.Background(), "acct", declarationFor(data, 1200, 800, "cuid-1"))
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}

	if !ticket.Created || ticket.Upload == nil {
		t.Fatalf("expected a newly created asset with upload instructions, got %+v", ticket)
	}
	if ticket.Asset.State != domain.StatePending {
		t.Errorf("state = %s, want pending", ticket.Asset.State)
	}
	if ticket.Asset.StorageKey != "photos/acct/"+string(ticket.Asset.ID) {
		t.Errorf("storage key = %q, want photos/acct/<id>", ticket.Asset.StorageKey)
	}
	req := h.storage.presigned[0]
	if req.Key != ticket.Asset.StorageKey || req.ContentType != domain.PhotoContentType ||
		req.ByteSize != int64(len(data)) || req.ChecksumSHA256 != sha256Hex(data) || req.TTL != 5*time.Minute {
		t.Errorf("presign not bound to declaration: %+v", req)
	}
}

func TestInitiate_RetryWithSameClientUploadID_ReusesTheAsset(t *testing.T) {
	h := newHarness(true)
	data := jpegBytes(t, 1200, 800)
	decl := declarationFor(data, 1200, 800, "cuid-1")

	first, _ := h.service.InitiatePhotoUpload(context.Background(), "acct", decl)
	second, err := h.service.InitiatePhotoUpload(context.Background(), "acct", decl)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}

	if second.Created {
		t.Error("a retry must not report a new creation")
	}
	if second.Asset.ID != first.Asset.ID {
		t.Errorf("retry produced a different asset: %s vs %s", second.Asset.ID, first.Asset.ID)
	}
	if second.Upload == nil {
		t.Error("a retry of a pending upload must get a fresh upload URL")
	}
	if len(h.repo.assets) != 1 {
		t.Errorf("expected exactly one asset row, got %d", len(h.repo.assets))
	}
}

func TestInitiate_SameClientUploadID_DifferentDeclaration_IsRefused(t *testing.T) {
	h := newHarness(true)
	a := jpegBytes(t, 1200, 800)
	b := jpegBytes(t, 1000, 700)

	if _, err := h.service.InitiatePhotoUpload(context.Background(), "acct", declarationFor(a, 1200, 800, "cuid-1")); err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err := h.service.InitiatePhotoUpload(context.Background(), "acct", declarationFor(b, 1000, 700, "cuid-1"))

	if !errors.Is(err, domain.ErrDeclarationMismatch) {
		t.Fatalf("expected ErrDeclarationMismatch, got %v", err)
	}
}

func TestInitiate_ForACommittedAsset_ReturnsItWithoutAnUploadURL(t *testing.T) {
	h := newHarness(true)
	data := jpegBytes(t, 1200, 800)
	id := h.uploaded(t, "acct", data, 1200, 800, "cuid-1")
	if err := h.service.CommitPhotoAssets(context.Background(), []string{id}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	ticket, err := h.service.InitiatePhotoUpload(context.Background(), "acct", declarationFor(data, 1200, 800, "cuid-1"))
	if err != nil {
		t.Fatalf("initiate after commit: %v", err)
	}

	if ticket.Upload != nil {
		t.Error("a committed asset's bytes are immutable; no upload URL may be issued")
	}
	if ticket.Asset.State != domain.StateCommitted {
		t.Errorf("state = %s, want committed", ticket.Asset.State)
	}
}

func TestInitiate_SameClientUploadID_DifferentAccounts_AreIndependent(t *testing.T) {
	h := newHarness(true)
	data := jpegBytes(t, 1200, 800)

	a, _ := h.service.InitiatePhotoUpload(context.Background(), "acct_a", declarationFor(data, 1200, 800, "shared"))
	b, _ := h.service.InitiatePhotoUpload(context.Background(), "acct_b", declarationFor(data, 1200, 800, "shared"))

	if a.Asset.ID == b.Asset.ID {
		t.Error("idempotency is scoped per account")
	}
}

// MARK: - Declaration validation

func TestInitiate_RejectsInvalidDeclarations(t *testing.T) {
	h := newHarness(true)
	good := declarationFor(jpegBytes(t, 1200, 800), 1200, 800, "cuid")

	cases := []struct {
		name   string
		mutate func(*domain.PhotoDeclaration)
		want   error
	}{
		{"missing client id", func(d *domain.PhotoDeclaration) { d.ClientUploadID = "  " }, domain.ErrInvalidClientUploadID},
		{"png", func(d *domain.PhotoDeclaration) { d.ContentType = "image/png" }, domain.ErrUnsupportedContentType},
		{"heic", func(d *domain.PhotoDeclaration) { d.ContentType = "image/heic" }, domain.ErrUnsupportedContentType},
		{"too large", func(d *domain.PhotoDeclaration) { d.ByteSize = domain.MaxPhotoBytes + 1 }, domain.ErrPhotoTooLarge},
		{"zero size", func(d *domain.PhotoDeclaration) { d.ByteSize = 0 }, domain.ErrPhotoTooLarge},
		{"long edge over 3072", func(d *domain.PhotoDeclaration) { d.PixelWidth = 3073 }, domain.ErrPhotoDimensions},
		{"short edge under 320", func(d *domain.PhotoDeclaration) { d.PixelHeight = 319 }, domain.ErrPhotoDimensions},
		{"portrait long edge over 3072", func(d *domain.PhotoDeclaration) { d.PixelWidth, d.PixelHeight = 800, 3073 }, domain.ErrPhotoDimensions},
		{"uppercase checksum", func(d *domain.PhotoDeclaration) { d.ChecksumSHA256 = strings.ToUpper(d.ChecksumSHA256) }, domain.ErrInvalidChecksum},
		{"short checksum", func(d *domain.PhotoDeclaration) { d.ChecksumSHA256 = "abc" }, domain.ErrInvalidChecksum},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := good
			tc.mutate(&d)
			_, err := h.service.InitiatePhotoUpload(context.Background(), "acct", d)
			if !errors.Is(err, tc.want) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
	if len(h.repo.assets) != 0 {
		t.Errorf("no asset row may be created for an invalid declaration; got %d", len(h.repo.assets))
	}
}

func TestInitiate_AcceptsTheBoundaryDimensions(t *testing.T) {
	h := newHarness(true)
	for _, dims := range [][2]int{{3072, 320}, {320, 3072}, {3072, 3072}, {320, 320}} {
		decl := declarationFor(jpegBytes(t, 8, 8), dims[0], dims[1], fmt.Sprintf("c-%dx%d", dims[0], dims[1]))
		if _, err := h.service.InitiatePhotoUpload(context.Background(), "acct", decl); err != nil {
			t.Errorf("%dx%d must be accepted, got %v", dims[0], dims[1], err)
		}
	}
}

// MARK: - Verification: never trust the declaration

func TestVerify_PassesWhenStoredBytesMatchTheDeclaration(t *testing.T) {
	for _, reportChecksums := range []bool{true, false} {
		t.Run(fmt.Sprintf("storeReportsChecksum=%v", reportChecksums), func(t *testing.T) {
			h := newHarness(reportChecksums)
			data := jpegBytes(t, 1200, 800)
			id := h.uploaded(t, "acct", data, 1200, 800, "cuid")

			if err := h.service.VerifyPhotoAssets(context.Background(), "acct", []string{id}); err != nil {
				t.Fatalf("verify: %v", err)
			}
		})
	}
}

func TestVerify_NothingUploaded_IsNotUploaded(t *testing.T) {
	h := newHarness(true)
	data := jpegBytes(t, 1200, 800)
	ticket, _ := h.service.InitiatePhotoUpload(context.Background(), "acct", declarationFor(data, 1200, 800, "cuid"))

	err := h.service.VerifyPhotoAssets(context.Background(), "acct", []string{string(ticket.Asset.ID)})

	if !errors.Is(err, domain.ErrAssetNotUploaded) {
		t.Fatalf("expected ErrAssetNotUploaded, got %v", err)
	}
	var assetErr *domain.AssetError
	if !errors.As(err, &assetErr) || assetErr.AssetID != ticket.Asset.ID {
		t.Errorf("the failing asset must be identified; got %v", err)
	}
}

func TestVerify_RefusesStoredBytesThatDoNotMatch(t *testing.T) {
	good := func(t *testing.T) []byte { return jpegBytes(t, 1200, 800) }

	cases := []struct {
		name   string
		stored func(t *testing.T) []byte
		ctype  string
	}{
		{"different bytes, same size class", func(t *testing.T) []byte { return jpegBytes(t, 1200, 801) }, domain.PhotoContentType},
		{"a PNG claiming to be a JPEG", func(t *testing.T) []byte { return pngBytes(t, 1200, 800) }, domain.PhotoContentType},
		{"truncated JPEG", func(t *testing.T) []byte { d := good(t); return d[:len(d)/2] }, domain.PhotoContentType},
		{"not an image at all", func(t *testing.T) []byte { return []byte("hello, wall") }, domain.PhotoContentType},
		{"wrong stored content type", good, "image/png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(true)
			declared := good(t)
			ticket, err := h.service.InitiatePhotoUpload(context.Background(), "acct", declarationFor(declared, 1200, 800, "cuid"))
			if err != nil {
				t.Fatalf("initiate: %v", err)
			}
			h.storage.put(ticket.Asset.StorageKey, tc.stored(t), tc.ctype)

			err = h.service.VerifyPhotoAssets(context.Background(), "acct", []string{string(ticket.Asset.ID)})

			if !errors.Is(err, domain.ErrAssetInvalid) {
				t.Fatalf("expected ErrAssetInvalid, got %v", err)
			}
		})
	}
}

func TestVerify_DeclaredDimensionsAreCheckedAgainstTheRealBytes(t *testing.T) {
	h := newHarness(true)
	data := jpegBytes(t, 1200, 800)
	decl := declarationFor(data, 1200, 800, "cuid")
	decl.PixelWidth, decl.PixelHeight = 1000, 800
	ticket, err := h.service.InitiatePhotoUpload(context.Background(), "acct", decl)
	if err != nil {
		t.Fatalf("initiate: %v", err)
	}
	h.storage.put(ticket.Asset.StorageKey, data, domain.PhotoContentType)

	err = h.service.VerifyPhotoAssets(context.Background(), "acct", []string{string(ticket.Asset.ID)})

	if !errors.Is(err, domain.ErrAssetInvalid) {
		t.Fatalf("expected ErrAssetInvalid for lied dimensions, got %v", err)
	}
}

func TestVerify_HeaderBeyondFirstProbeWindow_IsStillDecoded(t *testing.T) {
	h := newHarness(true)
	base := jpegBytes(t, 1200, 800)

	var padded bytes.Buffer
	padded.Write(base[:2])
	for i := 0; i < 2; i++ {
		body := bytes.Repeat([]byte{'x'}, 50<<10)
		padded.Write([]byte{0xFF, 0xFE, byte((len(body) + 2) >> 8), byte(len(body) + 2)})
		padded.Write(body)
	}
	padded.Write(base[2:])
	data := padded.Bytes()

	id := h.uploaded(t, "acct", data, 1200, 800, "cuid")

	if err := h.service.VerifyPhotoAssets(context.Background(), "acct", []string{id}); err != nil {
		t.Fatalf("verify must decode a header beyond 64 KiB via the fallback window: %v", err)
	}
}

func TestVerify_ChecksumMismatch_IsRefused_ViaStoreChecksumAndViaFullHash(t *testing.T) {
	for _, reportChecksums := range []bool{true, false} {
		t.Run(fmt.Sprintf("storeReportsChecksum=%v", reportChecksums), func(t *testing.T) {
			h := newHarness(reportChecksums)
			data := jpegBytes(t, 1200, 800)
			decl := declarationFor(data, 1200, 800, "cuid")
			decl.ChecksumSHA256 = strings.Repeat("0", 64)
			ticket, err := h.service.InitiatePhotoUpload(context.Background(), "acct", decl)
			if err != nil {
				t.Fatalf("initiate: %v", err)
			}
			h.storage.put(ticket.Asset.StorageKey, data, domain.PhotoContentType)

			err = h.service.VerifyPhotoAssets(context.Background(), "acct", []string{string(ticket.Asset.ID)})

			if !errors.Is(err, domain.ErrAssetInvalid) {
				t.Fatalf("expected ErrAssetInvalid for checksum mismatch, got %v", err)
			}
		})
	}
}

func TestVerify_OtherAccountsAsset_IsNotFound_NotForbidden(t *testing.T) {
	h := newHarness(true)
	id := h.uploaded(t, "acct_a", jpegBytes(t, 1200, 800), 1200, 800, "cuid")

	err := h.service.VerifyPhotoAssets(context.Background(), "acct_b", []string{id})

	if !errors.Is(err, domain.ErrAssetNotFound) {
		t.Fatalf("expected ErrAssetNotFound, got %v", err)
	}
}

func TestVerify_AlreadyCommitted_PassesWithoutReadingStorage(t *testing.T) {
	h := newHarness(true)
	id := h.uploaded(t, "acct", jpegBytes(t, 1200, 800), 1200, 800, "cuid")
	if err := h.service.CommitPhotoAssets(context.Background(), []string{id}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	h.storage.objects = map[string][]byte{}

	if err := h.service.VerifyPhotoAssets(context.Background(), "acct", []string{id}); err != nil {
		t.Fatalf("committed assets were verified at commit; got %v", err)
	}
}

// MARK: - Commit

func TestCommit_CountMismatch_IsAnError(t *testing.T) {
	h := newHarness(true)
	id := h.uploaded(t, "acct", jpegBytes(t, 1200, 800), 1200, 800, "cuid")
	if err := h.service.CommitPhotoAssets(context.Background(), []string{id}); err != nil {
		t.Fatalf("first commit: %v", err)
	}

	err := h.service.CommitPhotoAssets(context.Background(), []string{id})

	if !errors.Is(err, domain.ErrAssetNotPending) {
		t.Fatalf("expected ErrAssetNotPending, got %v", err)
	}
}

// MARK: - Delivery tickets

func TestDownloadTickets_OnlyForCommittedAssets(t *testing.T) {
	h := newHarness(true)
	pending := h.uploaded(t, "acct", jpegBytes(t, 1200, 800), 1200, 800, "p")
	committed := h.uploaded(t, "acct", jpegBytes(t, 3072, 2048), 3072, 2048, "c")
	if err := h.service.CommitPhotoAssets(context.Background(), []string{committed}); err != nil {
		t.Fatalf("commit: %v", err)
	}

	tickets, err := h.service.IssuePhotoDownloadTickets(context.Background(), "acct", []string{committed})
	if err != nil {
		t.Fatalf("tickets: %v", err)
	}
	if len(tickets) != 1 || tickets[0].PixelWidth != 3072 || tickets[0].PixelHeight != 2048 || tickets[0].URL == "" {
		t.Errorf("ticket must carry URL and stored dimensions: %+v", tickets)
	}

	if _, err := h.service.IssuePhotoDownloadTickets(context.Background(), "acct", []string{pending}); !errors.Is(err, domain.ErrAssetNotFound) {
		t.Errorf("a pending asset has no verified bytes to serve; got %v", err)
	}
	if _, err := h.service.IssuePhotoDownloadTickets(context.Background(), "someone_else", []string{committed}); !errors.Is(err, domain.ErrAssetNotFound) {
		t.Errorf("another account's asset must be not-found; got %v", err)
	}
}

// MARK: - Reclamation

func TestReclaim_DiscardsOnlyStalePendingUploads_BytesFirst(t *testing.T) {
	h := newHarness(true)
	data := jpegBytes(t, 1200, 800)

	stale := h.uploaded(t, "acct", data, 1200, 800, "stale")
	fresh := h.uploaded(t, "acct", data, 1200, 800, "fresh")
	committed := h.uploaded(t, "acct", data, 1200, 800, "committed")
	if err := h.service.CommitPhotoAssets(context.Background(), []string{committed}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	a := h.repo.assets[domain.AssetID(stale)]
	a.CreatedAt = time.Now().Add(-48 * time.Hour)
	h.repo.assets[domain.AssetID(stale)] = a

	n, err := h.service.ReclaimAbandonedUploads(context.Background(), 24*time.Hour, 100)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}

	if n != 1 {
		t.Errorf("reclaimed %d, want 1", n)
	}
	if h.repo.assets[domain.AssetID(stale)].State != domain.StateDiscarded {
		t.Error("stale pending asset must be discarded")
	}
	if h.repo.assets[domain.AssetID(fresh)].State != domain.StatePending {
		t.Error("a fresh pending asset must be left alone")
	}
	if h.repo.assets[domain.AssetID(committed)].State != domain.StateCommitted {
		t.Error("a committed asset must never be reclaimed")
	}
	if len(h.storage.deleted) != 1 || h.storage.deleted[0] != h.repo.assets[domain.AssetID(stale)].StorageKey {
		t.Errorf("exactly the stale object must be deleted; got %v", h.storage.deleted)
	}
}

func TestReclaim_DeleteFailure_LeavesTheRowPendingForRetry(t *testing.T) {
	h := newHarness(true)
	stale := h.uploaded(t, "acct", jpegBytes(t, 1200, 800), 1200, 800, "stale")
	a := h.repo.assets[domain.AssetID(stale)]
	a.CreatedAt = time.Now().Add(-48 * time.Hour)
	h.repo.assets[domain.AssetID(stale)] = a
	h.storage.failDelete = true

	n, err := h.service.ReclaimAbandonedUploads(context.Background(), 24*time.Hour, 100)
	if err != nil {
		t.Fatalf("reclaim must not fail the sweep for one object: %v", err)
	}

	if n != 0 {
		t.Errorf("reclaimed %d, want 0", n)
	}
	if h.repo.assets[domain.AssetID(stale)].State != domain.StatePending {
		t.Error("row must remain pending when its bytes could not be deleted")
	}
}

// MARK: - Release

func (h *harness) committedAsset(t *testing.T, cuid string) string {
	t.Helper()
	id := h.uploaded(t, "acct", jpegBytes(t, 1200, 800), 1200, 800, cuid)
	if err := h.service.CommitPhotoAssets(context.Background(), []string{id}); err != nil {
		t.Fatalf("commit %s: %v", cuid, err)
	}
	return id
}

func (h *harness) ageRelease(id string, by time.Duration) {
	a := h.repo.assets[domain.AssetID(id)]
	past := time.Now().Add(-by)
	a.ReleasedAt = &past
	h.repo.assets[domain.AssetID(id)] = a
}

func TestRelease_MovesACommittedAssetToReleased_AndKeepsItsBytes(t *testing.T) {
	h := newHarness(true)
	id := h.committedAsset(t, "old")

	if err := h.service.ReleasePhotoAssets(context.Background(), []string{id}); err != nil {
		t.Fatalf("release: %v", err)
	}

	asset := h.repo.assets[domain.AssetID(id)]
	if asset.State != domain.StateReleased || asset.ReleasedAt == nil {
		t.Errorf("expected a released asset with a timestamp, got %+v", asset)
	}
	if asset.CommittedAt == nil {
		t.Error("release must not erase the commit timestamp")
	}
	if len(h.storage.deleted) != 0 {
		t.Error("release must not delete bytes — that is the sweep's job, outside any transaction")
	}
}

func TestRelease_RefusesAnythingNotCommitted(t *testing.T) {
	h := newHarness(true)
	pending := h.uploaded(t, "acct", jpegBytes(t, 1200, 800), 1200, 800, "p")
	committed := h.committedAsset(t, "c")

	err := h.service.ReleasePhotoAssets(context.Background(), []string{committed, pending})

	if !errors.Is(err, domain.ErrAssetNotCommitted) {
		t.Fatalf("expected ErrAssetNotCommitted, got %v", err)
	}
	another := h.committedAsset(t, "another")
	if err := h.service.ReleasePhotoAssets(context.Background(), []string{another}); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := h.service.ReleasePhotoAssets(context.Background(), []string{another}); !errors.Is(err, domain.ErrAssetNotCommitted) {
		t.Errorf("a released asset is no longer committed and must not release twice; got %v", err)
	}
}

func TestReleasedAsset_CannotBeServedVerifiedOrReinitiated(t *testing.T) {
	h := newHarness(true)
	data := jpegBytes(t, 1200, 800)
	id := h.uploaded(t, "acct", data, 1200, 800, "cuid")
	if err := h.service.CommitPhotoAssets(context.Background(), []string{id}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := h.service.ReleasePhotoAssets(context.Background(), []string{id}); err != nil {
		t.Fatalf("release: %v", err)
	}

	if _, err := h.service.IssuePhotoDownloadTickets(context.Background(), "acct", []string{id}); !errors.Is(err, domain.ErrAssetNotFound) {
		t.Errorf("a released asset must not be served; got %v", err)
	}
	if err := h.service.VerifyPhotoAssets(context.Background(), "acct", []string{id}); !errors.Is(err, domain.ErrAssetDiscarded) {
		t.Errorf("a released asset must not verify into content again; got %v", err)
	}
	if _, err := h.service.InitiatePhotoUpload(context.Background(), "acct", declarationFor(data, 1200, 800, "cuid")); !errors.Is(err, domain.ErrAssetDiscarded) {
		t.Errorf("no upload URL may be issued for a released key; got %v", err)
	}
	if err := h.service.CommitPhotoAssets(context.Background(), []string{id}); !errors.Is(err, domain.ErrAssetNotPending) {
		t.Errorf("a released asset must not commit again; got %v", err)
	}
}

func TestReclaimReleased_DeletesBytesThenTombstones_OnlyAfterTheGracePeriod(t *testing.T) {
	h := newHarness(true)
	old := h.committedAsset(t, "old")
	fresh := h.committedAsset(t, "fresh")
	kept := h.committedAsset(t, "kept")
	if err := h.service.ReleasePhotoAssets(context.Background(), []string{old, fresh}); err != nil {
		t.Fatalf("release: %v", err)
	}
	h.ageRelease(old, 2*time.Hour)

	n, err := h.service.ReclaimReleasedAssets(context.Background(), time.Hour, 100)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}

	if n != 1 {
		t.Errorf("reclaimed %d, want 1", n)
	}
	if h.repo.assets[domain.AssetID(old)].State != domain.StateDiscarded {
		t.Error("an aged released asset must be discarded")
	}
	if h.repo.assets[domain.AssetID(fresh)].State != domain.StateReleased {
		t.Error("a freshly released asset must wait out the grace period")
	}
	if h.repo.assets[domain.AssetID(kept)].State != domain.StateCommitted {
		t.Error("a committed asset must never be reclaimed")
	}
	if len(h.storage.deleted) != 1 || h.storage.deleted[0] != h.repo.assets[domain.AssetID(old)].StorageKey {
		t.Errorf("exactly the aged released object must be deleted; got %v", h.storage.deleted)
	}
}

func TestReclaimReleased_DeleteFailure_LeavesTheRowReleasedForRetry(t *testing.T) {
	h := newHarness(true)
	old := h.committedAsset(t, "old")
	if err := h.service.ReleasePhotoAssets(context.Background(), []string{old}); err != nil {
		t.Fatalf("release: %v", err)
	}
	h.ageRelease(old, 2*time.Hour)
	h.storage.failDelete = true

	n, err := h.service.ReclaimReleasedAssets(context.Background(), time.Hour, 100)

	if err != nil || n != 0 {
		t.Fatalf("a failed delete must not fail the sweep or count; n=%d err=%v", n, err)
	}
	if h.repo.assets[domain.AssetID(old)].State != domain.StateReleased {
		t.Error("row must remain released when its bytes could not be deleted")
	}
}

func TestReclaim_AbandonedAndReleasedSweeps_DoNotCrossOver(t *testing.T) {
	h := newHarness(true)
	stale := h.uploaded(t, "acct", jpegBytes(t, 1200, 800), 1200, 800, "stale")
	a := h.repo.assets[domain.AssetID(stale)]
	a.CreatedAt = time.Now().Add(-48 * time.Hour)
	h.repo.assets[domain.AssetID(stale)] = a
	old := h.committedAsset(t, "old")
	if err := h.service.ReleasePhotoAssets(context.Background(), []string{old}); err != nil {
		t.Fatalf("release: %v", err)
	}
	h.ageRelease(old, 2*time.Hour)

	if n, _ := h.service.ReclaimAbandonedUploads(context.Background(), 24*time.Hour, 100); n != 1 {
		t.Errorf("abandoned sweep reclaimed %d, want 1", n)
	}
	if h.repo.assets[domain.AssetID(old)].State != domain.StateReleased {
		t.Error("the abandoned sweep must not touch released assets")
	}
	if n, _ := h.service.ReclaimReleasedAssets(context.Background(), time.Hour, 100); n != 1 {
		t.Errorf("released sweep reclaimed %d, want 1", n)
	}
}

func TestReclaim_ThenRetryWithSameClientUploadID_StartsFresh(t *testing.T) {
	h := newHarness(true)
	data := jpegBytes(t, 1200, 800)
	stale := h.uploaded(t, "acct", data, 1200, 800, "cuid")
	a := h.repo.assets[domain.AssetID(stale)]
	a.CreatedAt = time.Now().Add(-48 * time.Hour)
	h.repo.assets[domain.AssetID(stale)] = a
	if _, err := h.service.ReclaimAbandonedUploads(context.Background(), 24*time.Hour, 100); err != nil {
		t.Fatalf("reclaim: %v", err)
	}

	ticket, err := h.service.InitiatePhotoUpload(context.Background(), "acct", declarationFor(data, 1200, 800, "cuid"))
	if err != nil {
		t.Fatalf("re-initiate after reclaim: %v", err)
	}
	if !ticket.Created || string(ticket.Asset.ID) == stale {
		t.Errorf("expected a fresh asset after reclamation, got created=%v id=%s", ticket.Created, ticket.Asset.ID)
	}
}
