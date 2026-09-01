package application_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"muse-backend/internal/museum/application"
	"muse-backend/internal/museum/domain"
)

type fakeCatalog struct {
	styles      map[string]bool
	variants    map[string]string
	sculptures  map[string]bool
	musicTracks map[string]bool
}

func newFakeCatalog() *fakeCatalog {
	return &fakeCatalog{
		styles: map[string]bool{"style_modern": true, "style_gothic": true},
		variants: map[string]string{
			"modern_hall":  "style_modern",
			"gothic_crypt": "style_gothic",
		},
		sculptures: map[string]bool{},
	}
}

func (f *fakeCatalog) SculptureExists(_ context.Context, sculptureID string) (bool, error) {
	return f.sculptures[sculptureID], nil
}

func (f *fakeCatalog) MusicTrackExists(_ context.Context, trackID string) (bool, error) {
	return f.musicTracks[trackID], nil
}

func (f *fakeCatalog) StyleExists(_ context.Context, styleID string) (bool, error) {
	return f.styles[styleID], nil
}

func (f *fakeCatalog) VariantStyle(_ context.Context, variantID string) (string, bool, error) {
	styleID, found := f.variants[variantID]
	return styleID, found, nil
}

type fakeRepo struct {
	mu                  sync.Mutex
	museumsByAccount    map[string]domain.Museum
	roomsByID           map[domain.RoomID]domain.Room
	nextID              int
	styleUpdates        int
	roomWrites          int
	lockCalls           int
	failInsert          error
	reorderCalls        int
	failReorder         error
	captionWrites       int
	failCaption         error
	replaceCalls        int
	failReplace         error
	deleteCalls         int
	failDelete          error
	sculptureWrites     int
	failInsertSculpture error
	failDeleteSculpture error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		museumsByAccount: map[string]domain.Museum{},
		roomsByID:        map[domain.RoomID]domain.Room{},
	}
}

func (f *fakeRepo) CreateMuseum(_ context.Context, museum domain.Museum) (domain.Museum, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.museumsByAccount[museum.AccountID]; exists {
		return domain.Museum{}, domain.ErrMuseumAlreadyExists
	}
	f.nextID++
	museum.ID = domain.MuseumID(fmt.Sprintf("museum_%d", f.nextID))
	f.museumsByAccount[museum.AccountID] = museum
	return museum, nil
}

func (f *fakeRepo) FindMuseumByAccount(_ context.Context, accountID string) (domain.Museum, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	museum, ok := f.museumsByAccount[accountID]
	if !ok {
		return domain.Museum{}, domain.ErrMuseumNotFound
	}
	return museum, nil
}

func (f *fakeRepo) UpdateMuseumStyle(_ context.Context, id domain.MuseumID, styleID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.styleUpdates++
	for accountID, museum := range f.museumsByAccount {
		if museum.ID == id {
			museum.StyleID = styleID
			f.museumsByAccount[accountID] = museum
			return nil
		}
	}
	return domain.ErrMuseumNotFound
}

func (f *fakeRepo) UpdateMuseumPrivacy(_ context.Context, id domain.MuseumID, privacy domain.Privacy) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for accountID, museum := range f.museumsByAccount {
		if museum.ID == id {
			museum.Privacy = privacy
			f.museumsByAccount[accountID] = museum
			return nil
		}
	}
	return domain.ErrMuseumNotFound
}

func (f *fakeRepo) CreateRoom(_ context.Context, room domain.Room) (domain.Room, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.roomWrites++
	room.ID = domain.RoomID(fmt.Sprintf("room_%d", f.nextID))
	f.roomsByID[room.ID] = room
	return room, nil
}

func (f *fakeRepo) ListRooms(_ context.Context, museumID domain.MuseumID) ([]domain.Room, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var rooms []domain.Room
	for _, room := range f.roomsByID {
		if room.MuseumID == museumID {
			rooms = append(rooms, room)
		}
	}
	return rooms, nil
}

