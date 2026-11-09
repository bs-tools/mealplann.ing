package services_test

import (
	"context"
	"math"
	"testing"

	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
)

// TestKitchenUnits checks that a recipe's ingredients keep the kitchen unit
// they're entered in, like a tablespoon of oil, while nutrition and the
// pantry use it converted.
func TestKitchenUnits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	const (
		tablespoons = foodsv1.AmountUnit_AMOUNT_UNIT_TABLESPOONS
		pounds      = foodsv1.AmountUnit_AMOUNT_UNIT_POUNDS
	)
	// 100 kcal in 100 g of each, so kcal are grams.
	oil := customFood(t, e, "Olive oil", 100, 0, 0, 0, 0, func(f *foodsv1.Food) { f.SetGramsPerMilliliter(0.92) })
	potatoes := customFood(t, e, "Potatoes", 100, 0, 0, 0, 0, nil)
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Roast potatoes", Servings: 2,
		Ingredients: []*recipesv1.Ingredient{ingredient(oil.GetId(), 2, tablespoons), ingredient(potatoes.GetId(), 1, pounds)},
		Instructions: []*recipesv1.Step{
			recipesv1.Step_builder{Text: "Toss the potatoes in oil", IngredientIndexes: []int32{0, 1}, IngredientAmounts: []float64{1.5, 0}}.Build(),
			recipesv1.Step_builder{Text: "Drizzle the rest", IngredientIndexes: []int32{0}}.Build(),
		},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	r := created.GetRecipe()
	// The ingredients and steps read as entered.
	if in := r.GetIngredients()[0]; in.GetAmount() != 2 || in.GetUnit() != tablespoons {
		t.Errorf("got oil %v %v, want 2 tablespoons", in.GetAmount(), in.GetUnit())
	}
	if in := r.GetIngredients()[1]; in.GetAmount() != 1 || in.GetUnit() != pounds {
		t.Errorf("got potatoes %v %v, want 1 pound", in.GetAmount(), in.GetUnit())
	}
	if got := r.GetInstructions()[0].GetIngredientAmounts(); len(got) != 2 || math.Abs(got[0]-1.5) > 1e-9 {
		t.Errorf("got step amounts %v, want 1.5 tablespoons of oil", got)
	}
	// 30 ml of oil weighs 27.6 g, and a pound 453.6 g.
	if got, want := r.GetNutrition().GetTotalGrams(), 27.6+453.59237; math.Abs(got-want) > 0.01 {
		t.Errorf("got %v g, want %v", got, want)
	}

	// Elsewhere, kitchen units are converted: a pound of potatoes in the
	// pantry is 453.6 g.
	resp, err := e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: potatoes.GetId(), Amount: 1, Unit: pounds}.Build())
	if err != nil {
		t.Fatalf("AddToPantry: %v", err)
	}
	if got := resp.GetItem().GetGrams(); math.Abs(got-453.59) > 0.01 {
		t.Errorf("got %v g in the pantry, want 453.59", got)
	}
}
