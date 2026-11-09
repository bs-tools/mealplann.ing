// Package recipes stores the households' recipes, made of their foods.
package recipes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/foods"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/recipes/internal/gen"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrPlanned is returned when deleting a recipe that cooks or staples of
// the plan use.
var ErrPlanned = errors.New("the recipe is planned")

// ErrUsedAsComponent is returned when deleting a recipe that other recipes
// use as a component.
var ErrUsedAsComponent = errors.New("other recipes use the recipe")

// UnknownComponentsError is returned when a recipe's ingredients use
// recipes that aren't the household's as components.
type UnknownComponentsError struct {
	// RecipeIDs are the IDs of the unknown recipes, in the order of the
	// ingredients.
	RecipeIDs []pgtype.UUID
}

func (e *UnknownComponentsError) Error() string {
	return fmt.Sprintf("%d of the recipe's components aren't the household's recipes", len(e.RecipeIDs))
}

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
	// Instructions are the steps with their structure.
	Instructions []*Step
	Tags         []string
	SourceURL    string
	// FridgeDays is how many days leftovers keep in the fridge, nil if
	// unknown.
	FridgeDays *int32
	Freezable  bool
	// FreezerDays is how many days frozen leftovers keep, nil if unknown.
	FreezerDays *int32
	// PrepAheadDays is how many days before it's cooked it starts, e.g. 1
	// to soak beans the night before, and PrepAheadNote what to do then.
	PrepAheadDays int32
	PrepAheadNote string
	Notes         string
	// Favorite is whether the household marked the recipe as a favorite,
	// which suggestions score higher.
	Favorite bool
	// YieldAmount is what the whole recipe makes, in YieldUnit, 0 if not
	// given, to convert amounts of it as a component to servings.
	YieldAmount float64
	YieldUnit   nutrition.Unit
	// Draft is whether it's saved unfinished: its ingredients may be
	// unmatched, and it isn't planned or suggested.
	Draft      bool
	CreateTime time.Time
	UpdateTime time.Time
}

// MaxComponentDepth is how deep components can nest: a recipe's component
// can have components of its own, which can't. It keeps reading and
// expanding recipes bounded, and is deep enough for beans in a chili in a
// burrito.
const MaxComponentDepth = 2

// Ingredient is an amount of a food in a recipe, or servings of another
// recipe, a component, like cooked chickpeas.
type Ingredient struct {
	// FoodID is the food, invalid for a component.
	FoodID pgtype.UUID
	// ComponentRecipeID is the component's recipe, invalid for a food.
	ComponentRecipeID pgtype.UUID
	// Amount is in Unit, or in servings of a component without a unit. An
	// unmatched ingredient's is 0 if unknown.
	Amount float64
	// Unit is the unit of the amount: of a food, of what a component's
	// recipe makes (see ComponentServings), or 0 for a component in
	// servings or an unmatched ingredient without an amount.
	Unit nutrition.Unit
	// KitchenUnit is the unit the amount was entered in, like tablespoons,
	// to show it in, if not Unit. Amount is in Unit regardless.
	KitchenUnit  nutrition.KitchenUnit
	Note         string
	OriginalText string
	Optional     bool
	// FoodName, Food, FoodMeasure and FastCarb are read from the food, and
	// ignored when writing.
	FoodName    string
	Food        nutrition.Food
	FoodMeasure foods.Measure
	FastCarb    bool
	// PriceCentsPerKilogram is the food's price, 0 if unknown.
	PriceCentsPerKilogram int32
	// Component is the component's recipe, read with it, nil for a food or
	// a component nested deeper than MaxComponentDepth.
	Component *Recipe
}

// IsComponent reports whether the ingredient is another recipe.
func (in *Ingredient) IsComponent() bool {
	return in.ComponentRecipeID.Valid
}

// IsMatched reports whether the ingredient is a food or a component,
// rather than a draft's ingredient with only its original text.
func (in *Ingredient) IsMatched() bool {
	return in.FoodID.Valid || in.ComponentRecipeID.Valid
}

