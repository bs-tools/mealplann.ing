package plan

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	"github.com/bs-tools/mealplann.ing/internal/foods"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/shopping"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// useSoonDays is how many days ahead food counts as to be used soon, like
// the pantry page's.
const useSoonDays = 3

func (s *Service) SuggestFromPantry(ctx context.Context, req *suggestionsv1.SuggestFromPantryRequest) (*suggestionsv1.SuggestFromPantryResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	date, err := parseDate("date", req.GetDate())
	if err != nil {
		return nil, err
	}
	householdID := user.HouseholdID()
	var recipeList []*recipes.Recipe
	var lots []*pantry.Item
	var recipesErr, lotsErr error
	var wg sync.WaitGroup
	wg.Go(func() { recipeList, _, recipesErr = s.recipes.ListRecipes(ctx, householdID, recipes.ListFilter{}) })
	wg.Go(func() { lots, lotsErr = s.pantry.ListItems(ctx, householdID) })
	wg.Wait()
	if err := cmp.Or(recipesErr, lotsErr); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read recipes and pantry", "")
	}
	var foodIDs []pgtype.UUID
	for _, r := range recipeList {
		for _, in := range r.IngredientsWithout(float64(r.Servings), nil) {
			foodIDs = append(foodIDs, in.FoodID)
		}
	}
	foodList, err := s.foods.ListFoodsByID(ctx, householdID, unique(foodIDs))
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read foods", "")
	}
	byID := make(map[pgtype.UUID]*foods.Food, len(foodList))
	for _, f := range foodList {
		byID[f.ID] = f
	}

	type found struct {
		recipe  *recipes.Recipe
		useSoon []string
	}
	var cookable []found
	soon := date.AddDate(0, 0, useSoonDays)
	for _, r := range recipeList {
		if !r.Suggestable(date) {
			continue
		}
		ingredients := r.IngredientsWithout(float64(r.Servings), nil)
		if len(ingredients) == 0 {
			continue
		}
		needs := make([]shopping.Need, 0, len(ingredients))
		for _, in := range ingredients {
			needs = append(needs, shopping.Need{FoodID: in.FoodID, Date: date, Amount: in.Amount, Unit: in.Unit})
		}
		// What the shopping list would buy for the batch alone, cooked on
		// the date: nothing, if the pantry has it all.
		items := shopping.Generate(&shopping.Input{ShopDate: date, Needs: needs, Foods: byID, Lots: lots})
		if len(items) > 0 || !allKnown(needs, byID) {
			continue
		}
		var useSoon []string
		for _, n := range needs {
			f := byID[n.FoodID]
			if slices.Contains(useSoon, f.Name) {
				continue
			}
			if slices.ContainsFunc(lots, func(lot *pantry.Item) bool {
				useBy := lot.UseBy()
				return lot.FoodID == n.FoodID && !useBy.IsZero() && !useBy.Before(date) && !useBy.After(soon)
			}) {
				useSoon = append(useSoon, f.Name)
			}
		}
		cookable = append(cookable, found{recipe: r, useSoon: useSoon})
	}
	slices.SortStableFunc(cookable, func(a, b found) int {
		return cmp.Or(
			cmp.Compare(len(b.useSoon), len(a.useSoon)),
			strings.Compare(strings.ToLower(a.recipe.Name), strings.ToLower(b.recipe.Name)),
		)
	})
	out := make([]*suggestionsv1.PantryRecipe, 0, len(cookable))
	for _, c := range cookable {
		out = append(out, suggestionsv1.PantryRecipe_builder{
			RecipeId:         store.UUIDToString(c.recipe.ID),
			RecipeName:       c.recipe.Name,
			Servings:         c.recipe.Servings,
			TotalSeconds:     c.recipe.PrepSeconds + c.recipe.CookSeconds,
			UseSoonFoodNames: c.useSoon,
		}.Build())
	}
	return suggestionsv1.SuggestFromPantryResponse_builder{Recipes: out}.Build(), nil
}

// allKnown reports whether every need's food was read, as Generate leaves
// out needs of foods it doesn't know, which would count as in stock.
func allKnown(needs []shopping.Need, byID map[pgtype.UUID]*foods.Food) bool {
	return !slices.ContainsFunc(needs, func(n shopping.Need) bool { return byID[n.FoodID] == nil })
}
