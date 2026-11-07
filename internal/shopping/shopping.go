// Package shopping stores the households' stores and shopping lists, and
// generates the lists from the plan, less what's in the pantry.
package shopping

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/bs-tools/foodplann.ing/internal/foods"
	"github.com/bs-tools/foodplann.ing/internal/shopping/internal/gen"
	"github.com/bs-tools/foodplann.ing/internal/store"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	// ErrStoreNameTaken is returned when a household already has a store
	// with the name, ignoring case.
	ErrStoreNameTaken = errors.New("the household already has a store with this name")
	// ErrListComplete is returned when changing the items of a list whose
	// shopping is complete.
	ErrListComplete = errors.New("the shopping is complete")
	// ErrUnknownFood is returned when an item's food isn't one of the
	// household's.
	ErrUnknownFood = errors.New("the food isn't one of the household's foods")
	// ErrUnknownStore is returned when an item's store isn't one of the
	// household's.
	ErrUnknownStore = errors.New("the store isn't one of the household's stores")
)

// Store is a store a household shops at.
type Store struct {
	ID          pgtype.UUID
	HouseholdID pgtype.UUID
	Name        string
	// Position is the store's place in the order of stores, from 0.
	Position   int32
	CreateTime time.Time
	UpdateTime time.Time
}

// List is a shopping list for the meals of a range of dates.
type List struct {
	ID          pgtype.UUID
	HouseholdID pgtype.UUID
	// StartDate, the date of the shop, and EndDate are at midnight UTC.
	StartDate time.Time
	EndDate   time.Time
	// CompleteTime is when the shopping was completed, zero if it isn't.
	CompleteTime time.Time
	// Items are the list's items, in the order of the shop (see SortItems),
	// nil when lists are listed.
	Items []*Item
	// ItemCount, CheckedCount and TopUpCount count its items.
	ItemCount    int32
	CheckedCount int32
	TopUpCount   int32
	CreateTime   time.Time
	UpdateTime   time.Time
}

// count sets the list's counts from its items.
func (l *List) count() {
	l.ItemCount, l.CheckedCount, l.TopUpCount = 0, 0, 0
	for _, it := range l.Items {
		l.ItemCount++
		if it.Checked {
			l.CheckedCount++
		}
		if it.TopUp {
			l.TopUpCount++
		}
	}
}

// Item is an item of a shopping list.
type Item struct {
	ID          pgtype.UUID
	HouseholdID pgtype.UUID
	ListID      pgtype.UUID
	// FoodID is the food, invalid for free text.
	FoodID pgtype.UUID
	// Name is the food's name, or the free text.
	Name string
	// Grams is the amount to buy, rounded up to whole packs or pieces, 0 if
	// unknown.
	Grams float64
	// NeededGrams is what the plan needs beyond the pantry, before
	// rounding, and PantryGrams what the plan uses of the pantry.
	NeededGrams float64
	PantryGrams float64
	// Packages is the number of packs to buy, 0 if not sold in packs.
	Packages int32
	// AmountText is the amount for people, e.g. "2 × 400 g".
	AmountText string
	// StoreID is the store to buy it at, invalid for the main store. When
	// read, it's the store it's bought at: the main store if it has none, or
	// invalid if the household has no stores. StoreName, its name, is read,
	// and ignored when writing.
	StoreID      pgtype.UUID
	StoreName    string
	StoreSection foods.StoreSection
	// NeededBy is the first date it's needed, zero for items added by hand.
	NeededBy time.Time
	// TopUp is whether it won't keep from the shop until it's needed.
	TopUp bool
	// Warning is why it should be bought later, or what's missing to weigh
	// it.
	Warning string
	Checked bool
	// CheckedBy is the user who checked it, and CheckTime when, invalid and
	// zero if it isn't checked. CheckedByName, their name, is read, and
	// ignored when writing.
	CheckedBy     pgtype.UUID
	CheckedByName string
	CheckTime     time.Time
	Note          string
	// Manual is whether it was added by hand.
	Manual     bool
	CreateTime time.Time
	UpdateTime time.Time
}

