// Package foods stores the households' foods, the ingredients of their
// recipes, and the reference foods of USDA FoodData Central and the
// Canadian Nutrient File they're created from.
package foods

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/foods/internal/gen"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrNameTaken is returned when a household already has a food with the
// name, ignoring case.
var ErrNameTaken = errors.New("the household already has a food with this name")

// ErrUnknownStore is returned when a food's preferred store isn't one of
// its household's stores.
var ErrUnknownStore = errors.New("the preferred store isn't one of the household's stores")

// InUseError is returned when deleting a food that recipes or the plan
// use.
type InUseError struct {
	// Recipes are the names of the recipes using the food, by name.
	Recipes []string
	// PlanUses is how many portions and staples of the plan eat the food.
	PlanUses int64
}

func (e *InUseError) Error() string {
	if len(e.Recipes) == 0 {
		return fmt.Sprintf("the food is eaten in %d portions or staples", e.PlanUses)
	}
	return "the food is used by the recipes " + strings.Join(e.Recipes, ", ")
}

// StoreSection is the part of a store a food is found in.
type StoreSection string

// The store sections, as stored. The zero value is unspecified.
const (
	StoreSectionUnspecified StoreSection = ""
	StoreSectionProduce     StoreSection = "produce"
	StoreSectionBakery      StoreSection = "bakery"
	StoreSectionChilled     StoreSection = "chilled"
	StoreSectionFrozen      StoreSection = "frozen"
	StoreSectionDryGoods    StoreSection = "dry_goods"
	StoreSectionCanned      StoreSection = "canned"
	StoreSectionSpices      StoreSection = "spices"
	StoreSectionDrinks      StoreSection = "drinks"
	StoreSectionHousehold   StoreSection = "household"
	StoreSectionOther       StoreSection = "other"
)

// Food is a household's food: an ingredient, with its nutrients.
type Food struct {
	ID          pgtype.UUID
	HouseholdID pgtype.UUID
	Name        string
	// Source is the source of the reference food its nutrients were copied
	// from, SourceNone for custom foods.
	Source Source
	// SourceID is the ID of that reference food in its source, 0 for custom
	// foods.
	SourceID int32
	// Per100Grams are the nutrients in 100 g of the food, nil where unknown.
	Per100Grams nutrition.Nutrients
	// GramsPerPiece is the typical weight of a piece, 0 if unknown.
	GramsPerPiece float64
	// GramsPerMilliliter is the density, 0 if unknown.
	GramsPerMilliliter float64
	// Measure is how its amounts are shown to people, MeasureAuto to choose
	// by the amount.
	Measure Measure
	// FastCarb is whether the food's carbohydrate is quickly absorbed.
	FastCarb     bool
	StoreSection StoreSection
	// PackageGrams is the typical pack size, 0 if unknown.
	PackageGrams float64
	// ShelfLifeDays and OpenedShelfLifeDays are how long the food keeps,
	// unopened and opened, 0 if unknown.
	ShelfLifeDays       int32
	OpenedShelfLifeDays int32
	// FreezerShelfLifeDays is how long the food keeps frozen, 0 if unknown.
	FreezerShelfLifeDays int32
	// PriceCentsPerKilogram is what the household pays for it, in Canadian
	// cents per kilogram, seen on PriceDate, 0 and zero if unknown.
	PriceCentsPerKilogram int32
	PriceDate             time.Time
	Notes                 string
	// PreferredStoreID is the store the food is always bought at, invalid
	// for the household's main store.
	PreferredStoreID pgtype.UUID
	// AlwaysInStock is whether the household always has the food, like salt
	// or spices, so the pantry doesn't track it and the shopping list leaves
	// it out.
	AlwaysInStock bool
	CreateTime    time.Time
	UpdateTime    time.Time
}

// Measure is how a food's amounts are shown to people.
type Measure string

