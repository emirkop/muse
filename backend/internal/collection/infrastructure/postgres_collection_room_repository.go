package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"muse-backend/internal/collection/domain"
	"muse-backend/internal/platform/database"
)

type PostgresCollectionRoomRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresCollectionRoomRepository(pool *pgxpool.Pool) *PostgresCollectionRoomRepository {
	return &PostgresCollectionRoomRepository{pool: pool}
}

func (r *PostgresCollectionRoomRepository) db(ctx context.Context) database.Executor {
	return database.ExecutorFrom(ctx, r.pool)
}

const collectionRoomColumns = `id, account_id, name,
	COALESCE(category_id, ''), COALESCE(design_id, ''),
	current_tier, COALESCE(music_track_id, ''), created_at, updated_at`

func (r *PostgresCollectionRoomRepository) Create(
	ctx context.Context,
	room domain.CollectionRoom,
) (domain.CollectionRoom, error) {
	const query = `
		INSERT INTO collection_rooms (account_id, name, category_id, design_id, current_tier)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5)
		RETURNING ` + collectionRoomColumns
	row := r.db(ctx).QueryRow(ctx, query,
		room.AccountID, room.Name, room.CategoryID, room.DesignID, int(room.CurrentTier))
	created, err := scanCollectionRoom(row)
	if err != nil {
		return domain.CollectionRoom{}, fmt.Errorf("postgres_collection_room_repository: create: %w", err)
	}
	created.Items = []domain.CollectionItem{}
	return created, nil
}

func (r *PostgresCollectionRoomRepository) ListForAccount(
	ctx context.Context,
	accountID string,
) ([]domain.CollectionRoom, error) {
	if !IsPlausibleUUID(accountID) {
		return []domain.CollectionRoom{}, nil
	}
	const query = `
		SELECT ` + collectionRoomColumns + `
		FROM collection_rooms
		WHERE account_id = $1
		ORDER BY created_at, id
	`
	rows, err := r.db(ctx).Query(ctx, query, accountID)
	if err != nil {
		return nil, fmt.Errorf("postgres_collection_room_repository: list: %w", err)
	}
	defer rows.Close()

	rooms := []domain.CollectionRoom{}
	for rows.Next() {
		room, err := scanCollectionRoom(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres_collection_room_repository: scan: %w", err)
		}
		rooms = append(rooms, room)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres_collection_room_repository: list rows: %w", err)
	}
	if err := r.loadItemsForRooms(ctx, rooms); err != nil {
		return nil, err
	}
	return rooms, nil
}

