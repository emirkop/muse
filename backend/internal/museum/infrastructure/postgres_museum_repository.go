package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"muse-backend/internal/museum/domain"
	"muse-backend/internal/platform/database"
)

const (
	postgresUniqueViolation     = "23505"
	postgresForeignKeyViolation = "23503"
	postgresCheckViolation      = "23514"
)

type PostgresMuseumRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresMuseumRepository(pool *pgxpool.Pool) *PostgresMuseumRepository {
	return &PostgresMuseumRepository{pool: pool}
}

func (r *PostgresMuseumRepository) db(ctx context.Context) database.Executor {
	return database.ExecutorFrom(ctx, r.pool)
}

func (r *PostgresMuseumRepository) CreateMuseum(ctx context.Context, museum domain.Museum) (domain.Museum, error) {
	const query = `
		INSERT INTO museums (account_id, style_id, privacy)
		VALUES ($1, $2, $3)
		RETURNING id, account_id, style_id, privacy, created_at, updated_at
	`
	row := r.db(ctx).QueryRow(ctx, query, museum.AccountID, museum.StyleID, string(museum.Privacy))
	created, err := scanMuseum(row)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Museum{}, domain.ErrMuseumAlreadyExists
		}
		return domain.Museum{}, fmt.Errorf("postgres_museum_repository: create museum: %w", err)
	}
	return created, nil
}