// The measures.
const (
	// MeasureAuto chooses by the amount: small amounts the food's density
	// converts by volume, like spoons of spices or oil, and the rest as
	// entered.
	MeasureAuto Measure = ""
	// MeasureWeight is by weight, like tofu or pasta.
	MeasureWeight Measure = "weight"
	// MeasureVolume is by volume, like oil, spices or plant milk.
	MeasureVolume Measure = "volume"
	// MeasurePieces is in pieces, like onions or garlic cloves.
	MeasurePieces Measure = "pieces"
	// MeasureMilliliters is by volume in milliliters, whatever the user's
	// units, like a 400 ml can of coconut milk.
	MeasureMilliliters Measure = "milliliters"
)

// MeasureToDB converts a measure to the database's, nil for MeasureAuto.
func MeasureToDB(m Measure) *string {
	if m == MeasureAuto {
		return nil
	}
	s := string(m)
	return &s
}

// MeasureFromDB converts a measure from the database's.
func MeasureFromDB(m *string) Measure {
	if m == nil {
		return MeasureAuto
	}
	return Measure(*m)
}

// Nutrition returns what nutrition math needs of the food.
func (f *Food) Nutrition() *nutrition.Food {
	return &nutrition.Food{
		Per100Grams:        f.Per100Grams,
		GramsPerPiece:      f.GramsPerPiece,
		GramsPerMilliliter: f.GramsPerMilliliter,
		PackageGrams:       f.PackageGrams,
	}
}

// Repository stores foods.
type Repository struct {
	log     *slog.Logger
	db      *store.DB
	querier *gen.Queries
}

// NewRepository returns a repository of the foods in the database.
func NewRepository(log *slog.Logger, db *store.DB) *Repository {
	return &Repository{log: log, db: db, querier: gen.New(db)}
}

// CreateFood creates a food in its household. It returns ErrNameTaken if
// the household has a food with the same name.
func (r *Repository) CreateFood(ctx context.Context, f *Food) (*Food, error) {
	n := &f.Per100Grams
	row, err := r.querier.CreateFood(ctx, &gen.CreateFoodParams{
		HouseholdID:                     f.HouseholdID,
		Name:                            f.Name,
		ReferenceSource:                 sourceToDB(f.Source),
		ReferenceID:                     sourceIDToDB(f.Source, f.SourceID),
		FDCID:                           fdcIDToDB(f.Source, f.SourceID),
		EnergyKcalPer100Grams:           n[nutrition.EnergyKcal],
		ProteinGramsPer100Grams:         n[nutrition.ProteinGrams],
		CarbohydrateGramsPer100Grams:    n[nutrition.CarbohydrateGrams],
		SugarsGramsPer100Grams:          n[nutrition.SugarsGrams],
		FatGramsPer100Grams:             n[nutrition.FatGrams],
		FiberGramsPer100Grams:           n[nutrition.FiberGrams],
		VitaminB12MicrogramsPer100Grams: n[nutrition.VitaminB12Micrograms],
		IronMilligramsPer100Grams:       n[nutrition.IronMilligrams],
		CalciumMilligramsPer100Grams:    n[nutrition.CalciumMilligrams],
		VitaminDMicrogramsPer100Grams:   n[nutrition.VitaminDMicrograms],
		IodineMicrogramsPer100Grams:     n[nutrition.IodineMicrograms],
		ZincMilligramsPer100Grams:       n[nutrition.ZincMilligrams],
		Omega3ALAGramsPer100Grams:       n[nutrition.Omega3ALAGrams],
		FolateMicrogramsPer100Grams:     n[nutrition.FolateMicrograms],
		VitaminAMicrogramsPer100Grams:   n[nutrition.VitaminAMicrograms],
		VitaminEMilligramsPer100Grams:   n[nutrition.VitaminEMilligrams],
		VitaminKMicrogramsPer100Grams:   n[nutrition.VitaminKMicrograms],
		CopperMilligramsPer100Grams:     n[nutrition.CopperMilligrams],
		MagnesiumMilligramsPer100Grams:  n[nutrition.MagnesiumMilligrams],
		SodiumMilligramsPer100Grams:     n[nutrition.SodiumMilligrams],
		PotassiumMilligramsPer100Grams:  n[nutrition.PotassiumMilligrams],
		GramsPerPiece:                   positive(f.GramsPerPiece),
		GramsPerMilliliter:              positive(f.GramsPerMilliliter),
		Measure:                         MeasureToDB(f.Measure),
		FastCarb:                        f.FastCarb,
		StoreSection:                    storeSectionToDB(f.StoreSection),
		PackageGrams:                    positive(f.PackageGrams),
		ShelfLifeDays:                   positive(f.ShelfLifeDays),
		OpenedShelfLifeDays:             positive(f.OpenedShelfLifeDays),
		FreezerShelfLifeDays:            positive(f.FreezerShelfLifeDays),
		PriceCentsPerKilogram:           positive(f.PriceCentsPerKilogram),
		PriceDate:                       store.DateToPG(f.PriceDate),
		Notes:                           f.Notes,
		PreferredStoreID:                f.PreferredStoreID,
		AlwaysInStock:                   f.AlwaysInStock,
	})
	if err != nil {
		switch {
		case isNameTaken(err):
			return nil, ErrNameTaken
		case isUnknownStore(err):
			return nil, ErrUnknownStore
		}
		return nil, fmt.Errorf("failed to create food: %w", err)
	}
	return foodFromDB(row), nil
}

