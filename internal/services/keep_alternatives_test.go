package services_test

import (
	"context"
	"testing"

	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// TestUpdateKeepsAlternatives checks that replacing a recipe's ingredients
// without their alternatives, as a client that doesn't send them does,
// keeps them, with their amounts, and that the update mask path
// "ingredients.alternatives" changes or removes them.
func TestUpdateKeepsAlternatives(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	pinto := customFood(t, e, "Dried pinto beans", 347, 21, 63, 2, 1, nil)
	canned := customFood(t, e, "Canned pinto beans", 110, 7, 15, 1, 6, nil)
	onion := customFood(t, e, "Onion", 40, 1, 9, 0, 2, nil)
	beans, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Pinto beans", Servings: 3,
		Ingredients: []*recipesv1.Ingredient{ingredient(pinto.GetId(), 200, grams)},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	component := recipesv1.Ingredient_builder{
		RecipeId: beans.GetRecipe().GetId(), Amount: 250, Unit: grams,
		Alternatives: []*recipesv1.IngredientAlternative{
			recipesv1.IngredientAlternative_builder{FoodId: canned.GetId(), Amount: 400, Unit: grams}.Build(),
		},
	}.Build()
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Chili", Servings: 4, Ingredients: []*recipesv1.Ingredient{component, ingredient(onion.GetId(), 100, grams)},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	chili := created.GetRecipe()
	update := func(ingredients []*recipesv1.Ingredient, paths ...string) *recipesv1.Recipe {
		t.Helper()
		updated, err := e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
			Recipe:     recipesv1.Recipe_builder{Id: chili.GetId(), Ingredients: ingredients}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: paths},
		}.Build())
		if err != nil {
			t.Fatalf("UpdateRecipe %v: %v", paths, err)
		}
		return updated.GetRecipe()
	}
	bare := func(amount float64) []*recipesv1.Ingredient {
		return []*recipesv1.Ingredient{
			ingredient(onion.GetId(), 150, grams),
			recipesv1.Ingredient_builder{RecipeId: beans.GetRecipe().GetId(), Amount: amount, Unit: grams}.Build(),
		}
	}

	// Changing the amount, and the order, without the alternatives keeps
	// them on the component.
	got := update(bare(300), "ingredients")
	alternatives := got.GetIngredients()[1].GetAlternatives()
	if got.GetIngredients()[1].GetAmount() != 300 || len(alternatives) != 1 ||
		alternatives[0].GetFoodId() != canned.GetId() || alternatives[0].GetAmount() != 400 {
		t.Fatalf("got ingredients %v, want 300 g of beans keeping the canned ones at 400 g", got.GetIngredients())
	}
	if len(got.GetIngredients()[0].GetAlternatives()) != 0 {
		t.Errorf("got alternatives %v on the onion, want none", got.GetIngredients()[0].GetAlternatives())
	}
	// With "ingredients.alternatives", they're as given: none.
	got = update(bare(300), "ingredients", "ingredients.alternatives")
	if alternatives := got.GetIngredients()[1].GetAlternatives(); len(alternatives) != 0 {
		t.Errorf("got alternatives %v, want them removed", alternatives)
	}
}