// sectionOrder is the order of store sections in a typical shop, as the
// web app shows them: fresh produce first, household goods last, and
// unspecified sections after everything.
var sectionOrder = []foods.StoreSection{
	foods.StoreSectionProduce,
	foods.StoreSectionBakery,
	foods.StoreSectionChilled,
	foods.StoreSectionFrozen,
	foods.StoreSectionDryGoods,
	foods.StoreSectionCanned,
	foods.StoreSectionSpices,
	foods.StoreSectionDrinks,
	foods.StoreSectionHousehold,
	foods.StoreSectionOther,
	foods.StoreSectionUnspecified,
}

// StoreOf returns the store an item is bought at: its own if it's one of
// the stores, or the main store, the first. It returns nil if there are no
// stores.
func StoreOf(it *Item, stores []*Store) *Store {
	for _, s := range stores {
		if s.ID == it.StoreID {
			return s
		}
	}
	if len(stores) == 0 {
		return nil
	}
	return stores[0]
}

// SortItems sorts items in the order of the shop: by store, in the order of
// the stores, by store section, in the order of a typical shop, then by
// name, what keeps before what's topped up later.
func SortItems(items []*Item, stores []*Store) {
	storeIndex := func(it *Item) int {
		s := StoreOf(it, stores)
		if s == nil {
			return 0
		}
		return slices.Index(stores, s)
	}
	slices.SortStableFunc(items, func(a, b *Item) int {
		if c := cmp.Compare(storeIndex(a), storeIndex(b)); c != 0 {
			return c
		}
		if c := cmp.Compare(slices.Index(sectionOrder, a.StoreSection), slices.Index(sectionOrder, b.StoreSection)); c != 0 {
			return c
		}
		if c := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		if a.TopUp != b.TopUp {
			if a.TopUp {
				return 1
			}
			return -1
		}
		return 0
	})
}

// KeepChecks carries the checks and notes of a list's old generated items
// over to the generated items replacing them, matching items by food, and
// by whether they're topped up, then by food alone. Matched items keep the
// old item's ID.
func KeepChecks(old, generated []*Item) {
	used := make([]bool, len(old))
	match := func(it *Item, sameTopUp bool) bool {
		for i, o := range old {
			if used[i] || o.Manual || !o.FoodID.Valid || o.FoodID != it.FoodID || (sameTopUp && o.TopUp != it.TopUp) {
				continue
			}
			used[i] = true
			it.ID = o.ID
			it.Checked = o.Checked
			it.CheckedBy = o.CheckedBy
			it.CheckTime = o.CheckTime
			it.Note = o.Note
			return true
		}
		return false
	}
	var unmatched []*Item
	for _, it := range generated {
		if !match(it, true) {
			unmatched = append(unmatched, it)
		}
	}
	for _, it := range unmatched {
		match(it, false)
	}
}

// Repository stores stores and shopping lists.
type Repository struct {
	log *slog.Logger
	db  *store.DB
}

// NewRepository returns a repository of the stores and shopping lists in
// the database.
func NewRepository(log *slog.Logger, db *store.DB) *Repository {
	return &Repository{log: log, db: db}
}

// CreateStore creates a store after its household's others, and returns
// it. It returns ErrStoreNameTaken if the household has a store with the
// same name.
func (r *Repository) CreateStore(ctx context.Context, s *Store) (*Store, error) {
	row, err := gen.New(r.db).CreateStore(ctx, &gen.CreateStoreParams{HouseholdID: s.HouseholdID, Name: s.Name})
	if err != nil {
		if isStoreNameTaken(err) {
			return nil, ErrStoreNameTaken
		}
		return nil, fmt.Errorf("failed to create store: %w", err)
	}
	return storeFromDB(row), nil
}

// GetStore gets a household's store. It returns store.ErrNotFound if the
// household has no such store.
func (r *Repository) GetStore(ctx context.Context, householdID, id pgtype.UUID) (*Store, error) {
	row, err := gen.New(r.db).GetStore(ctx, &gen.GetStoreParams{ID: id, HouseholdID: householdID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get store: %w", err)
	}
	return storeFromDB(row), nil
}

// ListStores lists a household's stores, in order.
func (r *Repository) ListStores(ctx context.Context, householdID pgtype.UUID) ([]*Store, error) {
	rows, err := gen.New(r.db).ListStores(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("failed to list stores: %w", err)
	}
	return storesFromDB(rows), nil
}

// UpdateStore replaces a household's store's name and position. It returns
// store.ErrNotFound if the household has no such store, and
// ErrStoreNameTaken if another of its stores has the name.
func (r *Repository) UpdateStore(ctx context.Context, s *Store) (*Store, error) {
	row, err := gen.New(r.db).UpdateStore(ctx, &gen.UpdateStoreParams{
		ID:          s.ID,
		HouseholdID: s.HouseholdID,
		Name:        s.Name,
		Position:    s.Position,
	})
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, store.ErrNotFound
		case isStoreNameTaken(err):
			return nil, ErrStoreNameTaken
		}
		return nil, fmt.Errorf("failed to update store: %w", err)
	}
	return storeFromDB(row), nil
}

