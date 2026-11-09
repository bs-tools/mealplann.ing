package services_test

import (
	"context"
	"testing"

	"connectrpc.com/connect/v2"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
)

// TestCreatePortionWithNewCook checks that a recipe can be cooked for one
// person's eating time, with the portion and its cook created together.
func TestCreatePortionWithNewCook(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	ben := createPerson(t, e, ben())
	tofu := customFood(t, e, "Tofu", 140, 15, 3, 0, 8, nil)
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Tofu scramble", Servings: 2, Ingredients: []*recipesv1.Ingredient{ingredient(tofu.GetId(), 300, grams)},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	scramble := created.GetRecipe().GetId()
	request := func(cookID string, servings, cookServings float64) *plansv1.CreatePortionRequest {
		return plansv1.CreatePortionRequest_builder{
			Portion: plansv1.Portion_builder{
				PersonId: ben.GetId(), EatingTimeId: eatingTimeID(t, ben, "Breakfast"), Date: monday,
				Cook: plansv1.CookShare_builder{CookId: cookID, Servings: servings}.Build(),
			}.Build(),
			NewCook: plansv1.NewCook_builder{RecipeId: scramble, Servings: cookServings}.Build(),
		}.Build()
	}

	// The whole recipe, of 2 servings, cooked for Ben's breakfast, of 1.
	resp, err := e.plans.CreatePortion(ctx, request("", 1, 2))
	if err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}
	c, p := resp.GetCook(), resp.GetPortion()
	if c.GetRecipeId() != scramble || c.GetServings() != 2 || c.GetDate() != monday ||
		p.GetCook().GetCookId() != c.GetId() || p.GetCook().GetServings() != 1 {
		t.Errorf("got cook %v and portion %v, want 2 servings cooked with 1 for Ben", c, p)
	}
	week, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	if cooks := week.GetCooks(); len(cooks) != 1 || cooks[0].GetLeftServings() != 1 {
		t.Errorf("got cooks %v, want the scramble with a serving left", cooks)
	}

	// Without servings to cook, the portion's are cooked.
	resp, err = e.plans.CreatePortion(ctx, request("", 1.5, 0))
	if err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}
	if got := resp.GetCook().GetServings(); got != 1.5 {
		t.Errorf("got %v servings cooked, want the portion's 1.5", got)
	}

	// Fewer servings than the portion, or a cook_id too, fail.
	_, err = e.plans.CreatePortion(ctx, request("", 2, 1))
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.plans.CreatePortion(ctx, request(c.GetId(), 1, 0))
	wantCode(t, err, connect.CodeInvalidArgument)
}