func (f *fakeRepo) FindRoom(_ context.Context, id domain.RoomID) (domain.Room, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	room, ok := f.roomsByID[id]
	if !ok {
		return domain.Room{}, domain.ErrRoomNotFound
	}
	return room, nil
}

func (f *fakeRepo) UpdateRoom(_ context.Context, id domain.RoomID, patch domain.RoomPatch) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	room, ok := f.roomsByID[id]
	if !ok {
		return domain.ErrRoomNotFound
	}
	f.roomWrites++
	if patch.Name != nil {
		room.Name = *patch.Name
	}
	if patch.VariantID != nil {
		room.VariantID = *patch.VariantID
	}
	if patch.Privacy != nil {
		room.Privacy = *patch.Privacy
	}
	f.roomsByID[id] = room
	return nil
}

func (f *fakeRepo) SetRoomMusic(_ context.Context, id domain.RoomID, trackID *string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	room, ok := f.roomsByID[id]
	if !ok {
		return domain.ErrRoomNotFound
	}
	f.roomWrites++
	if trackID == nil {
		room.MusicTrackID = ""
	} else {
		room.MusicTrackID = *trackID
	}
	f.roomsByID[id] = room
	return nil
}

func (f *fakeRepo) FindMuseumByID(_ context.Context, id domain.MuseumID) (domain.Museum, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, museum := range f.museumsByAccount {
		if museum.ID == id {
			return museum, nil
		}
	}
	return domain.Museum{}, domain.ErrMuseumNotFound
}

func (f *fakeRepo) DeleteRoom(_ context.Context, id domain.RoomID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.roomsByID[id]; !ok {
		return domain.ErrRoomNotFound
	}
	f.roomWrites++
	delete(f.roomsByID, id)
	return nil
}

func newService() (*application.MuseumService, *fakeRepo) {
	service, repo, _ := newServiceWithCatalog()
	return service, repo
}

func newServiceWithCatalog() (*application.MuseumService, *fakeRepo, *fakeCatalog) {
	repo := newFakeRepo()
	catalog := newFakeCatalog()
	return application.NewMuseumService(repo, catalog), repo, catalog
}

// MARK: - Style validation

func TestCreateMuseum_UnknownStyle_IsRejected(t *testing.T) {
	service, _ := newService()

	_, err := service.CreateMuseum(context.Background(), "account_1", "style_invented")

	if !errors.Is(err, domain.ErrUnknownStyle) {
		t.Fatalf("expected ErrUnknownStyle, got %v", err)
	}
}

func TestCreateMuseum_DefaultsToPrivate(t *testing.T) {
	service, _ := newService()

	museum, err := service.CreateMuseum(context.Background(), "account_1", "style_modern")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if museum.Privacy != domain.PrivacyPrivate {
		t.Fatalf("a new Museum must not be publicly visible before its owner shares it, got %q", museum.Privacy)
	}
}

// MARK: - Style change touches nothing but the reference

func TestChangeStyle_WritesOnlyTheStyleReference(t *testing.T) {
	service, repo := newService()
	ctx := context.Background()

	if _, err := service.CreateMuseum(ctx, "account_1", "style_modern"); err != nil {
		t.Fatalf("create museum: %v", err)
	}
	if _, err := service.CreateRoom(ctx, "account_1", "Hall", "modern_hall"); err != nil {
		t.Fatalf("create room: %v", err)
	}
	roomWritesBefore := repo.roomWrites

	if err := service.ChangeStyle(ctx, "account_1", "style_gothic"); err != nil {
		t.Fatalf("change style: %v", err)
	}

	if repo.styleUpdates != 1 {
		t.Errorf("expected exactly one style update, got %d", repo.styleUpdates)
	}
	if repo.roomWrites != roomWritesBefore {
		t.Errorf("changing style wrote to Room content %d times — it must be a pure reference swap", repo.roomWrites-roomWritesBefore)
	}
}