// GetFood gets a household's food. It returns store.ErrNotFound if the
// household has no such food.
func (r *Repository) GetFood(ctx context.Context, householdID, id pgtype.UUID) (*Food, error) {
	row, err := r.querier.GetFood(ctx, &gen.GetFoodParams{ID: id, HouseholdID: householdID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get food: %w", err)
	}
	return foodFromDB(row), nil
}

// ListFoods lists a household's foods whose name contains query, ignoring
// case, ordered by name: limit of them, after skipping offset.
func (r *Repository) ListFoods(ctx context.Context, householdID pgtype.UUID, query string, needsMeasure bool, limit, offset int32) ([]*Food, error) {
	rows, err := r.querier.ListFoods(ctx, &gen.ListFoodsParams{
		HouseholdID:  householdID,
		Query:        query,
		NeedsMeasure: needsMeasure,
		RowLimit:     limit,
		RowOffset:    offset,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list foods: %w", err)
	}
	foods := make([]*Food, 0, len(rows))
	for _, row := range rows {
		foods = append(foods, foodFromDB(row))
	}
	return foods, nil
}

// UpdateFood replaces the fields of a household's food, other than its
// reference food and times. It returns store.ErrNotFound if the household has no
// such food, and ErrNameTaken if another of its foods has the name.
func (r *Repository) UpdateFood(ctx context.Context, f *Food) (*Food, error) {
	n := &f.Per100Grams
	row, err := r.querier.UpdateFood(ctx, &gen.UpdateFoodParams{
		ID:                              f.ID,
		HouseholdID:                     f.HouseholdID,
		Name:                            f.Name,
		EnergyKcalPer100Grams:           n[nutrition.EnergyKcal],
		ProteinGramsPer100Grams:         n[nutrition.ProteinGrams],
		CarbohydrateGramsPer100Grams:    n[nutrition.CarbohydrateGrams],
		SugarsGramsPer100Grams:          n[nutrition.SugarsGrams],
		FatGramsPer100Grams:             n[nutrition.FatGrams],
		FiberGramsPer100Grams:           n[nutrition.FiberGrams],
		VitaminB12MicrogramsPer100Grams: n[nutrition.VitaminB12Micrograms],
		IronMilligramsPer100Grams:       n[nutrition.IronMilligrams],
		CalciumMilligramsPer100Grams:    n[nutrition.CalciumMilligrams],
		VitaminDMicrogramsPer100Grams:   n[nutrition.VitaminDMicrograms],
		IodineMicrogramsPer100Grams:     n[nutrition.IodineMicrograms],
		ZincMilligramsPer100Grams:       n[nutrition.ZincMilligrams],
		Omega3ALAGramsPer100Grams:       n[nutrition.Omega3ALAGrams],
		FolateMicrogramsPer100Grams:     n[nutrition.FolateMicrograms],
		VitaminAMicrogramsPer100Grams:   n[nutrition.VitaminAMicrograms],
		VitaminEMilligramsPer100Grams:   n[nutrition.VitaminEMilligrams],
		VitaminKMicrogramsPer100Grams:   n[nutrition.VitaminKMicrograms],
		CopperMilligramsPer100Grams:     n[nutrition.CopperMilligrams],
		MagnesiumMilligramsPer100Grams:  n[nutrition.MagnesiumMilligrams],
		SodiumMilligramsPer100Grams:     n[nutrition.SodiumMilligrams],
		PotassiumMilligramsPer100Grams:  n[nutrition.PotassiumMilligrams],
		GramsPerPiece:                   positive(f.GramsPerPiece),
		GramsPerMilliliter:              positive(f.GramsPerMilliliter),
		Measure:                         MeasureToDB(f.Measure),
		FastCarb:                        f.FastCarb,
		StoreSection:                    storeSectionToDB(f.StoreSection),
		PackageGrams:                    positive(f.PackageGrams),
		ShelfLifeDays:                   positive(f.ShelfLifeDays),
		OpenedShelfLifeDays:             positive(f.OpenedShelfLifeDays),
		FreezerShelfLifeDays:            positive(f.FreezerShelfLifeDays),
		PriceCentsPerKilogram:           positive(f.PriceCentsPerKilogram),
		PriceDate:                       store.DateToPG(f.PriceDate),
		Notes:                           f.Notes,
		PreferredStoreID:                f.PreferredStoreID,
		AlwaysInStock:                   f.AlwaysInStock,
	})
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, store.ErrNotFound
		case isNameTaken(err):
			return nil, ErrNameTaken
		case isUnknownStore(err):
			return nil, ErrUnknownStore
		}
		return nil, fmt.Errorf("failed to update food: %w", err)
	}
	return foodFromDB(row), nil
}