// ComponentServings returns how many servings of its recipe a component
// is: its amount if in servings, or converted with its recipe's yield if
// in a unit of what it makes, or for grams without a yield in grams, with
// the weight of its recipe's ingredients. It returns 0 if the amount can't
// be converted, or the component wasn't read.
func (in *Ingredient) ComponentServings() float64 {
	return in.componentServings(0)
}

func (in *Ingredient) componentServings(depth int) float64 {
	if in.Unit == 0 {
		return in.Amount
	}
	c := in.Component
	if c == nil {
		return 0
	}
	servings := float64(max(c.Servings, 1))
	if c.YieldAmount > 0 && c.YieldUnit == in.Unit {
		return in.Amount / c.YieldAmount * servings
	}
	if in.Unit == nutrition.Grams && depth < MaxComponentDepth {
		if total := c.nutrition(depth + 1).TotalGrams; total > 0 {
			return in.Amount / total * servings
		}
	}
	return 0
}

// Name returns the name of the ingredient's food or component, "" if a
// component wasn't read, or an unmatched ingredient's original text.
func (in *Ingredient) Name() string {
	if !in.IsMatched() {
		return in.OriginalText
	}
	if in.IsComponent() {
		if in.Component != nil {
			return in.Component.Name
		}
		return ""
	}
	return in.FoodName
}

// Nutrition computes the recipe's nutrition from its ingredients. A
// component counts as a food with its own recipe's nutrition, weighed by
// its servings, and its incomplete nutrients are incomplete here too.
func (r *Recipe) Nutrition() *nutrition.Recipe {
	return r.nutrition(0)
}

// nutrition is Nutrition at a depth of components, which leaves out
// components deeper than MaxComponentDepth, so recipes using each other by
// mistake can't recurse forever.
func (r *Recipe) nutrition(depth int) *nutrition.Recipe {
	ingredients := make([]nutrition.Ingredient, 0, len(r.Ingredients))
	var componentIncomplete []nutrition.Nutrient
	for _, in := range r.Ingredients {
		if !in.IsMatched() {
			// It has no food, so it can't be weighed, which the issues
			// report, keeping each issue's index the ingredient's.
			ingredients = append(ingredients, nutrition.Ingredient{
				Food: &nutrition.Food{}, Amount: 1, Unit: nutrition.Pieces, Optional: in.Optional,
			})
			continue
		}
		if !in.IsComponent() {
			ingredients = append(ingredients, nutrition.Ingredient{
				Food:     &in.Food,
				Amount:   in.Amount,
				Unit:     in.Unit,
				Optional: in.Optional,
			})
			continue
		}
		// A component is weighed in servings, as pieces of its serving's
		// weight, so one that can't be weighed is an unknown weight.
		food := &nutrition.Food{}
		if in.Component != nil && depth < MaxComponentDepth {
			n := in.Component.nutrition(depth + 1)
			food.Per100Grams = n.Per100Grams
			food.GramsPerPiece = n.GramsPerServing
			if !in.Optional {
				componentIncomplete = append(componentIncomplete, n.Incomplete...)
			}
		}
		amount := in.componentServings(depth)
		if amount <= 0 {
			// Unconverted, it can't be weighed.
			food, amount = &nutrition.Food{}, 1
		}
		ingredients = append(ingredients, nutrition.Ingredient{
			Food:     food,
			Amount:   amount,
			Unit:     nutrition.Pieces,
			Optional: in.Optional,
		})
	}
	n := nutrition.ForRecipe(ingredients, int(r.Servings))
	for _, nutrient := range componentIncomplete {
		if !slices.Contains(n.Incomplete, nutrient) {
			n.Incomplete = append(n.Incomplete, nutrient)
		}
	}
	slices.Sort(n.Incomplete)
	return n
}

// IngredientsFor returns the foods used to make a number of servings of
// the recipe, with its components made of their own ingredients: copies of
// the ingredients scaled by servings divided by the recipe's servings,
// without the optional ones, which the shopping list and the pantry leave
// out like the nutrition does.
func (r *Recipe) IngredientsFor(servings float64) []*Ingredient {
	return r.IngredientsWithout(servings, nil)
}

