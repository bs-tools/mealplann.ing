package services_test

import (
	"context"
	"testing"

	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
)

// TestSuggestSides checks that a meal's sides are scored for a person's
// day: quinoa, with more protein, comes before rice for a day short of
// protein, and a side over the eating time's carbohydrate maximum comes
// last, saying so.
func TestSuggestSides(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	erin := createPerson(t, e, peoplev1.Person_builder{
		Name:        "Erin",
		EatingTimes: []*peoplev1.EatingTime{eatingTime("Dinner", mainMeal, "19:00", dinner)},
		Rules:       peoplev1.EatingRules_builder{MaxCarbohydrateGrams: new(60.0)}.Build(),
		DailyTargets: []*peoplev1.NutrientTarget{
			peoplev1.NutrientTarget_builder{Nutrient: foodsv1.Nutrient_NUTRIENT_PROTEIN_GRAMS, MinPerDay: new(40.0)}.Build(),
		},
	}.Build())
	dinnerID := eatingTimeID(t, erin, "Dinner")
	tofu := customFood(t, e, "Tofu, firm", 144, 15, 3, 0.6, 9, nil)
	quinoa := customFood(t, e, "Quinoa, cooked", 120, 4.4, 21, 0.9, 1.9, nil)
	rice := customFood(t, e, "White rice, cooked", 130, 2.7, 28, 0.1, 0.3, nil)
	noodles := customFood(t, e, "Noodles, cooked", 140, 5, 80, 0.5, 2, nil)
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Tofu stir-fry", Servings: 1,
		Ingredients: []*recipesv1.Ingredient{ingredient(tofu.GetId(), 150, grams)},
		ServedWith: []*recipesv1.RecipeSide{
			recipesv1.RecipeSide_builder{FoodId: rice.GetId(), GramsPerServing: 150}.Build(),
			recipesv1.RecipeSide_builder{FoodId: quinoa.GetId(), GramsPerServing: 150}.Build(),
			recipesv1.RecipeSide_builder{FoodId: noodles.GetId(), GramsPerServing: 100}.Build(),
		},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	stirFry := created.GetRecipe()
	if sides := stirFry.GetServedWith(); len(sides) != 3 || sides[0].GetFoodName() != "White rice, cooked" || sides[1].GetGramsPerServing() != 150 {
		t.Errorf("got sides %v, want rice, quinoa and noodles", sides)
	}
	// A serving of the stir-fry, which PlanMeal would size toward the
	// protein target.
	if _, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{
		Portion: plansv1.Portion_builder{
			PersonId: erin.GetId(), EatingTimeId: dinnerID, Date: monday,
			Cook: plansv1.CookShare_builder{Servings: 1}.Build(),
		}.Build(),
		NewCook: plansv1.NewCook_builder{RecipeId: stirFry.GetId()}.Build(),
	}.Build()); err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}

	suggest := func() *suggestionsv1.SuggestSidesResponse {
		t.Helper()
		resp, err := e.suggestions.SuggestSides(ctx, suggestionsv1.SuggestSidesRequest_builder{
			PersonId: erin.GetId(), Date: monday, EatingTimeId: dinnerID,
		}.Build())
		if err != nil {
			t.Fatalf("SuggestSides: %v", err)
		}
		return resp
	}
	resp := suggest()
	var names, reasons []string
	for _, side := range resp.GetSides() {
		names = append(names, side.GetFoodName())
		reasons = append(reasons, side.GetReason())
	}
	// The stir-fry has 22.5 g of protein, 17.5 g short of 40 g: quinoa adds
	// 6.6 g, rice 4 g, and noodles' 80 g of carbohydrate is over 60 g.
	if resp.GetRecipeName() != "Tofu stir-fry" || len(names) != 3 ||
		names[0] != "Quinoa, cooked" || names[1] != "White rice, cooked" || names[2] != "Noodles, cooked" {
		t.Fatalf("got sides %v, want quinoa, rice, then noodles", names)
	}
	if reasons[0] != "adds 7 g protein: today is 18 g short" || len(resp.GetSides()[2].GetWarnings()) == 0 {
		t.Errorf("got reasons %q, want quinoa's protein and the noodles' warning", reasons)
	}
	if side := resp.GetSides()[0]; side.GetGrams() != 150 || side.GetPlanned() {
		t.Errorf("got %v, want 150 g of quinoa, not planned", side)
	}

	// Once quinoa is planned, it says so, and is still compared with the
	// meal alone.
	portion, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{Portion: plansv1.Portion_builder{
		PersonId: erin.GetId(), EatingTimeId: dinnerID, Date: monday,
		Food: plansv1.FoodAmount_builder{FoodId: quinoa.GetId(), Amount: 150, Unit: grams}.Build(),
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}
	resp = suggest()
	if side := resp.GetSides()[0]; !side.GetPlanned() || side.GetPortionId() != portion.GetPortion().GetId() || side.GetReason() != reasons[0] {
		t.Errorf("got %v, want quinoa planned, with the same reason", side)
	}
}
