package services_test

import (
	"context"
	"testing"

	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// TestEarlierLeftovers checks that a week offers the leftovers of the
// week before that still keep, like Sunday's chili on Monday, and only
// those with servings left.
func TestEarlierLeftovers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	ben := createPerson(t, e, ben())
	beans := customFood(t, e, "Beans", 130, 9, 24, 0, 0.5, nil)
	recipe := func(name string, fridgeDays int32) string {
		t.Helper()
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
			Name: name, Servings: 4, FridgeDays: new(fridgeDays),
			Ingredients: []*recipesv1.Ingredient{ingredient(beans.GetId(), 400, grams)},
		}.Build()}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
		return resp.GetRecipe().GetId()
	}
	cook := func(recipeID, date string, servings float64) string {
		t.Helper()
		resp, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
			Cook: plansv1.Cook_builder{RecipeId: recipeID, Date: date, Servings: servings}.Build(),
		}.Build())
		if err != nil {
			t.Fatalf("CreateCook: %v", err)
		}
		return resp.GetCook().GetId()
	}
	const (
		sunday     = "2026-11-08"
		nextMonday = "2026-11-09"
	)
	// Sunday's chili keeps 3 days, with 1 of 4 servings eaten.
	chili := cook(recipe("Chili", 3), sunday, 4)
	if _, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{Portion: plansv1.Portion_builder{
		PersonId: ben.GetId(), EatingTimeId: eatingTimeID(t, ben, "Dinner"), Date: sunday,
		Cook: plansv1.CookShare_builder{CookId: chili, Servings: 1}.Build(),
	}.Build()}.Build()); err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}
	// Monday's soup keeps only a day, so it's gone by next Monday; and
	// Saturday's stew is all frozen.
	cook(recipe("Soup", 1), monday, 4)
	stew := cook(recipe("Stew", 5), saturday, 2)
	if _, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: stew, FrozenServings: 2}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"frozen_servings"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}

	week, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: nextMonday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	got := week.GetEarlierLeftovers()
	if len(got) != 1 || got[0].GetCook().GetId() != chili || got[0].GetLeftServings() != 3 {
		t.Fatalf("got earlier leftovers %v, want Sunday's chili with 3 servings left", got)
	}
	// The week's own cooks say what's left too.
	week, err = e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	for _, c := range week.GetCooks() {
		want := map[string]float64{"Soup": 4, "Stew": 0, "Chili": 3}[c.GetCook().GetRecipeName()]
		if c.GetLeftServings() != want {
			t.Errorf("got %s with %v servings left, want %v", c.GetCook().GetRecipeName(), c.GetLeftServings(), want)
		}
	}
}
