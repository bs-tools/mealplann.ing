package services_test

import (
	"context"
	"testing"

	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
)

// TestSuggestPlanLeftoverMeals checks that planning dinners for two people
// eats the rest of one at a later dinner, cooking once for both meals,
// and that saving the plan as suggested grows the first dinner's cook by
// the second's servings, rather than cooking again.
func TestSuggestPlanLeftoverMeals(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	createPerson(t, e, ben())
	createPerson(t, e, ben())
	tofu := customFood(t, e, "Tofu", 144, 15, 3, 0.6, 9, nil)
	for _, name := range []string{"Tofu curry", "Tofu stew"} {
		if _, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
			Name: name, Servings: 2, Tags: []string{"dinner"}, FridgeDays: new(int32(3)),
			Ingredients: []*recipesv1.Ingredient{ingredient(tofu.GetId(), 400, grams)},
		}.Build()}.Build()); err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
	}
	resp, err := e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{
		StartDate: monday, Days: 3, Meals: []peoplev1.HouseholdMeal{dinner}, Seed: 7, SkipPersonalEatingTimes: true,
	}.Build())
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	var meals []*plansv1.MealToPlan
	var rest *suggestionsv1.Suggestion
	for _, s := range resp.GetSlots() {
		sg := s.GetSuggestion()
		if sg.HasLeftoverOf() {
			rest = sg
			if sg.GetLeftoverOf().GetDate() >= s.GetDate() || sg.GetLeftoverOf().GetMeal() != dinner {
				t.Errorf("got %v, want the rest of an earlier day's dinner", slotSummary(s))
			}
		}
		b := plansv1.MealToPlan_builder{RecipeId: sg.GetRecipeId(), Date: s.GetDate(), Meal: dinner}
		if sg.HasLeftoverOf() {
			b.LeftoverOf = sg.GetLeftoverOf()
		}
		meals = append(meals, b.Build())
	}
	if rest == nil {
		t.Fatalf("got %v, want a dinner of the rest of an earlier one", planSummary(resp))
	}

	// Saved as suggested, the leftover's cook makes both dinners.
	if _, err := e.plans.PlanMeals(ctx, plansv1.PlanMealsRequest_builder{Meals: meals}.Build()); err != nil {
		t.Fatalf("PlanMeals: %v", err)
	}
	week, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	if n := len(week.GetCooks()); n != 2 {
		t.Fatalf("got %d cooks, want 2 for 3 dinners", n)
	}
	for _, c := range week.GetCooks() {
		if c.GetCook().GetRecipeId() != rest.GetRecipeId() || c.GetCook().GetDate() != rest.GetLeftoverOf().GetDate() {
			continue
		}
		var eaten float64
		dates := map[string]bool{}
		for _, day := range week.GetDays() {
			for _, p := range day.GetPeople() {
				for _, et := range p.GetEatingTimes() {
					for _, portion := range et.GetPortions() {
						if portion.GetCookId() == c.GetCook().GetId() {
							eaten += portion.GetServings()
							dates[day.GetDate()] = true
						}
					}
				}
			}
		}
		if len(dates) != 2 || c.GetCook().GetServings() < eaten-0.01 {
			t.Errorf("got the cook eaten on %v, %v of its %v servings, want two days' dinners from it",
				dates, eaten, c.GetCook().GetServings())
		}
		return
	}
	t.Errorf("got no cook of %s on %s", rest.GetRecipeName(), rest.GetLeftoverOf().GetDate())
}
