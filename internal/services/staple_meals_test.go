package services_test

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect/v2"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
)

// TestSharedMealsSkipStapled checks that people with a staple at a shared
// meal eat it instead: the meal is planned and suggested for the others,
// and is planned already if everyone has one.
func TestSharedMealsSkipStapled(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	b := createPerson(t, e, ben())
	cleo := createPerson(t, e, peoplev1.Person_builder{
		Name: "Cleo",
		EatingTimes: []*peoplev1.EatingTime{
			eatingTime("Breakfast", mainMeal, "08:00", breakfast),
			eatingTime("Dinner", mainMeal, "19:00", dinner),
		},
	}.Build())
	oats := customFood(t, e, "Oats", 380, 13, 60, 1, 7, nil)
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Porridge", Servings: 2, Tags: []string{"breakfast"},
		Ingredients: []*recipesv1.Ingredient{ingredient(oats.GetId(), 100, grams)},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	porridge := created.GetRecipe()
	// Ben has his own breakfast every day, but Wednesday, when he skips it.
	staple, err := e.plans.CreateStaple(ctx, plansv1.CreateStapleRequest_builder{Staple: plansv1.Staple_builder{
		PersonId: b.GetId(), EatingTimeId: eatingTimeID(t, b, "Breakfast"),
		Food: plansv1.FoodAmount_builder{FoodId: oats.GetId(), Amount: 60, Unit: grams}.Build(),
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateStaple: %v", err)
	}
	if _, err := e.plans.SkipStaple(ctx, plansv1.SkipStapleRequest_builder{Id: staple.GetStaple().GetId(), Date: wednesday}.Build()); err != nil {
		t.Fatalf("SkipStaple: %v", err)
	}
	eaters := func(portions []*plansv1.Portion) []string {
		var ids []string
		for _, p := range portions {
			ids = append(ids, p.GetPersonId())
		}
		return ids
	}

	// Monday's breakfast is Cleo's alone; on Wednesday, Ben's too.
	mon, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: porridge.GetId(), Date: monday, Meal: breakfast}.Build())
	if err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	if got := eaters(mon.GetPortions()); !slices.Equal(got, []string{cleo.GetId()}) || mon.GetCook().GetServings() != 1 {
		t.Errorf("got portions for %v and %v servings, want Cleo's one", got, mon.GetCook().GetServings())
	}
	wed, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: porridge.GetId(), Date: wednesday, Meal: breakfast}.Build())
	if err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	if got := eaters(wed.GetPortions()); len(got) != 2 {
		t.Errorf("got portions for %v, want both, as Ben skips his staple", got)
	}
	// Asked for by name, Ben gets the meal anyway.
	asked, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{
		RecipeId: porridge.GetId(), Date: tuesday, Meal: breakfast, PersonIds: []string{b.GetId(), cleo.GetId()},
	}.Build())
	if err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	if got := eaters(asked.GetPortions()); len(got) != 2 {
		t.Errorf("got portions for %v, want both, as asked", got)
	}

	// Suggestions for Thursday's breakfast plan Cleo's portion only.
	suggested, err := e.suggestions.SuggestRecipes(ctx, suggestionsv1.SuggestRecipesRequest_builder{Date: thursday, Meal: new(breakfast)}.Build())
	if err != nil {
		t.Fatalf("SuggestRecipes: %v", err)
	}
	for _, p := range suggested.GetSuggestions()[0].GetPortions() {
		if p.GetPersonId() != cleo.GetId() {
			t.Errorf("got a portion for %s, want only Cleo's", p.GetPersonName())
		}
	}
	// Planning the week leaves Ben out of breakfast too.
	plan, err := e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{
		StartDate: friday, Days: 1, Meals: []peoplev1.HouseholdMeal{breakfast},
	}.Build())
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	for _, p := range plan.GetSlots()[0].GetSuggestion().GetPortions() {
		if p.GetPersonId() != cleo.GetId() {
			t.Errorf("got a portion for %s in the plan, want only Cleo's", p.GetPersonName())
		}
	}

	// With Cleo's own breakfast staple too, Friday's breakfast is planned
	// already, and planning it fails, saying why.
	if _, err := e.plans.CreateStaple(ctx, plansv1.CreateStapleRequest_builder{Staple: plansv1.Staple_builder{
		PersonId: cleo.GetId(), EatingTimeId: eatingTimeID(t, cleo, "Breakfast"),
		Food: plansv1.FoodAmount_builder{FoodId: oats.GetId(), Amount: 50, Unit: grams}.Build(),
	}.Build()}.Build()); err != nil {
		t.Fatalf("CreateStaple: %v", err)
	}
	plan, err = e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{
		StartDate: friday, Days: 1, Meals: []peoplev1.HouseholdMeal{breakfast},
	}.Build())
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	if s := plan.GetSlots()[0]; s.GetState() != suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_PLANNED || !slices.Equal(s.GetPlannedRecipeNames(), []string{"Oats"}) {
		t.Errorf("got %v, want breakfast planned already with the oats", slotSummary(s))
	}
	_, err = e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: porridge.GetId(), Date: friday, Meal: breakfast}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
	_, err = e.plans.PlanMeals(ctx, plansv1.PlanMealsRequest_builder{Meals: []*plansv1.MealToPlan{
		plansv1.MealToPlan_builder{RecipeId: porridge.GetId(), Date: friday, Meal: breakfast}.Build(),
	}}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
}