// NameMatch is a food found by name.
type NameMatch struct {
	ID   pgtype.UUID
	Name string
}

// FindFoodsByName returns the household's food with the name, ignoring
// case, first, then up to limit foods with similar names.
func (r *Repository) FindFoodsByName(ctx context.Context, householdID pgtype.UUID, name string, limit int32) ([]NameMatch, error) {
	rows, err := r.querier.FindFoodsByName(ctx, &gen.FindFoodsByNameParams{HouseholdID: householdID, Name: name, RowLimit: limit})
	if err != nil {
		return nil, fmt.Errorf("failed to find foods by name: %w", err)
	}
	matches := make([]NameMatch, 0, len(rows))
	for _, row := range rows {
		matches = append(matches, NameMatch{ID: row.ID, Name: row.Name})
	}
	return matches, nil
}

// Usages are what uses a food.
type Usages struct {
	// Recipes are the recipes with the food as an ingredient, by name.
	Recipes []RecipeRef
	// PortionCount and StapleCount are how many portions and staples of
	// the plan eat the food.
	PortionCount, StapleCount int64
	// PantryLotCount is how many lots of the food the pantry has, with
	// PantryGrams in all.
	PantryLotCount int64
	PantryGrams    float64
}

// RecipeRef names a recipe.
type RecipeRef struct {
	ID   pgtype.UUID
	Name string
}

// GetUsages returns what uses a household's food, in a single round trip,
// or store.ErrNotFound if the household has no such food.
func (r *Repository) GetUsages(ctx context.Context, householdID, id pgtype.UUID) (*Usages, error) {
	var usages Usages
	var b store.Batch
	b.Read(func(db store.Querier) error {
		row, err := gen.New(db).GetFoodUsages(ctx, &gen.GetFoodUsagesParams{ID: id, HouseholdID: householdID})
		if err != nil {
			return err
		}
		usages.PortionCount, usages.StapleCount = row.PortionCount, row.StapleCount
		usages.PantryLotCount, usages.PantryGrams = row.PantryLotCount, row.PantryGrams
		return nil
	})
	b.Read(func(db store.Querier) error {
		rows, err := gen.New(db).ListFoodRecipes(ctx, &gen.ListFoodRecipesParams{HouseholdID: householdID, FoodID: id})
		for _, row := range rows {
			usages.Recipes = append(usages.Recipes, RecipeRef{ID: row.ID, Name: row.Name})
		}
		return err
	})
	if err := b.Send(ctx, r.db); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get food usages: %w", err)
	}
	return &usages, nil
}

