package services_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	shoppingv1 "github.com/bs-tools/mealplann.ing/internal/gen/shopping/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// TestPicks checks picking recipes to buy for and cook any time: that the
// suggestions put first a recipe using food to use soon, then one sharing
// ingredients with those picked; and that a pick, a cook to cook any time,
// is listed until it's cooked, bought for by the shopping list, and not
// warned about for its unplanned servings.
func TestPicks(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	createPerson(t, e, ben())
	today := time.Now().UTC().Format(time.DateOnly)
	soon := time.Now().UTC().AddDate(0, 0, 2).Format(time.DateOnly)
	broccoli := customFood(t, e, "Broccoli", 34, 3, 7, 2, 0.4, nil)
	ginger := customFood(t, e, "Ginger", 80, 2, 18, 2, 1, nil)
	lime := customFood(t, e, "Lime", 30, 1, 10, 2, 0.2, nil)
	tofu := customFood(t, e, "Tofu, extra firm", 145, 16, 3, 1, 8, nil)
	lentils := customFood(t, e, "Red lentils", 350, 24, 60, 2, 1, nil)
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: broccoli.GetId(), Amount: 300, Unit: grams, ExpiryDate: soon}.Build())
	create := func(name string, ingredients ...*recipesv1.Ingredient) *recipesv1.Recipe {
		t.Helper()
		created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
			Name: name, Servings: 2, Tags: []string{"dinner"}, Ingredients: ingredients,
		}.Build()}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe %s: %v", name, err)
		}
		return created.GetRecipe()
	}
	stirFry := create("Broccoli stir fry", ingredient(broccoli.GetId(), 250, grams), ingredient(ginger.GetId(), 20, grams), ingredient(tofu.GetId(), 300, grams))
	noodles := create("Ginger lime noodles", ingredient(ginger.GetId(), 20, grams), ingredient(lime.GetId(), 30, grams))
	dal := create("Red lentil dal", ingredient(lentils.GetId(), 200, grams))

	suggest := func(picked ...string) *suggestionsv1.SuggestPicksResponse {
		t.Helper()
		resp, err := e.suggestions.SuggestPicks(ctx, suggestionsv1.SuggestPicksRequest_builder{PickedRecipeIds: picked}.Build())
		if err != nil {
			t.Fatalf("SuggestPicks: %v", err)
		}
		return resp
	}
	// The stir fry uses the broccoli to use soon, so it comes first.
	first := suggest().GetSuggestions()
	if len(first) != 3 || first[0].GetRecipeId() != stirFry.GetId() || !strings.Contains(first[0].GetReasons()[0], "broccoli") {
		t.Fatalf("got %v, want the broccoli stir fry first, for the broccoli to use soon", first)
	}
	// With it picked, the noodles share its ginger, so they come before
	// the dal, and the picks need tofu and ginger bought.
	then := suggest(stirFry.GetId())
	if got := then.GetSuggestions(); len(got) != 2 || got[0].GetRecipeId() != noodles.GetId() ||
		!slices.ContainsFunc(got[0].GetReasons(), func(r string) bool { return strings.Contains(r, "shares ginger") }) {
		t.Errorf("got %v, want the noodles first, sharing ginger with the stir fry", got)
	}
	if got := then.GetPickedToBuy(); !slices.Equal(got, []string{"Ginger", "Tofu, extra firm"}) {
		t.Errorf("got %v to buy for the stir fry, want ginger and tofu", got)
	}
	_ = dal

	// Picked, it's a cook to cook any time, from today.
	created, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{Cook: plansv1.Cook_builder{
		RecipeId: stirFry.GetId(), Date: today, Servings: 2, Anytime: true,
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateCook: %v", err)
	}
	cook := created.GetCook()
	listed, err := e.plans.ListAnytimeCooks(ctx, plansv1.ListAnytimeCooksRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListAnytimeCooks: %v", err)
	}
	if got := listed.GetCooks(); len(got) != 1 || got[0].GetId() != cook.GetId() || !got[0].GetAnytime() {
		t.Fatalf("got %v, want the stir fry to cook any time", got)
	}
	monday := time.Now().UTC().AddDate(0, 0, -int(time.Now().UTC().Weekday()+6)%7).Format(time.DateOnly)
	week, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	for _, c := range week.GetCooks() {
		if c.GetCook().GetId() == cook.GetId() && len(c.GetWarnings()) > 0 {
			t.Errorf("got warnings %v for the pick, want none until it's cooked", c.GetWarnings())
		}
	}
	list, err := e.shopping.CreateShoppingList(ctx, shoppingv1.CreateShoppingListRequest_builder{StartDate: today}.Build())
	if err != nil {
		t.Fatalf("CreateShoppingList: %v", err)
	}
	var names []string
	for _, it := range list.GetList().GetItems() {
		names = append(names, it.GetName())
	}
	if !slices.Contains(names, "Tofu, extra firm") || slices.Contains(names, "Broccoli") {
		t.Errorf("got %v on the list, want the pick's tofu, but not the broccoli in the pantry", names)
	}

	// Cooked, it's no longer to cook any time.
	if _, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: cook.GetId(), Done: true}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done", "anytime"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}
	listed, err = e.plans.ListAnytimeCooks(ctx, plansv1.ListAnytimeCooksRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListAnytimeCooks: %v", err)
	}
	if len(listed.GetCooks()) != 0 {
		t.Errorf("got %v, want no cooks to cook any time once it's cooked", listed.GetCooks())
	}
}
