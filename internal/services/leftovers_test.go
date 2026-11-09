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

// TestLeftoverSnacks checks that a snack can be the rest of an earlier
// dinner, as suggested, and that planning it grows that dinner's cook,
// whether planned already or in the same request.
func TestLeftoverSnacks(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	cleo := createPerson(t, e, peoplev1.Person_builder{
		Name: "Cleo",
		EatingTimes: []*peoplev1.EatingTime{
			eatingTime("Mid-morning", smallMeal, "10:00", noMeal),
			eatingTime("Dinner", mainMeal, "19:00", dinner),
		},
	}.Build())
	snack := eatingTimeID(t, cleo, "Mid-morning")
	beans := customFood(t, e, "Beans", 130, 9, 24, 0, 0.5, nil)
	recipeID := map[string]string{}
	for _, name := range []string{"Curry", "Stew"} {
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
			Name: name, Servings: 4, Tags: []string{"dinner"}, FridgeDays: new(int32(3)),
			Ingredients: []*recipesv1.Ingredient{ingredient(beans.GetId(), 400, grams)},
		}.Build()}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
		recipeID[name] = resp.GetRecipe().GetId()
	}
	curry, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: recipeID["Curry"], Date: monday, Meal: dinner}.Build())
	if err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}

	// Tuesday's snack can be the rest of Monday's curry.
	suggested, err := e.suggestions.SuggestRecipes(ctx, suggestionsv1.SuggestRecipesRequest_builder{
		Date:       tuesday,
		EatingTime: suggestionsv1.EatingTimeSlot_builder{PersonId: cleo.GetId(), EatingTimeId: snack}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("SuggestRecipes: %v", err)
	}
	var leftover *suggestionsv1.Suggestion
	for _, sg := range suggested.GetSuggestions() {
		if sg.HasLeftoverOf() {
			leftover = sg
		}
	}
	if leftover == nil || leftover.GetRecipeId() != recipeID["Curry"] ||
		leftover.GetLeftoverOf().GetDate() != monday || leftover.GetLeftoverOf().GetMeal() != dinner {
		t.Fatalf("got leftover %v, want the rest of Monday's curry", leftover)
	}

	// Planned with Tuesday's stew, and Wednesday's snack of its rest, the
	// curry and the stew grow by the snacks.
	leftoverOf := func(date string) *plansv1.LeftoverOf {
		return plansv1.LeftoverOf_builder{Date: date, Meal: dinner}.Build()
	}
	planned, err := e.plans.PlanMeals(ctx, plansv1.PlanMealsRequest_builder{Meals: []*plansv1.MealToPlan{
		plansv1.MealToPlan_builder{RecipeId: recipeID["Stew"], Date: wednesday, PersonId: cleo.GetId(), EatingTimeId: snack, LeftoverOf: leftoverOf(tuesday)}.Build(),
		plansv1.MealToPlan_builder{RecipeId: recipeID["Curry"], Date: tuesday, PersonId: cleo.GetId(), EatingTimeId: snack, LeftoverOf: leftoverOf(monday)}.Build(),
		plansv1.MealToPlan_builder{RecipeId: recipeID["Stew"], Date: tuesday, Meal: dinner}.Build(),
	}}.Build())
	if err != nil {
		t.Fatalf("PlanMeals: %v", err)
	}
	if n := len(planned.GetCooks()); n != 2 || planned.GetPortionCount() != 3 {
		t.Fatalf("got %d cooks and %d portions, want the stew and the grown curry, and 3 portions", n, planned.GetPortionCount())
	}
	week, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	servings := map[string]float64{}
	for _, c := range week.GetCooks() {
		servings[c.GetCook().GetRecipeName()] += c.GetCook().GetServings()
	}
	if len(week.GetCooks()) != 2 || servings["Curry"] <= curry.GetCook().GetServings() || servings["Stew"] <= 1 {
		t.Errorf("got cooks of %v servings, want the curry grown from %v, and the stew of more than a serving",
			servings, curry.GetCook().GetServings())
	}

	// A leftover needs its meal's cook, and is only for an eating time.
	_, err = e.plans.PlanMeals(ctx, plansv1.PlanMealsRequest_builder{Meals: []*plansv1.MealToPlan{
		plansv1.MealToPlan_builder{RecipeId: recipeID["Stew"], Date: thursday, PersonId: cleo.GetId(), EatingTimeId: snack, LeftoverOf: leftoverOf(monday)}.Build(),
	}}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
	_, err = e.plans.PlanMeals(ctx, plansv1.PlanMealsRequest_builder{Meals: []*plansv1.MealToPlan{
		plansv1.MealToPlan_builder{RecipeId: recipeID["Curry"], Date: thursday, Meal: dinner, LeftoverOf: leftoverOf(monday)}.Build(),
	}}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
}
