package services_test

import (
	"context"
	"slices"
	"testing"
	"time"

	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
)

// TestMissingFoods checks that a cook to come says which of its foods the
// pantry is short of, to shop for before cooking it, and none once the
// pantry has them.
func TestMissingFoods(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	createPerson(t, e, ben())
	tofu := customFood(t, e, "Tofu", 144, 15, 3, 0.6, 9, nil)
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Tofu bowls", Servings: 2, Ingredients: []*recipesv1.Ingredient{ingredient(tofu.GetId(), 400, grams)},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	// Today, in this week, as only the cooks to come are checked.
	today := time.Now().UTC()
	week := today.AddDate(0, 0, -(int(today.Weekday())+6)%7).Format(time.DateOnly)
	if _, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook: plansv1.Cook_builder{RecipeId: created.GetRecipe().GetId(), Date: today.Format(time.DateOnly), Servings: 2}.Build(),
	}.Build()); err != nil {
		t.Fatalf("CreateCook: %v", err)
	}
	missing := func() []string {
		t.Helper()
		resp, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: week}.Build())
		if err != nil {
			t.Fatalf("GetWeek: %v", err)
		}
		return resp.GetCooks()[0].GetMissingFoodNames()
	}
	if got := missing(); !slices.Equal(got, []string{"Tofu"}) {
		t.Errorf("got missing %v, want the tofu", got)
	}
	if _, err := e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: tofu.GetId(), Amount: 400, Unit: grams}.Build()); err != nil {
		t.Fatalf("AddToPantry: %v", err)
	}
	if got := missing(); len(got) != 0 {
		t.Errorf("got missing %v, want none with the tofu in the pantry", got)
	}
}
