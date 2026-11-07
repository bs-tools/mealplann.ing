// Package recipes stores the households' recipes, made of their foods.
package recipes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/bs-tools/foodplann.ing/internal/nutrition"
	"github.com/bs-tools/foodplann.ing/internal/recipes/internal/gen"
	"github.com/bs-tools/foodplann.ing/internal/store"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrPlanned is returned when deleting a recipe that cooks or staples of
// the plan use.
var ErrPlanned = errors.New("the recipe is planned")

// UnknownFoodsError is returned when a recipe's ingredients use foods that
// aren't the household's.
type UnknownFoodsError struct {
	// FoodIDs are the IDs of the unknown foods, in the order of the
	// ingredients.
	FoodIDs []pgtype.UUID
}

func (e *UnknownFoodsError) Error() string {
	return fmt.Sprintf("%d of the recipe's foods aren't the household's", len(e.FoodIDs))
}

// Recipe is a household's recipe.
type Recipe struct {
	ID          pgtype.UUID
	HouseholdID pgtype.UUID
	Name        string
	Description string
	// Servings is how many servings the recipe makes, at least 1.
	Servings int32
	// PrepSeconds and CookSeconds are the preparation and cooking times, 0
	// if unknown.
	PrepSeconds int32
	CookSeconds int32
	Ingredients []*Ingredient
	Steps       []string
	Tags        []string
	SourceURL   string
	// FridgeDays is how many days leftovers keep in the fridge, nil if
	// unknown.
	FridgeDays *int32
	Freezable  bool
	Notes      string
	CreateTime time.Time
	UpdateTime time.Time
}

// Ingredient is an amount of a food in a recipe.
type Ingredient struct {
	FoodID       pgtype.UUID
	Amount       float64
	Unit         nutrition.Unit
	Note         string
	OriginalText string
	Optional     bool
	// FoodName, Food and FastCarb are read from the food, and ignored when
	// writing.
	FoodName string
	Food     nutrition.Food
	FastCarb bool
}

// Nutrition computes the recipe's nutrition from its ingredients.
func (r *Recipe) Nutrition() *nutrition.Recipe {
	ingredients := make([]nutrition.Ingredient, 0, len(r.Ingredients))
	for _, in := range r.Ingredients {
		ingredients = append(ingredients, nutrition.Ingredient{
			Food:     &in.Food,
			Amount:   in.Amount,
			Unit:     in.Unit,
			Optional: in.Optional,
		})
	}
	return nutrition.ForRecipe(ingredients, int(r.Servings))
}

// Repository stores recipes.
type Repository struct {
	log     *slog.Logger
	db      *store.DB
	querier *gen.Queries
}

// NewRepository returns a repository of the recipes in the database.
func NewRepository(log *slog.Logger, db *store.DB) *Repository {
	return &Repository{log: log, db: db, querier: gen.New(db)}
}

// CreateRecipe creates a recipe in its household, and returns it with its
// ingredients' foods. It returns an *UnknownFoodsError if ingredients use
// foods that aren't the household's.
func (r *Repository) CreateRecipe(ctx context.Context, recipe *Recipe) (*Recipe, error) {
	id := store.NewUUID()
	var row *gen.Recipe
	var ingredients []*gen.ListRecipeIngredientsRow
	// The statements run in a single round trip, in an implicit
	// transaction: if a food isn't the household's, its foreign key fails
	// and nothing is created.
	var b store.Batch
	checkFoods(ctx, &b, recipe)
	b.Read(func(db store.Querier) error {
		var err error
		row, err = gen.New(db).CreateRecipe(ctx, &gen.CreateRecipeParams{
			ID:          id,
			HouseholdID: recipe.HouseholdID,
			Name:        recipe.Name,
			Description: recipe.Description,
			Servings:    recipe.Servings,
			PrepSeconds: recipe.PrepSeconds,
			CookSeconds: recipe.CookSeconds,
			Steps:       nonNil(recipe.Steps),
			Tags:        nonNil(recipe.Tags),
			SourceURL:   recipe.SourceURL,
			FridgeDays:  recipe.FridgeDays,
			Freezable:   recipe.Freezable,
			Notes:       recipe.Notes,
		})
		return err
	})
	_ = gen.New(&b).CreateRecipeIngredients(ctx, ingredientsToDB(recipe.HouseholdID, id, recipe.Ingredients))
	readIngredients(ctx, &b, recipe.HouseholdID, id, &ingredients)
	if err := b.Send(ctx, r.db); err != nil {
		if _, ok := errors.AsType[*UnknownFoodsError](err); ok {
			return nil, err
		}
		return nil, fmt.Errorf("failed to create recipe: %w", err)
	}
	return recipeFromDB(row, ingredients), nil
}

