package services_test

import (
	"context"
	"math"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// TestComponentYield checks that a component can be given by weight or
// volume of what its recipe makes, converted to servings with its yield.
func TestComponentYield(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	create := func(r *recipesv1.Recipe) (*recipesv1.Recipe, error) {
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: r}.Build())
		return resp.GetRecipe(), err
	}
	// 500 g of dry pinto beans make 1250 g cooked, in 5 servings.
	dry := customFood(t, e, "Pinto beans, dry", 350, 20, 60, 0, 1, nil)
	beans, err := create(recipesv1.Recipe_builder{
		Name: "Pressure cooked pinto beans", Servings: 5,
		Ingredients: []*recipesv1.Ingredient{ingredient(dry.GetId(), 500, grams)},
		YieldAmount: 1.25, YieldUnit: foodsv1.AmountUnit_AMOUNT_UNIT_KILOGRAMS,
	}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	if beans.GetYieldAmount() != 1250 || beans.GetYieldUnit() != grams {
		t.Errorf("got yield %v %v, want 1250 g", beans.GetYieldAmount(), beans.GetYieldUnit())
	}
	// 400 g cooked is 1.6 servings, 160 g of the dry beans: 560 kcal.
	chili, err := create(recipesv1.Recipe_builder{
		Name: "Chili", Servings: 2,
		Ingredients: []*recipesv1.Ingredient{recipesv1.Ingredient_builder{RecipeId: beans.GetId(), Amount: 400, Unit: grams}.Build()},
	}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	in := chili.GetIngredients()[0]
	if in.GetAmount() != 400 || in.GetUnit() != grams || in.GetComponentServings() != 1.6 {
		t.Errorf("got %v %v, %v servings, want 400 g, 1.6 servings", in.GetAmount(), in.GetUnit(), in.GetComponentServings())
	}
	if got := chili.GetNutrition().GetPerServing().GetEnergyKcal(); math.Abs(got-280) > 0.01 {
		t.Errorf("got %v kcal a serving, want 280", got)
	}
	// Without a yield in milliliters, an amount in them can't be converted.
	_, err = create(recipesv1.Recipe_builder{
		Name: "Bean soup", Servings: 2,
		Ingredients: []*recipesv1.Ingredient{recipesv1.Ingredient_builder{RecipeId: beans.GetId(), Amount: 400, Unit: milliliters}.Build()},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	// Without a yield at all, grams count the ingredients' weight, raw.
	if _, err := e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
		Recipe:     recipesv1.Recipe_builder{Id: beans.GetId()}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"yield_amount", "yield_unit"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateRecipe: %v", err)
	}
	got, err := e.recipes.GetRecipe(ctx, recipesv1.GetRecipeRequest_builder{Id: chili.GetId()}.Build())
	if err != nil {
		t.Fatalf("GetRecipe: %v", err)
	}
	if s := got.GetRecipe().GetIngredients()[0].GetComponentServings(); s != 4 {
		t.Errorf("got %v servings without a yield, want 400 of 500 g raw in 5 servings, 4", s)
	}
}

// TestDraftRecipes checks that a draft can have unmatched ingredients, and
// isn't planned until it's finished.
func TestDraftRecipes(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	oats := customFood(t, e, "Oats", 380, 13, 60, 1, 7, nil)
	unmatched := recipesv1.Ingredient_builder{OriginalText: "1 cup almond milk", Amount: 1, Unit: foodsv1.AmountUnit_AMOUNT_UNIT_CUPS}.Build()
	draft := recipesv1.Recipe_builder{
		Name: "Overnight oats", Servings: 1, Draft: true,
		Ingredients: []*recipesv1.Ingredient{ingredient(oats.GetId(), 50, grams), unmatched},
		Instructions: []*recipesv1.Step{
			recipesv1.Step_builder{Text: "Stir together", IngredientIndexes: []int32{0, 1}}.Build(),
		},
	}.Build()
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: draft}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	r := created.GetRecipe()
	if !r.GetDraft() || r.GetIngredients()[1].GetFoodId() != "" || r.GetIngredients()[1].GetOriginalText() != "1 cup almond milk" ||
		r.GetIngredients()[1].GetUnit() != foodsv1.AmountUnit_AMOUNT_UNIT_CUPS {
		t.Errorf("got %v, want a draft with the almond milk unmatched", r)
	}
	// The unmatched ingredient is an issue, and left out of the nutrition.
	issues := r.GetNutrition().GetIssues()
	if len(issues) != 1 || !issues[0].GetUnmatched() || issues[0].GetIngredientIndex() != 1 || issues[0].GetUnknownWeight() {
		t.Errorf("got issues %v, want the almond milk unmatched", issues)
	}
	if got := r.GetNutrition().GetTotalGrams(); got != 50 {
		t.Errorf("got %v g, want the oats' 50", got)
	}
	// It isn't planned.
	_, err = e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook: plansv1.Cook_builder{RecipeId: r.GetId(), Date: monday, Servings: 1}.Build(),
	}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
	// A recipe that isn't a draft needs its ingredients matched.
	_, err = e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
		Recipe:     recipesv1.Recipe_builder{Id: r.GetId(), Draft: false}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"draft"}},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	milk := customFood(t, e, "Almond milk", 15, 0.5, 0.3, 0, 1, func(f *foodsv1.Food) { f.SetGramsPerMilliliter(1) })
	r.GetIngredients()[1].SetFoodId(milk.GetId())
	r.SetDraft(false)
	updated, err := e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
		Recipe:     r,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"draft", "ingredients"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdateRecipe: %v", err)
	}
	if updated.GetRecipe().GetDraft() || len(updated.GetRecipe().GetNutrition().GetIssues()) != 0 {
		t.Errorf("got %v, want it finished without issues", updated.GetRecipe())
	}
	if _, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook: plansv1.Cook_builder{RecipeId: r.GetId(), Date: monday, Servings: 1}.Build(),
	}.Build()); err != nil {
		t.Errorf("CreateCook: %v", err)
	}
}