// IngredientsWithout is IngredientsFor without the components in supplied,
// by recipe ID, which another cook makes. Components it doesn't supply
// are made of their own ingredients, like IngredientsFor's.
func (r *Recipe) IngredientsWithout(servings float64, supplied map[pgtype.UUID]bool) []*Ingredient {
	return r.ingredientsWithout(servings, supplied, 0)
}

// ingredientsWithout is IngredientsWithout at a depth of components,
// leaving out those deeper than MaxComponentDepth.
func (r *Recipe) ingredientsWithout(servings float64, supplied map[pgtype.UUID]bool, depth int) []*Ingredient {
	scale := servings / float64(max(r.Servings, 1))
	scaled := make([]*Ingredient, 0, len(r.Ingredients))
	for _, in := range r.Ingredients {
		switch {
		case in.Optional, !in.IsMatched():
		case in.IsComponent():
			if !supplied[in.ComponentRecipeID] && in.Component != nil && depth < MaxComponentDepth {
				scaled = append(scaled, in.Component.ingredientsWithout(in.componentServings(depth)*scale, nil, depth+1)...)
			}
		default:
			c := *in
			c.Amount *= scale
			scaled = append(scaled, &c)
		}
	}
	return scaled
}

// ComponentNeed is servings of a component a cook needs.
type ComponentNeed struct {
	Recipe   *Recipe
	Servings float64
}

// ComponentsFor returns the components used to make a number of servings
// of the recipe, with their servings, without the optional ones. A
// component used twice is needed once, in the servings of both.
func (r *Recipe) ComponentsFor(servings float64) []ComponentNeed {
	scale := servings / float64(max(r.Servings, 1))
	var needs []ComponentNeed
	for _, in := range r.Ingredients {
		if in.Optional || !in.IsComponent() || in.Component == nil {
			continue
		}
		servings := in.ComponentServings() * scale
		if i := slices.IndexFunc(needs, func(n ComponentNeed) bool { return n.Recipe.ID == in.ComponentRecipeID }); i >= 0 {
			needs[i].Servings += servings
			continue
		}
		needs = append(needs, ComponentNeed{Recipe: in.Component, Servings: servings})
	}
	return needs
}

// UsesRecipe reports whether the recipe uses another as a component, at
// any depth read.
func (r *Recipe) UsesRecipe(id pgtype.UUID) bool {
	return r.usesRecipe(id, 0)
}

func (r *Recipe) usesRecipe(id pgtype.UUID, depth int) bool {
	for _, in := range r.Ingredients {
		if in.ComponentRecipeID == id {
			return true
		}
		if in.Component != nil && depth <= MaxComponentDepth && in.Component.usesRecipe(id, depth+1) {
			return true
		}
	}
	return false
}

// Depth returns how deep the recipe's components nest: 0 without
// components, 1 with components without their own, and so on.
func (r *Recipe) Depth() int {
	return r.depth(0)
}

