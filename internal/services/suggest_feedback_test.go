package services_test

import (
	"context"
	"slices"
	"testing"

	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// TestSuggestFeedback checks that recipes the household doesn't want
// suggested, for good or for a while, aren't, and that one cooked lately
// scores higher.
func TestSuggestFeedback(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	createPerson(t, e, ben())
	tofu := customFood(t, e, "Tofu", 144, 15, 3, 0.6, 9, nil)
	newRecipe := func(name string) *recipesv1.Recipe {
		t.Helper()
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
			Name: name, Servings: 2, Tags: []string{"dinner"},
			Ingredients: []*recipesv1.Ingredient{ingredient(tofu.GetId(), 400, grams)},
		}.Build()}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
		return resp.GetRecipe()
	}
	disliked := newRecipe("Disliked stir-fry")
	tired := newRecipe("Tired curry")
	liked := newRecipe("Liked scramble")
	newRecipe("Plain tofu")
	update := func(r *recipesv1.Recipe, paths ...string) {
		t.Helper()
		if _, err := e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
			Recipe: r, UpdateMask: &fieldmaskpb.FieldMask{Paths: paths},
		}.Build()); err != nil {
			t.Fatalf("UpdateRecipe: %v", err)
		}
	}
	update(recipesv1.Recipe_builder{Id: disliked.GetId(), NotSuggested: true}.Build(), "not_suggested")
	update(recipesv1.Recipe_builder{Id: tired.GetId(), SnoozedUntil: thursday}.Build(), "snoozed_until")
	// The liked recipe was cooked on Monday.
	planned, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: liked.GetId(), Date: monday, Meal: dinner}.Build())
	if err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	if _, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: planned.GetCook().GetId(), Done: true}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}

	suggested := func(date string) map[string]*suggestionsv1.Suggestion {
		t.Helper()
		resp, err := e.suggestions.SuggestRecipes(ctx, suggestionsv1.SuggestRecipesRequest_builder{Date: date, Meal: new(dinner)}.Build())
		if err != nil {
			t.Fatalf("SuggestRecipes: %v", err)
		}
		byName := map[string]*suggestionsv1.Suggestion{}
		for _, s := range resp.GetSuggestions() {
			byName[s.GetRecipeName()] = s
		}
		return byName
	}
	// On Wednesday, the disliked recipe and the one snoozed until Thursday
	// aren't suggested.
	got := suggested(wednesday)
	if _, ok := got["Disliked stir-fry"]; ok {
		t.Errorf("got the disliked recipe suggested, want it left out")
	}
	if _, ok := got["Tired curry"]; ok {
		t.Errorf("got the snoozed recipe suggested on Wednesday, want it left out until Thursday")
	}
	cooked := slices.ContainsFunc(got["Liked scramble"].GetComponents(), func(c *suggestionsv1.ScoreComponent) bool {
		return c.GetKind() == suggestionsv1.ScoreKind_SCORE_KIND_COOKED && c.GetPoints() > 0
	})
	if !cooked {
		t.Errorf("got %v for the recipe cooked Monday, want points for being cooked lately", got["Liked scramble"].GetComponents())
	}
	// On Thursday, the snoozed recipe is back.
	if _, ok := suggested(thursday)["Tired curry"]; !ok {
		t.Errorf("got the snoozed recipe left out on Thursday, want it suggested again")
	}
	// The recipe reads as not suggested.
	resp, err := e.recipes.GetRecipe(ctx, recipesv1.GetRecipeRequest_builder{Id: disliked.GetId()}.Build())
	if err != nil {
		t.Fatalf("GetRecipe: %v", err)
	}
	if !resp.GetRecipe().GetNotSuggested() {
		t.Errorf("got not_suggested false, want true")
	}
}
