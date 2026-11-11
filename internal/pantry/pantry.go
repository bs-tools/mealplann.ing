// Package pantry stores the households' pantries: the lots of foods they
// have in stock, in grams, with when they expire. It also takes the
// ingredients of cooks marked done out of stock, and puts them back when
// they're un-marked.
package pantry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/foods"
	"github.com/bs-tools/mealplann.ing/internal/pantry/internal/gen"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrUnknownFood is returned when a lot's food isn't one of its
// household's.
var ErrUnknownFood = errors.New("the food isn't one of the household's foods")

// Storage is where a lot is kept. The zero value is unspecified.
type Storage string

// The storages, as stored.
const (
	StorageUnspecified Storage = ""
	StoragePantry      Storage = "pantry"
	StorageFridge      Storage = "fridge"
	StorageFreezer     Storage = "freezer"
)

// Item is a lot of a food in stock.
type Item struct {
	ID          pgtype.UUID
	HouseholdID pgtype.UUID
	FoodID      pgtype.UUID
	// Grams is the amount in stock, more than 0.
	Grams float64
	// ExpiryDate is when it expires unopened, and OpenedDate when it was
	// opened, at midnight UTC, zero if unknown or unopened.
	ExpiryDate time.Time
	OpenedDate time.Time
	Notes      string
	// Storage is where it's kept, and Location where in the home.
	Storage  Storage
	Location string
	// FrozenDate is when it was frozen, zero unless it's in the freezer.
	FrozenDate time.Time
	// Opened is whether it's been opened, on OpenedDate if that's known.
	Opened bool
	// Estimated is whether Grams is a guess.
	Estimated bool
	// Label is its brand or label.
	Label string
	// FoodName, StoreSection, GramsPerPiece, PackageGrams,
	// OpenedShelfLifeDays and FreezerShelfLifeDays are read from the food,
	// and ignored when writing.
	FoodName             string
	StoreSection         foods.StoreSection
	GramsPerPiece        float64
	PackageGrams         float64
	OpenedShelfLifeDays  int32
	FreezerShelfLifeDays int32
	CreateTime           time.Time
	UpdateTime           time.Time
}

// UseBy returns the date to use the lot by. In the freezer, it's the day
// it was frozen plus the food's freezer shelf life, if both are known, as
// freezing stops the clock its pack's expiry date counts on. Otherwise
// it's the earlier of its expiry date and the end of its opened shelf
// life, or zero if neither is known.
func (it *Item) UseBy() time.Time {
	if it.Storage == StorageFreezer && !it.FrozenDate.IsZero() && it.FreezerShelfLifeDays > 0 {
		return it.FrozenDate.AddDate(0, 0, int(it.FreezerShelfLifeDays))
	}
	useBy := it.ExpiryDate
	if !it.OpenedDate.IsZero() && it.OpenedShelfLifeDays > 0 {
		opened := it.OpenedDate.AddDate(0, 0, int(it.OpenedShelfLifeDays))
		if useBy.IsZero() || opened.Before(useBy) {
			useBy = opened
		}
	}
	return useBy
}

// Use is an amount of a food a cook takes out of the pantry.
type Use struct {
	FoodID pgtype.UUID
	Grams  float64
}

// Deduction is an amount taken out of a lot.
type Deduction struct {
	Item  *Item
	Grams float64
}

// Repository stores pantries.
type Repository struct {
	log *slog.Logger
	db  *store.DB
}

// NewRepository returns a repository of the pantries in the database.
func NewRepository(log *slog.Logger, db *store.DB) *Repository {
	return &Repository{log: log, db: db}
}

// ListItems lists a household's lots, ordered by food name and expiry
// date.
func (r *Repository) ListItems(ctx context.Context, householdID pgtype.UUID) ([]*Item, error) {
	rows, err := gen.New(r.db).ListPantryItems(ctx, &gen.ListPantryItemsParams{HouseholdID: householdID})
	if err != nil {
		return nil, fmt.Errorf("failed to list pantry: %w", err)
	}
	items := make([]*Item, 0, len(rows))
	for _, row := range rows {
		items = append(items, itemFromDB(row))
	}
	return items, nil
}

