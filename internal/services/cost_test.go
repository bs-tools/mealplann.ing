package services_test

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestCosts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	b := createPerson(t, e, ben())
	price := func(cents int32) func(*foodsv1.Food) {
		return func(f *foodsv1.Food) { f.SetPriceCentsPerKilogram(cents); f.SetPriceDate("2026-11-01") }
	}
	// Lentils at $4/kg, rice at $3/kg and oats at $5/kg; nobody knows what
	// the onions cost.
	lentils := customFood(t, e, "Red lentils", 350, 25, 60, 2, 1, price(400))
	rice := customFood(t, e, "Rice", 360, 8, 76, 1, 3, price(300))
	oats := customFood(t, e, "Oats", 380, 13, 60, 1, 7, price(500))
	onions := customFood(t, e, "Onions", 40, 1, 9, 4, 0.1, func(f *foodsv1.Food) { f.SetGramsPerPiece(150) })
	if lentils.GetPriceCentsPerKilogram() != 400 || lentils.GetPriceDate() != "2026-11-01" {
		t.Errorf("got %v, want the lentils' price", lentils)
	}

	// The dal's 250 g of lentils cost $1 and its 300 g of rice 90¢: $1.90
	// for 4 servings, 48¢ each, without the onion.
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Dal", Servings: 4,
		Ingredients: []*recipesv1.Ingredient{
			ingredient(lentils.GetId(), 250, grams), ingredient(rice.GetId(), 300, grams), ingredient(onions.GetId(), 1, pieces),
		},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	dal := created.GetRecipe()
	if c := dal.GetCost(); c.GetTotalCents() != 190 || c.GetCentsPerServing() != 48 || !slices.Equal(c.GetUnpricedFoodNames(), []string{"Onions"}) {
		t.Errorf("got cost %v, want $1.90, 48¢ a serving, without the onions", c)
	}
	list, err := e.recipes.ListRecipes(ctx, recipesv1.ListRecipesRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListRecipes: %v", err)
	}
	if got := list.GetRecipes()[0]; !got.HasCostCentsPerServing() || got.GetCostCentsPerServing() != 48 {
		t.Errorf("got summary %v, want 48¢ a serving", got)
	}

	// Ben has the dal for Monday's dinner, and 50 g of oats every
	// breakfast: 25¢ a day.
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: dal.GetId(), Date: monday, Meal: dinner, Servings: 4}.Build()); err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	if _, err := e.plans.CreateStaple(ctx, plansv1.CreateStapleRequest_builder{Staple: plansv1.Staple_builder{
		PersonId: b.GetId(), EatingTimeId: eatingTimeID(t, b, "Breakfast"),
		Food: plansv1.FoodAmount_builder{FoodId: oats.GetId(), Amount: 50, Unit: grams}.Build(),
	}.Build()}.Build()); err != nil {
		t.Fatalf("CreateStaple: %v", err)
	}
	cost := func() *plansv1.GetPlanCostResponse {
		t.Helper()
		resp, err := e.plans.GetPlanCost(ctx, plansv1.GetPlanCostRequest_builder{StartDate: monday, EndDate: wednesday}.Build())
		if err != nil {
			t.Fatalf("GetPlanCost: %v", err)
		}
		return resp
	}
	// Three days: the dal's $1.90 and 75¢ of oats.
	got := cost()
	if got.GetTotalCents() != 265 || got.GetToBuyCents() != 265 || got.GetFoodsCents() != 75 ||
		!slices.Equal(got.GetUnpricedFoodNames(), []string{"Onions"}) {
		t.Errorf("got %v, want $2.65 in all, all to buy, 75¢ of it oats, without the onions", got)
	}
	if c := got.GetCooks(); len(c) != 1 || c[0].GetRecipeName() != "Dal" || c[0].GetTotalCents() != 190 || c[0].GetCentsPerServing() != 48 || c[0].GetDate() != monday {
		t.Errorf("got cooks %v, want Monday's dal at $1.90", c)
	}

	// With 200 g of lentils in the pantry, 80¢ of them is owned already.
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: lentils.GetId(), Amount: 200, Unit: grams}.Build())
	got = cost()
	if got.GetTotalCents() != 265 || got.GetToBuyCents() != 185 {
		t.Errorf("got %d¢ in all and %d¢ to buy, want 265¢ and 185¢", got.GetTotalCents(), got.GetToBuyCents())
	}
	// Once cooked, the dal is eaten, but bought already.
	if _, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: got.GetCooks()[0].GetCookId(), Done: true}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}
	got = cost()
	if got.GetTotalCents() != 265 || got.GetToBuyCents() != 75 {
		t.Errorf("got %d¢ in all and %d¢ to buy, want 265¢ and the oats' 75¢", got.GetTotalCents(), got.GetToBuyCents())
	}

	// Ranges are checked, and prices too.
	_, err = e.plans.GetPlanCost(ctx, plansv1.GetPlanCostRequest_builder{StartDate: wednesday, EndDate: monday}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.plans.GetPlanCost(ctx, plansv1.GetPlanCostRequest_builder{StartDate: monday, EndDate: "2026-12-31"}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.foods.UpdateFood(ctx, foodsv1.UpdateFoodRequest_builder{
		Food:       foodsv1.Food_builder{Id: oats.GetId(), PriceCentsPerKilogram: -1}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"price_cents_per_kilogram"}},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
}
