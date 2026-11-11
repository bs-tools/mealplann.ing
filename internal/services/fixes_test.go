package services_test

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// TestSuggestFixes checks that a day over its energy maximum gets its
// portions rightsized, by as little as fixes it, like half the wraps, and
// an eating time short of protein a larger portion, never other recipes or
// foods swapped in.
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
	if _, err := e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: greens.GetId(), Amount: 400, Unit: grams}.Build()); err != nil {
		t.Fatalf("AddToPantry: %v", err)
	}
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: soup.GetId(), Date: monday, Meal: lunch}.Build()); err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	// A whole serving of the wraps, which PlanMeal would size down.
	if _, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{
		Portion: plansv1.Portion_builder{
			PersonId: dana.GetId(), EatingTimeId: eatingTimeID(t, dana, "Lunch"), Date: monday,
			Cook: plansv1.CookShare_builder{Servings: 1}.Build(),
		}.Build(),
		NewCook: plansv1.NewCook_builder{RecipeId: wraps.GetId()}.Build(),
	}.Build()); err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}
	// 50 g of tofu for a snack has 7.5 g of protein, under 10 g.
	snackID := eatingTimeID(t, dana, "Snack")
	snack, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{Portion: plansv1.Portion_builder{
		PersonId: dana.GetId(), EatingTimeId: snackID, Date: monday,
		Food: plansv1.FoodAmount_builder{FoodId: tofu.GetId(), Amount: 50, Unit: grams}.Build(),
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}

	suggest := func(eatingTimeID string) *suggestionsv1.SuggestFixesResponse {
		t.Helper()
		resp, err := e.suggestions.SuggestFixes(ctx, suggestionsv1.SuggestFixesRequest_builder{
			PersonId: dana.GetId(), Date: monday, EatingTimeId: eatingTimeID,
		}.Build())
		if err != nil {
			t.Fatalf("SuggestFixes: %v", err)
		}
		var descriptions []string
		for _, f := range resp.GetFixes() {
			descriptions = append(descriptions, f.GetDescription())
			if strings.Contains(f.GetDescription(), "Side salad") || strings.Contains(f.GetDescription(), "Greens") {
				t.Errorf("got %q, want nothing from the recipes or pantry swapped in", f.GetDescription())
			}
			switch f.GetKind() {
			case suggestionsv1.PlanFixKind_PLAN_FIX_KIND_RESIZE, suggestionsv1.PlanFixKind_PLAN_FIX_KIND_REMOVE:
			default:
				t.Errorf("got %q, want only portions resized or left out", f.GetDescription())
			}
		}
		t.Logf("fixes: %q", descriptions)
		return resp
	}

	// Lunch is 900 kcal, over the day's 700: half the wraps is the
	// smallest change that fixes it, and comes first.
	resp := suggest(eatingTimeID(t, dana, "Lunch"))
	if len(resp.GetFixes()) == 0 {
		t.Fatal("got no fixes, want the wraps resized")
	}
	fix := resp.GetFixes()[0]
	if fix.GetDescription() != "Have 0.5 servings of Wraps at Lunch, not 1 serving" || len(fix.GetFixedWarnings()) != 1 ||
		len(fix.GetResizes()) != 1 || fix.GetResizes()[0].GetServings() != 0.5 ||
		fix.GetEnergyKcal() > 700 || fix.GetPlannedEnergyKcal() < 900 {
		t.Errorf("got %v first, want half the wraps, fixing the energy maximum", fix)
	}

	// The whole day can be scaled together: each of lunch's portions a
	// little smaller, but not the snack's, as it's short of protein.
	resp = suggest("")
	scaled := false
	for _, f := range resp.GetFixes() {
		if f.GetKind() == suggestionsv1.PlanFixKind_PLAN_FIX_KIND_RESIZE && len(f.GetResizes()) > 1 {
			scaled = true
			if f.GetEatingTimeId() != "" || !strings.HasPrefix(f.GetDescription(), "Have each portion smaller") ||
				strings.Contains(f.GetDescription(), "Tofu") || f.GetEnergyKcal() > 700 {
				t.Errorf("got %v, want each of lunch's portions smaller, under the maximum", f)
			}
		}
	}
	if !scaled {
		t.Errorf("got %v, want lunch's portions scaled together", resp.GetFixes())
	}

	// The snack is short of protein: 70 g of tofu, not 50 g, fixes it.
	resp = suggest(snackID)
	if len(resp.GetFixes()) == 0 || resp.GetFixes()[0].GetDescription() != "Have 70 g of Tofu, firm at Snack, not 50 g" {
		t.Fatalf("got %v, want the tofu resized to 70 g first", resp.GetFixes())
	}
	// Apply it: the snack has its protein.
	r := resp.GetFixes()[0].GetResizes()[0]
	if r.GetPortionId() != snack.GetPortion().GetId() {
		t.Fatalf("got portion %s resized, want the snack's", r.GetPortionId())
	}
	if _, err := e.plans.UpdatePortion(ctx, plansv1.UpdatePortionRequest_builder{
		Portion: plansv1.Portion_builder{
			Id:   r.GetPortionId(),
			Food: plansv1.FoodAmount_builder{FoodId: tofu.GetId(), Amount: r.GetAmount(), Unit: r.GetUnit()}.Build(),
		}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"food"}},
	}.Build()); err != nil {
		t.Fatalf("UpdatePortion: %v", err)
	}
	// The day is still over its energy maximum, but the snack has its
	// protein.
	for _, w := range suggest(snackID).GetWarnings() {
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