// GetItem gets a household's lot. It returns store.ErrNotFound if the
// household has no such lot.
func (r *Repository) GetItem(ctx context.Context, householdID, id pgtype.UUID) (*Item, error) {
	rows, err := gen.New(r.db).ListPantryItems(ctx, &gen.ListPantryItemsParams{HouseholdID: householdID, Ids: []pgtype.UUID{id}})
	if err != nil {
		return nil, fmt.Errorf("failed to get pantry item: %w", err)
	}
	if len(rows) == 0 {
		return nil, store.ErrNotFound
	}
	return itemFromDB(rows[0]), nil
}

// CreateItems creates lots in a household, and returns them in their
// order. It returns ErrUnknownFood if a food isn't the household's.
func (r *Repository) CreateItems(ctx context.Context, householdID pgtype.UUID, items []*Item) ([]*Item, error) {
	if len(items) == 0 {
		return nil, nil
	}
	p := &gen.CreatePantryItemsParams{HouseholdID: householdID}
	for _, it := range items {
		p.Ids = append(p.Ids, store.NewUUID())
		p.FoodIds = append(p.FoodIds, it.FoodID)
		p.Grams = append(p.Grams, it.Grams)
		p.ExpiryDates = append(p.ExpiryDates, store.DateToPG(it.ExpiryDate))
		p.OpenedDates = append(p.OpenedDates, store.DateToPG(it.OpenedDate))
		p.Notes = append(p.Notes, it.Notes)
		p.Storages = append(p.Storages, string(it.Storage))
		p.Locations = append(p.Locations, it.Location)
		p.FrozenDates = append(p.FrozenDates, store.DateToPG(it.FrozenDate))
		p.Opened = append(p.Opened, it.Opened || !it.OpenedDate.IsZero())
		p.Estimated = append(p.Estimated, it.Estimated)
		p.Labels = append(p.Labels, it.Label)
	}
	var rows []*gen.ListPantryItemsRow
	var b store.Batch
	_, _ = gen.New(&b).CreatePantryItems(ctx, p)
	b.Read(func(db store.Querier) error {
		var err error
		rows, err = gen.New(db).ListPantryItems(ctx, &gen.ListPantryItemsParams{HouseholdID: householdID, Ids: p.Ids})
		return err
	})
	if err := b.Send(ctx, r.db); err != nil {
		return nil, writeError("add to pantry", err)
	}
	byID := map[pgtype.UUID]*Item{}
	for _, row := range rows {
		byID[row.PantryItem.ID] = itemFromDB(row)
	}
	created := make([]*Item, 0, len(rows))
	for _, id := range p.Ids {
		if it, ok := byID[id]; ok {
			created = append(created, it)
		}
	}
	return created, nil
}

// UpdateItem replaces a household's lot's grams, dates, notes and details. It
// returns store.ErrNotFound if the household has no such lot.
func (r *Repository) UpdateItem(ctx context.Context, it *Item) (*Item, error) {
	var updated int64
	var rows []*gen.ListPantryItemsRow
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		updated, err = gen.New(db).UpdatePantryItem(ctx, &gen.UpdatePantryItemParams{
			ID:          it.ID,
			HouseholdID: it.HouseholdID,
			Grams:       it.Grams,
			ExpiryDate:  store.DateToPG(it.ExpiryDate),
			OpenedDate:  store.DateToPG(it.OpenedDate),
			Notes:       it.Notes,
			Storage:     storageToDB(it.Storage),
			Location:    it.Location,
			FrozenDate:  store.DateToPG(it.FrozenDate),
			Opened:      it.Opened || !it.OpenedDate.IsZero(),
			Estimated:   it.Estimated,
			Label:       it.Label,
		})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		rows, err = gen.New(db).ListPantryItems(ctx, &gen.ListPantryItemsParams{HouseholdID: it.HouseholdID, Ids: []pgtype.UUID{it.ID}})
		return err
	})
	if err := b.Send(ctx, r.db); err != nil {
		return nil, fmt.Errorf("failed to update pantry item: %w", err)
	}
	if updated == 0 || len(rows) == 0 {
		return nil, store.ErrNotFound
	}
	return itemFromDB(rows[0]), nil
}

