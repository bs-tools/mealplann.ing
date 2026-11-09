package services_test

import (
	"context"
	"testing"

	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
)

// TestListSummaries checks the pantry's name filter, and the summaries of
// the pantry, foods and recipes, which keep large lists small.
func TestListSummaries(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	tofu := customFood(t, e, "Tofu, extra-firm", 144, 15, 3, 0.6, 9, func(f *foodsv1.Food) { f.SetNotes("The blue pack") })
	rice := customFood(t, e, "Rice", 360, 8, 76, 1, 3, nil)
	for _, f := range []*foodsv1.Food{tofu, rice} {
		addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: f.GetId(), Amount: 400, Unit: grams, Notes: "top shelf"}.Build())
	}

	listed, err := e.pantry.ListPantry(ctx, pantryv1.ListPantryRequest_builder{Query: "TOFU", Summary: true}.Build())
	if err != nil {
		t.Fatalf("ListPantry: %v", err)
	}
	if items := listed.GetItems(); len(items) != 1 || items[0].GetFoodName() != "Tofu, extra-firm" || items[0].GetGrams() != 400 ||
		items[0].GetNotes() != "" || items[0].HasCreateTime() || items[0].GetStoreSection() != foodsv1.StoreSection_STORE_SECTION_UNSPECIFIED {
		t.Errorf("got %v, want the tofu's lot alone, summarized", items)
	}

	foods, err := e.foods.ListFoods(ctx, foodsv1.ListFoodsRequest_builder{Query: "tofu", Summary: true}.Build())
	if err != nil {
		t.Fatalf("ListFoods: %v", err)
	}
	if f := foods.GetFoods(); len(f) != 1 || f[0].GetName() != "Tofu, extra-firm" || f[0].HasNutrients() || f[0].GetNotes() != "" || f[0].HasCreateTime() {
		t.Errorf("got %v, want the tofu without its nutrients, notes or times", f)
	}

	if _, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Tofu bowl", Description: "A long description of a bowl.", Servings: 2, PrepSeconds: 600,
		Ingredients: []*recipesv1.Ingredient{ingredient(tofu.GetId(), 200, grams)},
	}.Build()}.Build()); err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	recipes, err := e.recipes.ListRecipes(ctx, recipesv1.ListRecipesRequest_builder{Query: "tofu", Summary: true}.Build())
	if err != nil {
		t.Fatalf("ListRecipes: %v", err)
	}
	if r := recipes.GetRecipes(); len(r) != 1 || r[0].GetName() != "Tofu bowl" || r[0].GetDescription() != "" || r[0].GetPrepSeconds() != 0 ||
		!r[0].HasEnergyKcalPerServing() || r[0].HasUpdateTime() {
		t.Errorf("got %v, want the bowl with its nutrients, without its description or times", r)
	}
}
