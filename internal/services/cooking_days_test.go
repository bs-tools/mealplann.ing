package services_test

import (
	"context"
	"testing"

	"connectrpc.com/connect/v2"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
)

// TestCookingDays checks that with cooking days, the meals of other days
// are cooked on the cooking day before them, with recipes that keep until
// then, and saved as cooks made ahead.
func TestCookingDays(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	createPerson(t, e, ben())
	a := createPerson(t, e, anna())
	tofu := customFood(t, e, "Tofu", 144, 15, 3, 0.6, 9, nil)
	newRecipe := func(name string, fridgeDays int32) *recipesv1.Recipe {
		t.Helper()
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
			Name: name, Servings: 2, Tags: []string{"dinner"}, FridgeDays: new(fridgeDays),
			Ingredients: []*recipesv1.Ingredient{ingredient(tofu.GetId(), 400, grams)},
		}.Build()}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
		return resp.GetRecipe()
	}
	keeps := newRecipe("Tofu stew", 4)
	newRecipe("Tofu salad", 1)
	const sunday, tuesday = "2026-11-01", "2026-11-03"

	// The household cooks on Sundays: Monday's and Tuesday's dinners are
	// cooked on Sunday, and Tuesday's keeps two days.
	resp, err := e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{
		StartDate: sunday, Days: 3, Meals: []peoplev1.HouseholdMeal{dinner}, Seed: 1, SkipPersonalEatingTimes: true,
		Criteria: suggestionsv1.PlanCriteria_builder{CookingWeekdays: []int32{7}}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	slots := resp.GetSlots()
	if len(slots) != 3 {
		t.Fatalf("got %d slots, want 3", len(slots))
	}
	for i, want := range []string{"", sunday, sunday} {
		if got := slots[i].GetSuggestion().GetCookDate(); got != want {
			t.Errorf("got %s's dinner cooked on %q, want %q", slots[i].GetDate(), got, want)
		}
	}
	if got := slots[2].GetSuggestion().GetRecipeName(); got != keeps.GetName() {
		t.Errorf("got %s for Tuesday, want the stew, as the salad keeps a day", got)
	}
	for _, alt := range slots[2].GetAlternatives() {
		if alt.GetRecipeName() == "Tofu salad" {
			t.Errorf("got the salad as Tuesday's alternative, want only recipes that keep two days")
		}
	}

	// Saved, Tuesday's dinner is cooked on Sunday, not as Sunday's dinner.
	saved, err := e.plans.PlanMeals(ctx, plansv1.PlanMealsRequest_builder{Meals: []*plansv1.MealToPlan{
		plansv1.MealToPlan_builder{RecipeId: keeps.GetId(), Date: tuesday, Meal: dinner, CookDate: sunday}.Build(),
	}}.Build())
	if err != nil {
		t.Fatalf("PlanMeals: %v", err)
	}
	if c := saved.GetCooks()[0]; c.GetDate() != sunday || c.GetMeal() != peoplev1.HouseholdMeal_HOUSEHOLD_MEAL_UNSPECIFIED {
		t.Errorf("got cook on %s for %v, want Sunday's, for no meal", c.GetDate(), c.GetMeal())
	}

	// The rest of a meal made ahead can be eaten later, like Anna's
	// mid-afternoon, after Monday's lunch cooked on Sunday.
	if _, err := e.plans.PlanMeals(ctx, plansv1.PlanMealsRequest_builder{Meals: []*plansv1.MealToPlan{
		plansv1.MealToPlan_builder{RecipeId: keeps.GetId(), Date: monday, Meal: lunch, CookDate: sunday}.Build(),
		plansv1.MealToPlan_builder{
			RecipeId: keeps.GetId(), Date: monday, PersonId: a.GetId(), EatingTimeId: eatingTimeID(t, a, "Mid-afternoon"),
			LeftoverOf: plansv1.LeftoverOf_builder{Date: monday, Meal: lunch}.Build(),
		}.Build(),
	}}.Build()); err != nil {
		t.Errorf("PlanMeals with the rest of a meal made ahead: %v", err)
	}

	// Bad cook dates fail.
	for name, m := range map[string]*plansv1.MealToPlan{
		"after":     plansv1.MealToPlan_builder{RecipeId: keeps.GetId(), Date: sunday, Meal: dinner, CookDate: tuesday}.Build(),
		"too early": plansv1.MealToPlan_builder{RecipeId: keeps.GetId(), Date: tuesday, Meal: dinner, CookDate: "2026-10-01"}.Build(),
	} {
		_, err := e.plans.PlanMeals(ctx, plansv1.PlanMealsRequest_builder{Meals: []*plansv1.MealToPlan{m}}.Build())
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want INVALID_ARGUMENT", name, err)
		}
	}
	_, err = e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{
		StartDate: sunday, Days: 1, Meals: []peoplev1.HouseholdMeal{dinner},
		Criteria: suggestionsv1.PlanCriteria_builder{CookingWeekdays: []int32{8}}.Build(),
	}.Build())
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("got %v for weekday 8, want INVALID_ARGUMENT", err)
	}
}
