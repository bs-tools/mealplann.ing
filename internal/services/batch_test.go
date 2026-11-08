package services_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestBatchWrites(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()

	// Foods of a shelf, one of them a duplicate, which fails on its own.
	custom := func(name string) *foodsv1.CreateFoodRequest {
		return foodsv1.CreateFoodRequest_builder{Food: foodsv1.Food_builder{
			Name: name, Nutrients: foodsv1.Nutrients_builder{EnergyKcal: new(100.0)}.Build(),
		}.Build()}.Build()
	}
	created, err := e.foods.CreateFoods(ctx, foodsv1.CreateFoodsRequest_builder{Requests: []*foodsv1.CreateFoodRequest{
		custom("Gochujang"), custom("Tamari"), custom("Gochujang"), custom(""),
	}}.Build())
	if err != nil {
		t.Fatalf("CreateFoods: %v", err)
	}
	var outcomes []string
	for _, r := range created.GetResults() {
		outcomes = append(outcomes, r.GetFood().GetName()+r.GetErrorCode())
	}
	// Which of the two Gochujangs is created first depends on which runs
	// first.
	if !slices.Contains(outcomes, "Tamari") || !slices.Contains(outcomes, "Gochujang") ||
		!slices.Contains(outcomes, "already_exists") || outcomes[3] != "invalid_argument" {
		t.Errorf("got %v, want the tamari, one gochujang, a duplicate and a food without a name", outcomes)
	}
	if msg := created.GetResults()[3].GetErrorMessage(); !strings.Contains(msg, "name") {
		t.Errorf("got message %q, want it to say the name is missing", msg)
	}
	_, err = e.foods.CreateFoods(ctx, foodsv1.CreateFoodsRequest_builder{}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	tamari := created.GetResults()[1].GetFood()

	// Lots of a shelf, each converted on its own.
	added, err := e.pantry.AddLotsToPantry(ctx, pantryv1.AddLotsToPantryRequest_builder{Requests: []*pantryv1.AddToPantryRequest{
		pantryv1.AddToPantryRequest_builder{FoodId: tamari.GetId(), Amount: 250, Unit: grams}.Build(),
		pantryv1.AddToPantryRequest_builder{FoodId: tamari.GetId(), Amount: 3, Unit: pieces}.Build(),
	}}.Build())
	if err != nil {
		t.Fatalf("AddLotsToPantry: %v", err)
	}
	if r := added.GetResults(); len(r) != 2 || r[0].GetAdded().GetItem().GetGrams() != 250 || r[1].GetErrorCode() != "failed_precondition" {
		t.Errorf("got %v, want 250 g added and the pieces refused", r)
	}

	// Recipes updated together, one of them missing.
	beans := customFood(t, e, "Black beans, canned", 130, 9, 24, 0, 0.5, nil)
	cooked := customFood(t, e, "Black beans, cooked from dry", 132, 9, 24, 0, 0.5, nil)
	var ids []string
	for _, name := range []string{"Tacos", "Burrito bowl", "Rice"} {
		ingredients := []*recipesv1.Ingredient{ingredient(beans.GetId(), 200, grams)}
		if name == "Rice" {
			ingredients = []*recipesv1.Ingredient{ingredient(tamari.GetId(), 10, grams)}
		}
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
			Name: name, Servings: 2, Ingredients: ingredients,
		}.Build()}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
		ids = append(ids, resp.GetRecipe().GetId())
	}
	rename := func(id, name string) *recipesv1.UpdateRecipeRequest {
		return recipesv1.UpdateRecipeRequest_builder{
			Recipe:     recipesv1.Recipe_builder{Id: id, Name: name}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
		}.Build()
	}
	updated, err := e.recipes.UpdateRecipes(ctx, recipesv1.UpdateRecipesRequest_builder{Requests: []*recipesv1.UpdateRecipeRequest{
		rename(ids[0], "Black bean tacos"), rename("8a6e7f8a-0000-4000-8000-000000000000", "Nope"),
	}}.Build())
	if err != nil {
		t.Fatalf("UpdateRecipes: %v", err)
	}
	if r := updated.GetResults(); r[0].GetRecipe().GetName() != "Black bean tacos" || r[1].GetErrorCode() != "not_found" {
		t.Errorf("got %v, want the tacos renamed and the other not found", r)
	}

	// Replacing the canned beans with beans cooked from dry, of which it
	// takes a bit more, changes both recipes with them, and no other.
	replaced, err := e.recipes.ReplaceFood(ctx, recipesv1.ReplaceFoodRequest_builder{
		FoodId: beans.GetId(), NewFoodId: cooked.GetId(), AmountRatio: 1.1,
	}.Build())
	if err != nil {
		t.Fatalf("ReplaceFood: %v", err)
	}
	if got := recipeNames(replaced.GetRecipes()); !slices.Equal(got, []string{"Black bean tacos", "Burrito bowl"}) {
		t.Errorf("got %v replaced, want the tacos and the bowl", got)
	}
	tacos, err := e.recipes.GetRecipe(ctx, recipesv1.GetRecipeRequest_builder{Id: ids[0]}.Build())
	if err != nil {
		t.Fatalf("GetRecipe: %v", err)
	}
	if in := tacos.GetRecipe().GetIngredients()[0]; in.GetFoodId() != cooked.GetId() || !near(in.GetAmount(), 220) || in.GetUnit() != grams {
		t.Errorf("got %v, want 220 g of beans cooked from dry", in)
	}
	usages, err := e.foods.ListFoodUsages(ctx, foodsv1.ListFoodUsagesRequest_builder{FoodId: beans.GetId()}.Build())
	if err != nil {
		t.Fatalf("ListFoodUsages: %v", err)
	}
	if len(usages.GetRecipes()) != 0 {
		t.Errorf("got %v using the canned beans, want none", usages.GetRecipes())
	}
	// Only in some recipes, and with a food of another household, which
	// fails naming the recipe.
	replaced, err = e.recipes.ReplaceFood(ctx, recipesv1.ReplaceFoodRequest_builder{
		FoodId: cooked.GetId(), NewFoodId: beans.GetId(), RecipeIds: []string{ids[1]},
	}.Build())
	if err != nil {
		t.Fatalf("ReplaceFood: %v", err)
	}
	if got := recipeNames(replaced.GetRecipes()); !slices.Equal(got, []string{"Burrito bowl"}) {
		t.Errorf("got %v replaced, want only the bowl", got)
	}
	theirs := createFood(t, e.otherFoods, foodsv1.CreateFoodRequest_builder{Food: foodsv1.Food_builder{
		Name: "Their beans", Nutrients: foodsv1.Nutrients_builder{EnergyKcal: new(100.0)}.Build(),
	}.Build()}.Build())
	_, err = e.recipes.ReplaceFood(ctx, recipesv1.ReplaceFoodRequest_builder{FoodId: cooked.GetId(), NewFoodId: theirs.GetId()}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	if err == nil || !strings.Contains(err.Error(), "Black bean tacos") {
		t.Errorf("got %v, want it to name the tacos", err)
	}
	_, err = e.recipes.ReplaceFood(ctx, recipesv1.ReplaceFoodRequest_builder{FoodId: cooked.GetId(), NewFoodId: cooked.GetId()}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.recipes.ReplaceFood(ctx, recipesv1.ReplaceFoodRequest_builder{FoodId: cooked.GetId(), NewFoodId: beans.GetId(), AmountRatio: 1000}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
}

func TestRequestIDs(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	req := foodsv1.CreateFoodRequest_builder{
		Food:      foodsv1.Food_builder{Name: "Miso", Nutrients: foodsv1.Nutrients_builder{EnergyKcal: new(200.0)}.Build()}.Build(),
		RequestId: "shelf-1-miso",
	}.Build()

	// A retry returns the first response instead of failing as a
	// duplicate.
	first, err := e.foods.CreateFood(ctx, req)
	if err != nil {
		t.Fatalf("CreateFood: %v", err)
	}
	retry, err := e.foods.CreateFood(ctx, req)
	if err != nil {
		t.Fatalf("CreateFood again: %v", err)
	}
	if retry.GetFood().GetId() != first.GetFood().GetId() {
		t.Errorf("got food %v, want the first one, %v", retry.GetFood(), first.GetFood())
	}
	// Without the request ID, it's a new request, which fails as the name
	// is taken.
	req.SetRequestId("")
	_, err = e.foods.CreateFood(ctx, req)
	wantCode(t, err, connect.CodeAlreadyExists)

	// The same ID for another method is refused.
	_, err = e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{
		FoodId: first.GetFood().GetId(), Amount: 500, Unit: grams, RequestId: "shelf-1-miso",
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// A failed request is forgotten, so its retry runs again.
	lot := pantryv1.AddToPantryRequest_builder{FoodId: first.GetFood().GetId(), Amount: 2, Unit: pieces, RequestId: "lot-1"}.Build()
	_, err = e.pantry.AddToPantry(ctx, lot)
	wantCode(t, err, connect.CodeFailedPrecondition)
	lot.SetUnit(grams)
	added, err := e.pantry.AddToPantry(ctx, lot)
	if err != nil {
		t.Fatalf("AddToPantry: %v", err)
	}
	again, err := e.pantry.AddToPantry(ctx, lot)
	if err != nil {
		t.Fatalf("AddToPantry again: %v", err)
	}
	if again.GetItem().GetId() != added.GetItem().GetId() || len(listPantry(t, e).GetItems()) != 1 {
		t.Errorf("got %v and %v, want one lot", added.GetItem(), again.GetItem())
	}

	// Request IDs are the household's own.
	other, err := e.otherFoods.CreateFood(ctx, foodsv1.CreateFoodRequest_builder{
		Food:      foodsv1.Food_builder{Name: "Miso", Nutrients: foodsv1.Nutrients_builder{EnergyKcal: new(200.0)}.Build()}.Build(),
		RequestId: "shelf-1-miso",
	}.Build())
	if err != nil {
		t.Fatalf("CreateFood in the other household: %v", err)
	}
	if other.GetFood().GetId() == first.GetFood().GetId() {
		t.Error("got the first household's food in the other")
	}
}