// depth is Depth, stopping past MaxComponentDepth, as deeper is too deep
// anyway.
func (r *Recipe) depth(level int) int {
	deepest := 0
	for _, in := range r.Ingredients {
		if in.IsComponent() {
			d := 1
			if in.Component != nil && level <= MaxComponentDepth {
				d += in.Component.depth(level + 1)
			}
			deepest = max(deepest, d)
		}
	}
	return deepest
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
	instructions, err := instructionsToDB(recipe.Instructions)
	if err != nil {
		return nil, err
	}
	id := store.NewUUID()
	var row *gen.Recipe
	var ingredients []*gen.ListRecipeIngredientsRow
	// The statements run in a single round trip, in an implicit
	// transaction: if a food isn't the household's, its foreign key fails
	// and nothing is created.
	var b store.Batch
	checkFoods(ctx, &b, recipe)
	checkComponents(ctx, &b, recipe)
	b.Read(func(db store.Querier) error {
		var err error
		row, err = gen.New(db).CreateRecipe(ctx, &gen.CreateRecipeParams{
			ID:            id,
			HouseholdID:   recipe.HouseholdID,
			Name:          recipe.Name,
			Description:   recipe.Description,
			Servings:      recipe.Servings,
			PrepSeconds:   recipe.PrepSeconds,
			CookSeconds:   recipe.CookSeconds,
			Instructions:  instructions,
			Tags:          nonNil(recipe.Tags),
			SourceURL:     recipe.SourceURL,
			FridgeDays:    recipe.FridgeDays,
			Freezable:     recipe.Freezable,
			FreezerDays:   recipe.FreezerDays,
			PrepAheadDays: recipe.PrepAheadDays,
			PrepAheadNote: recipe.PrepAheadNote,
			Notes:         recipe.Notes,
			Favorite:      recipe.Favorite,
			YieldAmount:   yieldAmountToDB(recipe),
			YieldUnit:     yieldUnitToDB(recipe),
			Draft:         recipe.Draft,
		})
		return err
	})
	_ = gen.New(&b).CreateRecipeIngredients(ctx, ingredientsToDB(recipe.HouseholdID, id, recipe.Ingredients))
	readIngredients(ctx, &b, recipe.HouseholdID, id, &ingredients)
	if err := b.Send(ctx, r.db); err != nil {
		if _, ok := errors.AsType[*UnknownFoodsError](err); ok {
			return nil, err
		}
		if _, ok := errors.AsType[*UnknownComponentsError](err); ok {
			return nil, err
		}
		return nil, fmt.Errorf("failed to create recipe: %w", err)
	}
	return r.resolved(ctx, recipeFromDB(row, ingredients))
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
	return r.resolved(ctx, recipeFromDB(row, ingredients))
}

// ListFilter selects the recipes to list. Its zero value selects all.
type ListFilter struct {
	// Query is a text the name or description contains, ignoring case,
	// empty for any.
	Query string
	// Tags are tags the recipes all have, empty for any.
	Tags []string
	// FavoritesOnly is whether to list only favorites.
	FavoritesOnly bool
	// FoodIDs are foods the recipes have any of as an ingredient, empty
	// for any.
	FoodIDs []pgtype.UUID
}

// NameMatch is a recipe found by name.
type NameMatch struct {
	ID   pgtype.UUID
	Name string
}

// FindRecipesByName returns the household's recipe with the name, ignoring
// case, first, then up to limit recipes with similar names.
func (r *Repository) FindRecipesByName(ctx context.Context, householdID pgtype.UUID, name string, limit int32) ([]NameMatch, error) {
	rows, err := r.querier.FindRecipesByName(ctx, &gen.FindRecipesByNameParams{HouseholdID: householdID, Name: name, RowLimit: limit})
	if err != nil {
		return nil, fmt.Errorf("failed to find recipes by name: %w", err)
	}
	matches := make([]NameMatch, 0, len(rows))
	for _, row := range rows {
		matches = append(matches, NameMatch{ID: row.ID, Name: row.Name})
	}
	return matches, nil
}

