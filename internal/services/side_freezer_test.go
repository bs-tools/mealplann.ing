package services_test

import (
	"context"
	"testing"

	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
)

// TestSideFromFreezer checks that a recipe side, like rice, is taken from
// leftovers of it before cooking it again: suggested from the freezer, and
// planned from it with a meal, without growing the frozen cook; and once
// there are leftovers in the fridge, planned by Plan for me from those
// first.
func TestSideFromFreezer(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	erin := createPerson(t, e, peoplev1.Person_builder{
		Name:        "Erin",
		EatingTimes: []*peoplev1.EatingTime{eatingTime("Dinner", mainMeal, "19:00", dinner)},
	}.Build())
	dinnerID := eatingTimeID(t, erin, "Dinner")
	tofu := customFood(t, e, "Tofu, firm", 144, 15, 3, 0.6, 9, nil)
	rice := customFood(t, e, "White rice, dry", 360, 7, 80, 1, 0.6, nil)
	create := func(r *recipesv1.Recipe) *recipesv1.Recipe {
		t.Helper()
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: r}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe %s: %v", r.GetName(), err)
		}
		return resp.GetRecipe()
	}
	pot := create(recipesv1.Recipe_builder{
		Name: "Pot of rice", Servings: 4, Tags: []string{"side"}, Freezable: true, FreezerDays: new(int32(90)), FridgeDays: new(int32(4)),
		Ingredients: []*recipesv1.Ingredient{ingredient(rice.GetId(), 240, grams)},
	}.Build())
	bowls := create(recipesv1.Recipe_builder{
		Name: "Tofu bowls", Servings: 2,
		Ingredients: []*recipesv1.Ingredient{ingredient(tofu.GetId(), 300, grams)},
		ServedWith:  []*recipesv1.RecipeSide{recipesv1.RecipeSide_builder{RecipeId: pot.GetId(), ServingsPerServing: 1}.Build()},
	}.Build())
	// Rice frozen on Monday, 4 servings.
	frozen, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook:      plansv1.Cook_builder{RecipeId: pot.GetId(), Date: monday, Servings: 4, FrozenServings: 4, Done: true}.Build(),
		FromStock: true,
	}.Build())
	if err != nil {
		t.Fatalf("CreateCook: %v", err)
	}

	// Planning Wednesday's bowls without a side suggests the frozen rice.
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: bowls.GetId(), Date: wednesday, Meal: dinner, Servings: 1}.Build()); err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	resp, err := e.suggestions.SuggestSides(ctx, suggestionsv1.SuggestSidesRequest_builder{
		PersonId: erin.GetId(), Date: wednesday, EatingTimeId: dinnerID,
	}.Build())
	if err != nil {
		t.Fatalf("SuggestSides: %v", err)
	}
	if s := resp.GetSides(); len(s) != 1 || !s[0].GetFromFreezer() || s[0].GetCookId() != frozen.GetCook().GetId() || s[0].GetFrozenServingsLeft() != 4 {
		t.Errorf("got sides %v, want the frozen rice", s)
	}

	// Planning Friday's bowls with the rice takes it from the freezer: no
	// new pot of rice, and a serving less in the freezer.
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{
		RecipeId: bowls.GetId(), Date: friday, Meal: dinner, Servings: 1,
		Side: recipesv1.RecipeSide_builder{RecipeId: pot.GetId()}.Build(),
	}.Build()); err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	_, cooks := weekCooks(t, e, monday)
	if cooks["Pot of rice "+friday] != nil {
		t.Errorf("got a new pot of rice on Friday, want the frozen rice")
	}
	left, err := e.plans.ListFrozenLeftovers(ctx, plansv1.ListFrozenLeftoversRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListFrozenLeftovers: %v", err)
	}
	if l := left.GetLeftovers(); len(l) != 1 || l[0].GetRemainingServings() != 3 {
		t.Errorf("got frozen %v, want 3 servings of rice left", l)
	}
	if c := cooks["Pot of rice "+monday]; c == nil || c.GetCook().GetServings() != 4 {
		t.Errorf("got the frozen rice %v, want it still 4 servings", c)
	}

	// Rice cooked on Tuesday, left in the fridge: Plan for me's Thursday
	// bowls with rice eat it, before the frozen rice, and cook none.
	fridge, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook:      plansv1.Cook_builder{RecipeId: pot.GetId(), Date: tuesday, Servings: 4, Done: true}.Build(),
		FromStock: true,
	}.Build())
	if err != nil {
		t.Fatalf("CreateCook: %v", err)
	}
	if _, err := e.plans.PlanMeals(ctx, plansv1.PlanMealsRequest_builder{Meals: []*plansv1.MealToPlan{
		plansv1.MealToPlan_builder{
			RecipeId: bowls.GetId(), Date: thursday, Meal: dinner,
			Side: recipesv1.RecipeSide_builder{RecipeId: pot.GetId()}.Build(),
		}.Build(),
	}}.Build()); err != nil {
		t.Fatalf("PlanMeals: %v", err)
	}
	_, cooks = weekCooks(t, e, monday)
	if cooks["Pot of rice "+thursday] != nil {
		t.Errorf("got a new pot of rice on Thursday, want Tuesday's leftovers")
	}
	if c := cooks["Pot of rice "+tuesday]; c == nil || c.GetCook().GetId() != fridge.GetCook().GetId() || c.GetAssignedServings() != 1 || c.GetCook().GetServings() != 4 {
		t.Errorf("got Tuesday's rice %v, want a serving of it planned", c)
	}
}
