package services_test

import (
	"context"
	"testing"
	"time"

	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
)

// TestSuggestFromPantry checks that only recipes the pantry has everything
// for are suggested, those using food to use soon first.
func TestSuggestFromPantry(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	tofu := customFood(t, e, "Tofu, firm", 144, 15, 3, 0.6, 9, nil)
	spinach := customFood(t, e, "Spinach", 23, 3, 4, 2, 0.4, nil)
	lentils := customFood(t, e, "Lentils", 120, 9, 20, 1, 0.5, nil)
	recipe := func(name string, ings ...*recipesv1.Ingredient) {
		t.Helper()
		// The scramble is a dinner; the others aren't tagged for a meal.
		var tags []string
		if name == "Tofu scramble" {
			tags = []string{"dinner"}
		}
		if _, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
			Name: name, Servings: 2, Ingredients: ings, Tags: tags,
		}.Build()}.Build()); err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
	}
	recipe("Tofu scramble", ingredient(tofu.GetId(), 300, grams), ingredient(spinach.GetId(), 100, grams))
	recipe("Plain tofu", ingredient(tofu.GetId(), 200, grams))
	recipe("Dal", ingredient(lentils.GetId(), 500, grams))

	date := time.Now().UTC().Format(time.DateOnly)
	soon := time.Now().UTC().AddDate(0, 0, 1).Format(time.DateOnly)
	for _, lot := range []*pantryv1.AddToPantryRequest{
		pantryv1.AddToPantryRequest_builder{FoodId: tofu.GetId(), Amount: 400, Unit: grams}.Build(),
		pantryv1.AddToPantryRequest_builder{FoodId: spinach.GetId(), Amount: 150, Unit: grams, ExpiryDate: soon}.Build(),
		// Not enough lentils for the dal.
		pantryv1.AddToPantryRequest_builder{FoodId: lentils.GetId(), Amount: 200, Unit: grams}.Build(),
	} {
		if _, err := e.pantry.AddToPantry(ctx, lot); err != nil {
			t.Fatalf("AddToPantry: %v", err)
		}
	}

	resp, err := e.suggestions.SuggestFromPantry(ctx, suggestionsv1.SuggestFromPantryRequest_builder{Date: date}.Build())
	if err != nil {
		t.Fatalf("SuggestFromPantry: %v", err)
	}
	var names []string
	for _, r := range resp.GetRecipes() {
		names = append(names, r.GetRecipeName())
	}
	if len(names) != 2 || names[0] != "Tofu scramble" || names[1] != "Plain tofu" {
		t.Fatalf("got %q, want the scramble, using the spinach, then the plain tofu, and not the dal", names)
	}
	if got := resp.GetRecipes()[0].GetUseSoonFoodNames(); len(got) != 1 || got[0] != "Spinach" {
		t.Errorf("got %q to use soon, want the spinach", got)
	}

	// Only meals: the plain tofu isn't tagged lunch or dinner.
	resp, err = e.suggestions.SuggestFromPantry(ctx, suggestionsv1.SuggestFromPantryRequest_builder{
		Date:  date,
		Meals: []peoplev1.HouseholdMeal{peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_LUNCH, peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_DINNER},
	}.Build())
	if err != nil {
		t.Fatalf("SuggestFromPantry: %v", err)
	}
	if got := resp.GetRecipes(); len(got) != 1 || got[0].GetRecipeName() != "Tofu scramble" {
		t.Errorf("got %v for lunch or dinner, want only the scramble", got)
	}
}
