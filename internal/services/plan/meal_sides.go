package plan

import (
	"context"
	"math"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/foods"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/rules"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// sideToProto converts a side a meal is served with to the API, nil for
// none.
func sideToProto(side *recipes.Side) *recipesv1.RecipeSide {
	if side == nil {
		return nil
	}
	if side.RecipeID.Valid {
		return recipesv1.RecipeSide_builder{
			RecipeId:           store.UUIDToString(side.RecipeID),
			ServingsPerServing: side.Servings,
			RecipeName:         side.RecipeName,
		}.Build()
	}
	return recipesv1.RecipeSide_builder{
		FoodId:          store.UUIDToString(side.FoodID),
		GramsPerServing: side.Grams,
		FoodName:        side.FoodName,
	}.Build()
}

// sideFromProto returns the side of a recipe's served_with that a request
// chose, by its food or recipe, with the amounts the recipe gives it, nil
// if it chose none.
func sideFromProto(field string, side *recipesv1.RecipeSide, r *recipes.Recipe) (*recipes.Side, error) {
	if side == nil {
		return nil, nil
	}
	for i := range r.ServedWith {
		s := r.ServedWith[i]
		if (side.GetRecipeId() != "" && s.RecipeID.Valid && store.UUIDToString(s.RecipeID) == side.GetRecipeId()) ||
			(side.GetFoodId() != "" && s.FoodID.Valid && store.UUIDToString(s.FoodID) == side.GetFoodId()) {
			return &s, nil
		}
	}
	return nil, connect.Errorf(connect.CodeInvalidArgument,
		"%s must be one of %s's served_with, by its food_id or recipe_id: see GetRecipe, or leave it out to serve it with nothing", field, r.Name)
}

// loadSides reads the recipes and foods of the sides that d doesn't have,
// with the recipes' nutrition, as the plan read only has those it uses.
func (s *Service) loadSides(ctx context.Context, householdID pgtype.UUID, d *planData, sides []*recipes.Side) error {
	var recipeIDs, foodIDs []pgtype.UUID
	for _, side := range sides {
		if side == nil {
			continue
		}
		if _, ok := d.recipes[side.RecipeID]; side.RecipeID.Valid && !ok {
			recipeIDs = append(recipeIDs, side.RecipeID)
		}
		if _, ok := d.foods[side.FoodID]; side.FoodID.Valid && !ok {
			foodIDs = append(foodIDs, side.FoodID)
		}
	}
	if len(recipeIDs) > 0 {
		list, err := s.recipes.ListRecipesByID(ctx, householdID, unique(recipeIDs))
		if err != nil {
			return err
		}
		for _, r := range list {
			d.recipes[r.ID] = r
			d.recipeNutrition[r.ID] = r.Nutrition()
		}
	}
	if len(foodIDs) > 0 {
		list, err := s.foods.ListFoodsByID(ctx, householdID, unique(foodIDs))
		if err != nil {
			return err
		}
		if d.foods == nil {
			d.foods = map[pgtype.UUID]*foods.Food{}
		}
		for _, f := range list {
			d.foods[f.ID] = f
		}
	}
	return nil
}

// mealNutrition returns the nutrition of the recipe served with its side,
// as a meal, the recipe's alone without one.
func (d *planData) mealNutrition(r *recipes.Recipe, side *recipes.Side) *nutrition.Recipe {
	if side == nil {
		return d.recipeNutrition[r.ID]
	}
	return r.WithSide(side, d.recipes[side.RecipeID], d.foods[side.FoodID]).Nutrition()
}

// planSide plans a side with a meal's portions: a cook of the side's
// recipe on the cook's date, for its meal, with a share for each portion,
// or a portion of the side's food for each. Each has the side's amount for
// each serving of the meal.
func planSide(householdID pgtype.UUID, side *recipes.Side, cook *plans.Cook, portions []*plans.Portion) (*plans.CookWithPortions, []*plans.Portion) {
	if side.RecipeID.Valid {
		var total float64
		shares := make([]*plans.Portion, 0, len(portions))
		for _, p := range portions {
			servings := p.Servings * side.Servings
			total += servings
			shares = append(shares, &plans.Portion{
				PersonID:     p.PersonID,
				EatingTimeID: p.EatingTimeID,
				Date:         p.Date,
				Servings:     servings,
			})
		}
		return &plans.CookWithPortions{
			Cook: &plans.Cook{
				HouseholdID: householdID,
				RecipeID:    side.RecipeID,
				Date:        cook.Date,
				// Recipes scale by quarters of a serving.
				Servings: math.Max(0.25, math.Ceil(total*4)/4),
				Meal:     cook.Meal,
			},
			Portions: shares,
		}, nil
	}
	foodPortions := make([]*plans.Portion, 0, len(portions))
	for _, p := range portions {
		foodPortions = append(foodPortions, &plans.Portion{
			PersonID:     p.PersonID,
			EatingTimeID: p.EatingTimeID,
			Date:         p.Date,
			FoodID:       side.FoodID,
			Amount:       p.Servings * side.Grams,
			Unit:         nutrition.Grams,
		})
	}
	return nil, foodPortions
}

// addPlannedSide adds a side eaten with servings of a meal at a slot to
// the nutrition planned at each slot, like addPlanned.
func (d *planData) addPlannedSide(planned map[plans.Slot]*rules.EatingTime, slot plans.Slot, side *recipes.Side, servings float64) {
	if side.RecipeID.Valid {
		d.addPlanned(planned, slot, side.RecipeID, servings*side.Servings)
		return
	}
	f, ok := d.foods[side.FoodID]
	if !ok {
		return
	}
	if planned[slot] == nil {
		planned[slot] = &rules.EatingTime{}
	}
	a := f.Nutrition().AmountOf(servings*side.Grams, nutrition.Grams)
	planned[slot].Amount.Add(&a)
}
