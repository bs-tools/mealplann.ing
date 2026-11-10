package services_test

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
)

// TestSuggestFixes checks that a day over its energy maximum gets changes
// that bring it under, like replacing the wraps with a side salad, and an
// eating time short of protein gets a protein food from the pantry added.
func TestSuggestFixes(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	dana := createPerson(t, e, peoplev1.Person_builder{
		Name: "Dana",
		EatingTimes: []*peoplev1.EatingTime{
			eatingTime("Lunch", mainMeal, "12:30", lunch),
			eatingTime("Snack", smallMeal, "15:30", noMeal),
		},
		Rules: peoplev1.EatingRules_builder{MinProteinGrams: new(10.0)}.Build(),
		DailyTargets: []*peoplev1.NutrientTarget{
			peoplev1.NutrientTarget_builder{Nutrient: foodsv1.Nutrient_NUTRIENT_ENERGY_KCAL, MaxPerDay: new(700.0)}.Build(),
		},
	}.Build())
	tortilla := customFood(t, e, "Tortillas", 300, 8, 50, 2, 7, nil)
	lentils := customFood(t, e, "Lentils", 120, 9, 20, 1, 0.5, nil)
	greens := customFood(t, e, "Greens", 20, 2, 3, 1, 0.2, nil)
	tofu := customFood(t, e, "Tofu, firm", 144, 15, 3, 0.6, 9, nil)
	apple := customFood(t, e, "Apple", 52, 0.3, 14, 10, 0.2, nil)
	recipe := func(name string, f *foodsv1.Food, amount float64) *recipesv1.Recipe {
		t.Helper()
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
			Name: name, Servings: 1, Ingredients: []*recipesv1.Ingredient{ingredient(f.GetId(), amount, grams)},
		}.Build()}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
		return resp.GetRecipe()
	}
	wraps := recipe("Wraps", tortilla, 200)     // 600 kcal
	soup := recipe("Lentil soup", lentils, 250) // 300 kcal
	recipe("Side salad", greens, 150)           // 30 kcal
	if _, err := e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: tofu.GetId(), Amount: 400, Unit: grams}.Build()); err != nil {
		t.Fatalf("AddToPantry: %v", err)
	}
	var wrapsCook string
	for _, r := range []*recipesv1.Recipe{soup, wraps} {
		planned, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: r.GetId(), Date: monday, Meal: lunch}.Build())
		if err != nil {
			t.Fatalf("PlanMeal: %v", err)
		}
		wrapsCook = planned.GetCook().GetId()
	}
	// An apple for a snack has too little protein.
	snackID := eatingTimeID(t, dana, "Snack")
	if _, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{Portion: plansv1.Portion_builder{
		PersonId: dana.GetId(), EatingTimeId: snackID, Date: monday,
		Food: plansv1.FoodAmount_builder{FoodId: apple.GetId(), Amount: 150, Unit: grams}.Build(),
	}.Build()}.Build()); err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}

	// Lunch is 900 kcal, over the day's 700: replacing the wraps with the
	// salad fixes it.
	resp, err := e.suggestions.SuggestFixes(ctx, suggestionsv1.SuggestFixesRequest_builder{
		PersonId: dana.GetId(), Date: monday, EatingTimeId: eatingTimeID(t, dana, "Lunch"),
	}.Build())
	if err != nil {
		t.Fatalf("SuggestFixes: %v", err)
	}
	var descriptions []string
	for _, f := range resp.GetFixes() {
		descriptions = append(descriptions, f.GetDescription())
	}
	i := slices.Index(descriptions, "Replace Wraps with Side salad (1 serving) at Lunch")
	if i < 0 {
		t.Fatalf("got fixes %q, want the wraps replaced with the salad", descriptions)
	}
	if fix := resp.GetFixes()[i]; fix.GetKind() != suggestionsv1.PlanFixKind_PLAN_FIX_KIND_REPLACE || len(fix.GetFixedWarnings()) != 1 ||
		fix.GetEnergyKcal() >= 700 || fix.GetPlannedEnergyKcal() < 900 || fix.GetDeleteCookId() != wrapsCook {
		t.Errorf("got %v, want it to fix the energy maximum, deleting the wraps' cook", fix)
	}
	// The soup is at lunch already, so it doesn't replace the wraps.
	if slices.Contains(descriptions, "Replace Wraps with Lentil soup (1 serving) at Lunch") {
		t.Errorf("got %q, want the soup not added twice", descriptions)
	}
	for _, f := range resp.GetFixes() {
		if f.GetKind() == suggestionsv1.PlanFixKind_PLAN_FIX_KIND_ADD {
			t.Errorf("got %q, want nothing added to a day over its maximum", f.GetDescription())
		}
	}

	// The snack is short of protein: adding tofu from the pantry fixes it.
	resp, err = e.suggestions.SuggestFixes(ctx, suggestionsv1.SuggestFixesRequest_builder{
		PersonId: dana.GetId(), Date: monday, EatingTimeId: snackID,
	}.Build())
	if err != nil {
		t.Fatalf("SuggestFixes: %v", err)
	}
	found := false
	for _, f := range resp.GetFixes() {
		if f.GetKind() == suggestionsv1.PlanFixKind_PLAN_FIX_KIND_ADD && f.GetFoodId() == tofu.GetId() {
			found = true
			// Apply it: the snack has its protein.
			if _, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{Portion: plansv1.Portion_builder{
				PersonId: dana.GetId(), EatingTimeId: f.GetEatingTimeId(), Date: monday,
				Food: plansv1.FoodAmount_builder{FoodId: f.GetFoodId(), Amount: f.GetAmount(), Unit: f.GetUnit()}.Build(),
			}.Build()}.Build()); err != nil {
				t.Fatalf("CreatePortion: %v", err)
			}
			break
		}
	}
	if !found {
		t.Fatalf("got %v, want tofu added to the snack", resp.GetFixes())
	}
	resp, err = e.suggestions.SuggestFixes(ctx, suggestionsv1.SuggestFixesRequest_builder{
		PersonId: dana.GetId(), Date: monday, EatingTimeId: snackID,
	}.Build())
	if err != nil {
		t.Fatalf("SuggestFixes: %v", err)
	}
	// The day is still over its energy maximum, but the snack has its
	// protein.
	for _, w := range resp.GetWarnings() {
		if w.GetEatingTimeId() == snackID {
			t.Errorf("got %q, want the snack fixed", w.GetMessage())
		}
	}

	// Bad requests fail.
	for name, req := range map[string]*suggestionsv1.SuggestFixesRequest{
		"no person":    suggestionsv1.SuggestFixesRequest_builder{Date: monday}.Build(),
		"bad date":     suggestionsv1.SuggestFixesRequest_builder{PersonId: dana.GetId(), Date: "Monday"}.Build(),
		"other's time": suggestionsv1.SuggestFixesRequest_builder{PersonId: dana.GetId(), Date: monday, EatingTimeId: dana.GetId()}.Build(),
	} {
		if _, err := e.suggestions.SuggestFixes(ctx, req); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want INVALID_ARGUMENT", name, err)
		}
	}
}