// DeleteFood deletes a household's food. It returns an *InUseError if
// recipes use it, and store.ErrNotFound if the household has no such food.
func (r *Repository) DeleteFood(ctx context.Context, householdID, id pgtype.UUID) error {
	// Finding the recipes and deleting the food unless there are any take a
	// single round trip.
	var recipes []string
	var planUses, deleted int64
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		recipes, err = gen.New(db).ListRecipesUsingFood(ctx, &gen.ListRecipesUsingFoodParams{HouseholdID: householdID, FoodID: id})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		planUses, err = gen.New(db).CountFoodPlanUses(ctx, &gen.CountFoodPlanUsesParams{HouseholdID: householdID, FoodID: id})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		deleted, err = gen.New(db).DeleteUnusedFood(ctx, &gen.DeleteUnusedFoodParams{ID: id, HouseholdID: householdID})
		return err
	})
	if err := b.Send(ctx, r.db); err != nil {
		// A recipe can start using the food between the two statements.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation {
			return &InUseError{}
		}
		return fmt.Errorf("failed to delete food: %w", err)
	}
	if len(recipes) > 0 || planUses > 0 {
		return &InUseError{Recipes: recipes, PlanUses: planUses}
	}
	if deleted == 0 {
		return store.ErrNotFound
	}
	return nil
}

// ListFoodsByID lists the household's foods with the given IDs, ordered by
// name. IDs of foods the household doesn't have are left out.
func (r *Repository) ListFoodsByID(ctx context.Context, householdID pgtype.UUID, ids []pgtype.UUID) ([]*Food, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.querier.ListFoodsByID(ctx, &gen.ListFoodsByIDParams{HouseholdID: householdID, Ids: ids})
	if err != nil {
		return nil, fmt.Errorf("failed to list foods: %w", err)
	}
	list := make([]*Food, 0, len(rows))
	for _, row := range rows {
		list = append(list, foodFromDB(row))
	}
	return list, nil
}

// ListAlwaysInStockFoods lists a household's foods marked always in stock,
// ordered by name.
func (r *Repository) ListAlwaysInStockFoods(ctx context.Context, householdID pgtype.UUID) ([]*Food, error) {
	rows, err := r.querier.ListAlwaysInStockFoods(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("failed to list foods always in stock: %w", err)
	}
	list := make([]*Food, 0, len(rows))
	for _, row := range rows {
		list = append(list, foodFromDB(row))
	}
	return list, nil
}

// isUnknownStore reports whether err is the violation of the foreign key
// of a food's preferred store.
func isUnknownStore(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation &&
		pgErr.ConstraintName == "foods_household_id_preferred_store_id_fkey"
}

// isNameTaken reports whether err is the violation of the unique name of a
// household's foods.
func isNameTaken(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation &&
		pgErr.ConstraintName == "foods_household_id_lower_name"
}

