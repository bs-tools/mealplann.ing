package services_test

import (
	"context"
	"testing"

	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
)

// TestLatePortionLeftovers checks that a portion eaten after the recipe's
// fridge days, with nothing frozen, counts against its cook's servings:
// they aren't left as leftovers too.
func TestLatePortionLeftovers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	ben := createPerson(t, e, ben())
	tofu := customFood(t, e, "Tofu", 144, 15, 3, 0.6, 9, nil)
	salad, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Caesar salad", Servings: 2, FridgeDays: new(int32(1)),
		Ingredients: []*recipesv1.Ingredient{ingredient(tofu.GetId(), 300, grams)},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	cook, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook:      plansv1.Cook_builder{RecipeId: salad.GetRecipe().GetId(), Date: monday, Servings: 2, Done: true}.Build(),
		FromStock: true,
	}.Build())
	if err != nil {
		t.Fatalf("CreateCook: %v", err)
	}
	// A serving on Monday, and the other on Wednesday, past its fridge day,
	// both eaten.
	for _, date := range []string{monday, wednesday} {
		p, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{Portion: plansv1.Portion_builder{
			PersonId: ben.GetId(), EatingTimeId: eatingTimeID(t, ben, "Lunch"), Date: date,
			Cook: plansv1.CookShare_builder{CookId: cook.GetCook().GetId(), Servings: 1}.Build(),
		}.Build()}.Build())
		if err != nil {
			t.Fatalf("CreatePortion: %v", err)
		}
		if _, err := e.plans.SetPortionEaten(ctx, plansv1.SetPortionEatenRequest_builder{Id: p.GetPortion().GetId(), Eaten: true}.Build()); err != nil {
			t.Fatalf("SetPortionEaten: %v", err)
		}
	}
	_, cooks := weekCooks(t, e, monday)
	c := cooks["Caesar salad "+monday]
	if c == nil || c.GetAssignedServings() != 2 || c.GetLeftServings() != 0 {
		t.Errorf("got the salad %v, want both servings eaten and none left", c)
	}
}