func (r *PostgresCollectionRoomRepository) Find(
	ctx context.Context,
	id domain.CollectionRoomID,
) (domain.CollectionRoom, error) {
	if !IsPlausibleUUID(string(id)) {
		return domain.CollectionRoom{}, domain.ErrCollectionRoomNotFound
	}
	const query = `
		SELECT ` + collectionRoomColumns + `
		FROM collection_rooms
		WHERE id = $1
	`
	room, err := scanCollectionRoom(r.db(ctx).QueryRow(ctx, query, string(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CollectionRoom{}, domain.ErrCollectionRoomNotFound
	}
	if err != nil {
		return domain.CollectionRoom{}, fmt.Errorf("postgres_collection_room_repository: find: %w", err)
	}
	if err := r.loadItems(ctx, &room); err != nil {
		return domain.CollectionRoom{}, err
	}
	return room, nil
}

func (r *PostgresCollectionRoomRepository) Update(
	ctx context.Context,
	id domain.CollectionRoomID,
	patch domain.CollectionRoomPatch,
) error {
	if !IsPlausibleUUID(string(id)) {
		return domain.ErrCollectionRoomNotFound
	}

	sets := []string{"updated_at = now()"}
	args := []any{}
	if patch.Name != nil {
		args = append(args, *patch.Name)
		sets = append(sets, fmt.Sprintf("name = $%d", len(args)))
	}
	if patch.CategoryID != nil {
		args = append(args, nullIfEmpty(*patch.CategoryID))
		sets = append(sets, fmt.Sprintf("category_id = $%d", len(args)))
	}
	if patch.DesignID != nil {
		args = append(args, nullIfEmpty(*patch.DesignID))
		sets = append(sets, fmt.Sprintf("design_id = $%d", len(args)))
	}
	if len(args) == 0 {
		return domain.ErrEmptyPatch
	}

	args = append(args, string(id))
	query := fmt.Sprintf(
		"UPDATE collection_rooms SET %s WHERE id = $%d",
		strings.Join(sets, ", "), len(args),
	)
	tag, err := r.db(ctx).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("postgres_collection_room_repository: update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrCollectionRoomNotFound
	}
	return nil
}

func (r *PostgresCollectionRoomRepository) SetMusic(ctx context.Context, id domain.CollectionRoomID, trackID *string) error {
	if !IsPlausibleUUID(string(id)) {
		return domain.ErrCollectionRoomNotFound
	}
	const query = `UPDATE collection_rooms SET music_track_id = $1, updated_at = now() WHERE id = $2`
	tag, err := r.db(ctx).Exec(ctx, query, trackID, string(id))
	if err != nil {
		return fmt.Errorf("postgres_collection_room_repository: set music: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrCollectionRoomNotFound
	}
	return nil
}

func (r *PostgresCollectionRoomRepository) RatchetTier(
	ctx context.Context,
	id domain.CollectionRoomID,
	tier domain.Tier,
) (raised bool, err error) {
	if !IsPlausibleUUID(string(id)) {
		return false, domain.ErrCollectionRoomNotFound
	}
	const query = `
		UPDATE collection_rooms
		   SET current_tier = $2, updated_at = now()
		 WHERE id = $1 AND current_tier < $2
	`
	tag, err := r.db(ctx).Exec(ctx, query, string(id), int(tier))
	if err != nil {
		return false, fmt.Errorf("postgres_collection_room_repository: ratchet tier: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *PostgresCollectionRoomRepository) LockRoomForUpdate(
	ctx context.Context,
	id domain.CollectionRoomID,
) (domain.CollectionRoom, error) {
	if !IsPlausibleUUID(string(id)) {
		return domain.CollectionRoom{}, domain.ErrCollectionRoomNotFound
	}
	const query = `
		SELECT ` + collectionRoomColumns + `
		FROM collection_rooms
		WHERE id = $1
		FOR UPDATE
	`
	room, err := scanCollectionRoom(r.db(ctx).QueryRow(ctx, query, string(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CollectionRoom{}, domain.ErrCollectionRoomNotFound
	}
	if err != nil {
		return domain.CollectionRoom{}, fmt.Errorf("postgres_collection_room_repository: lock room: %w", err)
	}
	if err := r.loadItems(ctx, &room); err != nil {
		return domain.CollectionRoom{}, err
	}
	return room, nil
}

func (r *PostgresCollectionRoomRepository) LockAccountItems(ctx context.Context, accountID string) error {
	if _, err := r.db(ctx).Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('collection_items:' || $1::text))`, accountID); err != nil {
		return fmt.Errorf("postgres_collection_room_repository: lock account items: %w", err)
	}
	return nil
}

func (r *PostgresCollectionRoomRepository) CountItemsForAccount(ctx context.Context, accountID string) (int, error) {
	if !IsPlausibleUUID(accountID) {
		return 0, nil
	}
	var count int
	err := r.db(ctx).QueryRow(ctx, `
		SELECT count(*) FROM collection_items i
		JOIN collection_rooms r ON r.id = i.collection_room_id
		WHERE r.account_id = $1`, accountID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("postgres_collection_room_repository: count items for account: %w", err)
	}
	return count, nil
}

func (r *PostgresCollectionRoomRepository) InsertItem(
	ctx context.Context,
	roomID domain.CollectionRoomID,
	slotIndex int,
	catalogModelID string,
) (domain.CollectionItem, error) {
	if !IsPlausibleUUID(string(roomID)) {
		return domain.CollectionItem{}, domain.ErrCollectionRoomNotFound
	}
	const query = `
		INSERT INTO collection_items (collection_room_id, slot_index, catalog_model_id)
		VALUES ($1, $2, $3)
		RETURNING id, slot_index, catalog_model_id, created_at, updated_at
	`
	var item domain.CollectionItem
	var id string
	err := r.db(ctx).QueryRow(ctx, query, string(roomID), slotIndex, catalogModelID).
		Scan(&id, &item.SlotIndex, &item.CatalogModelID, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.ConstraintName {
			case "collection_items_model_fk":
				return domain.CollectionItem{}, domain.ErrModelNotAvailable
			case "collection_items_unique_slot":
				return domain.CollectionItem{}, domain.ErrItemSlotTaken
			}
		}
		return domain.CollectionItem{}, fmt.Errorf("postgres_collection_room_repository: insert item: %w", err)
	}
	item.ID = domain.CollectionItemID(id)
	return item, nil
}

func (r *PostgresCollectionRoomRepository) MoveItemToSlot(
	ctx context.Context,
	roomID domain.CollectionRoomID,
	itemID domain.CollectionItemID,
	slotIndex int,
) error {
	if !IsPlausibleUUID(string(roomID)) || !IsPlausibleUUID(string(itemID)) {
		return domain.ErrItemNotInRoom
	}
	const query = `
		UPDATE collection_items
		   SET slot_index = $3, updated_at = now()
		 WHERE collection_room_id = $1 AND id = $2
	`
	tag, err := r.db(ctx).Exec(ctx, query, string(roomID), string(itemID), slotIndex)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "collection_items_unique_slot" {
			return domain.ErrItemSlotTaken
		}
		return fmt.Errorf("postgres_collection_room_repository: move item: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrItemNotInRoom
	}
	return nil
}

func (r *PostgresCollectionRoomRepository) SwapItemSlots(
	ctx context.Context,
	roomID domain.CollectionRoomID,
	first domain.CollectionItemID,
	second domain.CollectionItemID,
) error {
	if !IsPlausibleUUID(string(roomID)) ||
		!IsPlausibleUUID(string(first)) || !IsPlausibleUUID(string(second)) {
		return domain.ErrItemNotInRoom
	}
	if first == second {
		return domain.ErrItemNotInRoom
	}
	const query = `
		UPDATE collection_items AS target
		   SET slot_index = other.slot_index, updated_at = now()
		  FROM collection_items AS other
		 WHERE target.collection_room_id = $1
		   AND other.collection_room_id = $1
		   AND ((target.id = $2 AND other.id = $3) OR (target.id = $3 AND other.id = $2))
	`
	tag, err := r.db(ctx).Exec(ctx, query, string(roomID), string(first), string(second))
	if err != nil {
		return fmt.Errorf("postgres_collection_room_repository: swap item slots: %w", err)
	}
	if tag.RowsAffected() != 2 {
		return domain.ErrItemNotInRoom
	}
	return nil
}

func (r *PostgresCollectionRoomRepository) Delete(ctx context.Context, id domain.CollectionRoomID) error {
	if !IsPlausibleUUID(string(id)) {
		return domain.ErrCollectionRoomNotFound
	}
	tag, err := r.db(ctx).Exec(ctx, `DELETE FROM collection_rooms WHERE id = $1`, string(id))
	if err != nil {
		return fmt.Errorf("postgres_collection_room_repository: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrCollectionRoomNotFound
	}
	return nil
}

func (r *PostgresCollectionRoomRepository) loadItems(ctx context.Context, room *domain.CollectionRoom) error {
	const query = `
		SELECT id, slot_index, catalog_model_id, created_at, updated_at
		FROM collection_items
		WHERE collection_room_id = $1
		ORDER BY slot_index
	`
	rows, err := r.db(ctx).Query(ctx, query, string(room.ID))
	if err != nil {
		return fmt.Errorf("postgres_collection_room_repository: load items: %w", err)
	}
	defer rows.Close()

	room.Items = []domain.CollectionItem{}
	for rows.Next() {
		var item domain.CollectionItem
		var id string
		if err := rows.Scan(&id, &item.SlotIndex, &item.CatalogModelID, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return fmt.Errorf("postgres_collection_room_repository: scan item: %w", err)
		}
		item.ID = domain.CollectionItemID(id)
		room.Items = append(room.Items, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("postgres_collection_room_repository: item rows: %w", err)
	}
	return nil
}

func (r *PostgresCollectionRoomRepository) loadItemsForRooms(ctx context.Context, rooms []domain.CollectionRoom) error {
	if len(rooms) == 0 {
		return nil
	}
	ids := make([]string, 0, len(rooms))
	byID := make(map[string]*domain.CollectionRoom, len(rooms))
	for index := range rooms {
		id := string(rooms[index].ID)
		ids = append(ids, id)
		rooms[index].Items = []domain.CollectionItem{}
		byID[id] = &rooms[index]
	}

	const query = `
		SELECT collection_room_id, id, slot_index, catalog_model_id, created_at, updated_at
		FROM collection_items
		WHERE collection_room_id = ANY($1)
		ORDER BY collection_room_id, slot_index
	`
	rows, err := r.db(ctx).Query(ctx, query, ids)
	if err != nil {
		return fmt.Errorf("postgres_collection_room_repository: load items for rooms: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var roomID, itemID string
		var item domain.CollectionItem
		if err := rows.Scan(&roomID, &itemID, &item.SlotIndex, &item.CatalogModelID,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return fmt.Errorf("postgres_collection_room_repository: scan item: %w", err)
		}
		item.ID = domain.CollectionItemID(itemID)
		if room := byID[roomID]; room != nil {
			room.Items = append(room.Items, item)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("postgres_collection_room_repository: item rows: %w", err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCollectionRoom(row rowScanner) (domain.CollectionRoom, error) {
	var room domain.CollectionRoom
	var id string
	var tier int
	if err := row.Scan(
		&id, &room.AccountID, &room.Name,
		&room.CategoryID, &room.DesignID,
		&tier, &room.MusicTrackID, &room.CreatedAt, &room.UpdatedAt,
	); err != nil {
		return domain.CollectionRoom{}, err
	}
	room.ID = domain.CollectionRoomID(id)
	room.CurrentTier = domain.Tier(tier)
	return room, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