func foodFromDB(row *gen.Food) *Food {
	source, sourceID := SourceNone, int32(0)
	switch {
	case row.ReferenceSource != nil && row.ReferenceID != nil:
		source, sourceID = Source(*row.ReferenceSource), *row.ReferenceID
	case row.FDCID != nil:
		// The previous revision creates foods from FDC with only their FDC
		// ID while this one starts, or after a rollback, see migration 005.
		source, sourceID = SourceFDC, *row.FDCID
	}
	return &Food{
		ID:          row.ID,
		HouseholdID: row.HouseholdID,
		Name:        row.Name,
		Source:      source,
		SourceID:    sourceID,
		Per100Grams: nutrition.Nutrients{
			nutrition.EnergyKcal:           row.EnergyKcalPer100Grams,
			nutrition.ProteinGrams:         row.ProteinGramsPer100Grams,
			nutrition.CarbohydrateGrams:    row.CarbohydrateGramsPer100Grams,
			nutrition.SugarsGrams:          row.SugarsGramsPer100Grams,
			nutrition.FatGrams:             row.FatGramsPer100Grams,
			nutrition.FiberGrams:           row.FiberGramsPer100Grams,
			nutrition.VitaminB12Micrograms: row.VitaminB12MicrogramsPer100Grams,
			nutrition.IronMilligrams:       row.IronMilligramsPer100Grams,
			nutrition.CalciumMilligrams:    row.CalciumMilligramsPer100Grams,
			nutrition.VitaminDMicrograms:   row.VitaminDMicrogramsPer100Grams,
			nutrition.IodineMicrograms:     row.IodineMicrogramsPer100Grams,
			nutrition.ZincMilligrams:       row.ZincMilligramsPer100Grams,
			nutrition.Omega3ALAGrams:       row.Omega3ALAGramsPer100Grams,
			nutrition.FolateMicrograms:     row.FolateMicrogramsPer100Grams,
			nutrition.VitaminAMicrograms:   row.VitaminAMicrogramsPer100Grams,
			nutrition.VitaminEMilligrams:   row.VitaminEMilligramsPer100Grams,
			nutrition.VitaminKMicrograms:   row.VitaminKMicrogramsPer100Grams,
			nutrition.CopperMilligrams:     row.CopperMilligramsPer100Grams,
			nutrition.MagnesiumMilligrams:  row.MagnesiumMilligramsPer100Grams,
			nutrition.SodiumMilligrams:     row.SodiumMilligramsPer100Grams,
			nutrition.PotassiumMilligrams:  row.PotassiumMilligramsPer100Grams,
		},
		GramsPerPiece:         orZero(row.GramsPerPiece),
		GramsPerMilliliter:    orZero(row.GramsPerMilliliter),
		Measure:               MeasureFromDB(row.Measure),
		FastCarb:              row.FastCarb,
		StoreSection:          storeSectionFromDB(row.StoreSection),
		PackageGrams:          orZero(row.PackageGrams),
		ShelfLifeDays:         orZero(row.ShelfLifeDays),
		OpenedShelfLifeDays:   orZero(row.OpenedShelfLifeDays),
		FreezerShelfLifeDays:  orZero(row.FreezerShelfLifeDays),
		PriceCentsPerKilogram: orZero(row.PriceCentsPerKilogram),
		PriceDate:             store.DateFromPG(row.PriceDate),
		Notes:                 row.Notes,
		PreferredStoreID:      row.PreferredStoreID,
		AlwaysInStock:         row.AlwaysInStock,
		CreateTime:            row.CreateTime.Time,
		UpdateTime:            row.UpdateTime.Time,
	}
}

// sourceToDB returns the reference source of a food, nil for custom foods.
func sourceToDB(s Source) *gen.ReferenceFoodSource {
	if s == SourceNone {
		return nil
	}
	return new(gen.ReferenceFoodSource(s))
}

// sourceIDToDB returns the reference ID of a food, nil for custom foods.
func sourceIDToDB(s Source, id int32) *int32 {
	if s == SourceNone {
		return nil
	}
	return &id
}

// fdcIDToDB returns the FDC ID of a food, nil unless it's from FDC. It's
// still written for the previous revision, which only knows fdc_id, see
// migration 005.
func fdcIDToDB(s Source, id int32) *int32 {
	if s != SourceFDC {
		return nil
	}
	return &id
}

func storeSectionToDB(s StoreSection) *gen.StoreSection {
	if s == StoreSectionUnspecified {
		return nil
	}
	return new(gen.StoreSection(s))
}

func storeSectionFromDB(s *gen.StoreSection) StoreSection {
	if s == nil {
		return StoreSectionUnspecified
	}
	return StoreSection(*s)
}

// positive returns a pointer to v, or nil for the null of unknown values,
// which the API sends as 0.
func positive[T int32 | float64](v T) *T {
	if v <= 0 {
		return nil
	}
	return &v
}

// orZero returns *v, or 0 for null.
func orZero[T int32 | float64](v *T) T {
	if v == nil {
		return 0
	}
	return *v
}