// ListRecipes lists a household's recipes that match the filter, with
// their ingredients' foods, ordered by name. It also returns all the tags
// of the household's recipes, in order.
func (r *Repository) ListRecipes(ctx context.Context, householdID pgtype.UUID, filter ListFilter) ([]*Recipe, []string, error) {
	var rows []*gen.Recipe
	var allTags []string
	var ingredients []*gen.ListRecipeIngredientsRow
	var b store.Batch
	b.Read(func(db store.Querier) error {
		var err error
		rows, err = gen.New(db).ListRecipes(ctx, &gen.ListRecipesParams{
			HouseholdID:   householdID,
			Query:         filter.Query,
			Tags:          nonNil(filter.Tags),
			FavoritesOnly: filter.FavoritesOnly,
			FoodIds:       nonNil(filter.FoodIDs),
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
	// The ingredients of all recipes are read, so components among the
	// recipes listed resolve without reading them again.
	if err := r.resolve(ctx, householdID, recipes); err != nil {
		return nil, nil, fmt.Errorf("failed to read components: %w", err)
	}
	return recipes, allTags, nil
}

// ListRecipesByID lists the household's recipes with the given IDs, with
// their ingredients' foods, ordered by name. IDs of recipes the household
// doesn't have are left out.
func (r *Repository) ListRecipesByID(ctx context.Context, householdID pgtype.UUID, ids []pgtype.UUID) ([]*Recipe, error) {
	list, err := r.listRecipesByID(ctx, householdID, ids)
	if err != nil {
		return nil, err
	}
	if err := r.resolve(ctx, householdID, list); err != nil {
		return nil, fmt.Errorf("failed to read components: %w", err)
	}
	return list, nil
}

// resolved returns a recipe with its components resolved.
func (r *Repository) resolved(ctx context.Context, recipe *Recipe) (*Recipe, error) {
	if err := r.resolve(ctx, recipe.HouseholdID, []*Recipe{recipe}); err != nil {
		return nil, fmt.Errorf("failed to read components: %w", err)
	}
	return recipe, nil
}

// listRecipesByID is ListRecipesByID without resolving components.
func (r *Repository) listRecipesByID(ctx context.Context, householdID pgtype.UUID, ids []pgtype.UUID) ([]*Recipe, error) {
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
	instructions, err := instructionsToDB(recipe.Instructions)
	if err != nil {
		return nil, err
	}
	var row *gen.Recipe
	var ingredients []*gen.ListRecipeIngredientsRow
	// As in CreateRecipe, a failed statement leaves the recipe as it was.
	var b store.Batch
	checkFoods(ctx, &b, recipe)
	checkComponents(ctx, &b, recipe)
	b.Read(func(db store.Querier) error {
		var err error
		row, err = gen.New(db).UpdateRecipe(ctx, &gen.UpdateRecipeParams{
			ID:            recipe.ID,
			HouseholdID:   recipe.HouseholdID,
			Name:          recipe.Name,
			Description:   recipe.Description,
			Servings:      recipe.Servings,
			PrepSeconds:   recipe.PrepSeconds,
			CookSeconds:   recipe.CookSeconds,
			Instructions:  instructions,
			Tags:          nonNil(recipe.Tags),
			SourceURL:     recipe.SourceURL,
			FridgeDays:    recipe.FridgeDays,
			Freezable:     recipe.Freezable,
			FreezerDays:   recipe.FreezerDays,
			PrepAheadDays: recipe.PrepAheadDays,
			PrepAheadNote: recipe.PrepAheadNote,
			Notes:         recipe.Notes,
			Favorite:      recipe.Favorite,
			YieldAmount:   yieldAmountToDB(recipe),
			YieldUnit:     yieldUnitToDB(recipe),
			Draft:         recipe.Draft,
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
		if _, ok := errors.AsType[*UnknownComponentsError](err); ok {
			return nil, err
		}
		return nil, fmt.Errorf("failed to update recipe: %w", err)
	}
	return r.resolved(ctx, recipeFromDB(row, ingredients))
}

// DeleteRecipe deletes a household's recipe. It returns store.ErrNotFound
// if the household has no such recipe, ErrUsedAsComponent if other recipes
// use it, and ErrPlanned if cooks or staples do.
func (r *Repository) DeleteRecipe(ctx context.Context, householdID, id pgtype.UUID) error {
	n, err := r.querier.DeleteRecipe(ctx, &gen.DeleteRecipeParams{ID: id, HouseholdID: householdID})
	if err != nil {
		// The plan's foreign keys keep planned recipes, and the
		// ingredients' those used as components.
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == pgerrcode.ForeignKeyViolation {
			if pgErr.ConstraintName == "recipe_ingredients_component_recipe_id_fkey" {
				return ErrUsedAsComponent
			}
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
		if in.FoodID.Valid {
			ids = append(ids, in.FoodID)
		}
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

// checkComponents queues reading which of the recipe's components are the
// household's recipes, failing with an *UnknownComponentsError if any
// aren't, as checkFoods does for foods.
func checkComponents(ctx context.Context, b *store.Batch, recipe *Recipe) {
	var ids []pgtype.UUID
	for _, in := range recipe.Ingredients {
		if in.IsComponent() {
			ids = append(ids, in.ComponentRecipeID)
		}
	}
	if len(ids) == 0 {
		return
	}
	b.Read(func(db store.Querier) error {
		known, err := gen.New(db).ListHouseholdRecipeIDs(ctx, &gen.ListHouseholdRecipeIDsParams{
			HouseholdID: recipe.HouseholdID,
			Ids:         ids,
		})
		if err != nil {
			return err
		}
		var unknown []pgtype.UUID
		for _, id := range ids {
			if !slices.Contains(known, id) && !slices.Contains(unknown, id) {
				unknown = append(unknown, id)
			}
		}
		if len(unknown) > 0 {
			return &UnknownComponentsError{RecipeIDs: unknown}
		}
		return nil
	})
}

// resolve sets the Component of the recipes' component ingredients, to
// MaxComponentDepth levels, reading those not among the recipes in one
// round trip per level, and none if all are.
func (r *Repository) resolve(ctx context.Context, householdID pgtype.UUID, list []*Recipe) error {
	known := map[pgtype.UUID]*Recipe{}
	for _, rc := range list {
		known[rc.ID] = rc
	}
	level := list
	for depth := 1; depth <= MaxComponentDepth && len(level) > 0; depth++ {
		var missing []pgtype.UUID
		for _, rc := range level {
			for _, in := range rc.Ingredients {
				if _, ok := known[in.ComponentRecipeID]; in.IsComponent() && !ok && !slices.Contains(missing, in.ComponentRecipeID) {
					missing = append(missing, in.ComponentRecipeID)
				}
			}
		}
		if len(missing) > 0 {
			read, err := r.listRecipesByID(ctx, householdID, missing)
			if err != nil {
				return err
			}
			for _, rc := range read {
				known[rc.ID] = rc
			}
		}
		var next []*Recipe
		for _, rc := range level {
			for _, in := range rc.Ingredients {
				if c, ok := known[in.ComponentRecipeID]; in.IsComponent() && ok {
					in.Component = c
					next = append(next, c)
				}
			}
		}
		level = next
	}
	return nil
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
		RecipeID:           recipeID,
		HouseholdID:        householdID,
		FoodIds:            make([]pgtype.UUID, 0, len(ingredients)),
		ComponentRecipeIds: make([]pgtype.UUID, 0, len(ingredients)),
		Amounts:            make([]float64, 0, len(ingredients)),
		Units:              make([]string, 0, len(ingredients)),
		Notes:              make([]string, 0, len(ingredients)),
		OriginalTexts:      make([]string, 0, len(ingredients)),
		Optionals:          make([]bool, 0, len(ingredients)),
		KitchenUnits:       make([]string, 0, len(ingredients)),
	}
	for _, in := range ingredients {
		p.FoodIds = append(p.FoodIds, in.FoodID)
		p.ComponentRecipeIds = append(p.ComponentRecipeIds, in.ComponentRecipeID)
		p.Amounts = append(p.Amounts, in.Amount)
		unit := ""
		if in.Unit != 0 {
			unit = string(unitToDB(in.Unit))
		}
		p.Units = append(p.Units, unit)
		p.Notes = append(p.Notes, in.Note)
		p.OriginalTexts = append(p.OriginalTexts, in.OriginalText)
		p.Optionals = append(p.Optionals, in.Optional)
		p.KitchenUnits = append(p.KitchenUnits, string(in.KitchenUnit))
	}
	return p
}

func recipeFromDB(row *gen.Recipe, ingredients []*gen.ListRecipeIngredientsRow) *Recipe {
	r := &Recipe{
		ID:            row.ID,
		HouseholdID:   row.HouseholdID,
		Name:          row.Name,
		Description:   row.Description,
		Servings:      row.Servings,
		PrepSeconds:   row.PrepSeconds,
		CookSeconds:   row.CookSeconds,
		Tags:          row.Tags,
		SourceURL:     row.SourceURL,
		FridgeDays:    row.FridgeDays,
		Freezable:     row.Freezable,
		FreezerDays:   row.FreezerDays,
		PrepAheadDays: row.PrepAheadDays,
		PrepAheadNote: row.PrepAheadNote,
		Notes:         row.Notes,
		Favorite:      row.Favorite,
		Draft:         row.Draft,
		CreateTime:    row.CreateTime.Time,
		UpdateTime:    row.UpdateTime.Time,
		Ingredients:   make([]*Ingredient, 0, len(ingredients)),
	}
	if row.YieldAmount != nil && row.YieldUnit != nil {
		r.YieldAmount, r.YieldUnit = *row.YieldAmount, unitFromDB(*row.YieldUnit)
	}
	for _, in := range ingredients {
		r.Ingredients = append(r.Ingredients, ingredientFromDB(in))
	}
	// Instructions are only written encoded by instructionsToDB, so they
	// decode; if they somehow don't, the recipe still reads, without
	// steps, rather than failing every read of it.
	var err error
	if r.Instructions, err = instructionsFromDB(row.Instructions); err != nil {
		r.Instructions = []*Step{}
	}
	return r
}

// kitchenUnitFromDB converts a stored kitchen unit, ignoring one the app
// doesn't know, which a later release may have stored.
func kitchenUnitFromDB(u *string) nutrition.KitchenUnit {
	if u == nil {
		return nutrition.NoKitchenUnit
	}
	if _, size := nutrition.KitchenUnit(*u).Base(); size == 0 {
		return nutrition.NoKitchenUnit
	}
	return nutrition.KitchenUnit(*u)
}

func ingredientFromDB(row *gen.ListRecipeIngredientsRow) *Ingredient {
	in := &row.RecipeIngredient
	// A component in servings, or an unmatched ingredient, has no unit.
	var unit nutrition.Unit
	if in.Unit != nil {
		unit = unitFromDB(*in.Unit)
	}
	return &Ingredient{
		FoodID:                in.FoodID,
		ComponentRecipeID:     in.ComponentRecipeID,
		Amount:                in.Amount,
		Unit:                  unit,
		Note:                  in.Note,
		OriginalText:          in.OriginalText,
		Optional:              in.Optional,
		FoodName:              row.FoodName,
		KitchenUnit:           kitchenUnitFromDB(in.KitchenUnit),
		FoodMeasure:           foods.MeasureFromDB(row.Measure),
		PriceCentsPerKilogram: orZero(row.PriceCentsPerKilogram),
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

// yieldAmountToDB and yieldUnitToDB return a recipe's yield as stored, nil
// for none.
func yieldAmountToDB(r *Recipe) *float64 {
	if r.YieldAmount <= 0 || r.YieldUnit == 0 {
		return nil
	}
	return new(r.YieldAmount)
}

func yieldUnitToDB(r *Recipe) *gen.AmountUnit {
	if r.YieldAmount <= 0 || r.YieldUnit == 0 {
		return nil
	}
	return new(unitToDB(r.YieldUnit))
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

// nonNil returns s, or an empty slice for nil, as array columns and
// parameters aren't null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// orZero returns *v, or 0 for null.
func orZero[T int32 | float64](v *T) T {
	if v == nil {
		return 0
	}
	return *v
}

// FoodMatch is a household's food matching a name.
type FoodMatch struct {
	ID   pgtype.UUID
	Name string
}

// MatchFoodNames returns the household's foods named any of names,
// ignoring case, by the lowercase name they match.
func (r *Repository) MatchFoodNames(ctx context.Context, householdID pgtype.UUID, names []string) (map[string]FoodMatch, error) {
	matches := map[string]FoodMatch{}
	if len(names) == 0 {
		return matches, nil
	}
	rows, err := r.querier.MatchFoodNames(ctx, &gen.MatchFoodNamesParams{HouseholdID: householdID, Names: names})
	if err != nil {
		return nil, fmt.Errorf("failed to match food names: %w", err)
	}
	for _, row := range rows {
		key := strings.ToLower(row.Matched)
		if _, ok := matches[key]; !ok {
			matches[key] = FoodMatch{ID: row.ID, Name: row.Name}
		}
	}
	return matches, nil
}