// DeleteItem deletes a household's lot. It returns store.ErrNotFound if
// the household has no such lot.
func (r *Repository) DeleteItem(ctx context.Context, householdID, id pgtype.UUID) error {
	n, err := gen.New(r.db).DeletePantryItem(ctx, &gen.DeletePantryItemParams{ID: id, HouseholdID: householdID})
	if err != nil {
		return fmt.Errorf("failed to delete pantry item: %w", err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// errNoChange stops a transaction that has nothing to change.
var errNoChange = errors.New("nothing to change")

// UseForCook takes what a cook uses out of a household's pantry, from the
// lots that expire first, as far as they're in stock, and records what it
// took out of each lot, so RestoreForCook can put it back. It returns what
// it took out, and nothing if the cook already took its ingredients out,
// so a cook marked done twice takes them out once. It returns
// store.ErrNotFound if the household has no such cook.
func (r *Repository) UseForCook(ctx context.Context, householdID, cookID pgtype.UUID, uses []Use) ([]Deduction, error) {
	deductions, err := r.use(ctx, householdID, uses,
		func(q *gen.Queries) (int64, error) {
			return q.CreateCookPantryUse(ctx, &gen.CreateCookPantryUseParams{CookID: cookID, HouseholdID: householdID})
		},
		func(q *gen.Queries, l *lotDetails) {
			_ = q.CreateCookPantryDeductions(ctx, &gen.CreateCookPantryDeductionsParams{
				CookID: cookID, HouseholdID: householdID,
				PantryItemIds: l.ids, FoodIds: l.foodIDs, Grams: l.grams, ExpiryDates: l.expiryDates,
				OpenedDates: l.openedDates, Notes: l.notes, Storages: l.storages, Locations: l.locations,
				FrozenDates: l.frozenDates, Opened: l.opened, Estimated: l.estimated, Labels: l.labels,
			})
		})
	if err != nil {
		return nil, fmt.Errorf("failed to take a cook's ingredients out of the pantry: %w", err)
	}
	return deductions, nil
}

// UseForStaple takes what a staple eaten on a date uses out of a
// household's pantry, like UseForCook, so RestoreForStaple can put it back.
// It returns nothing if the staple was already marked eaten on the date. It
// returns store.ErrNotFound if the household has no such staple.
func (r *Repository) UseForStaple(ctx context.Context, householdID, stapleID pgtype.UUID, date time.Time, uses []Use) ([]Deduction, error) {
	pgDate := store.DateToPG(date)
	deductions, err := r.use(ctx, householdID, uses,
		func(q *gen.Queries) (int64, error) {
			return q.CreateStaplePantryUse(ctx, &gen.CreateStaplePantryUseParams{StapleID: stapleID, Date: pgDate, HouseholdID: householdID})
		},
		func(q *gen.Queries, l *lotDetails) {
			_ = q.CreateStaplePantryDeductions(ctx, &gen.CreateStaplePantryDeductionsParams{
				StapleID: stapleID, Date: pgDate, HouseholdID: householdID,
				PantryItemIds: l.ids, FoodIds: l.foodIDs, Grams: l.grams, ExpiryDates: l.expiryDates,
				OpenedDates: l.openedDates, Notes: l.notes, Storages: l.storages, Locations: l.locations,
				FrozenDates: l.frozenDates, Opened: l.opened, Estimated: l.estimated, Labels: l.labels,
			})
		})
	if err != nil {
		return nil, fmt.Errorf("failed to take a staple's food out of the pantry: %w", err)
	}
	return deductions, nil
}

// UseForPortion takes a portion of a food marked eaten out of a
// household's pantry, like UseForStaple, so RestoreForPortion can put it
// back. It returns nothing if the portion was already marked eaten.
func (r *Repository) UseForPortion(ctx context.Context, householdID, portionID pgtype.UUID, uses []Use) ([]Deduction, error) {
	deductions, err := r.use(ctx, householdID, uses,
		func(q *gen.Queries) (int64, error) {
			return q.CreatePortionPantryUse(ctx, &gen.CreatePortionPantryUseParams{PortionID: portionID, HouseholdID: householdID})
		},
		func(q *gen.Queries, l *lotDetails) {
			_ = q.CreatePortionPantryDeductions(ctx, &gen.CreatePortionPantryDeductionsParams{
				PortionID: portionID, HouseholdID: householdID,
				PantryItemIds: l.ids, FoodIds: l.foodIDs, Grams: l.grams, ExpiryDates: l.expiryDates,
				OpenedDates: l.openedDates, Notes: l.notes, Storages: l.storages, Locations: l.locations,
				FrozenDates: l.frozenDates, Opened: l.opened, Estimated: l.estimated, Labels: l.labels,
			})
		})
	if err != nil {
		return nil, fmt.Errorf("failed to take a portion's food out of the pantry: %w", err)
	}
	return deductions, nil
}

// lotDetails are what was taken out of lots, with the lots' details to
// recreate them, as the arrays the queries recording it take.
type lotDetails struct {
	ids         []pgtype.UUID
	foodIDs     []pgtype.UUID
	grams       []float64
	expiryDates []pgtype.Date
	openedDates []pgtype.Date
	notes       []string
	storages    []string
	locations   []string
	frozenDates []pgtype.Date
	opened      []bool
	estimated   []bool
	labels      []string
}

// use takes uses out of a household's pantry, from the lots that expire
// first, once: create records the use, returning 0 rows if it was already
// recorded, which takes nothing out, and record records what was taken
// out of each lot. It returns what it took out.
func (r *Repository) use(ctx context.Context, householdID pgtype.UUID, uses []Use,
	create func(*gen.Queries) (int64, error), record func(*gen.Queries, *lotDetails),
) ([]Deduction, error) {
	var foodIDs []pgtype.UUID
	for _, u := range uses {
		foodIDs = append(foodIDs, u.FoodID)
	}
	var created int64
	var lots []*gen.LockPantryItemsRow
	var deductions []Deduction
	// The first batch records the use, which waits for another request
	// recording it at the same time and then records nothing, and locks the
	// lots; the second updates them from what the first read.
	err := r.db.Transact(ctx,
		func(b *store.Batch) error {
			b.Read(func(db store.Querier) error {
				var err error
				created, err = create(gen.New(db))
				return err
			})
			b.Read(func(db store.Querier) error {
				var err error
				lots, err = gen.New(db).LockPantryItems(ctx, &gen.LockPantryItemsParams{HouseholdID: householdID, FoodIds: foodIDs})
				return err
			})
			return nil
		},
		func(b *store.Batch) error {
			if created == 0 {
				return errNoChange
			}
			items := make([]*Item, 0, len(lots))
			for _, row := range lots {
				items = append(items, itemFromDB((*gen.ListPantryItemsRow)(row)))
			}
			deductions = Deduct(items, uses)
			if len(deductions) == 0 {
				return nil
			}
			q := gen.New(b)
			set := &gen.SetPantryItemGramsParams{HouseholdID: householdID}
			emptied := &gen.DeletePantryItemsParams{HouseholdID: householdID}
			var l lotDetails
			for _, d := range deductions {
				if left := d.Item.Grams - d.Grams; left > emptyGrams {
					set.Ids = append(set.Ids, d.Item.ID)
					set.Grams = append(set.Grams, left)
				} else {
					emptied.Ids = append(emptied.Ids, d.Item.ID)
				}
				l.ids = append(l.ids, d.Item.ID)
				l.foodIDs = append(l.foodIDs, d.Item.FoodID)
				l.grams = append(l.grams, d.Grams)
				l.expiryDates = append(l.expiryDates, store.DateToPG(d.Item.ExpiryDate))
				l.openedDates = append(l.openedDates, store.DateToPG(d.Item.OpenedDate))
				l.notes = append(l.notes, d.Item.Notes)
				l.storages = append(l.storages, string(d.Item.Storage))
				l.locations = append(l.locations, d.Item.Location)
				l.frozenDates = append(l.frozenDates, store.DateToPG(d.Item.FrozenDate))
				l.opened = append(l.opened, d.Item.Opened)
				l.estimated = append(l.estimated, d.Item.Estimated)
				l.labels = append(l.labels, d.Item.Label)
			}
			if len(set.Ids) > 0 {
				_ = q.SetPantryItemGrams(ctx, set)
			}
			if len(emptied.Ids) > 0 {
				_ = q.DeletePantryItems(ctx, emptied)
			}
			record(q, &l)
			return nil
		},
	)
	switch {
	case errors.Is(err, errNoChange):
		return nil, nil
	case err != nil:
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == pgerrcode.ForeignKeyViolation {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return deductions, nil
}

// RestoreForCook puts back what a cook took out of a household's pantry,
// in the lots it was taken from, and returns it by food, nothing if the
// cook took nothing out.
func (r *Repository) RestoreForCook(ctx context.Context, householdID, cookID pgtype.UUID) ([]Use, error) {
	rows, err := gen.New(r.db).RestoreCookPantryUse(ctx, &gen.RestoreCookPantryUseParams{CookID: cookID, HouseholdID: householdID})
	if err != nil {
		return nil, fmt.Errorf("failed to put a cook's ingredients back into the pantry: %w", err)
	}
	deducted := make([]Use, 0, len(rows))
	for _, row := range rows {
		deducted = append(deducted, Use(*row))
	}
	return byFood(deducted), nil
}

// RestoreForStaple puts back what a staple eaten on a date took out of a
// household's pantry, like RestoreForCook, and un-marks it eaten.
func (r *Repository) RestoreForStaple(ctx context.Context, householdID, stapleID pgtype.UUID, date time.Time) ([]Use, error) {
	rows, err := gen.New(r.db).RestoreStaplePantryUse(ctx, &gen.RestoreStaplePantryUseParams{
		StapleID: stapleID, Date: store.DateToPG(date), HouseholdID: householdID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to put a staple's food back into the pantry: %w", err)
	}
	deducted := make([]Use, 0, len(rows))
	for _, row := range rows {
		deducted = append(deducted, Use(*row))
	}
	return byFood(deducted), nil
}

// RestoreForPortion puts back what a portion marked eaten took out of a
// household's pantry, like RestoreForStaple.
func (r *Repository) RestoreForPortion(ctx context.Context, householdID, portionID pgtype.UUID) ([]Use, error) {
	rows, err := gen.New(r.db).RestorePortionPantryUse(ctx, &gen.RestorePortionPantryUseParams{
		PortionID: portionID, HouseholdID: householdID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to put a portion's food back into the pantry: %w", err)
	}
	deducted := make([]Use, 0, len(rows))
	for _, row := range rows {
		deducted = append(deducted, Use(*row))
	}
	return byFood(deducted), nil
}

// byFood sums deductions put back, in order of their foods, by food.
func byFood(deducted []Use) []Use {
	var restored []Use
	for _, u := range deducted {
		if n := len(restored); n > 0 && restored[n-1].FoodID == u.FoodID {
			restored[n-1].Grams += u.Grams
			continue
		}
		restored = append(restored, u)
	}
	return restored
}

// writeError translates the errors of writing lots.
func writeError(what string, err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == pgerrcode.ForeignKeyViolation {
		return ErrUnknownFood
	}
	return fmt.Errorf("failed to %s: %w", what, err)
}

func itemFromDB(row *gen.ListPantryItemsRow) *Item {
	p := &row.PantryItem
	it := &Item{
		ID:          p.ID,
		HouseholdID: p.HouseholdID,
		FoodID:      p.FoodID,
		Grams:       p.Grams,
		ExpiryDate:  store.DateFromPG(p.ExpiryDate),
		OpenedDate:  store.DateFromPG(p.OpenedDate),
		Notes:       p.Notes,
		Location:    p.Location,
		FrozenDate:  store.DateFromPG(p.FrozenDate),
		Opened:      p.Opened,
		Estimated:   p.Estimated,
		Label:       p.Label,
		FoodName:    row.FoodName,
		CreateTime:  p.CreateTime.Time,
		UpdateTime:  p.UpdateTime.Time,
	}
	if row.StoreSection != nil {
		it.StoreSection = foods.StoreSection(*row.StoreSection)
	}
	if row.GramsPerPiece != nil {
		it.GramsPerPiece = *row.GramsPerPiece
	}
	if row.PackageGrams != nil {
		it.PackageGrams = *row.PackageGrams
	}
	if row.OpenedShelfLifeDays != nil {
		it.OpenedShelfLifeDays = *row.OpenedShelfLifeDays
	}
	if row.FreezerShelfLifeDays != nil {
		it.FreezerShelfLifeDays = *row.FreezerShelfLifeDays
	}
	if p.Storage != nil {
		it.Storage = Storage(*p.Storage)
	}
	return it
}

// storageToDB returns a storage as stored, nil for unspecified.
func storageToDB(s Storage) *gen.PantryStorage {
	if s == StorageUnspecified {
		return nil
	}
	v := gen.PantryStorage(s)
	return &v
}