// DeleteStore deletes a household's store. Its foods and items move to the
// main store. It returns store.ErrNotFound if the household has no such
// store.
func (r *Repository) DeleteStore(ctx context.Context, householdID, id pgtype.UUID) error {
	n, err := gen.New(r.db).DeleteStore(ctx, &gen.DeleteStoreParams{ID: id, HouseholdID: householdID})
	if err != nil {
		return fmt.Errorf("failed to delete store: %w", err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// CreateList creates a shopping list with its items, and returns it with
// its items in order, and the household's stores.
func (r *Repository) CreateList(ctx context.Context, l *List, items []*Item) (*List, []*Store, error) {
	id := store.NewUUID()
	var b store.Batch
	_, _ = gen.New(&b).CreateShoppingList(ctx, &gen.CreateShoppingListParams{
		ID:          id,
		HouseholdID: l.HouseholdID,
		StartDate:   store.DateToPG(l.StartDate),
		EndDate:     store.DateToPG(l.EndDate),
	})
	if len(items) > 0 {
		_, _ = gen.New(&b).CreateShoppingItems(ctx, itemsToDB(l.HouseholdID, id, items))
	}
	var list *List
	var stores []*Store
	readList(ctx, &b, l.HouseholdID, id, &list, &stores)
	if err := b.Send(ctx, r.db); err != nil {
		return nil, nil, fmt.Errorf("failed to create shopping list: %w", err)
	}
	if list == nil {
		return nil, nil, errors.New("failed to read the created shopping list")
	}
	return list, stores, nil
}

// GetList gets a household's shopping list with its items in order, and
// the household's stores. It returns store.ErrNotFound if the household
// has no such list.
func (r *Repository) GetList(ctx context.Context, householdID, id pgtype.UUID) (*List, []*Store, error) {
	var list *List
	var stores []*Store
	var b store.Batch
	readList(ctx, &b, householdID, id, &list, &stores)
	if err := b.Send(ctx, r.db); err != nil {
		return nil, nil, fmt.Errorf("failed to get shopping list: %w", err)
	}
	if list == nil {
		return nil, nil, store.ErrNotFound
	}
	return list, stores, nil
}

// ListLists lists a household's shopping lists, latest start date first,
// without their items: limit of them, after skipping offset.
func (r *Repository) ListLists(ctx context.Context, householdID pgtype.UUID, limit, offset int32) ([]*List, error) {
	rows, err := gen.New(r.db).ListShoppingLists(ctx, &gen.ListShoppingListsParams{
		HouseholdID: householdID,
		RowLimit:    limit,
		RowOffset:   offset,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list shopping lists: %w", err)
	}
	lists := make([]*List, 0, len(rows))
	for _, row := range rows {
		l := listFromDB(&row.ShoppingList)
		l.ItemCount, l.CheckedCount, l.TopUpCount = row.ItemCount, row.CheckedCount, row.TopUpCount
		lists = append(lists, l)
	}
	return lists, nil
}

// ReplaceGeneratedItems replaces the generated items of a household's
// list, keeping those added by hand, and returns the list with its items,
// and the household's stores. It returns store.ErrNotFound if the
// household has no such list, and ErrListComplete if its shopping is
// complete.
func (r *Repository) ReplaceGeneratedItems(ctx context.Context, householdID, listID pgtype.UUID, items []*Item) (*List, []*Store, error) {
	var touched int64
	var list *List
	var stores []*Store
	// The first batch checks that the list can change, and locks it; the
	// second replaces the items.
	err := r.db.Transact(ctx,
		func(b *store.Batch) error {
			b.Read(func(db store.Querier) error {
				var err error
				touched, err = gen.New(db).TouchShoppingList(ctx, &gen.TouchShoppingListParams{ID: listID, HouseholdID: householdID})
				return err
			})
			return nil
		},
		func(b *store.Batch) error {
			if touched == 0 {
				return errNotTouched
			}
			q := gen.New(b)
			_ = q.DeleteGeneratedShoppingItems(ctx, &gen.DeleteGeneratedShoppingItemsParams{ListID: listID, HouseholdID: householdID})
			if len(items) > 0 {
				_, _ = q.CreateShoppingItems(ctx, itemsToDB(householdID, listID, items))
			}
			readList(ctx, b, householdID, listID, &list, &stores)
			return nil
		},
	)
	if errors.Is(err, errNotTouched) {
		if _, _, err := r.GetList(ctx, householdID, listID); err != nil {
			return nil, nil, err
		}
		return nil, nil, ErrListComplete
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to regenerate shopping list: %w", err)
	}
	if list == nil {
		return nil, nil, store.ErrNotFound
	}
	return list, stores, nil
}

// errNotTouched stops regenerating a list that's complete or missing.
var errNotTouched = errors.New("the list is complete or missing")

// AddItem adds an item to a household's list, and returns it. It returns
// store.ErrNotFound if the household has no such list, and ErrUnknownFood
// or ErrUnknownStore if its food or store isn't the household's.
func (r *Repository) AddItem(ctx context.Context, it *Item) (*Item, error) {
	it.ID = store.NewUUID()
	var rows []*gen.ListShoppingItemsRow
	var b store.Batch
	_, _ = gen.New(&b).CreateShoppingItems(ctx, itemsToDB(it.HouseholdID, it.ListID, []*Item{it}))
	b.Read(func(db store.Querier) error {
		var err error
		rows, err = gen.New(db).ListShoppingItems(ctx, &gen.ListShoppingItemsParams{HouseholdID: it.HouseholdID, ID: it.ID})
		return err
	})
	if err := b.Send(ctx, r.db); err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == pgerrcode.ForeignKeyViolation {
			switch pgErr.ConstraintName {
			case "shopping_items_household_id_food_id_fkey":
				return nil, ErrUnknownFood
			case "shopping_items_household_id_store_id_fkey":
				return nil, ErrUnknownStore
			default:
				return nil, store.ErrNotFound
			}
		}
		return nil, fmt.Errorf("failed to add shopping item: %w", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("failed to read the added shopping item")
	}
	return listedItem(rows[0]), nil
}

// SetItemChecked checks or unchecks a household's item, as checked by a
// user, and returns it. It returns store.ErrNotFound if the household has
// no such item.
func (r *Repository) SetItemChecked(ctx context.Context, householdID, id, userID pgtype.UUID, checked bool) (*Item, error) {
	row, err := gen.New(r.db).SetShoppingItemChecked(ctx, &gen.SetShoppingItemCheckedParams{
		ID:          id,
		HouseholdID: householdID,
		UserID:      userID,
		Checked:     checked,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("failed to check shopping item: %w", err)
	}
	it := itemFromDB(&gen.ShoppingItem{
		ID:           row.ID,
		HouseholdID:  row.HouseholdID,
		ListID:       row.ListID,
		FoodID:       row.FoodID,
		Name:         row.Name,
		Grams:        row.Grams,
		NeededGrams:  row.NeededGrams,
		PantryGrams:  row.PantryGrams,
		Packages:     row.Packages,
		AmountText:   row.AmountText,
		StoreID:      row.StoreID,
		StoreSection: row.StoreSection,
		NeededBy:     row.NeededBy,
		TopUp:        row.TopUp,
		Warning:      row.Warning,
		Checked:      row.Checked,
		CheckedBy:    row.CheckedBy,
		CheckTime:    row.CheckTime,
		Note:         row.Note,
		Manual:       row.Manual,
		CreateTime:   row.CreateTime,
		UpdateTime:   row.UpdateTime,
	}, row.CheckedByName)
	it.StoreID, it.StoreName = row.ResolvedStoreID, row.StoreName
	return it, nil
}

// GetItem gets a household's item. It returns store.ErrNotFound if the
// household has no such item.
func (r *Repository) GetItem(ctx context.Context, householdID, id pgtype.UUID) (*Item, error) {
	rows, err := gen.New(r.db).ListShoppingItems(ctx, &gen.ListShoppingItemsParams{HouseholdID: householdID, ID: id})
	if err != nil {
		return nil, fmt.Errorf("failed to get shopping item: %w", err)
	}
	if len(rows) == 0 {
		return nil, store.ErrNotFound
	}
	return listedItem(rows[0]), nil
}

// DeleteItem deletes a household's item. It returns store.ErrNotFound if
// the household has no such item.
func (r *Repository) DeleteItem(ctx context.Context, householdID, id pgtype.UUID) error {
	n, err := gen.New(r.db).DeleteShoppingItem(ctx, &gen.DeleteShoppingItemParams{ID: id, HouseholdID: householdID})
	if err != nil {
		return fmt.Errorf("failed to delete shopping item: %w", err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// CompleteList marks a household's list complete, and returns when. It
// returns store.ErrNotFound if the household has no such list, and
// ErrListComplete if it's already complete, so completing a list twice,
// even at the same time on two phones, completes it once.
func (r *Repository) CompleteList(ctx context.Context, householdID, id pgtype.UUID) (time.Time, error) {
	var completed int64
	var row *gen.ShoppingList
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		completed, err = gen.New(db).CompleteShoppingList(ctx, &gen.CompleteShoppingListParams{ID: id, HouseholdID: householdID})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		row, err = gen.New(db).GetShoppingList(ctx, &gen.GetShoppingListParams{ID: id, HouseholdID: householdID})
		if errors.Is(err, pgx.ErrNoRows) {
			// sqlc returns a zero row with the error.
			row = nil
			return nil
		}
		return err
	})
	if err := b.Send(ctx, r.db); err != nil {
		return time.Time{}, fmt.Errorf("failed to complete shopping list: %w", err)
	}
	switch {
	case row == nil:
		return time.Time{}, store.ErrNotFound
	case completed == 0:
		return time.Time{}, ErrListComplete
	}
	return row.CompleteTime.Time, nil
}

// ReopenList marks a household's list not complete, after failing to put
// its items into the pantry.
func (r *Repository) ReopenList(ctx context.Context, householdID, id pgtype.UUID) error {
	if err := gen.New(r.db).ReopenShoppingList(ctx, &gen.ReopenShoppingListParams{ID: id, HouseholdID: householdID}); err != nil {
		return fmt.Errorf("failed to reopen shopping list: %w", err)
	}
	return nil
}

// DeleteList deletes a household's list with its items. It returns
// store.ErrNotFound if the household has no such list.
func (r *Repository) DeleteList(ctx context.Context, householdID, id pgtype.UUID) error {
	n, err := gen.New(r.db).DeleteShoppingList(ctx, &gen.DeleteShoppingListParams{ID: id, HouseholdID: householdID})
	if err != nil {
		return fmt.Errorf("failed to delete shopping list: %w", err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// readList queues reading a household's list with its items in order, and
// its stores. list stays nil if the household has no such list.
func readList(ctx context.Context, b *store.Batch, householdID, id pgtype.UUID, list **List, stores *[]*Store) {
	var row *gen.ShoppingList
	var items []*gen.ListShoppingItemsRow
	b.Read(func(db store.Querier) error {
		var err error
		row, err = gen.New(db).GetShoppingList(ctx, &gen.GetShoppingListParams{ID: id, HouseholdID: householdID})
		if errors.Is(err, pgx.ErrNoRows) {
			// sqlc returns a zero row with the error.
			row = nil
			return nil
		}
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		items, err = gen.New(db).ListShoppingItems(ctx, &gen.ListShoppingItemsParams{HouseholdID: householdID, ListID: id})
		return err
	})
	b.Read(func(db store.Querier) error {
		rows, err := gen.New(db).ListStores(ctx, householdID)
		if err != nil {
			return err
		}
		*stores = storesFromDB(rows)
		if row == nil {
			*list = nil
			return nil
		}
		l := listFromDB(row)
		for _, it := range items {
			l.Items = append(l.Items, listedItem(it))
		}
		SortItems(l.Items, *stores)
		l.count()
		*list = l
		return nil
	})
}

// isStoreNameTaken reports whether err is the violation of the unique name
// of a household's stores.
func isStoreNameTaken(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "stores_household_id_lower_name"
}

func storeFromDB(row *gen.Store) *Store {
	return &Store{
		ID:          row.ID,
		HouseholdID: row.HouseholdID,
		Name:        row.Name,
		Position:    row.Position,
		CreateTime:  row.CreateTime.Time,
		UpdateTime:  row.UpdateTime.Time,
	}
}

func storesFromDB(rows []*gen.Store) []*Store {
	stores := make([]*Store, 0, len(rows))
	for _, row := range rows {
		stores = append(stores, storeFromDB(row))
	}
	return stores
}

func listFromDB(row *gen.ShoppingList) *List {
	return &List{
		ID:           row.ID,
		HouseholdID:  row.HouseholdID,
		StartDate:    store.DateFromPG(row.StartDate),
		EndDate:      store.DateFromPG(row.EndDate),
		CompleteTime: row.CompleteTime.Time,
		CreateTime:   row.CreateTime.Time,
		UpdateTime:   row.UpdateTime.Time,
	}
}

// listedItem converts an item read with who checked it and its store.
func listedItem(row *gen.ListShoppingItemsRow) *Item {
	it := itemFromDB(&row.ShoppingItem, row.CheckedByName)
	it.StoreID, it.StoreName = row.ResolvedStoreID, row.StoreName
	return it
}

func itemFromDB(row *gen.ShoppingItem, checkedByName string) *Item {
	it := &Item{
		ID:            row.ID,
		HouseholdID:   row.HouseholdID,
		ListID:        row.ListID,
		FoodID:        row.FoodID,
		Name:          row.Name,
		Grams:         row.Grams,
		NeededGrams:   row.NeededGrams,
		PantryGrams:   row.PantryGrams,
		Packages:      row.Packages,
		AmountText:    row.AmountText,
		StoreID:       row.StoreID,
		NeededBy:      store.DateFromPG(row.NeededBy),
		TopUp:         row.TopUp,
		Warning:       row.Warning,
		Checked:       row.Checked,
		CheckedBy:     row.CheckedBy,
		CheckedByName: checkedByName,
		CheckTime:     row.CheckTime.Time,
		Note:          row.Note,
		Manual:        row.Manual,
		CreateTime:    row.CreateTime.Time,
		UpdateTime:    row.UpdateTime.Time,
	}
	if row.StoreSection != nil {
		it.StoreSection = foods.StoreSection(*row.StoreSection)
	}
	return it
}

// itemsToDB converts items to the arrays that create them, giving new IDs
// to those without.
func itemsToDB(householdID, listID pgtype.UUID, items []*Item) *gen.CreateShoppingItemsParams {
	p := &gen.CreateShoppingItemsParams{HouseholdID: householdID, ListID: listID}
	for _, it := range items {
		if !it.ID.Valid {
			it.ID = store.NewUUID()
		}
		var checkTime pgtype.Timestamptz
		if !it.CheckTime.IsZero() {
			checkTime = pgtype.Timestamptz{Time: it.CheckTime, Valid: true}
		}
		p.Ids = append(p.Ids, it.ID)
		p.FoodIds = append(p.FoodIds, it.FoodID)
		p.Names = append(p.Names, it.Name)
		p.Grams = append(p.Grams, it.Grams)
		p.NeededGrams = append(p.NeededGrams, it.NeededGrams)
		p.PantryGrams = append(p.PantryGrams, it.PantryGrams)
		p.Packages = append(p.Packages, it.Packages)
		p.AmountTexts = append(p.AmountTexts, it.AmountText)
		p.StoreIds = append(p.StoreIds, it.StoreID)
		p.StoreSections = append(p.StoreSections, string(it.StoreSection))
		p.NeededBy = append(p.NeededBy, store.DateToPG(it.NeededBy))
		p.TopUps = append(p.TopUps, it.TopUp)
		p.Warnings = append(p.Warnings, it.Warning)
		p.Checked = append(p.Checked, it.Checked)
		p.CheckedBy = append(p.CheckedBy, it.CheckedBy)
		p.CheckTimes = append(p.CheckTimes, checkTime)
		p.Notes = append(p.Notes, it.Note)
		p.Manual = append(p.Manual, it.Manual)
	}
	return p
}