// GetRecipe gets a household's recipe with its ingredients' foods. It
// returns store.ErrNotFound if the household has no such recipe.
func (r *Repository) GetRecipe(ctx context.Context, householdID, id pgtype.UUID) (*Recipe, error) {
	var row *gen.Recipe
	var ingredients []*gen.ListRecipeIngredientsRow
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		row, err = gen.New(db).GetRecipe(ctx, &gen.GetRecipeParams{ID: id, HouseholdID: householdID})
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return err
	})
	readIngredients(ctx, &b, householdID, id, &ingredients)
	if err := b.Send(ctx, r.db); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to get recipe: %w", err)
	}
	return recipeFromDB(row, ingredients), nil
}

// ListRecipes lists a household's recipes, with their ingredients' foods,
// whose name or description contains query, ignoring case, and that have
// all of tags, ordered by name. It also returns all the tags of the
// household's recipes, in order.
func (r *Repository) ListRecipes(ctx context.Context, householdID pgtype.UUID, query string, tags []string) ([]*Recipe, []string, error) {
	var rows []*gen.Recipe
	var allTags []string
	var ingredients []*gen.ListRecipeIngredientsRow
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		rows, err = gen.New(db).ListRecipes(ctx, &gen.ListRecipesParams{
			HouseholdID: householdID,
			Query:       query,
			Tags:        nonNil(tags),
		})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		allTags, err = gen.New(db).ListRecipeTags(ctx, householdID)
		return err
	})
	// The ingredients of all the household's recipes are read in the same
	// round trip, rather than waiting for the matching recipes' IDs.
	b.Read(func(db store.Querier) error {
		var err error
		ingredients, err = gen.New(db).ListRecipeIngredients(ctx, &gen.ListRecipeIngredientsParams{
			HouseholdID: householdID,
			AllRecipes:  true,
		})
		return err
	})
	if err := b.Send(ctx, r.db); err != nil {
		return nil, nil, fmt.Errorf("failed to list recipes: %w", err)
	}
	byRecipe := map[pgtype.UUID][]*gen.ListRecipeIngredientsRow{}
	for _, in := range ingredients {
		byRecipe[in.RecipeIngredient.RecipeID] = append(byRecipe[in.RecipeIngredient.RecipeID], in)
	}
	recipes := make([]*Recipe, 0, len(rows))
	for _, row := range rows {
		recipes = append(recipes, recipeFromDB(row, byRecipe[row.ID]))
	}
	return recipes, allTags, nil
}

// ListRecipesByID lists the household's recipes with the given IDs, with
// their ingredients' foods, ordered by name. IDs of recipes the household
// doesn't have are left out.
func (r *Repository) ListRecipesByID(ctx context.Context, householdID pgtype.UUID, ids []pgtype.UUID) ([]*Recipe, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []*gen.Recipe
	var ingredients []*gen.ListRecipeIngredientsRow
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		rows, err = gen.New(db).ListRecipesByID(ctx, &gen.ListRecipesByIDParams{HouseholdID: householdID, Ids: ids})
		return err
	})
	b.Read(func(db store.Querier) error {
		var err error
		ingredients, err = gen.New(db).ListRecipeIngredients(ctx, &gen.ListRecipeIngredientsParams{
			HouseholdID: householdID,
			RecipeIds:   ids,
		})
		return err
	})
	if err := b.Send(ctx, r.db); err != nil {
		return nil, fmt.Errorf("failed to list recipes: %w", err)
	}
	byRecipe := map[pgtype.UUID][]*gen.ListRecipeIngredientsRow{}
	for _, in := range ingredients {
		byRecipe[in.RecipeIngredient.RecipeID] = append(byRecipe[in.RecipeIngredient.RecipeID], in)
	}
	recipes := make([]*Recipe, 0, len(rows))
	for _, row := range rows {
		recipes = append(recipes, recipeFromDB(row, byRecipe[row.ID]))
	}
	return recipes, nil
}

