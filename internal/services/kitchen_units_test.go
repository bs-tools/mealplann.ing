package services_test

import (
	"context"
	"math"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
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

// TestPackages checks that an amount in packages, like a block of tofu,
// weighs the food's package_grams, in recipes, the pantry and cooking.
func TestPackages(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	const packages = foodsv1.AmountUnit_AMOUNT_UNIT_PACKAGES
	createPerson(t, e, ben())
	// 100 kcal in 100 g, so kcal are grams.
	tofu := customFood(t, e, "Tofu, firm", 100, 0, 0, 0, 0, func(f *foodsv1.Food) { f.SetPackageGrams(454) })
	loose := customFood(t, e, "Tofu, loose", 100, 0, 0, 0, 0, nil)
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Tofu scramble", Servings: 2,
		Ingredients: []*recipesv1.Ingredient{ingredient(tofu.GetId(), 1, packages)},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	r := created.GetRecipe()
	if in := r.GetIngredients()[0]; in.GetAmount() != 1 || in.GetUnit() != packages {
		t.Errorf("got %v %v, want 1 package", in.GetAmount(), in.GetUnit())
	}
	if got := r.GetNutrition().GetTotalGrams(); math.Abs(got-454) > 0.01 {
		t.Errorf("got %v g, want a 454 g block", got)
	}

	// Two blocks in the pantry are 908 g; a food without a package size
	// can't be added in packages.
	if _, err := e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: tofu.GetId(), Amount: 2, Unit: packages}.Build()); err != nil {
		t.Fatalf("AddToPantry: %v", err)
	}
	_, err = e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: loose.GetId(), Amount: 1, Unit: packages}.Build())
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("got %v adding packages of a food without a package size, want FAILED_PRECONDITION", err)
	}

	// Cooking the recipe takes a block out.
	planned, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: r.GetId(), Date: monday, Meal: dinner, Servings: 2}.Build())
	if err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	if _, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: planned.GetCook().GetId(), Done: true}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}
	stock, err := e.pantry.ListPantry(ctx, pantryv1.ListPantryRequest_builder{FoodIds: []string{tofu.GetId()}}.Build())
	if err != nil {
		t.Fatalf("ListPantry: %v", err)
	}
	if items := stock.GetItems(); len(items) != 1 || math.Abs(items[0].GetGrams()-454) > 0.01 {
		t.Errorf("got %v left, want one 454 g block", items)
	}
}
