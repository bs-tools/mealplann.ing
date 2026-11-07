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

	"github.com/bs-tools/foodplann.ing/internal/foods"
	"github.com/bs-tools/foodplann.ing/internal/pantry/internal/gen"
	"github.com/bs-tools/foodplann.ing/internal/store"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrUnknownFood is returned when a lot's food isn't one of its
// household's.
var ErrUnknownFood = errors.New("the food isn't one of the household's foods")

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
	// FoodName, StoreSection, GramsPerPiece and OpenedShelfLifeDays are
	// read from the food, and ignored when writing.
	FoodName            string
	StoreSection        foods.StoreSection
	GramsPerPiece       float64
	OpenedShelfLifeDays int32
	CreateTime          time.Time
	UpdateTime          time.Time
}

// UseBy returns the date to use the lot by: the earlier of its expiry date
// and the end of its opened shelf life, or zero if neither is known.
func (it *Item) UseBy() time.Time {
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

// UpdateItem replaces a household's lot's grams, dates and notes. It
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
				created, err = gen.New(db).CreateCookPantryUse(ctx, &gen.CreateCookPantryUseParams{CookID: cookID, HouseholdID: householdID})
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
			record := &gen.CreateCookPantryDeductionsParams{CookID: cookID, HouseholdID: householdID}
			for _, d := range deductions {
				if left := d.Item.Grams - d.Grams; left > emptyGrams {
					set.Ids = append(set.Ids, d.Item.ID)
					set.Grams = append(set.Grams, left)
				} else {
					emptied.Ids = append(emptied.Ids, d.Item.ID)
				}
				record.PantryItemIds = append(record.PantryItemIds, d.Item.ID)
				record.FoodIds = append(record.FoodIds, d.Item.FoodID)
				record.Grams = append(record.Grams, d.Grams)
				record.ExpiryDates = append(record.ExpiryDates, store.DateToPG(d.Item.ExpiryDate))
				record.OpenedDates = append(record.OpenedDates, store.DateToPG(d.Item.OpenedDate))
				record.Notes = append(record.Notes, d.Item.Notes)
			}
			if len(set.Ids) > 0 {
				_ = q.SetPantryItemGrams(ctx, set)
			}
			if len(emptied.Ids) > 0 {
				_ = q.DeletePantryItems(ctx, emptied)
			}
			_ = q.CreateCookPantryDeductions(ctx, record)
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
		return nil, fmt.Errorf("failed to take a cook's ingredients out of the pantry: %w", err)
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
	var restored []Use
	for _, row := range rows {
		if n := len(restored); n > 0 && restored[n-1].FoodID == row.FoodID {
			restored[n-1].Grams += row.Grams
			continue
		}
		restored = append(restored, Use{FoodID: row.FoodID, Grams: row.Grams})
	}
	return restored, nil
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
	if row.OpenedShelfLifeDays != nil {
		it.OpenedShelfLifeDays = *row.OpenedShelfLifeDays
	}
	return it
}