// UpdateRecipe replaces a household's recipe, other than its times, and
// its ingredients. It returns store.ErrNotFound if the household has no
// such recipe, and an *UnknownFoodsError if ingredients use foods that
// aren't the household's.
func (r *Repository) UpdateRecipe(ctx context.Context, recipe *Recipe) (*Recipe, error) {
	var row *gen.Recipe
	var ingredients []*gen.ListRecipeIngredientsRow
	// As in CreateRecipe, a failed statement leaves the recipe as it was.
	var b store.Batch
	checkFoods(ctx, &b, recipe)
	b.Read(func(db store.Querier) error {
		var err error
		row, err = gen.New(db).UpdateRecipe(ctx, &gen.UpdateRecipeParams{
			ID:          recipe.ID,
			HouseholdID: recipe.HouseholdID,
			Name:        recipe.Name,
			Description: recipe.Description,
			Servings:    recipe.Servings,
			PrepSeconds: recipe.PrepSeconds,
			CookSeconds: recipe.CookSeconds,
			Steps:       nonNil(recipe.Steps),
			Tags:        nonNil(recipe.Tags),
			SourceURL:   recipe.SourceURL,
			FridgeDays:  recipe.FridgeDays,
			Freezable:   recipe.Freezable,
			Notes:       recipe.Notes,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return store.ErrNotFound
		}
		return err
	})
	q := gen.New(&b)
	_ = q.DeleteRecipeIngredients(ctx, &gen.DeleteRecipeIngredientsParams{RecipeID: recipe.ID, HouseholdID: recipe.HouseholdID})
	_ = q.CreateRecipeIngredients(ctx, ingredientsToDB(recipe.HouseholdID, recipe.ID, recipe.Ingredients))
	readIngredients(ctx, &b, recipe.HouseholdID, recipe.ID, &ingredients)
	if err := b.Send(ctx, r.db); err != nil {
		if _, ok := errors.AsType[*UnknownFoodsError](err); ok || errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to update recipe: %w", err)
	}
	return recipeFromDB(row, ingredients), nil
}

// DeleteRecipe deletes a household's recipe. It returns store.ErrNotFound
// if the household has no such recipe, and ErrPlanned if cooks or staples
// use it.
func (r *Repository) DeleteRecipe(ctx context.Context, householdID, id pgtype.UUID) error {
	n, err := r.querier.DeleteRecipe(ctx, &gen.DeleteRecipeParams{ID: id, HouseholdID: householdID})
	if err != nil {
		// The plan's foreign keys keep planned recipes.
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == pgerrcode.ForeignKeyViolation {
			return ErrPlanned
		}
		return fmt.Errorf("failed to delete recipe: %w", err)
	}
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// checkFoods queues reading which of the recipe's foods are the
// household's, failing with an *UnknownFoodsError if any aren't. The
// foreign keys would refuse them anyway, but couldn't say which.
func checkFoods(ctx context.Context, b *store.Batch, recipe *Recipe) {
	ids := make([]pgtype.UUID, 0, len(recipe.Ingredients))
	for _, in := range recipe.Ingredients {
		ids = append(ids, in.FoodID)
	}
	b.Read(func(db store.Querier) error {
		known, err := gen.New(db).ListHouseholdFoodIDs(ctx, &gen.ListHouseholdFoodIDsParams{
			HouseholdID: recipe.HouseholdID,
			Ids:         ids,
		})
		if err != nil {
			return err
		}
		found := map[pgtype.UUID]bool{}
		for _, id := range known {
			found[id] = true
		}
		var unknown []pgtype.UUID
		for _, id := range ids {
			if !found[id] {
				unknown = append(unknown, id)
				// Each unknown food is reported once.
				found[id] = true
			}
		}
		if len(unknown) > 0 {
			return &UnknownFoodsError{FoodIDs: unknown}
		}
		return nil
	})
}

// readIngredients queues reading a recipe's ingredients into rows.
func readIngredients(ctx context.Context, b *store.Batch, householdID, recipeID pgtype.UUID, rows *[]*gen.ListRecipeIngredientsRow) {
	b.Read(func(db store.Querier) error {
		var err error
		*rows, err = gen.New(db).ListRecipeIngredients(ctx, &gen.ListRecipeIngredientsParams{
			HouseholdID: householdID,
			RecipeIds:   []pgtype.UUID{recipeID},
		})
		return err
	})
}

func ingredientsToDB(householdID, recipeID pgtype.UUID, ingredients []*Ingredient) *gen.CreateRecipeIngredientsParams {
	p := &gen.CreateRecipeIngredientsParams{
		RecipeID:      recipeID,
		HouseholdID:   householdID,
		FoodIds:       make([]pgtype.UUID, 0, len(ingredients)),
		Amounts:       make([]float64, 0, len(ingredients)),
		Units:         make([]string, 0, len(ingredients)),
		Notes:         make([]string, 0, len(ingredients)),
		OriginalTexts: make([]string, 0, len(ingredients)),
		Optionals:     make([]bool, 0, len(ingredients)),
	}
	for _, in := range ingredients {
		p.FoodIds = append(p.FoodIds, in.FoodID)
		p.Amounts = append(p.Amounts, in.Amount)
		p.Units = append(p.Units, string(unitToDB(in.Unit)))
		p.Notes = append(p.Notes, in.Note)
		p.OriginalTexts = append(p.OriginalTexts, in.OriginalText)
		p.Optionals = append(p.Optionals, in.Optional)
	}
	return p
}

func recipeFromDB(row *gen.Recipe, ingredients []*gen.ListRecipeIngredientsRow) *Recipe {
	r := &Recipe{
		ID:          row.ID,
		HouseholdID: row.HouseholdID,
		Name:        row.Name,
		Description: row.Description,
		Servings:    row.Servings,
		PrepSeconds: row.PrepSeconds,
		CookSeconds: row.CookSeconds,
		Steps:       row.Steps,
		Tags:        row.Tags,
		SourceURL:   row.SourceURL,
		FridgeDays:  row.FridgeDays,
		Freezable:   row.Freezable,
		Notes:       row.Notes,
		CreateTime:  row.CreateTime.Time,
		UpdateTime:  row.UpdateTime.Time,
		Ingredients: make([]*Ingredient, 0, len(ingredients)),
	}
	for _, in := range ingredients {
		r.Ingredients = append(r.Ingredients, ingredientFromDB(in))
	}
	return r
}

func ingredientFromDB(row *gen.ListRecipeIngredientsRow) *Ingredient {
	in := &row.RecipeIngredient
	return &Ingredient{
		FoodID:       in.FoodID,
		Amount:       in.Amount,
		Unit:         unitFromDB(in.Unit),
		Note:         in.Note,
		OriginalText: in.OriginalText,
		Optional:     in.Optional,
		FoodName:     row.FoodName,
		Food: nutrition.Food{
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
			GramsPerPiece:      orZero(row.GramsPerPiece),
			GramsPerMilliliter: orZero(row.GramsPerMilliliter),
		},
		FastCarb: row.FastCarb,
	}
}

func unitToDB(u nutrition.Unit) gen.AmountUnit {
	switch u {
	case nutrition.Milliliters:
		return gen.AmountUnitMilliliters
	case nutrition.Pieces:
		return gen.AmountUnitPieces
	default:
		return gen.AmountUnitGrams
	}
}

func unitFromDB(u gen.AmountUnit) nutrition.Unit {
	switch u {
	case gen.AmountUnitMilliliters:
		return nutrition.Milliliters
	case gen.AmountUnitPieces:
		return nutrition.Pieces
	default:
		return nutrition.Grams
	}
}

// nonNil returns s, or an empty slice for nil, as text[] columns aren't
// null.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// orZero returns *v, or 0 for null.
func orZero(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