func TestChangeStyle_UnknownStyle_IsRejected(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()
	if _, err := service.CreateMuseum(ctx, "account_1", "style_modern"); err != nil {
		t.Fatalf("create: %v", err)
	}

	err := service.ChangeStyle(ctx, "account_1", "style_invented")

	if !errors.Is(err, domain.ErrUnknownStyle) {
		t.Fatalf("expected ErrUnknownStyle, got %v", err)
	}
}

// MARK: - Variant scoping

func TestCreateRoom_VariantFromAnotherStyle_IsRejected(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()
	if _, err := service.CreateMuseum(ctx, "account_1", "style_modern"); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := service.CreateRoom(ctx, "account_1", "Crypt", "gothic_crypt")

	if !errors.Is(err, domain.ErrVariantStyleMismatch) {
		t.Fatalf("expected ErrVariantStyleMismatch, got %v", err)
	}
}

func TestCreateRoom_UnknownVariant_IsRejected(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()
	if _, err := service.CreateMuseum(ctx, "account_1", "style_modern"); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := service.CreateRoom(ctx, "account_1", "Nowhere", "variant_invented")

	if !errors.Is(err, domain.ErrUnknownVariant) {
		t.Fatalf("expected ErrUnknownVariant, got %v", err)
	}
}

// MARK: - Ownership enforced server-side

func TestFindRoom_BelongingToAnotherAccount_IsForbidden(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()

	if _, err := service.CreateMuseum(ctx, "owner", "style_modern"); err != nil {
		t.Fatalf("create owner museum: %v", err)
	}
	room, err := service.CreateRoom(ctx, "owner", "Private Hall", "modern_hall")
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	if _, err := service.CreateMuseum(ctx, "intruder", "style_modern"); err != nil {
		t.Fatalf("create intruder museum: %v", err)
	}

	_, err = service.FindRoom(ctx, "intruder", room.ID)

	if !errors.Is(err, domain.ErrNotOwner) {
		t.Fatalf("expected ErrNotOwner, got %v", err)
	}
}

func TestUpdateAndDeleteRoom_BelongingToAnotherAccount_AreForbidden(t *testing.T) {
	service, repo := newService()
	ctx := context.Background()

	if _, err := service.CreateMuseum(ctx, "owner", "style_modern"); err != nil {
		t.Fatalf("create owner museum: %v", err)
	}
	room, _ := service.CreateRoom(ctx, "owner", "Hall", "modern_hall")
	if _, err := service.CreateMuseum(ctx, "intruder", "style_modern"); err != nil {
		t.Fatalf("create intruder museum: %v", err)
	}
	writesBefore := repo.roomWrites

	if err := service.UpdateRoom(ctx, "intruder", room.ID, patchOf("Hijacked", "modern_hall", domain.PrivacyPublic)); !errors.Is(err, domain.ErrNotOwner) {
		t.Fatalf("update: expected ErrNotOwner, got %v", err)
	}
	if err := service.DeleteRoom(ctx, "intruder", room.ID); !errors.Is(err, domain.ErrNotOwner) {
		t.Fatalf("delete: expected ErrNotOwner, got %v", err)
	}

	if repo.roomWrites != writesBefore {
		t.Error("a forbidden request must not reach the repository at all")
	}
}

func TestOperations_WithoutAMuseum_ReportNotFound(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()

	if _, err := service.ListRooms(ctx, "account_without_museum"); !errors.Is(err, domain.ErrMuseumNotFound) {
		t.Fatalf("expected ErrMuseumNotFound, got %v", err)
	}
}

// MARK: - Privacy validation

func TestChangePrivacy_InvalidValue_IsRejected(t *testing.T) {
	service, _ := newService()
	ctx := context.Background()
	if _, err := service.CreateMuseum(ctx, "account_1", "style_modern"); err != nil {
		t.Fatalf("create: %v", err)
	}

	err := service.ChangePrivacy(ctx, "account_1", domain.Privacy("password-protected"))

	if !errors.Is(err, domain.ErrInvalidPrivacy) {
		t.Fatalf("expected ErrInvalidPrivacy, got %v", err)
	}
}