func (r *PostgresMuseumRepository) FindMuseumByAccount(ctx context.Context, accountID string) (domain.Museum, error) {
	const query = `
		SELECT id, account_id, style_id, privacy, created_at, updated_at
		FROM museums
		WHERE account_id = $1
	`
	museum, err := scanMuseum(r.db(ctx).QueryRow(ctx, query, accountID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Museum{}, domain.ErrMuseumNotFound
	}
	if err != nil {
		return domain.Museum{}, fmt.Errorf("postgres_museum_repository: find museum: %w", err)
	}
	return museum, nil
}

func (r *PostgresMuseumRepository) FindMuseumByID(ctx context.Context, id domain.MuseumID) (domain.Museum, error) {
	if !IsPlausibleUUID(string(id)) {
		return domain.Museum{}, domain.ErrMuseumNotFound
	}
	const query = `
		SELECT id, account_id, style_id, privacy, created_at, updated_at
		FROM museums
		WHERE id = $1
	`
	museum, err := scanMuseum(r.db(ctx).QueryRow(ctx, query, string(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Museum{}, domain.ErrMuseumNotFound
	}
	if err != nil {
		return domain.Museum{}, fmt.Errorf("postgres_museum_repository: find museum by id: %w", err)
	}
	return museum, nil
}

func (r *PostgresMuseumRepository) UpdateMuseumStyle(ctx context.Context, id domain.MuseumID, styleID string) error {
	const query = `UPDATE museums SET style_id = $1, updated_at = now() WHERE id = $2`
	tag, err := r.db(ctx).Exec(ctx, query, styleID, string(id))
	if err != nil {
		return fmt.Errorf("postgres_museum_repository: update style: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMuseumNotFound
	}
	return nil
}

func (r *PostgresMuseumRepository) UpdateMuseumPrivacy(ctx context.Context, id domain.MuseumID, privacy domain.Privacy) error {
	const query = `UPDATE museums SET privacy = $1, updated_at = now() WHERE id = $2`
	tag, err := r.db(ctx).Exec(ctx, query, string(privacy), string(id))
	if err != nil {
		return fmt.Errorf("postgres_museum_repository: update privacy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMuseumNotFound
	}
	return nil
}

func (r *PostgresMuseumRepository) CreateRoom(ctx context.Context, room domain.Room) (domain.Room, error) {
	const query = `
		INSERT INTO rooms (museum_id, name, variant_id, privacy)
		VALUES ($1, $2, $3, $4)
		RETURNING id, museum_id, name, variant_id, privacy, music_track_id, created_at, updated_at
	`
	row := r.db(ctx).QueryRow(ctx, query, string(room.MuseumID), room.Name, room.VariantID, string(room.Privacy))
	created, err := scanRoom(row)
	if err != nil {
		return domain.Room{}, fmt.Errorf("postgres_museum_repository: create room: %w", err)
	}
	return created, nil
}

func (r *PostgresMuseumRepository) ListRooms(ctx context.Context, museumID domain.MuseumID) ([]domain.Room, error) {
	const query = `
		SELECT id, museum_id, name, variant_id, privacy, music_track_id, created_at, updated_at
		FROM rooms
		WHERE museum_id = $1
		ORDER BY created_at
	`
	rows, err := r.db(ctx).Query(ctx, query, string(museumID))
	if err != nil {
		return nil, fmt.Errorf("postgres_museum_repository: list rooms: %w", err)
	}
	defer rows.Close()

	var rooms []domain.Room
	for rows.Next() {
		room, err := scanRoom(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres_museum_repository: scan room: %w", err)
		}
		rooms = append(rooms, room)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for index := range rooms {
		if err := r.loadRoomContent(ctx, &rooms[index]); err != nil {
			return nil, err
		}
	}
	return rooms, nil
}

func (r *PostgresMuseumRepository) FindRoom(ctx context.Context, id domain.RoomID) (domain.Room, error) {
	if !IsPlausibleUUID(string(id)) {
		return domain.Room{}, domain.ErrRoomNotFound
	}
	const query = `
		SELECT id, museum_id, name, variant_id, privacy, music_track_id, created_at, updated_at
		FROM rooms
		WHERE id = $1
	`
	room, err := scanRoom(r.db(ctx).QueryRow(ctx, query, string(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Room{}, domain.ErrRoomNotFound
	}
	if err != nil {
		return domain.Room{}, fmt.Errorf("postgres_museum_repository: find room: %w", err)
	}
	if err := r.loadRoomContent(ctx, &room); err != nil {
		return domain.Room{}, err
	}
	return room, nil
}

func (r *PostgresMuseumRepository) UpdateRoom(ctx context.Context, id domain.RoomID, patch domain.RoomPatch) error {
	sets := []string{"updated_at = now()"}
	args := []any{}
	if patch.Name != nil {
		args = append(args, *patch.Name)
		sets = append(sets, fmt.Sprintf("name = $%d", len(args)))
	}
	if patch.VariantID != nil {
		args = append(args, *patch.VariantID)
		sets = append(sets, fmt.Sprintf("variant_id = $%d", len(args)))
	}
	if patch.Privacy != nil {
		args = append(args, string(*patch.Privacy))
		sets = append(sets, fmt.Sprintf("privacy = $%d", len(args)))
	}
	args = append(args, string(id))
	query := fmt.Sprintf("UPDATE rooms SET %s WHERE id = $%d", strings.Join(sets, ", "), len(args))

	tag, err := r.db(ctx).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("postgres_museum_repository: update room: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRoomNotFound
	}
	return nil
}

func (r *PostgresMuseumRepository) DeleteRoom(ctx context.Context, id domain.RoomID) error {
	const query = `DELETE FROM rooms WHERE id = $1`
	tag, err := r.db(ctx).Exec(ctx, query, string(id))
	if err != nil {
		return fmt.Errorf("postgres_museum_repository: delete room: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRoomNotFound
	}
	return nil
}

func (r *PostgresMuseumRepository) loadRoomContent(ctx context.Context, room *domain.Room) error {
	const photoQuery = `
		SELECT slot_index, photo_asset_id, caption, created_at, updated_at
		FROM room_photo_slots
		WHERE room_id = $1
		ORDER BY slot_index
	`
	photoRows, err := r.db(ctx).Query(ctx, photoQuery, string(room.ID))
	if err != nil {
		return fmt.Errorf("postgres_museum_repository: load photo slots: %w", err)
	}
	defer photoRows.Close()

	room.PhotoSlots = nil
	for photoRows.Next() {
		var (
			slot    domain.PhotoSlotAssignment
			assetID *string
		)
		if err := photoRows.Scan(&slot.SlotIndex, &assetID, &slot.Caption, &slot.CreatedAt, &slot.UpdatedAt); err != nil {
			return fmt.Errorf("postgres_museum_repository: scan photo slot: %w", err)
		}
		if assetID != nil {
			slot.PhotoAssetID = *assetID
		}
		room.PhotoSlots = append(room.PhotoSlots, slot)
	}
	if err := photoRows.Err(); err != nil {
		return err
	}

	const sculptureQuery = `
		SELECT slot_index, catalog_id, created_at
		FROM room_sculptures
		WHERE room_id = $1
		ORDER BY slot_index
	`
	sculptureRows, err := r.db(ctx).Query(ctx, sculptureQuery, string(room.ID))
	if err != nil {
		return fmt.Errorf("postgres_museum_repository: load sculptures: %w", err)
	}
	defer sculptureRows.Close()

	room.Sculptures = nil
	for sculptureRows.Next() {
		var sculpture domain.SculptureInstance
		if err := sculptureRows.Scan(&sculpture.SlotIndex, &sculpture.CatalogID, &sculpture.CreatedAt); err != nil {
			return fmt.Errorf("postgres_museum_repository: scan sculpture: %w", err)
		}
		room.Sculptures = append(room.Sculptures, sculpture)
	}
	return sculptureRows.Err()
}

// MARK: - Photo assignment

func (r *PostgresMuseumRepository) LockRoomForUpdate(ctx context.Context, id domain.RoomID) (domain.Room, error) {
	const query = `
		SELECT id, museum_id, name, variant_id, privacy, music_track_id, created_at, updated_at
		FROM rooms
		WHERE id = $1
		FOR UPDATE
	`
	room, err := scanRoom(r.db(ctx).QueryRow(ctx, query, string(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Room{}, domain.ErrRoomNotFound
	}
	if err != nil {
		return domain.Room{}, fmt.Errorf("postgres_museum_repository: lock room: %w", err)
	}
	if err := r.loadRoomContent(ctx, &room); err != nil {
		return domain.Room{}, err
	}
	return room, nil
}

func (r *PostgresMuseumRepository) InsertPhotoSlots(ctx context.Context, roomID domain.RoomID, slots []domain.PhotoSlotAssignment) error {
	const query = `
		INSERT INTO room_photo_slots (room_id, slot_index, photo_asset_id, caption, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	for _, slot := range slots {
		_, err := r.db(ctx).Exec(ctx, query,
			string(roomID), slot.SlotIndex, slot.PhotoAssetID, slot.Caption, slot.CreatedAt, slot.UpdatedAt)
		if err == nil {
			continue
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == postgresUniqueViolation {
			switch pgErr.ConstraintName {
			case "room_photo_slots_photo_asset_unique":
				return &domain.PhotoAssetError{AssetID: slot.PhotoAssetID, Err: domain.ErrPhotoAssetAlreadyAssigned}
			default:
				return domain.ErrSlotOccupied
			}
		}
		return fmt.Errorf("postgres_museum_repository: insert photo slot: %w", err)
	}
	return nil
}

func (r *PostgresMuseumRepository) FindPhotoSlotRoomsByAssetIDs(ctx context.Context, assetIDs []string) (map[string]domain.RoomID, error) {
	if len(assetIDs) == 0 {
		return map[string]domain.RoomID{}, nil
	}
	const query = `
		SELECT photo_asset_id, room_id
		FROM room_photo_slots
		WHERE photo_asset_id = ANY($1::uuid[])
	`
	rows, err := r.db(ctx).Query(ctx, query, assetIDs)
	if err != nil {
		return nil, fmt.Errorf("postgres_museum_repository: find slots by asset: %w", err)
	}
	defer rows.Close()

	result := make(map[string]domain.RoomID)
	for rows.Next() {
		var assetID, roomID string
		if err := rows.Scan(&assetID, &roomID); err != nil {
			return nil, fmt.Errorf("postgres_museum_repository: scan slot by asset: %w", err)
		}
		result[assetID] = domain.RoomID(roomID)
	}
	return result, rows.Err()
}

func (r *PostgresMuseumRepository) ReorderPhotoSlots(ctx context.Context, roomID domain.RoomID, orderedAssetIDs []string) error {
	if len(orderedAssetIDs) == 0 {
		return domain.ErrInvalidPhotoOrder
	}
	db := r.db(ctx)

	if _, err := db.Exec(ctx, `SET CONSTRAINTS room_photo_slots_unique_slot DEFERRED`); err != nil {
		return fmt.Errorf("postgres_museum_repository: defer slot uniqueness: %w", err)
	}

	const query = `
		UPDATE room_photo_slots AS s
		SET slot_index = v.new_index, updated_at = now()
		FROM (
			SELECT unnest($2::uuid[]) AS photo_asset_id,
			       generate_subscripts($2::uuid[], 1) - 1 AS new_index
		) AS v
		WHERE s.room_id = $1 AND s.photo_asset_id = v.photo_asset_id
	`
	tag, err := db.Exec(ctx, query, string(roomID), orderedAssetIDs)
	if err != nil {
		return fmt.Errorf("postgres_museum_repository: reorder photo slots: %w", err)
	}
	if tag.RowsAffected() != int64(len(orderedAssetIDs)) {
		return domain.ErrPhotoOrderMismatch
	}
	return nil
}

func (r *PostgresMuseumRepository) UpdatePhotoCaption(ctx context.Context, roomID domain.RoomID, photoAssetID string, caption string) error {
	const query = `
		UPDATE room_photo_slots
		SET caption = $3, updated_at = now()
		WHERE room_id = $1 AND photo_asset_id = $2
	`
	tag, err := r.db(ctx).Exec(ctx, query, string(roomID), photoAssetID, caption)
	if err != nil {
		return fmt.Errorf("postgres_museum_repository: update photo caption: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrPhotoNotInRoom
	}
	return nil
}

func (r *PostgresMuseumRepository) ReplacePhotoSlotAsset(ctx context.Context, roomID domain.RoomID, photoAssetID string, replacementAssetID string) error {
	const query = `
		UPDATE room_photo_slots
		SET photo_asset_id = $3, updated_at = now()
		WHERE room_id = $1 AND photo_asset_id = $2
	`
	tag, err := r.db(ctx).Exec(ctx, query, string(roomID), photoAssetID, replacementAssetID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case postgresUniqueViolation:
				return &domain.PhotoAssetError{AssetID: replacementAssetID, Err: domain.ErrPhotoAssetAlreadyAssigned}
			case postgresForeignKeyViolation:
				return &domain.PhotoAssetError{AssetID: replacementAssetID, Err: domain.ErrPhotoAssetNotFound}
			}
		}
		return fmt.Errorf("postgres_museum_repository: replace photo slot asset: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrPhotoNotInRoom
	}
	return nil
}

func (r *PostgresMuseumRepository) DeletePhotoSlotCompacting(ctx context.Context, roomID domain.RoomID, photoAssetID string) error {
	db := r.db(ctx)

	var removedIndex int
	err := db.QueryRow(ctx,
		`DELETE FROM room_photo_slots WHERE room_id = $1 AND photo_asset_id = $2 RETURNING slot_index`,
		string(roomID), photoAssetID,
	).Scan(&removedIndex)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrPhotoNotInRoom
	}
	if err != nil {
		return fmt.Errorf("postgres_museum_repository: delete photo slot: %w", err)
	}

	if _, err := db.Exec(ctx, `SET CONSTRAINTS room_photo_slots_unique_slot DEFERRED`); err != nil {
		return fmt.Errorf("postgres_museum_repository: defer slot uniqueness: %w", err)
	}
	const compact = `
		UPDATE room_photo_slots
		SET slot_index = slot_index - 1, updated_at = now()
		WHERE room_id = $1 AND slot_index > $2
	`
	if _, err := db.Exec(ctx, compact, string(roomID), removedIndex); err != nil {
		return fmt.Errorf("postgres_museum_repository: compact photo slots: %w", err)
	}
	return nil
}

// MARK: - Sculptures

func (r *PostgresMuseumRepository) InsertSculpture(ctx context.Context, roomID domain.RoomID, sculpture domain.SculptureInstance) error {
	const query = `
		INSERT INTO room_sculptures (room_id, slot_index, catalog_id, created_at)
		VALUES ($1, $2, $3, $4)
	`
	_, err := r.db(ctx).Exec(ctx, query, string(roomID), sculpture.SlotIndex, sculpture.CatalogID, sculpture.CreatedAt)
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case postgresUniqueViolation:
			return domain.ErrSlotOccupied
		case postgresForeignKeyViolation:
			return domain.ErrUnknownSculpture
		case postgresCheckViolation:
			return domain.ErrInvalidSculptureSlot
		}
	}
	return fmt.Errorf("postgres_museum_repository: insert sculpture: %w", err)
}

func (r *PostgresMuseumRepository) DeleteSculpture(ctx context.Context, roomID domain.RoomID, slotIndex int) error {
	const query = `DELETE FROM room_sculptures WHERE room_id = $1 AND slot_index = $2`
	tag, err := r.db(ctx).Exec(ctx, query, string(roomID), slotIndex)
	if err != nil {
		return fmt.Errorf("postgres_museum_repository: delete sculpture: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrSculptureNotInRoom
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanMuseum(row rowScanner) (domain.Museum, error) {
	var (
		id, accountID, styleID, privacy string
		createdAt, updatedAt            time.Time
	)
	if err := row.Scan(&id, &accountID, &styleID, &privacy, &createdAt, &updatedAt); err != nil {
		return domain.Museum{}, err
	}
	return domain.Museum{
		ID:        domain.MuseumID(id),
		AccountID: accountID,
		StyleID:   styleID,
		Privacy:   domain.Privacy(privacy),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func scanRoom(row rowScanner) (domain.Room, error) {
	var (
		id, museumID, name, variantID, privacy string
		createdAt, updatedAt                   time.Time
		musicTrackID                           *string
	)
	if err := row.Scan(&id, &museumID, &name, &variantID, &privacy, &musicTrackID, &createdAt, &updatedAt); err != nil {
		return domain.Room{}, err
	}
	room := domain.Room{
		ID:        domain.RoomID(id),
		MuseumID:  domain.MuseumID(museumID),
		Name:      name,
		VariantID: variantID,
		Privacy:   domain.Privacy(privacy),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
	if musicTrackID != nil {
		room.MusicTrackID = *musicTrackID
	}
	return room, nil
}

func (r *PostgresMuseumRepository) SetRoomMusic(ctx context.Context, id domain.RoomID, trackID *string) error {
	const query = `UPDATE rooms SET music_track_id = $1, updated_at = now() WHERE id = $2`
	tag, err := r.db(ctx).Exec(ctx, query, trackID, string(id))
	if err != nil {
		return fmt.Errorf("postgres_museum_repository: set room music: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRoomNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == postgresUniqueViolation
}
